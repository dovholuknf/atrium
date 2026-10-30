package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/pulls"
	"github.com/dovholuknf/atrium/internal/store"
)

// changesFor answers the endpoint for one card.
func (d *Daemon) changesFor(ctx context.Context, taskID, against string, turn time.Time) (any, error) {
	return pulls.Changes{Get: d.st.Get, Transcript: d.cardTranscript, ListMax: d.hk.listMax}.For(ctx, taskID, against, turn)
}

// cardTranscript is the transcript a card's replies are read from, or "" when atrium
// cannot read one (a runner that is not Claude, or no file). The same choice /replies makes.
func (d *Daemon) cardTranscript(t *store.Task) string {
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return ""
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return ""
	}
	return d.usage.transcript(t.Worktree, session)
}

// fillEdited sets each reply's `edited`: how many files the edit calls of its turn named
// inside the worktree. From the transcript alone, no git. A failure leaves the counts 0.
func fillEdited(t *store.Task, transcript string, replies []Reply) {
	if len(replies) == 0 {
		return
	}
	x, err := pulls.TurnsOf(transcript)
	if err != nil {
		return
	}
	byTurn := map[time.Time]int{}
	for i := range replies {
		start, end := x.Span(replies[i].At)
		n, ok := byTurn[start]
		if !ok {
			rels, _ := pulls.InsideWorktreeVia(t.Worktree, x.EditsIn(start, end), pulls.ResolveInsideMemo)
			n = len(rels)
			byTurn[start] = n
		}
		replies[i].Edited = n
	}
}
