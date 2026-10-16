package link

import (
	"context"
	"errors"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// pageGit is a LandGit that answers the page-changed question from a table, and fails for a path it was told to.
type pageGit struct {
	LandGit
	changed map[string]gitsync.PageChange
	fail    string
	asked   int
}

func (g *pageGit) PageChangedSince(_ context.Context, since, path string) (gitsync.PageChange, bool, error) {
	g.asked++
	if path == g.fail {
		return gitsync.PageChange{}, false, errors.New("git failed")
	}
	c, ok := g.changed[since+" "+path]
	return c, ok, nil
}

func hintMeter() hubstore.DocDebtMeter {
	return hubstore.DocDebtMeter{Counted: []hubstore.DocDebtCounted{
		{ID: "d-1", Feature: "f-one", LandedSHA: "aaa", Pages: []string{"README.md", "docs/cli.md"}},
		{ID: "d-2", Pages: []string{"README.md"}},
	}}
}

func hintProxy(g LandGit) *Proxy {
	p := &Proxy{}
	p.landGit = g
	return p
}

// A TOUCHED PAGE SHOWS THE HINT, AN UNTOUCHED ONE DOES NOT, and a hand-filed item (no landed sha) is never asked.
func TestPageHintsNameTouchedPagesOnly(t *testing.T) {
	g := &pageGit{changed: map[string]gitsync.PageChange{"aaa README.md": {SHA: "bbb", Date: "2026-10-16T10:00:00Z"}}}
	m := hintMeter()
	hintProxy(g).addPageHints(context.Background(), &m)
	want := hubstore.DocDebtPageChange{Path: "README.md", SHA: "bbb", Date: "2026-10-16T10:00:00Z"}
	if c := m.Counted[0].Changed; len(c) != 1 || c[0] != want {
		t.Fatalf("touched and untouched: %+v", c)
	}
	if m.Counted[1].Changed != nil {
		t.Fatalf("a hand-filed item has a hint: %+v", m.Counted[1].Changed)
	}
	if g.asked != 2 {
		t.Fatalf("asked git %d times, want 2 (the hand-filed item is not asked)", g.asked)
	}
}

// A FAILED GIT COMMAND LEAVES ITS PAGE OUT and the rest of the answer stands, and with no mirror wired there are no hints.
func TestPageHintsLeaveOutWhatCannotBeRead(t *testing.T) {
	g := &pageGit{changed: map[string]gitsync.PageChange{"aaa docs/cli.md": {SHA: "ccc", Date: "d"}}, fail: "README.md"}
	m := hintMeter()
	hintProxy(g).addPageHints(context.Background(), &m)
	if c := m.Counted[0].Changed; len(c) != 1 || c[0].Path != "docs/cli.md" {
		t.Fatalf("one page failed: %+v", c)
	}
	for name, lg := range map[string]LandGit{"no git": nil, "a git without the question": noPageGit{}} {
		m := hintMeter()
		hintProxy(lg).addPageHints(context.Background(), &m)
		if m.Counted[0].Changed != nil || m.Counted[1].Changed != nil {
			t.Fatalf("%s: %+v", name, m.Counted)
		}
	}
}

// noPageGit is a LandGit that cannot say when a page changed.
type noPageGit struct{ LandGit }
