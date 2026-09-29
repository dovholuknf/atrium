package daemon

import (
	"os"
	"sort"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// BACKFILL FROM TRANSCRIPTS. A one-time command (`atrium usage backfill`), never
// a timer. It reads the main transcript of every card's known resume id with the
// same reader the live record uses, and writes rows with cause `backfill` for
// replies no row covers, so running it twice adds nothing. A reply is covered
// when it falls inside a recorded row's span, or is that row's last message.
//
// There are no Stop hooks in a transcript, so replies are grouped into turns by
// the quiet between them. Subagent transcripts are not read. The department and
// director come from the card as it is now, since that is all there is.

// backfillGap is the quiet between two replies that starts a new row.
const backfillGap = 5 * time.Minute

// BackfillResult is what a backfill did, or would do.
type BackfillResult struct {
	Cards, Rows, Replies int
	// Skipped is cards with a resume id and no transcript on this machine.
	Skipped int
}

// BackfillUsage writes the rows the room missed for every Claude card, from
// since, held to store.UsageMaxBack. With dry true it counts and writes nothing.
// transcript is api.TranscriptPath when nil.
func BackfillUsage(st *store.Store, since time.Time, dry bool, transcript func(cwd, id string) string) (*BackfillResult, error) {
	if transcript == nil {
		transcript = api.TranscriptPath
	}
	floor := time.Now().UTC().Add(-store.UsageMaxBack)
	if since.Before(floor) {
		since = floor
	}
	tasks, err := st.List()
	if err != nil {
		return nil, err
	}
	res := &BackfillResult{}
	for _, t := range tasks {
		if t.ResumeID == "" || t.Worktree == "" {
			continue
		}
		if h, err := st.Harness(t.Runner); err != nil || !isClaude(h) {
			continue
		}
		path := transcript(t.Worktree, t.ResumeID)
		if path == "" {
			res.Skipped++
			continue
		}
		rows, replies, err := backfillCard(st, t, path, since, dry)
		if err != nil {
			return res, err
		}
		if rows > 0 {
			res.Cards++
		}
		res.Rows += rows
		res.Replies += replies
	}
	return res, nil
}

func backfillCard(st *store.Store, t *store.Task, path string, since time.Time, dry bool) (int, int, error) {
	spans, err := st.UsageSpans(t.ID, t.ResumeID, since)
	if err != nil {
		return 0, 0, err
	}
	last, err := st.LastTranscriptUsage(t.ID, t.ResumeID)
	if err != nil {
		return 0, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, nil
	}
	defer f.Close()
	byID := map[string]*mainReply{}
	var order []string
	if _, err := scanMainReplies(f, func(r *mainReply) {
		if r.At.Before(since) {
			return
		}
		if r.MessageID == "" {
			r.MessageID = r.At.Format(time.RFC3339Nano)
		}
		if last != nil && r.MessageID == last.LastMessage {
			return
		}
		for _, sp := range spans {
			if !r.At.Before(sp.Started) && !r.At.After(sp.Ended) {
				return
			}
		}
		if _, seen := byID[r.MessageID]; !seen {
			order = append(order, r.MessageID)
		}
		byID[r.MessageID] = r
	}); err != nil {
		return 0, 0, err
	}
	sort.SliceStable(order, func(i, j int) bool { return byID[order[i]].At.Before(byID[order[j]].At) })
	dept, launcher, launcherID := usageGroups(st, t)
	var (
		rows int
		set  *replySet
		prev time.Time
	)
	flush := func() error {
		if set == nil || len(set.order) == 0 {
			return nil
		}
		row := set.row(t)
		row.Cause = store.UsageBackfill
		row.Dept, row.Launcher, row.LauncherID = dept, launcher, launcherID
		rows++
		set = nil
		if dry {
			return nil
		}
		return st.AddSessionUsage(row)
	}
	for _, id := range order {
		r := byID[id]
		if set != nil && r.At.Sub(prev) > backfillGap {
			if err := flush(); err != nil {
				return rows, len(order), err
			}
		}
		if set == nil {
			set = newReplySet()
		}
		set.take(r, &usageCursor{})
		prev = r.At
	}
	if err := flush(); err != nil {
		return rows, len(order), err
	}
	return rows, len(order), nil
}
