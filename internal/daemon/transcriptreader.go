package daemon

import (
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// transcriptReader is one runner's way to read a card's conversation as replies
// and prompts, for GET /v1/tasks/{id}/replies. A runner with no reader (or whose
// reader says no) keeps the screen fallback.
//
// page answers one page: the newest n replies and n prompts when before is
// zero, else those strictly older than before, as finishPage cuts them.
//
//   - ok false, err nil: this reader has nothing for the card (another runner's
//     card, no session id yet, no file). Silent.
//   - err non-nil: it tried and could not read. The reader logs it once; the
//     caller answers from the screen.
//
// A new runner (codex's rollout, gemini's chat log) is one more reader in
// transcriptReaders. The reader owns its own bounds and cache.
type transcriptReader interface {
	page(t *store.Task, n int, before time.Time) (pg replyPage, ok bool, err error)
}

// transcriptReaders are tried in order, the first with an answer wins.
func (d *Daemon) transcriptReaders() []transcriptReader {
	return []transcriptReader{claudeReader{d}, opencodeSource(d)}
}

// claudeReader reads Claude Code's JSONL transcript. It is repliesPage's
// original read, unchanged.
type claudeReader struct{ d *Daemon }

func (r claudeReader) page(t *store.Task, n int, before time.Time) (replyPage, bool, error) {
	d := r.d
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return replyPage{}, false, nil
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return replyPage{}, false, nil
	}
	path := d.usage.transcript(t.Worktree, session)
	if path == "" {
		return replyPage{}, false, nil
	}
	var (
		pg  replyPage
		err error
	)
	if before.IsZero() {
		pg, err = readTranscriptPage(path, n)
	} else {
		pg, err = readTranscriptBefore(path, n, before)
	}
	if err != nil {
		logRepliesFallback(path, err)
		return replyPage{}, false, err
	}
	fillEdited(t, path, pg.replies)
	return pg, true, nil
}
