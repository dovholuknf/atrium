//go:build integration

package pulls

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestPRRunLogSaysEachSubStepWithItsSeconds(t *testing.T) {
	t.Parallel()
	f := newPRFix(t)
	diff, err := os.ReadFile("../../base/prreview/render/testdata/run/pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	var asked []forge.HubAsk
	remote := forge.NewRemote(func(_ context.Context, _ string, in, out any) error {
		a := in.(forge.HubAsk)
		asked = append(asked, a)
		*(out.(*forge.HubPR)) = forge.HubPR{Kind: forge.GitHub, PR: &forge.PR{Title: "via the hub", Head: prTestHead, HeadRef: "feat",
			BaseRef: "main", Files: []forge.File{{Path: "src/tls_engine.c", Additions: 4}}}, Diff: diff,
			URL: "https://github.com/o/r/pull/1", Store: "github/o/r", Ref: forge.PRRef(a.Number)}
		return nil
	})
	f.r.hub = func() *forge.Remote { return remote }
	f.r.hubSource = func(context.Context, string) (string, func(), error) {
		return "http://127.0.0.1:1/tok/git/hub/github/o/r.git", func() {}, nil
	}
	p := f.run(t)
	if p.State != store.PRReady {
		t.Fatalf("%s %q", p.State, p.RunError)
	}
	if len(asked) != 1 || !asked[0].Fetch || !asked[0].Diff {
		t.Fatalf("the hub was asked %d times, not once for the view, the head and the diff: %+v", len(asked), asked)
	}
	b, err := os.ReadFile(filepath.Join(filepath.FromSlash(p.RunDir), "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	for _, re := range []string{
		`fetch: asking the forge\n`, `fetch: forge answered \(\d+\.\ds\)\n`,
		`fetch: opening the hub's store\n`, `fetch: hub's store open \(\d+\.\ds\)\n`,
		`fetch: fetching the head into the source folder\n`, `fetch: head fetched \(\d+\.\ds\)\n`,
		`fetch: making the worktree\n`, `fetch: worktree made \(\d+\.\ds\)\n`,
		`fetch: writing the bundle\n`, `fetch: bundle written \(\d+\.\ds\)\n`,
		`prime: reading the bundle and preparing the agent\n`, `prime: agent prepared \(\d+\.\ds\)\n`,
		`prime: launching the agent prime\n`, `prime: agent prime answered \(\d+\.\ds\)\n`,
	} {
		if !regexp.MustCompile(re).MatchString(log) {
			t.Errorf("run.log has no line like %q:\n%s", re, log)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) < 20 {
		t.Errorf("the row was published %d times, so the board sees few of the steps", len(f.events))
	}
}

func TestPRFetchSourceTimedAgainstALocalRemote(t *testing.T) {
	t.Parallel()
	remote := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=a@b", "-c", "user.name=a"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(remote, "init", "-q")
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(remote, "f"+string(rune('a'+i%26))+".txt"), []byte(strings.Repeat("line\n", 500+i)), 0o644)
		git(remote, "add", ".")
		git(remote, "commit", "-q", "-m", "c")
	}
	head := git(remote, "rev-parse", "HEAD")
	git(remote, "update-ref", "refs/changes/1", head)

	f := newPRFix(t)
	f.r.run = f.r.runBounded
	f.r.baseEnv = os.Environ
	dir := t.TempDir()
	pr := &prRun{r: f.r, ctx: context.Background(), id: f.id, dir: dir}
	if err := pr.fetchSource(forge.FetchSpec{Remote: "file://" + filepath.ToSlash(remote), Refspec: "refs/changes/1"}, head); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "run.log"))
	t.Logf("run.log of a local fetch:\n%s", b)
	if !strings.Contains(string(b), "fetch: head fetched (") || !strings.Contains(string(b), "fetch: worktree made (") {
		t.Fatalf("no timing in:\n%s", b)
	}
}
