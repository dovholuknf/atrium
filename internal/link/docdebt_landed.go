package link

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE LANDED CHECK. docs/rnd/doc-debt-from-features.md sections 2.8 and 4 (phase 5). Two things said on an item's detail once
// it has landed, both read and neither written:
//
//   - a FEATURE (kind code or mixed) whose docs/changes/<item>.md, as the commit it landed as held it, says neither
//     `Docs: d-<n>` nor `Docs: none, <why>` above its `## Test plan`: "docs: not said". It files nothing and blocks nothing,
//     the same posture as the fold's warning, and it covers a branch that merged without the fold running.
//   - a DOC WORKER'S item (kind doc) whose backlog item is still open: the offer "close d-7?". The hub does not close it.
//     Whether the offer was dismissed is the board's to remember (localStorage), so there is no column for it.
//
// A file the commit does not hold, a git that could not be read and an item that has not landed say nothing: the note is
// for a line that is missing, never for a file that could not be found, since the fold deletes the file after it is read.

// DocsFileGit is what the check asks of the hub's repository: a file as a commit held it. The repository the LandGit reads
// answers it when it can, and without it the note is left out.
type DocsFileGit interface {
	FileAt(ctx context.Context, sha, path string) (text string, found bool, err error)
}

// docsLineRe is the `Docs:` line, the same one scripts/fold-changes.ps1 looks for.
var docsLineRe = regexp.MustCompile(`^Docs: (d-\d+|none, \S)`)

// docsHeadMax is how many lines above the test plan are read for the line, as the fold reads them.
const docsHeadMax = 40

// docsSaid is whether a change file carries the `Docs:` line above its `## Test plan`.
func docsSaid(text string) bool {
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "## Test plan") || i >= docsHeadMax {
			return false
		}
		if docsLineRe.MatchString(line) {
			return true
		}
	}
	return false
}

// DocsOffer is "close d-7?": a doc worker's item has landed and its doc item is still open.
type DocsOffer struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// DocsView is the `docs` of GET /_hub/work/<id>. Both fields empty is nothing to say and the answer leaves it out.
type DocsView struct {
	// NotSaid is a landed feature whose change file says nothing of its docs.
	NotSaid bool `json:"not_said,omitempty"`
	// Offer is a landed doc worker's item, offered once for closing.
	Offer *DocsOffer `json:"offer,omitempty"`
}

// docsNotSaid is the note: a feature that landed as `sha` and whose docs/changes/<id>.md says nothing of its docs. A doc item is
// never a feature, so kind doc says nothing.
func docsNotSaid(ctx context.Context, g DocsFileGit, id, kind, sha string) bool {
	if g == nil || sha == "" || kind == "doc" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text, found, err := g.FileAt(ctx, sha, "docs/changes/"+id+".md")
	return err == nil && found && !docsSaid(text)
}

// docsOffered is the offer: a doc worker's item that landed, whose backlog item is not yet done or dropped.
func docsOffered(kind, status string, it hubstore.BacklogItem) *DocsOffer {
	if kind != "doc" || status == "" || status == "done" || status == "dropped" {
		return nil
	}
	return &DocsOffer{ID: it.ID, Title: it.Title}
}

// docsOf is the landed check of one item. It reads the stored steps, which the detail has just refreshed, and the repository,
// and writes nothing. Nil when there is nothing to say.
func (p *Proxy) docsOf(ctx context.Context, st *hubstore.Store, id string) *DocsView {
	wk, err := st.WorkByID(id)
	if err != nil {
		return nil
	}
	steps, err := st.WorkSteps(id)
	if err != nil {
		return nil
	}
	sha, landed := "", false
	for _, s := range steps {
		if s.Step == hubstore.StepLanded && s.State == hubstore.StepDone {
			sha, landed = s.SHA, true
		}
	}
	if !landed {
		return nil
	}
	v := &DocsView{}
	if wk.Kind == "doc" {
		if it, err := st.ItemGet(id); err == nil {
			v.Offer = docsOffered(wk.Kind, it.Status, it)
		}
	} else {
		p.mu.Lock()
		g, _ := p.landGit.(DocsFileGit)
		p.mu.Unlock()
		v.NotSaid = docsNotSaid(ctx, g, id, wk.Kind, sha)
	}
	if !v.NotSaid && v.Offer == nil {
		return nil
	}
	return v
}
