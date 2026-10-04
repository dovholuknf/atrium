package daemon

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// fakeForge answers from memory, so a run needs no gh.
type fakeForge struct {
	err  error
	diff []byte
}

func (f *fakeForge) Kind() string { return "fake" }
func (f *fakeForge) View(context.Context, forge.Ref) (*forge.PR, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &forge.PR{Title: "tls engine: session resumption", Author: "ekoby", Head: prTestHead, HeadRef: "feat",
		BaseRef: "main", Files: []forge.File{{Path: "src/tls_engine.c", Additions: 4}, {Path: "src/http.c", Additions: 3}}}, nil
}
func (f *fakeForge) Diff(context.Context, forge.Ref) ([]byte, error) {
	return f.diff, nil
}
func (f *fakeForge) Head(context.Context, forge.Ref) (string, error) { return prTestHead, nil }
func (f *fakeForge) FetchSpec(r forge.Ref) forge.FetchSpec {
	return forge.FetchSpec{Remote: "https://example.test/o/r.git", Refspec: "refs/changes/1"}
}
func (f *fakeForge) PRURL(r forge.Ref) string { return "https://example.test/o/r/change/1" }

func TestPRRunnerWorksWithNoGh(t *testing.T) {
	f := newPRFix(t)
	diff, err := os.ReadFile("../prreview/render/testdata/run/pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	f.r.forgeOf = func(string) (forge.Forge, error) { return &fakeForge{diff: diff}, nil }
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "gh ") {
			t.Fatalf("the runner called gh: %s", c)
		}
	}
	var fetched bool
	for _, c := range f.calls {
		if strings.HasPrefix(c, "git fetch") && strings.HasSuffix(c, " origin refs/changes/1") {
			fetched = true
		}
	}
	if !fetched {
		t.Fatalf("the fake forge's fetch spec was not used: %v", f.calls)
	}
}

func TestPRRunnerNoForgeFailsTheRow(t *testing.T) {
	f := newPRFix(t)
	if _, err := f.st.SaveProvider(store.Provider{Name: "off", Root: t.TempDir(), Host: "github.com",
		Forge: "none", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	p := f.run(t)
	if p.State != store.PRFailed || !strings.HasPrefix(p.RunError, "fetch: no_forge: ") ||
		!strings.Contains(p.RunError, "github.com") || !strings.Contains(p.RunError, "provider") {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
}

func TestPRRunnerAccessErrorSentenceIsTheReason(t *testing.T) {
	f := newPRFix(t)
	f.r.forgeOf = func(string) (forge.Forge, error) {
		return &fakeForge{err: &forge.AccessError{Tool: "gh", Host: "github.com", Detail: "x"}}, nil
	}
	p := f.run(t)
	if p.State != store.PRFailed || p.RunError != "fetch: gh is not logged in for github.com. "+
		"Run `gh auth login --hostname github.com`" {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
}

func TestPRRunnerPicksForgeFromProviderHost(t *testing.T) {
	f := newPRFix(t)
	if _, err := f.st.SaveProvider(store.Provider{Name: "ghe", Root: t.TempDir(), Host: "ghe.example",
		Forge: "github", ForgeCmd: "ghw", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	got, err := f.r.forgeFor("ghe.example")
	if err != nil || got.Kind() != forge.GitHub {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := f.r.forgeFor("unknown.example"); err == nil || !strings.Contains(err.Error(), "unknown.example") {
		t.Fatalf("%v", err)
	}
}
