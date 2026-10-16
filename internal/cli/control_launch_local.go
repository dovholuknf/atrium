package cli

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// launchLocalWait bounds the post to /v1/launch. A room answers it after trusting the worktree and starting
// the runner, which is routinely past the 8 s every other call here gets. Under launchAcrossWait, so the
// hub's own sentence still reaches a caller that went through it. A variable so a test can shorten it.
var launchLocalWait = 45 * time.Second

// launchAttempts is, per directory, the card ids that were on the room before a launch whose answer was lost
// and whose card had not shown yet. The next launch for that directory looks outside them BEFORE it posts, so
// a card that turned up late is returned and not started a second time. Kept for the life of this process
// only: it is the retrying session's own memory, and nothing about it is worth a disk.
var launchAttempts = struct {
	sync.Mutex
	known map[string]map[string]bool
}{known: map[string]map[string]bool{}}

func launchAttemptKey(cwd string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(cwd), "\\", "/"), "/"))
}

// listLaunchRows is the cards on this room, or false when they cannot be read.
func listLaunchRows(ctx context.Context) ([]card, bool) {
	var body struct {
		Tasks []card `json:"tasks"`
	}
	if err := ask(ctx, http.MethodGet, "/v1/tasks", nil, &body); err != nil {
		return nil, false
	}
	return body.Tasks, true
}

func launchRowsOf(cards []card) []link.LaunchRow {
	rows := make([]link.LaunchRow, len(cards))
	for i, c := range cards {
		rows[i] = link.LaunchRow{ID: c.ID, Worktree: c.Worktree, Created: c.Created}
	}
	return rows
}

// pickLaunched is the card among `cards` that the rule names, or nil.
func pickLaunched(known map[string]bool, cwd string, cards []card) *card {
	id := link.PickLaunched(known, cwd, launchRowsOf(cards))
	for i := range cards {
		if id != "" && cards[i].ID == id {
			return &cards[i]
		}
	}
	return nil
}

// postLaunch posts /v1/launch and fills t. recovered is true when t is a card found by looking, because the
// answer was lost.
//
// THE ANSWER MAY BE LOST WHILE THE CARD EXISTS: the room keeps starting after the caller gives up, so a
// timeout is not a launch that did not happen. Cards are listed just before the post, and a card that was not
// there and sits in `cwd` is the launch's. No clock is compared, because the room's is not this one's.
func postLaunch(ctx context.Context, cwd string, req map[string]any, t *card) (recovered bool, err error) {
	key := launchAttemptKey(cwd)
	before, canTell := listLaunchRows(ctx)

	// A PREVIOUS ATTEMPT THAT LOST ITS ANSWER: the card may have turned up since.
	launchAttempts.Lock()
	prior := launchAttempts.known[key]
	launchAttempts.Unlock()
	if prior != nil && canTell {
		if found := pickLaunched(prior, cwd, before); found != nil {
			launchAttempts.Lock()
			delete(launchAttempts.known, key)
			launchAttempts.Unlock()
			*t = *found
			return true, nil
		}
	}

	known := link.IDSet(launchRowsOf(before))
	err = askFor(ctx, launchLocalWait, http.MethodPost, "/v1/launch", req, t)
	if err == nil {
		clearLaunchAttempt(key)
		return false, nil
	}
	var refused *peerToolErr
	if errors.As(err, &refused) && refused.code != http.StatusBadGateway {
		// The room answered no. Nothing was started, and an old attempt is no longer worth remembering.
		clearLaunchAttempt(key)
		return false, err
	}
	if !canTell {
		return false, err
	}
	// Ids from the older attempt stay in the snapshot, so its card is still not taken for this one's.
	if prior != nil {
		for id := range prior {
			known[id] = true
		}
	}
	if cards, ok := listLaunchRows(ctx); ok {
		if found := pickLaunched(known, cwd, cards); found != nil {
			clearLaunchAttempt(key)
			*t = *found
			return true, nil
		}
	}
	launchAttempts.Lock()
	launchAttempts.known[key] = known
	launchAttempts.Unlock()
	return false, err
}

func clearLaunchAttempt(key string) {
	launchAttempts.Lock()
	delete(launchAttempts.known, key)
	launchAttempts.Unlock()
}
