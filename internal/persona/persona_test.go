package persona

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Every test here builds its own pack in a temp dir. None of them reads or
// writes clint's real dotagents.

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// run is a test-side git, which may write: it is building the fixture repo.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goSecYAML = `id: go-sec
name: "go-sec"
runners: claude
reviews:
  paths: ["**/*.go"]
  surfaces: [security, http]
  skip_when: "no Go file changed"
claude:
  frontmatter: |
    name: "go-sec"
    description: "Go security reviewer."
    model: sonnet
`

const goSecRender = "---\nname: \"go-sec\"\ndescription: \"Go security reviewer.\"\n---\n\nYou review Go.\n"

// newPack makes dotagents/personas with two personas, committed, and returns
// the personas directory.
func newPack(t *testing.T) string {
	t.Helper()
	needGit(t)
	root := filepath.Join(t.TempDir(), "dotagents")
	pack := filepath.Join(root, "personas")
	write(t, filepath.Join(pack, "README.md"), "pack\n")
	write(t, filepath.Join(pack, "go-sec", "persona.yaml"), goSecYAML)
	write(t, filepath.Join(pack, "go-sec", "persona.md"), "You review Go.\n")
	write(t, filepath.Join(pack, "go-sec", "render", "claude", "go-sec.md"), goSecRender)
	write(t, filepath.Join(pack, "go-sec", "memory", "MEMORY.md"),
		"- [Old lesson](old.md) an old one\n- [Other](other.md) another\n")
	write(t, filepath.Join(pack, "go-sec", "memory", "old.md"),
		"---\nname: old\ndescription: timeouts on every client\nmetadata:\n  type: project\n---\n\nbody\n")
	write(t, filepath.Join(pack, "go-sec", "memory", "other.md"),
		"---\nname: other\ndescription: another lesson\nrepo: general\n---\n\nbody\n\n**Why:** a review taught it\n")
	// A persona that declares a runner it has no render for.
	write(t, filepath.Join(pack, "styler", "persona.yaml"),
		"id: styler\nname: Styler\ndescription: style\nruns: x\nrunners: [claude, codex]\n")
	// Not a persona: no yaml.
	write(t, filepath.Join(pack, "notes", "x.md"), "x\n")
	run(t, root, "init")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "pack")
	return pack
}

func TestCatalogParsesThePack(t *testing.T) {
	pack := newPack(t)
	runs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(runs, "go-sec", "20260101-000000-abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Catalog(pack, runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 personas, got %d: %+v", len(got), got)
	}
	g := got[0]
	if g.ID != "go-sec" || g.Name != "go-sec" || g.Description != "Go security reviewer." {
		t.Fatalf("go-sec parsed wrong: %+v", g)
	}
	if strings.Join(g.Runners, ",") != "claude" || strings.Join(g.Renders, ",") != "claude" {
		t.Fatalf("runners %v renders %v", g.Runners, g.Renders)
	}
	if g.Reviews == nil || g.Reviews.Paths[0] != "**/*.go" || len(g.Reviews.Surfaces) != 2 ||
		g.Reviews.SkipWhen != "no Go file changed" {
		t.Fatalf("reviews parsed wrong: %+v", g.Reviews)
	}
	if g.LastRun == "" {
		t.Fatal("a persona with a run directory has no last run")
	}
	s := got[1]
	if s.ID != "styler" || strings.Join(s.Runners, ",") != "claude,codex" || len(s.Renders) != 0 {
		t.Fatalf("styler: %+v", s)
	}
	if s.RendersFor("claude") {
		t.Fatal("styler has no render and must not be launchable")
	}
}

func TestCatalogSaysWhenTheIDDisagreesWithTheFolder(t *testing.T) {
	pack := t.TempDir()
	write(t, filepath.Join(pack, "a", "persona.yaml"), "id: b\nname: A\n")
	got, err := Catalog(pack, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Problem, "folder is a") {
		t.Fatalf("want a problem naming the mismatch, got %+v", got)
	}
}

func TestCatalogRefusesAnEmptyPackPath(t *testing.T) {
	if _, err := Catalog("", ""); err == nil || !strings.Contains(err.Error(), PackPathSetting) {
		t.Fatalf("want the setting named, got %v", err)
	}
}

func TestRepoKeyFromRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/openziti/ziti.git":   "github/openziti/ziti",
		"git@github.com:dovholuknf/atrium.git":   "github/dovholuknf/atrium",
		"ssh://git@bitbucket.org/netfoundry/x":   "bitbucket/netfoundry/x",
		"https://user@github.com/openziti/ziti/": "github/openziti/ziti",
		"not a url":                              "",
	} {
		if got := RepoKeyFromRemote(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// newWorktree makes a repo on main with one commit, then a branch with one
// more, and an origin url.
func newWorktree(t *testing.T) (dir, base string) {
	t.Helper()
	needGit(t)
	dir = t.TempDir()
	run(t, dir, "init")
	write(t, filepath.Join(dir, "a.go"), "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", "one")
	base = run(t, dir, "rev-parse", "HEAD")
	run(t, dir, "checkout", "-b", "feature")
	write(t, filepath.Join(dir, "b.go"), "package a\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-m", "two")
	run(t, dir, "remote", "add", "origin", "https://github.com/acme/widget.git")
	return dir, base
}

func TestRunDirectoryHoldsTargetAndRender(t *testing.T) {
	pack := newPack(t)
	wt, base := newWorktree(t)
	write(t, filepath.Join(wt, "dirty.go"), "package a\n")
	// A rejection for this repo, one for another, and one general.
	write(t, filepath.Join(pack, "go-sec", "memory", "rejected.md"), "# rejected\n\n"+
		"- 2026-09-01 repo: github/acme/widget | leak in close | freed by callback\n"+
		"- 2026-09-01 repo: github/acme/other | nope | nope\n"+
		"- 2026-09-01 repo: general | ctx misuse | it was fine\n")
	write(t, filepath.Join(pack, "go-sec", "memory", "elsewhere.md"),
		"---\nname: elsewhere\nrepo: github/acme/other\n---\nbody\n")
	write(t, filepath.Join(pack, "go-sec", "knowledge", "github", "acme", "widget.md"), "- fact\n")

	tgt, err := ResolveTarget(wt, RepoHint{})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Branch != "feature" || tgt.DefaultBranch != "main" || tgt.MergeBase != base ||
		tgt.Range != base+"..feature" || !tgt.Dirty || tgt.RepoKey != "github/acme/widget" {
		t.Fatalf("target: %+v", tgt)
	}

	p, err := Find(pack, "go-sec")
	if err != nil {
		t.Fatal(err)
	}
	runs := filepath.Join(t.TempDir(), RunsDirName)
	r, err := NewRun(runs, pack, p, "claude", tgt, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.ID, "20260923-120000-") {
		t.Fatalf("run id %q", r.ID)
	}
	if want := filepath.ToSlash(filepath.Join(runs, "go-sec", r.ID)); !strings.EqualFold(r.Dir, want) {
		// Resolved paths can differ in case or 8.3 form on Windows; the
		// suffix is what matters.
		if !strings.HasSuffix(r.Dir, "/go-sec/"+r.ID) {
			t.Fatalf("run dir %q, want %q", r.Dir, want)
		}
	}
	if r.Agent != "go-sec" || strings.Join(r.Args, " ") != "--agent go-sec" {
		t.Fatalf("agent %q args %v", r.Agent, r.Args)
	}
	agent, err := os.ReadFile(filepath.Join(filepath.FromSlash(r.Dir), ".claude", "agents", "go-sec.md"))
	if err != nil || string(agent) != goSecRender {
		t.Fatalf("the rendered file was not copied: %v %q", err, agent)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(r.Dir), "TARGET.md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(raw)
	for _, want := range []string{
		filepath.ToSlash(wt), "github/acme/widget", base + "..feature", "default branch: `main`",
		"uncommitted changes", "leak in close", "ctx misuse", "old.md", "other.md",
		"knowledge/github/acme/widget.md", `"severity": "blocking|high|medium|low|nit"`, "prod_survival",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("TARGET.md is missing %q", want)
		}
	}
	for _, not := range []string{"github/acme/other |", "elsewhere.md"} {
		if strings.Contains(md, not) {
			t.Errorf("TARGET.md carries another repo's %q", not)
		}
	}
	if !strings.Contains(r.Prompt, "TARGET.md") || !strings.Contains(r.Prompt, "atrium_report") {
		t.Fatalf("prompt: %q", r.Prompt)
	}
}

func TestRunRefusesARunnerWithNoRender(t *testing.T) {
	pack := newPack(t)
	p, err := Find(pack, "styler")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRun(t.TempDir(), pack, p, "claude", Target{}, time.Now()); err == nil {
		t.Fatal("a persona with no render was set up to run")
	}
	g, _ := Find(pack, "go-sec")
	if _, err := NewRun(t.TempDir(), pack, g, "codex", Target{}, time.Now()); err == nil {
		t.Fatal("an unmeasured runner was set up to run")
	}
}

func TestFindRefusesAnIDThatClimbs(t *testing.T) {
	pack := newPack(t)
	for _, id := range []string{"..", "../x", "a/b", `a\b`, "", "UP"} {
		if _, err := Find(pack, id); err == nil {
			t.Errorf("%q was accepted as a persona id", id)
		}
	}
}

func lessonFiles(r *Review) map[string]string {
	out := map[string]string{}
	for _, l := range r.Lessons {
		out[l.File] = l.Status
	}
	return out
}

func TestLessonsNeverReviewedCoversTheWholeHistory(t *testing.T) {
	pack := newPack(t)
	s := NewService()
	r, err := s.Read(pack, "go-sec")
	if err != nil {
		t.Fatal(err)
	}
	if r.Baseline != "" {
		t.Fatalf("baseline %q with no review commit", r.Baseline)
	}
	got := lessonFiles(r)
	if got["memory/old.md"] != "unreviewed" || got["memory/other.md"] != "unreviewed" || len(got) != 2 {
		t.Fatalf("lessons %v", got)
	}
	if _, ok := got["memory/MEMORY.md"]; ok {
		t.Fatal("the index is listed as a lesson")
	}
}

func TestLessonsBaselineIsTheTrailerCommit(t *testing.T) {
	pack := newPack(t)
	root := filepath.Dir(pack)
	// A review commit for ANOTHER persona is not this one's baseline.
	write(t, filepath.Join(pack, "README.md"), "pack 2\n")
	run(t, root, "commit", "-am", "personas: lessons review\n\nLessons-reviewed: styler")
	// This one's review. It touches nothing in the folder, which still counts.
	write(t, filepath.Join(pack, "README.md"), "pack 3\n")
	run(t, root, "commit", "-am", "personas: lessons review, 0 promoted\n\nLessons-reviewed: go-sec")
	reviewed := run(t, root, "rev-parse", "HEAD")
	// After it: one committed change, one uncommitted, one untracked, and a
	// rejection.
	write(t, filepath.Join(pack, "go-sec", "memory", "old.md"),
		"---\nname: old\ndescription: timeouts on every client, changed\n---\n\nbody\n")
	run(t, root, "commit", "-am", "review wrote memory")
	write(t, filepath.Join(pack, "go-sec", "memory", "other.md"),
		"---\nname: other\ndescription: another lesson\nrepo: general\n---\n\nedited\n\n**Why:** a review taught it\n")
	write(t, filepath.Join(pack, "go-sec", "memory", "fresh.md"),
		"---\nname: fresh\ndescription: brand new\nrepo: github/acme/widget\n---\n\n**Why:** seen twice\n")
	write(t, filepath.Join(pack, "go-sec", "memory", "rejected.md"),
		"# rejected\n\n- 2026-09-22 repo: general | x | y\n")

	s := NewService()
	r, err := s.Read(pack, "go-sec")
	if err != nil {
		t.Fatal(err)
	}
	if r.Baseline != reviewed {
		t.Fatalf("baseline %q, want the go-sec review %q", r.Baseline, reviewed)
	}
	got := lessonFiles(r)
	if got["memory/old.md"] != "modified" || got["memory/other.md"] != "modified" ||
		got["memory/fresh.md"] != "untracked" || len(got) != 3 {
		t.Fatalf("lessons %v", got)
	}
	for _, l := range r.Lessons {
		if l.File == "memory/fresh.md" && (l.Repo != "github/acme/widget" || l.Why != "seen twice") {
			t.Fatalf("fresh parsed wrong: %+v", l)
		}
		if l.File == "memory/old.md" && !strings.Contains(l.Diff, "+description: timeouts on every client, changed") {
			t.Fatalf("old diff: %q", l.Diff)
		}
	}
	if len(r.Rejections) != 1 || !strings.Contains(r.Rejections[0], "repo: general | x") {
		t.Fatalf("rejections %v", r.Rejections)
	}
	if !strings.HasSuffix(r.Commit, "\n\nLessons-reviewed: go-sec") ||
		!strings.Contains(r.Command, `git add -- "personas/go-sec"`) {
		t.Fatalf("commit %q command %q", r.Commit, r.Command)
	}
}

func TestLessonsRejectionsAreOnlyTheNewLines(t *testing.T) {
	pack := newPack(t)
	root := filepath.Dir(pack)
	write(t, filepath.Join(pack, "go-sec", "memory", "rejected.md"), "# rejected\n\n- old one\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "review\n\nLessons-reviewed: go-sec")
	write(t, filepath.Join(pack, "go-sec", "memory", "rejected.md"), "# rejected\n\n- old one\n- new one\n")
	r, err := NewService().Read(pack, "go-sec")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rejections) != 1 || r.Rejections[0] != "- new one" {
		t.Fatalf("rejections %v", r.Rejections)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPromoteMovesALessonIntoKnowledge(t *testing.T) {
	pack := newPack(t)
	s := NewService()
	// other.md says repo: general and has a Why.
	r, err := s.Apply(pack, "go-sec", Decision{File: "memory/other.md", Action: "promote"})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(pack, "go-sec")
	k := read(t, filepath.Join(dir, "knowledge", "_general.md"))
	if !strings.Contains(k, "- another lesson Why: a review taught it") {
		t.Fatalf("knowledge: %q", k)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "other.md")); !os.IsNotExist(err) {
		t.Fatal("the promoted memory file is still there")
	}
	if idx := read(t, filepath.Join(dir, "memory", "MEMORY.md")); strings.Contains(idx, "(other.md)") ||
		!strings.Contains(idx, "(old.md)") {
		t.Fatalf("index: %q", idx)
	}
	if r.Tally.Promoted != 1 || !strings.Contains(r.Commit, "1 promoted") {
		t.Fatalf("tally %+v commit %q", r.Tally, r.Commit)
	}

	// old.md has no repo line, so a promote has to be told which repo.
	if _, err := s.Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "promote"}); err == nil {
		t.Fatal("a lesson with no repo was promoted without being told where")
	}
	if _, err := s.Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "promote",
		Repo: "github/acme/widget", Why: "a panel confirmed it"}); err != nil {
		t.Fatal(err)
	}
	k = read(t, filepath.Join(dir, "knowledge", "github", "acme", "widget.md"))
	if !strings.HasPrefix(k, "# github/acme/widget") ||
		!strings.Contains(k, "- timeouts on every client Why: a panel confirmed it") {
		t.Fatalf("repo knowledge: %q", k)
	}
}

func TestKeepAddsWhatALessonIsMissing(t *testing.T) {
	pack := newPack(t)
	s := NewService()
	if _, err := s.Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "keep"}); err == nil ||
		!strings.Contains(err.Error(), "a repo and a Why line") {
		t.Fatalf("keep without repo or why: %v", err)
	}
	r, err := s.Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "keep",
		Repo: "general", Why: "the ziti review"})
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(pack, "go-sec", "memory", "old.md"))
	if !strings.Contains(got, "\nrepo: general\n---\n") || !strings.HasSuffix(got, "**Why:** the ziti review\n") {
		t.Fatalf("kept file: %q", got)
	}
	if r.Tally.Kept != 1 {
		t.Fatalf("tally %+v", r.Tally)
	}
	// Already complete: keep changes nothing.
	before := read(t, filepath.Join(pack, "go-sec", "memory", "other.md"))
	if _, err := s.Apply(pack, "go-sec", Decision{File: "memory/other.md", Action: "keep"}); err != nil {
		t.Fatal(err)
	}
	if after := read(t, filepath.Join(pack, "go-sec", "memory", "other.md")); after != before {
		t.Fatalf("a complete lesson was rewritten: %q", after)
	}
}

func TestKeepMergesAnInboxLesson(t *testing.T) {
	pack := newPack(t)
	dir := filepath.Join(pack, "go-sec")
	write(t, filepath.Join(dir, "memory", "inbox", "codex_one.md"),
		"---\nname: codex one\ndescription: from codex\nrepo: general\n---\n\n**Why:** codex saw it\n")
	if _, err := NewService().Apply(pack, "go-sec", Decision{File: "memory/inbox/codex_one.md", Action: "keep"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "inbox", "codex_one.md")); !os.IsNotExist(err) {
		t.Fatal("the inbox file was not moved")
	}
	if got := read(t, filepath.Join(dir, "memory", "codex_one.md")); !strings.Contains(got, "from codex") {
		t.Fatalf("merged file: %q", got)
	}
	if idx := read(t, filepath.Join(dir, "memory", "MEMORY.md")); !strings.Contains(idx, "- [codex one](codex_one.md) from codex") {
		t.Fatalf("index: %q", idx)
	}
}

func TestDeleteRemovesTheFileAndItsIndexLine(t *testing.T) {
	pack := newPack(t)
	dir := filepath.Join(pack, "go-sec")
	r, err := NewService().Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "old.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if idx := read(t, filepath.Join(dir, "memory", "MEMORY.md")); strings.Contains(idx, "(old.md)") ||
		!strings.Contains(idx, "(other.md)") {
		t.Fatalf("index: %q", idx)
	}
	// Deleted since the (absent) baseline: an unreviewed file that is gone is
	// no longer listed, since the whole history is what is on disk.
	if got := lessonFiles(r); got["memory/old.md"] != "" {
		t.Fatalf("lessons after delete: %v", got)
	}
	if r.Tally.Deleted != 1 {
		t.Fatalf("tally %+v", r.Tally)
	}
}

func TestApplyRefusesAFileThatIsNotUnderReview(t *testing.T) {
	pack := newPack(t)
	s := NewService()
	for _, f := range []string{"persona.md", "memory/MEMORY.md", "../styler/persona.yaml",
		"memory/../../styler/persona.yaml", "memory/nope.md"} {
		if _, err := s.Apply(pack, "go-sec", Decision{File: f, Action: "delete"}); err == nil {
			t.Errorf("%q was deleted", f)
		}
	}
	if _, err := os.Stat(filepath.Join(pack, "styler", "persona.yaml")); err != nil {
		t.Fatal("a file outside the persona was touched")
	}
	if _, err := s.Apply(pack, "go-sec", Decision{File: "memory/old.md", Action: "promote",
		Repo: "../../../etc"}); err == nil {
		t.Fatal("a repo key that climbs was accepted")
	}
}

// A memory folder that is a link out of the pack is the case safepath exists
// for: every string check passes, and the write would land elsewhere.
func TestSafepathRefusesALinkOutOfThePack(t *testing.T) {
	pack := newPack(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "victim.md"),
		"---\nname: victim\ndescription: outside\nrepo: general\n---\n\n**Why:** x\n")
	link := filepath.Join(pack, "go-sec", "memory", "inbox")
	if err := symlinkDir(outside, link); err != nil {
		t.Skipf("cannot make a directory link here: %v", err)
	}
	e := editor{pack: pack, dir: filepath.Join(pack, "go-sec")}
	if _, err := e.path("memory/inbox/victim.md"); !errors.Is(err, safepath.ErrOutside) {
		t.Fatalf("a path through a link out of the pack resolved: %v", err)
	}
	err := e.remove(Lesson{File: "memory/inbox/victim.md", Inbox: true})
	if !errors.Is(err, safepath.ErrOutside) {
		t.Fatalf("remove through the link: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "victim.md")); err != nil {
		t.Fatal("the file outside the pack was deleted")
	}
}

func symlinkDir(target, link string) error {
	return os.Symlink(target, link)
}

// A JUNCTION IS NOT A SYMLINK TO GO. Since Go 1.23 filepath.EvalSymlinks does
// not follow a Windows mount point, so safepath cannot see through one, and a
// junction is what any user can make without privilege. The editor refuses
// any reparse point on the way to a file for that reason.
func TestEditorRefusesAJunctionOutOfThePack(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows thing")
	}
	pack := newPack(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "victim.md"), "x\n")
	link := filepath.Join(pack, "go-sec", "memory", "inbox")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction: %v %s", err, out)
	}
	e := editor{pack: pack, dir: filepath.Join(pack, "go-sec")}
	if _, err := e.path("memory/inbox/victim.md"); err == nil {
		t.Fatal("a path through a junction out of the pack resolved")
	}
	if err := e.remove(Lesson{File: "memory/inbox/victim.md", Inbox: true}); err == nil {
		t.Fatal("remove went through the junction")
	}
	if _, err := os.Stat(filepath.Join(outside, "victim.md")); err != nil {
		t.Fatal("the file outside the pack was deleted")
	}
}

func TestParseLessonReadsRepoAndWhy(t *testing.T) {
	m := parseLesson("---\r\nname: x\r\ndescription: \"d\"\r\nmetadata:\r\n  repo: github/a/b\r\n---\r\n\r\n" +
		"Why this matters is not a Why line.\r\n**Why:** the real one\r\n")
	if m.Name != "x" || m.Description != "d" || m.Repo != "github/a/b" || m.Why != "the real one" {
		t.Fatalf("%+v", m)
	}
}
