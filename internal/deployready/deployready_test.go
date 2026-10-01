package deployready

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// repo is a real temporary git repository on a branch called claude/main.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "claude/main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"-c", "core.autocrlf=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.dir
	cmd.Env = gitsync.CleanEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(rel, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// commit writes the files and commits them. Returns the SHA.
func (r *repo) commit(msg string, files map[string]string) string {
	r.t.Helper()
	for k, v := range files {
		r.write(k, v)
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// review commits a review file carrying the given trailers.
func (r *repo) review(name string, trailers ...string) string {
	r.t.Helper()
	msg := "Review of " + name + "\n\n" + strings.Join(trailers, "\n")
	return r.commit(msg, map[string]string{"docs/backlog/rt/r-new-review-" + name + ".md": "# review " + name + "\n"})
}

func (r *repo) checker() *Checker {
	return &Checker{Git: gitsync.NewRunner(), Dir: r.dir, Branch: "claude/main"}
}

func blockedSHAs(rep Report) []string {
	var out []string
	for _, b := range rep.Blocking {
		out = append(out, b.SHA)
	}
	return out
}

func wantState(t *testing.T, rep Report, state string) {
	t.Helper()
	if rep.State != state {
		t.Fatalf("state = %q (error %q, blocking %v, notes %v), want %q", rep.State, rep.Error, blockedSHAs(rep),
			rep.Notes, state)
	}
	if rep.Ready != (state == StateReady) {
		t.Fatalf("ready = %v in state %q", rep.Ready, state)
	}
}

func TestNeeds(t *testing.T) {
	cases := map[string]bool{
		"internal/daemon/daemon.go":        true,
		"internal/api/web/index.html":      true,
		"internal/link/hub.go":             true,
		"cmd/atrium/main.go":               true,
		"scripts/live/deploy-hub-only.ps1": true,
		"scripts/test-board-headless.js":   false,
		"scripts/other.js":                 true,
		"go.mod":                           true,
		"docs/design.md":                   false,
		"changelog/runtime/x.md":           false,
		"README.md":                        false,
		"internal/link/README.md":          false,
		"internal/api/web/logo.png":        false,
		"internal/api/web/LOGO.SVG":        false,
		"website/x.js":                     false,
		"docs/x.go":                        false,
	}
	for p, want := range cases {
		if got := Needs(p); got != want {
			t.Errorf("Needs(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRoomSide(t *testing.T) {
	cases := map[string]bool{
		"internal/daemon/daemon.go":        true,
		"internal/api/web/index.html":      true,
		"cmd/atrium/main.go":               true,
		"internal/link/hub.go":             true,
		"internal/link/dialer.go":          true,
		"internal/hubstore/store.go":       false,
		"scripts/live/deploy-hub-only.ps1": false,
	}
	for p, want := range cases {
		if got := RoomSide(p); got != want {
			t.Errorf("RoomSide(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestParseTrailers(t *testing.T) {
	msg := "Review of x\n\nbody\n\nAtrium-Verdict: hub-ok 108ced12~1..86240e3a\nAtrium-Verdict: room-ok abc1234\n" +
		"Atrium-Verdict: hold -rf..x\nAtrium-Verdict: maybe abc1234\n  Atrium-Verdict: hub-ok indented\n"
	got := parseTrailers(msg)
	if len(got) != 2 || got[0].kind != "hub-ok" || got[0].spec != "108ced12~1..86240e3a" ||
		got[1].kind != "room-ok" || got[1].spec != "abc1234" {
		t.Fatalf("trailers = %+v", got)
	}
}

func TestNothingLandedIsCurrent(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateCurrent)

	r.commit("docs only", map[string]string{"docs/x.md": "x", "changelog/rt/y.md": "y", "pic.png": "p"})
	rep = r.checker().Check(context.Background(), base)
	wantState(t, rep, StateCurrent)
	if rep.Commits != 0 {
		t.Fatalf("commits = %d", rep.Commits)
	}
}

func TestCodeWithoutVerdictBlocksAndNamesTheCommit(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("change the hub", map[string]string{"internal/hubstore/a.go": "b"})
	r.commit("docs", map[string]string{"docs/x.md": "x"})
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Blocking) != 1 || rep.Blocking[0].SHA != code || rep.Blocking[0].Why != WhyNoVerdict ||
		rep.Blocking[0].Subject != "change the hub" {
		t.Fatalf("blocking = %+v", rep.Blocking)
	}
	if rep.Commits != 1 {
		t.Fatalf("commits = %d", rep.Commits)
	}
	if !strings.Contains(rep.Line, code[:8]) || !strings.Contains(rep.Line, "change the hub") {
		t.Fatalf("line does not name the commit: %q", rep.Line)
	}
}

func TestVerdictByRangeMakesItReady(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	tip := r.commit("two", map[string]string{"scripts/live/x.ps1": "2"})
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+tip)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateReady)
	if rep.Commits != 2 || len(rep.Blocking) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if !strings.HasPrefix(rep.Line, "deploy ready") {
		t.Fatalf("line = %q", rep.Line)
	}
}

func TestVerdictByRangeCoversOnlyTheRange(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	one := r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	r.commit("two", map[string]string{"internal/hubstore/b.go": "2"})
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+one)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Blocking) != 1 || rep.Blocking[0].Subject != "two" {
		t.Fatalf("blocking = %+v", rep.Blocking)
	}
}

func TestSingleCommitVerdict(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	one := r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	r.review("rt", "Atrium-Verdict: hub-ok "+one)
	wantState(t, r.checker().Check(context.Background(), base), StateReady)
}

func TestRoomSideNeedsARoomVerdict(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("room side", map[string]string{"internal/daemon/d.go": "d"})
	r.review("hub", "Atrium-Verdict: hub-ok "+code)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if rep.Blocking[0].Why != WhyNoRoomVerdict {
		t.Fatalf("why = %q", rep.Blocking[0].Why)
	}

	r.review("room", "Atrium-Verdict: room-ok "+code)
	wantState(t, r.checker().Check(context.Background(), base), StateReady)
}

func TestRoomVerdictAloneIsNotAHubVerdict(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("room side", map[string]string{"internal/daemon/d.go": "d"})
	r.review("room", "Atrium-Verdict: room-ok "+code)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if rep.Blocking[0].Why != WhyNoVerdict {
		t.Fatalf("why = %q", rep.Blocking[0].Why)
	}
}

// A landing rewrites SHAs. The verdict names the branch tip that was read, and the commit that lands is a cherry-pick
// of it.
func TestVerdictSurvivesARebase(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.git("checkout", "-q", "-b", "feature")
	f1 := r.commit("f1", map[string]string{"internal/hubstore/f1.go": "1"})
	f2 := r.commit("f2", map[string]string{"internal/hubstore/f2.go": "2"})
	r.git("checkout", "-q", "claude/main")
	// The branch moved while it was being read, so the landing is not a fast-forward.
	moved := r.commit("moved on", map[string]string{"docs/moved.md": "m"})
	r.review("feature", "Atrium-Verdict: hub-ok "+base+".."+f2)
	r.git("cherry-pick", f1, f2)
	landed := r.git("rev-parse", "HEAD")
	if landed == f2 || moved == "" {
		t.Fatal("the landing kept its SHA, so this tests nothing")
	}
	wantState(t, r.checker().Check(context.Background(), base), StateReady)
}

func TestVerdictDoesNotSurviveAnAlteredPatch(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.git("checkout", "-q", "-b", "feature")
	f1 := r.commit("f1", map[string]string{"internal/hubstore/f1.go": "reviewed"})
	r.git("checkout", "-q", "claude/main")
	r.review("feature", "Atrium-Verdict: hub-ok "+base+".."+f1)
	r.git("cherry-pick", "-n", f1)
	r.write("internal/hubstore/f1.go", "changed on the way")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "f1 landed")
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Blocking) != 1 || rep.Blocking[0].Subject != "f1 landed" {
		t.Fatalf("blocking = %+v", rep.Blocking)
	}
}

func TestNewestVerdictWins(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})

	r.review("one", "Atrium-Verdict: hold "+code)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if rep.Blocking[0].Why != WhyHold {
		t.Fatalf("why = %q", rep.Blocking[0].Why)
	}

	r.review("two", "Atrium-Verdict: hub-ok "+code)
	wantState(t, r.checker().Check(context.Background(), base), StateReady)

	r.review("three", "Atrium-Verdict: hold "+code)
	rep = r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if rep.Blocking[0].Why != WhyHold {
		t.Fatalf("why = %q", rep.Blocking[0].Why)
	}
}

func TestHoldThenHubOkOnlyStillHoldsTheRoom(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("room side", map[string]string{"internal/daemon/d.go": "d"})
	r.review("one", "Atrium-Verdict: hold "+code)
	r.review("two", "Atrium-Verdict: hub-ok "+code)
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if rep.Blocking[0].Why != WhyRoomHold {
		t.Fatalf("why = %q", rep.Blocking[0].Why)
	}
	r.review("three", "Atrium-Verdict: room-ok "+code)
	wantState(t, r.checker().Check(context.Background(), base), StateReady)
}

func TestConditionalOKCountsAsOK(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	r.commit("Review: OK, low to follow, given the gate\n\nAtrium-Verdict: hub-ok "+code,
		map[string]string{"docs/backlog/rt/r-new-review-cond.md": "ok, low to follow"})
	wantState(t, r.checker().Check(context.Background(), base), StateReady)
}

func TestVerdictFoldedIntoACodeCommitIsRefused(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code\n\nAtrium-Verdict: hub-ok HEAD", map[string]string{"internal/hubstore/a.go": "1"})
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Notes) == 0 || !strings.Contains(rep.Notes[0], code[:8]) {
		t.Fatalf("notes = %v", rep.Notes)
	}

	// Mixed with a review file is not a review commit either.
	r.commit("review and code\n\nAtrium-Verdict: hub-ok "+code, map[string]string{
		"docs/backlog/rt/r-new-review-x.md": "x", "internal/hubstore/b.go": "2"})
	wantState(t, r.checker().Check(context.Background(), base), StateBlocked)
}

func TestVerdictOnAnUnknownRangeIsIgnoredWithANote(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	r.review("rt", "Atrium-Verdict: hub-ok deadbeef..cafebabe")
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "not in this checkout") {
		t.Fatalf("notes = %v", rep.Notes)
	}
}

// The reviewed branch merged claude/main into itself. A range over its first parent covers the branch's own commits
// and not the unreviewed claude/main commit that came in through the merge.
func TestRangeIsFirstParentOnly(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	r.git("checkout", "-q", "-b", "feature")
	r.commit("f1", map[string]string{"internal/hubstore/f1.go": "1"})
	r.git("checkout", "-q", "claude/main")
	r.commit("m1 unreviewed", map[string]string{"internal/hubstore/m1.go": "m"})
	r.git("checkout", "-q", "feature")
	r.git("merge", "-q", "--no-ff", "-m", "Merge claude/main into feature", "claude/main")
	f2 := r.commit("f2", map[string]string{"internal/hubstore/f2.go": "2"})
	r.review("feature", "Atrium-Verdict: hub-ok "+base+".."+f2)
	r.git("checkout", "-q", "claude/main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge feature", "feature")

	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Blocking) != 1 || rep.Blocking[0].Subject != "m1 unreviewed" {
		t.Fatalf("blocking = %+v", rep.Blocking)
	}
}

// A merge counts as code only when it carries a change of its own.
func TestMergeNeedsAVerdictOnlyForItsResolution(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/hubstore/c.go": "base\n"})
	r.git("checkout", "-q", "-b", "side")
	side := r.commit("side", map[string]string{"internal/hubstore/c.go": "side\n"})
	r.git("checkout", "-q", "claude/main")
	mainSide := r.commit("main", map[string]string{"internal/hubstore/c.go": "main\n"})
	r.review("both", "Atrium-Verdict: hub-ok "+base+".."+mainSide, "Atrium-Verdict: hub-ok "+base+".."+side)

	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"merge", "--no-ff", "-m", "Merge side", "side")
	cmd.Dir = r.dir
	cmd.Env = gitsync.CleanEnv()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected a conflict: %s", out)
	}
	r.write("internal/hubstore/c.go", "resolved\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "Merge side")

	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateBlocked)
	if len(rep.Blocking) != 1 || rep.Blocking[0].Subject != "Merge side" {
		t.Fatalf("blocking = %+v", rep.Blocking)
	}
}

func TestCleanMergeIsNotCode(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/hubstore/c.go": "base\n"})
	r.git("checkout", "-q", "-b", "side")
	side := r.commit("side", map[string]string{"internal/hubstore/s.go": "s\n"})
	r.git("checkout", "-q", "claude/main")
	m := r.commit("main", map[string]string{"internal/hubstore/m.go": "m\n"})
	r.review("both", "Atrium-Verdict: hub-ok "+m, "Atrium-Verdict: hub-ok "+side)
	r.git("merge", "-q", "--no-ff", "-m", "Merge side", "side")
	rep := r.checker().Check(context.Background(), base)
	wantState(t, rep, StateReady)
	if rep.Commits != 2 {
		t.Fatalf("commits = %d, want 2 (the merge carries nothing)", rep.Commits)
	}
}

func TestInstalledBuildProblems(t *testing.T) {
	r := newRepo(t)
	r.commit("base", map[string]string{"internal/a.go": "a"})
	c := r.checker()

	rep := c.Check(context.Background(), "")
	wantState(t, rep, StateUnknown)
	rep = c.Check(context.Background(), "0123456789abcdef0123456789abcdef01234567")
	wantState(t, rep, StateUnknown)

	// A side branch the installed build was made on, which claude/main does not contain.
	r.git("checkout", "-q", "-b", "other")
	other := r.commit("other", map[string]string{"internal/o.go": "o"})
	r.git("checkout", "-q", "claude/main")
	r.commit("main", map[string]string{"internal/m.go": "m"})
	rep = c.Check(context.Background(), other)
	wantState(t, rep, StateUnknown)
	if !strings.Contains(rep.Error, "not an ancestor") {
		t.Fatalf("error = %q", rep.Error)
	}
}

func TestMissingBranchIsUnknown(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	c := r.checker()
	c.Branch = "nope"
	wantState(t, c.Check(context.Background(), base), StateUnknown)
}

func TestSignatureMovesWithTheAnswer(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	c := r.checker()
	before := c.Check(context.Background(), base)
	again := c.Check(context.Background(), base)
	if before.Signature() != again.Signature() {
		t.Fatal("signature changed with nothing changing")
	}
	r.review("rt", "Atrium-Verdict: hub-ok "+code)
	after := c.Check(context.Background(), base)
	if before.Signature() == after.Signature() {
		t.Fatal("signature did not move when the verdict landed")
	}
}

// countGit counts the calls that reach git, and can be told to hang until its context ends.
type countGit struct {
	inner Git
	mu    sync.Mutex
	calls int
	hang  bool
}

func (g *countGit) Git(ctx context.Context, dir string, args ...string) (string, error) {
	g.mu.Lock()
	g.calls++
	hang := g.hang
	g.mu.Unlock()
	if hang {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return g.inner.Git(ctx, dir, args...)
}

func (g *countGit) GitInput(ctx context.Context, dir string, in []byte, args ...string) (string, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	return g.inner.GitInput(ctx, dir, in, args...)
}

func (g *countGit) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func TestSlowGitEndsAtTheContextBoundAsUnknown(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	g := &countGit{inner: gitsync.NewRunner(), hang: true}
	c := &Checker{Git: g, Dir: r.dir, Branch: "claude/main"}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	rep := c.Check(ctx, base)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %s", d)
	}
	wantState(t, rep, StateUnknown)
}

func TestSecondPassReadsAlmostNothingAndAgrees(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	var tips []string
	for i := 0; i < 6; i++ {
		file := fmt.Sprintf("internal/hubstore/%d.go", i)
		tips = append(tips, r.commit(fmt.Sprintf("code %d", i), map[string]string{file: "x"}))
	}
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+tips[2], "Atrium-Verdict: hub-ok "+tips[3]+"^.."+tips[5])
	g := &countGit{inner: gitsync.NewRunner()}
	c := &Checker{Git: g, Dir: r.dir, Branch: "claude/main"}
	first := c.Check(context.Background(), base)
	cold := g.count()
	second := c.Check(context.Background(), base)
	warm := g.count() - cold
	wantState(t, first, StateReady)
	if first.Signature() != second.Signature() {
		t.Fatalf("answers differ: %q vs %q", first.Signature(), second.Signature())
	}
	if warm >= cold {
		t.Fatalf("warm pass made %d git calls, cold made %d", warm, cold)
	}
}

// failMergeGit fails the `--remerge-diff` read once with the given error.
type failMergeGit struct {
	inner Git
	err   error
	fired bool
}

func (g *failMergeGit) Git(ctx context.Context, dir string, args ...string) (string, error) {
	if !g.fired && strings.Contains(strings.Join(args, " "), "--remerge-diff") {
		g.fired = true
		return "", g.err
	}
	return g.inner.Git(ctx, dir, args...)
}

func (g *failMergeGit) GitInput(ctx context.Context, dir string, in []byte, args ...string) (string, error) {
	return g.inner.GitInput(ctx, dir, in, args...)
}

// resolvedMergeRepo is a repo whose newest commit is a merge that resolved a conflict. Returns the base.
func resolvedMergeRepo(t *testing.T) (*repo, string) {
	t.Helper()
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/hubstore/c.go": "base\n"})
	r.git("checkout", "-q", "-b", "side")
	side := r.commit("side", map[string]string{"internal/hubstore/c.go": "side\n"})
	r.git("checkout", "-q", "claude/main")
	mainSide := r.commit("main", map[string]string{"internal/hubstore/c.go": "main\n"})
	r.review("both", "Atrium-Verdict: hub-ok "+base+".."+mainSide, "Atrium-Verdict: hub-ok "+base+".."+side)
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"merge", "--no-ff", "-m", "Merge side", "side")
	cmd.Dir = r.dir
	cmd.Env = gitsync.CleanEnv()
	_, _ = cmd.CombinedOutput()
	r.write("internal/hubstore/c.go", "resolved\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "Merge side")
	return r, base
}

// A merge read that fails for any reason but an old git must fail the pass and cache nothing. Cached as "carries
// nothing", a conflict resolution would need no verdict and the answer would be ready for the life of the process.
func TestFailedMergeReadFailsThePassAndIsNotCachedAsEmpty(t *testing.T) {
	r, base := resolvedMergeRepo(t)
	g := &failMergeGit{inner: gitsync.NewRunner(), err: errors.New("transient: unable to read tree")}
	c := &Checker{Git: g, Dir: r.dir, Branch: "claude/main"}
	first := c.Check(context.Background(), base)
	wantState(t, first, StateUnknown)
	second := c.Check(context.Background(), base)
	wantState(t, second, StateBlocked)
	if len(second.Blocking) != 1 || second.Blocking[0].Subject != "Merge side" {
		t.Fatalf("blocking = %+v", second.Blocking)
	}
}

func TestGitTooOldForRemergeDiffCountsMergesAsCarryingNothing(t *testing.T) {
	r, base := resolvedMergeRepo(t)
	err := errors.New("git log --remerge-diff: error: unknown option `remerge-diff'")
	c := &Checker{Git: &failMergeGit{inner: gitsync.NewRunner(), err: err}, Dir: r.dir, Branch: "claude/main"}
	rep := c.Check(context.Background(), base)
	wantState(t, rep, StateReady)
}

func TestVerdictRangeCutAtTheLimitIsReportedInTheNotes(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	var shas []string
	for i := 0; i < 4; i++ {
		shas = append(shas, r.commit(fmt.Sprintf("code %d", i), map[string]string{fmt.Sprintf("internal/hubstore/%d.go", i): "x"}))
	}
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+shas[3])
	c := r.checker()
	c.MaxCommits = 3
	// Two commits since the installed build fit the limit, the four the verdict names do not.
	rep := c.Check(context.Background(), shas[1])
	found := false
	for _, n := range rep.Notes {
		if strings.Contains(n, "only the newest 3") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes = %q", rep.Notes)
	}
}

func TestTooManyCommitsSinceInstalledIsUnknownNotAWalk(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	for i := 0; i < 4; i++ {
		r.commit(fmt.Sprintf("code %d", i), map[string]string{fmt.Sprintf("internal/hubstore/%d.go", i): "x"})
	}
	c := r.checker()
	c.MaxCommits = 3
	rep := c.Check(context.Background(), base)
	wantState(t, rep, StateUnknown)
	if !strings.Contains(rep.Error, "more than the 3") {
		t.Fatalf("error = %q", rep.Error)
	}
}
