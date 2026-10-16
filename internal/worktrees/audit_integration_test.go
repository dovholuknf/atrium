//go:build integration && git

package worktrees

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// A repository in a temp directory, with `claude/main` checked out in the main checkout, and real worktrees off it.
type repoFix struct {
	t    *testing.T
	root string
	main string
}

func (f *repoFix) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitsync.CleanEnv(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *repoFix) write(dir, name, body string) {
	f.t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// commit writes the files and commits them with the subject.
func (f *repoFix) commit(dir, subject string, files map[string]string) {
	f.t.Helper()
	for n, b := range files {
		f.write(dir, n, b)
	}
	f.git(dir, "add", "-A")
	f.git(dir, "commit", "-q", "-m", subject)
}

// worktree adds a worktree on a new branch cut from claude/main.
func (f *repoFix) worktree(name string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "wt", name)
	f.git(f.main, "worktree", "add", "-q", "-b", "claude/"+name, dir, "claude/main")
	return dir
}

func newRepo(t *testing.T) *repoFix {
	t.Helper()
	f := &repoFix{t: t, root: t.TempDir()}
	f.main = filepath.Join(f.root, "repo")
	if err := os.MkdirAll(f.main, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(f.main, "init", "-q", "-b", "claude/main")
	f.commit(f.main, "base", map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	return f
}

func (f *repoFix) audit(in Input) *Result {
	f.t.Helper()
	in.Repo = f.main
	res, err := Audit(context.Background(), in)
	if err != nil {
		f.t.Fatalf("audit: %v", err)
	}
	return res
}

func (f *repoFix) line(res *Result, dir string) Line {
	f.t.Helper()
	for _, l := range res.Lines {
		if SamePath(l.Path, dir) {
			return l
		}
	}
	f.t.Fatalf("no line for %s in\n%s", dir, res.Text)
	return Line{}
}

func TestLandedByEachRule(t *testing.T) {
	f := newRepo(t)

	// fast-forward: claude/main moves onto the branch's own commit
	ff := f.worktree("ff")
	f.commit(ff, "ff work", map[string]string{"ff.txt": "1\n"})
	f.git(f.main, "merge", "-q", "--ff-only", "claude/ff")

	// cherry-pick: the same diff, another sha
	cp := f.worktree("cp")
	f.commit(cp, "cp work", map[string]string{"cp.txt": "1\n"})
	f.commit(f.main, "main moves", map[string]string{"m1.txt": "1\n"})
	f.git(f.main, "cherry-pick", "claude/cp")

	// same subject and the same files, other content: carried by subject
	sj := f.worktree("sj")
	f.commit(sj, "sj work", map[string]string{"sj.txt": "mine\n"})
	f.commit(f.main, "sj work", map[string]string{"sj.txt": "theirs\n"})

	// same subject, other files: not carried
	ot := f.worktree("ot")
	f.commit(ot, "ot work", map[string]string{"ot.txt": "1\n"})
	f.commit(f.main, "ot work", map[string]string{"elsewhere.txt": "1\n"})

	// a merge commit on the branch is ignored, and what it carried is the cherry-picked commit
	mg := f.worktree("mg")
	f.commit(mg, "mg work", map[string]string{"mg.txt": "1\n"})
	f.commit(f.main, "main moves again", map[string]string{"m2.txt": "1\n"})
	f.git(f.main, "cherry-pick", "claude/mg")
	f.commit(f.main, "after mg", map[string]string{"after.txt": "1\n"})
	f.git(mg, "merge", "-q", "--no-ff", "-m", "merge main", "claude/main")

	// only its changelog fragment is on main: a squash may have dropped the rest
	cl := f.worktree("cl")
	f.commit(cl, "cl work", map[string]string{"cl.txt": "1\n", "changelog/dept/2026-10-16-cl.md": "x\n"})
	f.commit(f.main, "squashed cl", map[string]string{"changelog/dept/2026-10-16-cl.md": "x\n"})

	res := f.audit(Input{})
	for _, c := range []struct {
		dir, landed string
		unlanded    int
	}{
		{ff, "ancestry", 0}, {cp, "patch", 0}, {sj, "subject", 0}, {ot, "", 1}, {mg, "patch", 0}, {cl, "", 1},
	} {
		l := f.line(res, c.dir)
		if l.Landed != c.landed || l.Unlanded != c.unlanded {
			t.Errorf("%s: landed %q unlanded %d, want %q %d\n%s", filepath.Base(c.dir), l.Landed, l.Unlanded,
				c.landed, c.unlanded, res.Text)
		}
	}
	if l := f.line(res, ot); l.Subject != "ot work" || len(l.Newest) != 40 {
		t.Errorf("the unlanded commit is %q %q", l.Newest, l.Subject)
	}
	if !strings.Contains(res.Text, `unlanded 1 `) || !strings.Contains(res.Text, `"ot work"`) {
		t.Errorf("the line does not carry the commit:\n%s", res.Text)
	}
}

func TestUnlandedCountsAndNamesTheNewest(t *testing.T) {
	f := newRepo(t)
	w := f.worktree("many")
	f.commit(w, "first", map[string]string{"1.txt": "1\n"})
	f.commit(w, "second", map[string]string{"2.txt": "1\n"})
	f.commit(w, "third", map[string]string{"3.txt": "1\n"})
	l := f.line(f.audit(Input{}), w)
	if l.Unlanded != 3 || l.Subject != "third" {
		t.Errorf("unlanded %d newest %q, want 3 third", l.Unlanded, l.Subject)
	}
	if got := l.Format(len(l.Path)); !strings.Contains(got, `"third" +2`) {
		t.Errorf("the line is %q", got)
	}
}

func TestDirtyCounting(t *testing.T) {
	f := newRepo(t)

	flags := f.worktree("flags")
	f.write(flags, "BRIEF.md", "brief\n")
	f.write(flags, "REPORT.md", "report\n")

	sub := f.worktree("sub")
	f.write(sub, "sub/BRIEF.md", "brief\n")

	three := f.worktree("three")
	f.write(three, "staged.txt", "s\n")
	f.git(three, "add", "staged.txt")
	f.write(three, "a.txt", "changed\n")
	f.write(three, "untracked.txt", "u\n")

	res := f.audit(Input{})
	if l := f.line(res, flags); l.Dirty != 0 || strings.Join(l.Flags, ",") != "BRIEF,REPORT" {
		t.Errorf("flags: dirty %d flags %v", l.Dirty, l.Flags)
	}
	if got := f.line(res, flags).Format(0); !strings.Contains(got, "clean +BRIEF +REPORT") {
		t.Errorf("flags line %q", got)
	}
	if l := f.line(res, sub); l.Dirty != 1 || len(l.Flags) != 0 {
		t.Errorf("a BRIEF.md in a subdirectory: dirty %d flags %v", l.Dirty, l.Flags)
	}
	if l := f.line(res, three); l.Dirty != 3 {
		t.Errorf("staged, modified and untracked: dirty %d, want 3", l.Dirty)
	}
}

func TestHeaderNamesTheRef(t *testing.T) {
	f := newRepo(t)
	res := f.audit(Input{})
	if res.From != "local" || !strings.HasPrefix(res.Text, "claude/main "+res.RefSHA[:8]+" from local, 1 worktree\n") {
		t.Errorf("local header: %q", strings.SplitN(res.Text, "\n", 2)[0])
	}

	// the hub's own tip as the last sync fetched it wins
	f.commit(f.main, "ahead", map[string]string{"ahead.txt": "1\n"})
	hubTip := f.git(f.main, "rev-parse", "HEAD")
	f.git(f.main, "update-ref", "refs/remotes/hub/claude/main", hubTip)
	f.commit(f.main, "local only", map[string]string{"local.txt": "1\n"})
	res = f.audit(Input{})
	if res.From != "hub" || res.RefSHA != hubTip || !strings.Contains(res.Text, " from hub, ") {
		t.Errorf("hub header: %q from %s", strings.SplitN(res.Text, "\n", 2)[0], res.From)
	}

	// neither ref: nothing is measured
	other := t.TempDir()
	f.git(other, "init", "-q", "-b", "other")
	f.commit(other, "x", map[string]string{"x.txt": "1\n"})
	if _, err := Audit(context.Background(), Input{Repo: other}); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("a repo with neither ref: %v", err)
	}
}

func TestPrefixAndPathsTogetherAreRefused(t *testing.T) {
	f := newRepo(t)
	_, err := Audit(context.Background(), Input{Repo: f.main, Prefix: "x", Paths: []string{"y"}})
	if err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrefixAndPathsNarrow(t *testing.T) {
	f := newRepo(t)
	a, b := f.worktree("aa"), f.worktree("bb")
	res := f.audit(Input{Prefix: filepath.Join(f.root, "wt", "a")})
	if len(res.Lines) != 1 || !SamePath(res.Lines[0].Path, a) {
		t.Errorf("prefix: %s", res.Text)
	}
	res = f.audit(Input{Paths: []string{strings.ToUpper(filepath.ToSlash(b)), filepath.Join(f.root, "nope")}})
	if len(res.Lines) != 2 {
		t.Errorf("paths: %s", res.Text)
	}
	if !strings.Contains(res.Text, "keep: not a worktree") {
		t.Errorf("a path git does not list: %s", res.Text)
	}
}

func TestMissingDirectoryIsPrunable(t *testing.T) {
	f := newRepo(t)
	w := f.worktree("gone")
	if err := os.RemoveAll(w); err != nil {
		t.Fatal(err)
	}
	l := f.line(f.audit(Input{}), w)
	if l.Branch != "(missing)" || !l.Prune || !l.Removable {
		t.Errorf("missing: %+v", l)
	}
	if got := l.Format(0); !strings.HasSuffix(got, "(missing)  -  -  removable: prune") {
		t.Errorf("line %q", got)
	}
}

func TestVerdictFromTheGitSide(t *testing.T) {
	f := newRepo(t)
	clean := f.worktree("clean")
	dirty := f.worktree("dirty")
	f.write(dirty, "x.txt", "x\n")
	locked := f.worktree("locked")
	f.git(f.main, "worktree", "lock", locked)
	det := f.worktree("det")
	f.git(det, "checkout", "-q", "--detach")
	onMain := filepath.Join(f.root, "wt", "onmain")
	f.git(f.main, "branch", "master")
	f.git(f.main, "worktree", "add", "-q", onMain, "master")

	res := f.audit(Input{})
	want := map[string]string{
		f.main: "keep: main checkout, protected branch", clean: "removable", dirty: "keep: dirty",
		locked: "keep: locked", det: "removable", onMain: "keep: protected branch",
	}
	for dir, v := range want {
		if got := f.line(res, dir).Format(0); !strings.HasSuffix(got, "  "+v) {
			t.Errorf("%s: %q, want the verdict %q", filepath.Base(dir), got, v)
		}
	}
	if b := f.line(res, det).Branch; !strings.HasPrefix(b, "detached@") || len(b) != len("detached@")+7 {
		t.Errorf("detached branch %q", b)
	}
}

func TestJudgeAddsNotAtriumsFirstAndTheRest(t *testing.T) {
	f := newRepo(t)
	w := f.worktree("judged")
	res := f.audit(Input{Judge: func(string) Judgment {
		return Judgment{Atriums: false, Later: []string{"in use: card"}}
	}})
	if got := f.line(res, w).Format(0); !strings.HasSuffix(got, "keep: not atrium's, in use: card") {
		t.Errorf("line %q", got)
	}
}
