package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// The room's side of being given work.
//
// `room.go` reports this machine's cards to a hub every twenty seconds. The
// reply to that check-in may carry launches. Running them is all this file
// does, and the interesting half of it is the refusals.
//
// THE DIRECTORY IS THE WHOLE PROBLEM AND ATRIUM DOES NOT SOLVE IT. Every launch
// names a directory that already exists because something outside atrium made
// it. A cloud box has no `D:/worktrees/...` and never will. Three answers were
// on the table and this is the one taken, with the other two named so the next
// person does not re-litigate it:
//
//   - REJECTED: the launch carries a prepare command the room runs first. The
//     harness table already has `Prepare`, so this is nearly free, and that is
//     the trap. `Prepare` exists to capture an ENVIRONMENT, and overloading it
//     into "the thing that makes the workspace" is where atrium starts holding
//     git commands somebody wrote on another machine. The rule that atrium does
//     not learn git is worth more than the convenience.
//   - REJECTED: the room is given a workspace root and clones into it. Same
//     objection, one layer down.
//   - TAKEN: preparing the directory was somebody else's job, and the room is
//     the authority on its own filesystem. The ordinary dispatch names NO
//     directory at all, and the room's own harness row answers: a cloud box
//     with one checkout points its `claude` runner at it once, and every item
//     queued for that box lands there. A dispatch that does name a directory is
//     only honoured when the room was started with `--workspace`, and only
//     inside it.
//
// So the resolution order is: the dispatch, then this room's own harness row,
// then REFUSE. Never `os.Getwd()`, which is where the daemon falls back for a
// local launch and which here would start a session in whatever directory the
// room process happens to be sitting in. A card that quietly starts in the
// wrong place on a machine nobody is watching is the failure this file exists
// to make impossible.

// roomLaunchTimeout bounds one launch against this machine's own daemon.
//
// Generous next to the twenty second heartbeat, because the daemon watches a
// new runner for a moment before answering and a prepare command in front of it
// can take longer than that.
//
// AND BOUNDED BY THE LEASE, which is the constraint that actually sets it. A
// handout is at most `store.MaxHandout` items run one after another, so the
// worst case here multiplied by that has to stay comfortably under
// `store.DispatchLease`. Otherwise the hub takes an item back while this room is
// still starting it, hands it out again, and one instruction becomes two runners
// in one directory.
const roomLaunchTimeout = time.Minute

// handout is one launch the hub is asking this room to start.
type handout struct {
	ID      string   `json:"id"`
	Token   string   `json:"token"`
	Harness string   `json:"harness"`
	Cwd     string   `json:"cwd"`
	Title   string   `json:"title"`
	Prompt  string   `json:"prompt"`
	Why     string   `json:"why"`
	Tags    []string `json:"tags"`
	Window  string   `json:"window"`
}

// outcome is what this room did with one item.
//
// Kept after it has been reported, keyed by item id, so that an item handed out
// a second time is ANSWERED rather than run again. The hub takes a claim back
// when it hears nothing, and the case that produces is a result POST that never
// arrived: the work started here and the hub does not know. Re-reporting under
// the new token is the correct answer to being asked twice, and launching again
// would put two runners in one directory because a network hiccup.
//
// In memory, so it dies with this process. That is the honest limit: a room
// restarted mid-launch cannot answer for what the previous process did, and
// pretending otherwise would mean writing a second durable record of somebody
// else's queue.
type outcome struct {
	OK      bool
	CardID  string
	CardURL string
	Err     string
}

// roomWorker runs handouts off the heartbeat, and remembers what it did.
//
// OFF THE HEARTBEAT, which is not an optimisation. A launch takes up to a
// minute and a handout is several of them, and running that inline would stop
// the check-in for as long as it took. The hub would call the room stale
// halfway through the work it just sent, which is the worst possible reading of
// a machine that is doing exactly what it was asked.
type roomWorker struct {
	mu   sync.Mutex
	busy bool
	// done is what this room has already finished, keyed by item id. See
	// `outcome`.
	done map[string]outcome
}

// taking reports whether this room is free to be handed more work.
//
// A ROOM THAT IS STARTING THINGS IS NOT TAKING MORE, and it says so through the
// same field `--no-launch` uses, so the hub needs no second concept. Without
// this a room part way through a batch would collect another one on the next
// heartbeat, and the two batches together would outlast the lease on the first.
func (w *roomWorker) taking(o roomOpts) bool {
	if o.NoLaunch {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.busy
}

// run starts what the hub asked for, in the background, and tells it what
// happened.
//
// Sequential inside the batch, not concurrent. Three launches at once on a
// machine nobody is looking at is how a cloud box with two cores becomes
// unusable, and the whole reason work is being sent there is that the machine
// it came from is already saturated.
func (w *roomWorker) run(ctx context.Context, client *http.Client, hubURL, local string,
	o roomOpts, items []handout) {
	w.mu.Lock()
	if w.busy {
		// Cannot happen while `taking` gates the report, and cheap insurance
		// against it starting to.
		w.mu.Unlock()
		return
	}
	w.busy = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.busy = false
			w.mu.Unlock()
		}()
		for _, it := range items {
			w.mu.Lock()
			prev, seen := w.done[it.ID]
			w.mu.Unlock()
			if seen {
				// Asked twice. See `outcome`.
				log.Printf("[atrium] %s was already handled here, saying so again rather "+
					"than starting a second runner", it.ID)
				reportOutcome(ctx, client, hubURL, it, prev)
				continue
			}
			res := runOneHandout(ctx, local, o, it)
			w.mu.Lock()
			w.done[it.ID] = res
			w.mu.Unlock()
			if res.OK {
				log.Printf("[atrium] started %s for the hub, filed here as %s",
					it.Harness, res.CardID)
			} else {
				log.Printf("[atrium] refused %s from the hub: %s", it.ID, res.Err)
			}
			reportOutcome(ctx, client, hubURL, it, res)
		}
	}()
}

// runOneHandout resolves the directory, refuses if it cannot, and otherwise
// launches through this machine's own daemon.
func runOneHandout(ctx context.Context, local string, o roomOpts, it handout) outcome {
	if o.NoLaunch {
		return outcome{Err: "this room was started with --no-launch, so it reports its " +
			"cards and does not take work"}
	}
	if strings.TrimSpace(it.Harness) == "" {
		return outcome{Err: "that item does not say which runner to start"}
	}

	h, err := localHarness(ctx, local, it.Harness)
	if err != nil {
		return outcome{Err: err.Error()}
	}
	cwd, err := resolveHandoutCwd(o, it, h.Cwd)
	if err != nil {
		return outcome{Err: err.Error()}
	}

	body, err := json.Marshal(map[string]any{
		"harness": it.Harness, "cwd": filepath.ToSlash(cwd), "title": it.Title,
		"prompt": it.Prompt, "why": it.Why, "tags": it.Tags, "window": it.Window,
	})
	if err != nil {
		return outcome{Err: err.Error()}
	}
	lctx, cancel := context.WithTimeout(ctx, roomLaunchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(lctx, http.MethodPost,
		strings.TrimRight(local, "/")+"/v1/launch", bytes.NewReader(body))
	if err != nil {
		return outcome{Err: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: roomLaunchTimeout}).Do(req)
	if err != nil {
		return outcome{Err: "this room's own daemon did not answer: " + err.Error()}
	}
	defer res.Body.Close()

	var card struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&card)
	if res.StatusCode >= 400 {
		// The daemon's own words. A missing runner, a directory that vanished
		// between the check and the launch, or a harness with no way to take a
		// prompt all arrive here already readable, and rewording them would
		// only make them worse.
		if strings.TrimSpace(card.Error) != "" {
			return outcome{Err: card.Error}
		}
		return outcome{Err: "this room's own daemon answered " + res.Status}
	}
	return outcome{OK: true, CardID: card.ID, CardURL: cardURL(o.Board, card.ID)}
}

// resolveHandoutCwd decides where a dispatched launch runs, and refuses rather
// than guessing.
//
// A DISPATCH MAY ONLY NAME A DIRECTORY ON A ROOM THAT HAS SAID WHERE. Without
// `--workspace`, a hub naming an absolute path is a hub with arbitrary reach
// into this filesystem, and the room granted it nothing of the sort by agreeing
// to report its cards. With `--workspace`, the path is resolved through
// `internal/safepath`, which follows symlinks on both sides, so a junction
// inside the workspace pointing at the rest of the disk does not get through.
//
// The harness's own `cwd` is NOT checked against the workspace, and that is not
// an oversight. The workspace bounds what the HUB may name. What this room's
// own operator configured on this room's own harness row is this room's
// business, and running it through a containment check would refuse the
// ordinary setup: one checkout outside whatever directory was nominated as the
// place hub-supplied paths live.
func resolveHandoutCwd(o roomOpts, it handout, harnessCwd string) (string, error) {
	asked := strings.TrimSpace(it.Cwd)
	root := strings.TrimSpace(o.Workspace)

	if asked != "" {
		if root == "" {
			return "", fmt.Errorf("this item names the directory %q, and this room was "+
				"started without --workspace, so a hub may not name one here. queue it "+
				"without a directory and this room's %q runner answers, or restart the "+
				"room with --workspace", asked, it.Harness)
		}
		full, err := safepath.Contained(root, asked)
		if err != nil {
			return "", fmt.Errorf("%q is not inside this room's workspace %q: %w",
				asked, root, err)
		}
		return statDir(full)
	}

	if strings.TrimSpace(harnessCwd) != "" {
		return statDir(filepath.FromSlash(strings.TrimSpace(harnessCwd)))
	}

	// NO FALLBACK. See the header: the local launcher lands on `os.Getwd()`
	// here, and doing that for a remote instruction starts a session wherever
	// this process happens to be running.
	return "", fmt.Errorf("neither this item nor this room's %q runner says which "+
		"directory to work in, and a remote launch will not guess. set a working "+
		"directory on that runner here, or queue the item with one and start this "+
		"room with --workspace", it.Harness)
}

// statDir is the refusal the whole feature is built around.
//
// A remote machine does not have the worktree, and atrium does not make one.
// Saying so here, before anything is started, is the difference between a card
// that fails immediately with a readable reason and a card that comes up in the
// wrong place and looks fine.
func statDir(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory on this machine. atrium does not "+
			"create worktrees, so whatever makes them has to have run here first",
			filepath.ToSlash(path))
	}
	return path, nil
}

// cardURL points at the card on THIS machine's board, since that is where its
// terminal is and always will be.
func cardURL(board, id string) string {
	board = strings.TrimRight(strings.TrimSpace(board), "/")
	if board == "" || strings.TrimSpace(id) == "" {
		return ""
	}
	return board + "/#card=" + id
}

// roomHarness is the little of a runner row this file needs: whether it exists
// here at all, and where it says to work.
type roomHarness struct {
	ID      string `json:"id"`
	Cwd     string `json:"cwd"`
	Enabled bool   `json:"enabled"`
}

// localHarness reads one runner row off this machine's own daemon.
//
// Over loopback and through the same JSON API the board uses, for the reason
// `localCards` gives: two processes on one sqlite file is a question nobody
// needs to answer.
func localHarness(ctx context.Context, local, id string) (*roomHarness, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(local, "/")+"/v1/harnesses", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("this room's own daemon did not answer: %w", err)
	}
	defer res.Body.Close()
	var body struct {
		Harnesses []roomHarness `json:"harnesses"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(body.Harnesses))
	for _, h := range body.Harnesses {
		if strings.EqualFold(h.ID, id) {
			found := h
			return &found, nil
		}
		if h.Enabled {
			names = append(names, h.ID)
		}
	}
	// The list, because a hub queueing for four machines has no way to know
	// what each of them calls its runners, and "unknown harness" alone sends
	// somebody to another terminal to find out. Only the enabled ones: a
	// disabled row is not something a dispatch can use either.
	if len(names) == 0 {
		return nil, fmt.Errorf("this room has no runner called %q, and none enabled at all", id)
	}
	return nil, fmt.Errorf("this room has no runner called %q. it has: %s",
		id, strings.Join(names, ", "))
}

// reportOutcome tells the hub what happened, and gives up rather than looping.
//
// A HANDFUL OF TRIES, THEN SILENCE. The hub takes a claim back when it hears
// nothing, which is the safety net this does not need to duplicate, and a room
// that retries a result forever against a hub that has gone is a room spending
// its heartbeat on bookkeeping. What stops the retry from turning into a second
// launch is `done`, not this.
func reportOutcome(ctx context.Context, client *http.Client, hubURL string,
	it handout, res outcome) {
	body, err := json.Marshal(map[string]any{
		"token": it.Token, "ok": res.OK, "card_id": res.CardID,
		"card_url": res.CardURL, "error": res.Err,
	})
	if err != nil {
		return
	}
	url := strings.TrimRight(hubURL, "/") + "/v1/dispatch/" + it.ID + "/result"
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			status := resp.StatusCode
			resp.Body.Close()
			if status < 400 {
				return
			}
			if status == http.StatusConflict {
				// The hub took it back and gave it to somebody, or it was
				// already settled. Retrying cannot change that, and it is worth
				// one line because it means something ran here that the hub
				// does not credit to this item.
				log.Printf("[atrium] the hub would not take the result for %s: "+
					"it had already moved on", it.ID)
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	log.Printf("[atrium] could not tell the hub what happened to %s. it will take the "+
		"claim back, and this room will answer rather than start it again", it.ID)
}
