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

// path is the file of the conversation the runner last started, false when the card has none to read.
func (r claudeReader) path(t *store.Task) (string, bool) {
	d := r.d
	if d.usage == nil || !d.usage.isClaude(t.Runner) {
		return "", false
	}
	session := strings.TrimSpace(t.ResumeID)
	if d.ctx != nil {
		session = d.ctx.sessionOf(t)
	}
	if session == "" {
		return "", false
	}
	path := d.usage.transcript(t.Worktree, session)
	return path, path != ""
}

// newest file first, a file that runs out hands its oldest time on as the cursor into the next
func (r claudeReader) page(t *store.Task, n int, before time.Time) (replyPage, bool, error) {
	cur, ok := r.path(t)
	if !ok {
		return replyPage{}, false, nil
	}
	files := append([]string{cur}, siblingTranscripts(cur)...)
	if len(files) > repliesAllFiles {
		files = files[:repliesAllFiles]
	}
	pg, err := r.across(t, files, n, before)
	return pg, err == nil, err
}

func (r claudeReader) across(t *store.Task, files []string, n int, before time.Time) (replyPage, error) {
	var pg replyPage
	for i, path := range files {
		var err error
		if pg, err = r.pageOf(t, path, n, before); err != nil {
			if i == 0 {
				return replyPage{}, err
			}
			continue
		}
		if pg.more {
			break
		}
		if len(pg.replies) > 0 || len(pg.prompts) > 0 {
			if i+1 < len(files) {
				pg.more, pg.next = true, oldestOf(pg)
			}
			break
		}
	}
	return pg, nil
}

func oldestOf(pg replyPage) time.Time {
	var at time.Time
	if len(pg.replies) > 0 {
		at = pg.replies[0].At
	}
	if len(pg.prompts) > 0 && (at.IsZero() || pg.prompts[0].At.Before(at)) {
		at = pg.prompts[0].At
	}
	return at
}

func (r claudeReader) pageOf(t *store.Task, path string, n int, before time.Time) (replyPage, error) {
	d := r.d
	var (
		pg  replyPage
		err error
	)
	if before.IsZero() {
		pg, err = readTranscriptPage(path, n, &d.hk)
	} else {
		pg, err = readTranscriptBefore(path, n, before, &d.hk)
	}
	if err != nil {
		logRepliesFallback(path, err)
		return replyPage{}, err
	}
	fillEdited(t, path, pg.replies)
	return pg, nil
}
