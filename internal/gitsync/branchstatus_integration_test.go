//go:build integration && git

package gitsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

// bsFix is a work tree with a claude/main whose history was REWRITTEN after the branches were cut (so no branch is an
// ancestor of it), and the hub's bare repository that every branch is pushed to.
type bsFix struct {
	t    *testing.T
	work string
	st   *Store
	tick int // git calls so far: each gets its own committer second, so a cherry-pick never equals its source
}

func (x *bsFix) git(args ...string) string {
	x.tick++
	x.t.Helper()
	c := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	c.Dir = x.work
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t", "GIT_COMMITTER_DATE="+time.Now().Add(time.Hour+time.Duration(x.tick)*time.Second).Format(time.RFC3339))
	out, err := c.CombinedOutput()
	if err != nil {
		x.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files (path to body) and commits them under msg.
func (x *bsFix) commit(msg string, files map[string]string) string {
	x.t.Helper()
	for p, body := range files {
		fp := filepath.Join(x.work, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			x.t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(body), 0o644); err != nil {
			x.t.Fatal(err)
		}
	}
	x.git("add", "-A")
	x.git("commit", "-q", "--allow-empty", "-m", msg)
	return x.git("rev-parse", "HEAD")
}

func (x *bsFix) branch(name, from string) { x.git("switch", "-q", "-C", name, from) }

func newBranchFix(t *testing.T) *bsFix {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "github", "o", "r.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, Marker), []byte("made by test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &Hub{Dir: root, StoreRoot: func() string { return root }, Runner: NewRunner()}
	x := &bsFix{t: t, work: work, st: h.Store()}
	x.git("init", "-q", "-b", "old")
	x.git("remote", "add", "hub", dir)
	return x
}

func (x *bsFix) pass(st *branchState) RepoBranches {
	x.t.Helper()
	got, err := x.st.branchPass(bg, "github/o/r", st)
	if err != nil {
		x.t.Fatal(err)
	}
	return got
}

func byName(r RepoBranches) map[string]BranchStatus {
	m := map[string]BranchStatus{}
	for _, b := range r.Branches {
		m[b.Name] = b
	}
	return m
}

// A BRANCH IS LANDED BY WHAT IT CHANGES: a rewritten main, a squash, a pick with a resolved conflict, an empty commit
// and a subject that only looks the same, each called the way section 3 of branch-status-design.md says.
func TestBranchStatusCallsLandedByContentNotAncestry(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses")
	}
	x := newBranchFix(t)
	base := x.commit("base", map[string]string{"c.txt": "1\n", "a.txt": "a\n"})

	x.branch("claude/pick", base)
	pick := x.commit("add b", map[string]string{"b.txt": "b\n"})
	x.branch("claude/later", pick)
	later := x.commit("after landing", map[string]string{"f.txt": "f\n"})

	x.branch("claude/squash", base)
	x.commit("s1", map[string]string{"s1.txt": "1\n"})
	x.commit("s2", map[string]string{"s2.txt": "2\n", "changelog/fabric/2026-10-16-sq.md": "sq\n"})

	x.branch("claude/resolved", base)
	x.commit("tweak c", map[string]string{"c.txt": "2\n"})

	x.branch("claude/falsematch", base)
	x.commit("tweak c", map[string]string{"d.txt": "d\n"})

	x.branch("claude/empty", base)
	x.commit("empty note", nil)

	x.branch("claude/docs", base)
	x.commit("write it down", map[string]string{"docs/x.md": "x\n"})

	// Main is rebuilt from a new root, so no branch is an ancestor of it, and everything lands on it as a new commit.
	x.git("switch", "-q", "--orphan", "claude/main")
	x.git("cherry-pick", base)
	x.git("cherry-pick", pick)
	x.commit("squash sq", map[string]string{"s1.txt": "1\n", "s2.txt": "two\n", "changelog/fabric/2026-10-16-sq.md": "sq\n"})
	x.commit("tweak c", map[string]string{"c.txt": "3\n"})
	x.commit("empty note", nil)
	x.git("branch", "claude/anc", "HEAD")
	x.git("push", "-q", "hub", "refs/heads/*:refs/heads/*")

	st := &branchState{}
	got := x.pass(st)
	if got.Why != "" {
		t.Fatalf("why = %q", got.Why)
	}
	b := byName(got)
	want := []struct{ name, status, how, kind string }{
		{"claude/anc", BranchLanded, HowAncestry, ""},
		{"claude/pick", BranchLanded, HowPatchID, ""},
		{"claude/squash", BranchLanded, HowChangelog, ""},
		{"claude/resolved", BranchLanded, HowSubject, ""},
		{"claude/empty", BranchLanded, HowSubject, ""},
		{"claude/falsematch", BranchOrphaned, "", KindCode},
		{"claude/docs", BranchOrphaned, "", KindDocs},
		{"claude/later", BranchOrphaned, "", KindCode},
	}
	for _, w := range want {
		g, ok := b[w.name]
		if !ok || g.Status != w.status || g.How != w.how || g.Kind != w.kind {
			t.Errorf("%s = %+v, want %s %q %q", w.name, g, w.status, w.how, w.kind)
		}
	}
	if len(got.Branches) != len(want) || got.Counts[BranchLanded] != 5 || got.Counts[BranchOrphaned] != 3 {
		t.Fatalf("counts = %v over %d branches", got.Counts, len(got.Branches))
	}
	// Per commit facts, per branch status: the pick is landed, the branch built on it has one commit of three (base, pick, later) that is not.
	if g := b["claude/later"]; g.Commits != 3 || g.Uncarried != 1 || len(g.Files) != 1 || g.Files[0] != "f.txt" {
		t.Errorf("later = %+v", g)
	}
	if b["claude/falsematch"].Uncarried != 1 || b["claude/docs"].Files[0] != "docs/x.md" {
		t.Errorf("falsematch or docs = %+v %+v", b["claude/falsematch"], b["claude/docs"])
	}

	// Nothing moved: the second pass asks git for the refs and nothing else.
	before := x.st.h.runner().Spawned.Load()
	x.pass(st)
	if n := x.st.h.runner().Spawned.Load() - before; n > 3 {
		t.Errorf("a pass with nothing moved started %d git processes", n)
	}

	// The post-landing commit lands on main as a pick: main moved, only its new commit is hashed, and the branch flips.
	x.git("switch", "-q", "claude/main")
	x.git("cherry-pick", later)
	x.git("push", "-q", "hub", "claude/main")
	before = x.st.h.runner().Spawned.Load()
	again := x.pass(st)
	if g := byName(again)["claude/later"]; g.Status != BranchLanded || g.How != HowPatchID {
		t.Errorf("later after main moved = %+v", g)
	}
	if n := x.st.h.runner().Spawned.Load() - before; n > 40 {
		t.Errorf("an extended pass started %d git processes", n)
	}
}

// A REPOSITORY WITH NO claude/main IS SKIPPED WITH A REASON, and the snapshot answers from memory once built.
func TestBranchesSnapshotRunsNoGitOnARequest(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses")
	}
	x := newBranchFix(t)
	x.commit("base", map[string]string{"a.txt": "a\n"})
	x.git("push", "-q", "hub", "old:refs/heads/claude/x")
	if got := x.pass(&branchState{}); got.Why == "" || len(got.Branches) != 0 {
		t.Fatalf("no claude/main: %+v", got)
	}
	x.git("push", "-q", "hub", "old:refs/heads/claude/main")

	v, err := x.st.BranchesSnapshot("github/o/r", BranchFilter{})
	if err != nil || !v.Building || v.Computed != "" || len(v.Branches) != 0 {
		t.Fatalf("first ask: %+v %v", v, err)
	}
	testwait.Eventually(t, branchPassBound, "the first pass to finish", func() bool {
		v, _ = x.st.BranchesSnapshot("github/o/r", BranchFilter{})
		return v.Computed != ""
	})
	if v.Computed == "" || v.Main == nil || v.Counts[BranchLanded] != 1 || len(v.Branches) != 1 {
		t.Fatalf("built: %+v", v)
	}
	before := x.st.h.runner().Spawned.Load()
	if f, _ := x.st.BranchesSnapshot("github/o/r", BranchFilter{Statuses: []string{BranchOrphaned}}); len(f.Branches) != 0 || f.Counts[BranchLanded] != 1 {
		t.Fatalf("filtered: %+v", f)
	}
	if n := x.st.h.runner().Spawned.Load() - before; n != 0 {
		t.Fatalf("a request started %d git processes", n)
	}
	if _, err := x.st.BranchesSnapshot("not a repo", BranchFilter{}); err == nil {
		t.Fatal("a bad name was taken")
	}
}
