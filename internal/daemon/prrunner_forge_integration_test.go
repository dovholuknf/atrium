//go:build integration

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

func TestPRRunnerTellsTheDaemonOfAnAccessErrorAndOfASuccess(t *testing.T) {
	f := newPRFix(t)
	ae := &forge.AccessError{Tool: "gh", Host: "github.com", Detail: "x"}
	f.r.forgeOf = func(string) (forge.Forge, error) { return &fakeForge{err: ae}, nil }
	var got error
	f.r.onAccess = func(kind string, err error) bool { got = err; return true }
	f.r.onWorked = func(string, string) { t.Errorf("a failed fetch cleared the alert") }
	f.run(t)
	if got != ae {
		t.Fatalf("onAccess got %v", got)
	}
	g := newPRFix(t)
	g.r.forgeOf = func(string) (forge.Forge, error) { return &fakeForge{}, nil }
	var worked string
	g.r.onWorked = func(kind, host string) { worked = kind }
	g.run(t)
	if worked != "fake" {
		t.Errorf("a forge that answered did not clear: %q", worked)
	}
}

// A ROOM WITH A HUB ASKS THE HUB AND READS THE HEAD FROM ITS STORE: no gh, no forge URL, a whole fetch of the
// hub's ref through the loopback, and the loopback closed after.
func TestPRRunnerWithAHubAsksTheHubAndFetchesFromItsStore(t *testing.T) {
	f := newPRFix(t)
	diff, err := os.ReadFile("../prreview/render/testdata/run/pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	var asked []forge.HubAsk
	remote := forge.NewRemote(func(_ context.Context, path string, in, out any) error {
		a := in.(forge.HubAsk)
		asked = append(asked, a)
		ans := out.(*forge.HubPR)
		*ans = forge.HubPR{Kind: forge.GitHub, PR: &forge.PR{Title: "via the hub", Head: prTestHead, HeadRef: "feat",
			BaseRef: "main", Files: []forge.File{{Path: "src/tls_engine.c", Additions: 4}}}, Diff: diff,
			URL: "https://github.com/o/r/pull/1", Store: "github/o/r", Ref: forge.PRRef(a.Number)}
		return nil
	})
	f.r.hub = func() *forge.Remote { return remote }
	var sources []string
	closed := 0
	f.r.hubSource = func(_ context.Context, name string) (string, func(), error) {
		sources = append(sources, name)
		return "http://127.0.0.1:1/tok/git/hub/" + name + ".git", func() { closed++ }, nil
	}
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	if len(asked) == 0 || !asked[0].Fetch {
		t.Fatalf("the hub was not asked to fetch the head: %+v", asked)
	}
	if len(sources) != 1 || sources[0] != "github/o/r" || closed != 1 {
		t.Fatalf("sources %v closed %d", sources, closed)
	}
	var added, fetched bool
	for _, c := range f.calls {
		if strings.HasPrefix(c, "gh ") || strings.HasPrefix(c, "bb ") {
			t.Fatalf("a room with a hub ran a forge CLI: %s", c)
		}
		if strings.Contains(c, "github.com") && strings.HasPrefix(c, "git ") {
			t.Fatalf("a room with a hub fetched from the forge: %s", c)
		}
		if strings.HasPrefix(c, "git remote add origin http://127.0.0.1:1/tok/git/hub/github/o/r.git") {
			added = true
		}
		if strings.HasPrefix(c, "git fetch") && strings.HasSuffix(c, " origin "+forge.PRRef(p.Number)) {
			fetched = true
			if strings.Contains(c, "--depth") || strings.Contains(c, "--filter") {
				t.Fatalf("the hub's store serves whole fetches only: %s", c)
			}
		}
	}
	if !added || !fetched {
		t.Fatalf("the head was not read from the hub: %v", f.calls)
	}
}

// A ROOM WITH A HUB PICKS THE HUB'S FORGE for every host, and never a provider of its own.
func TestPRRunnerForgeForIsTheHubsWhenThereIsOne(t *testing.T) {
	f := newPRFix(t)
	remote := forge.NewRemote(nil)
	f.r.hub = func() *forge.Remote { return remote }
	for _, host := range []string{"github.com", "bitbucket.org", "unknown.example", ""} {
		if got, err := f.r.forgeFor(host); err != nil || got != forge.Forge(remote) {
			t.Fatalf("%q: %v %v", host, got, err)
		}
	}
	f.r.hub = func() *forge.Remote { return nil }
	if got, err := f.r.forgeFor("github.com"); err != nil || got.Kind() != forge.GitHub {
		t.Fatalf("a room with no hub: %v %v", got, err)
	}
}
