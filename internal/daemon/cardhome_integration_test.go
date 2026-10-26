//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/store"
)

func homeRoom(t *testing.T) (*Daemon, string) {
	t.Helper()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "cards")
	if err := d.st.SetSetting(cardroot.Setting, root); err != nil {
		t.Fatal(err)
	}
	return d, root
}

func homeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cullGit(t, repo, "init", "-q", "-b", "main")
	cullGit(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	cullGit(t, repo, "branch", "claude/main")
	return repo
}

func worktreeHome(t *testing.T, d *Daemon, repo string) (*store.Task, store.CardHome, homeDirs) {
	t.Helper()
	req := LaunchRequest{Repo: repo, Title: "Fix The Thing"}
	row, dd, err := d.launchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "worker", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return task, row, dd
}

func TestHomeSlugAndName(t *testing.T) {
	t.Parallel()
	if got := homeSlug("Fix: the  THING!!", "w"); got != "fix-the-thing" {
		t.Fatalf("slug %q", got)
	}
	if got := homeSlug("", "Worker_1"); got != "worker-1" {
		t.Fatalf("wire slug %q", got)
	}
	if got := homeSlug("!!!", ""); got != "card" {
		t.Fatalf("empty slug %q", got)
	}
	if got := homeSlug(strings.Repeat("ab ", 40), ""); len(got) > 40 || strings.HasSuffix(got, "-") {
		t.Fatalf("long slug %q", got)
	}
}

func TestACardRootNobodyMayRemoveInIsRefused(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	home, _ := os.UserHomeDir()
	for _, bad := range []string{filepath.VolumeName(home) + string(filepath.Separator), home} {
		if err := d.st.SetSetting(cardroot.Setting, bad); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.makeHome(store.CardHomeScratch, "t", "w"); err == nil {
			t.Fatalf("a home was made under %s", bad)
		}
	}
}

func TestAHomeIsFlatWithARow(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "Fix The Thing", "w")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dd.Dir) != root || !strings.HasPrefix(filepath.Base(dd.Dir), "fix-the-thing-") {
		t.Fatalf("home at %s", dd.Dir)
	}
	for _, p := range []string{dd.Work, dd.Tmp, dd.Meta} {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Fatalf("%s is missing", p)
		}
	}
	got, ok, _ := d.st.CardHomeByPath(row.Path)
	if !ok || got.Kind != store.CardHomeScratch || got.State != store.CardHomeLive || got.ID == "" {
		t.Fatalf("row %+v", got)
	}
}

func TestARepoLaunchMakesWorkAsAWorktree(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	_, row, dd := worktreeHome(t, d, repo)
	if _, err := os.Stat(filepath.Join(dd.Work, ".git")); err != nil {
		t.Fatalf("work/ is not a worktree: %v", err)
	}
	if got := cullGit(t, dd.Work, "symbolic-ref", "--short", "HEAD"); got != "claude/fix-the-thing" {
		t.Fatalf("branch %q", got)
	}
	if row.Kind != store.CardHomeWorktree || row.Branch != "claude/fix-the-thing" || row.Base != "claude/main" ||
		filepath.ToSlash(row.Clone) != filepath.ToSlash(repo) {
		t.Fatalf("row %+v", row)
	}
}

func TestCwdWithARepoIsRefused(t *testing.T) {
	t.Parallel()
	if err := homeLaunchRefusal(LaunchRequest{Repo: "github.com/o/r"}, "/x"); err == nil {
		t.Fatal("cwd with repo was accepted")
	}
	if err := homeLaunchRefusal(LaunchRequest{Repo: "o/r"}, "/x"); err != nil {
		t.Fatalf("a label repo with cwd: %v", err)
	}
	if err := homeLaunchRefusal(LaunchRequest{}, "/x"); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunnerSeesItsHomeForTemp(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	_, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	env := homeEnv(dd)
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		if env[k] != dd.Tmp {
			t.Fatalf("%s=%q, want %q", k, env[k], dd.Tmp)
		}
	}
	if env["ATRIUM_HOME"] != filepath.ToSlash(dd.Dir) {
		t.Fatalf("ATRIUM_HOME=%q", env["ATRIUM_HOME"])
	}
}

func TestReclaimRemovesACleanLandedHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)

	removed, kept, _ := d.reclaimHome(task, row, false)
	if !removed || kept != "" {
		t.Fatalf("removed=%v kept=%q", removed, kept)
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeRemoved || got.RemovedAt == "" {
		t.Fatalf("row %+v", got)
	}
	if out := cullGit(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, "fix-the-thing") {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
}

func TestReclaimKeepsADirtyOrUnlandedHomeAndSaysSoOnce(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	launcher := peerCard(t, d, "orchestrator")
	if err := d.st.SetTags(task.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(task.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)

	if err := os.WriteFile(filepath.Join(dd.Work, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, _, _ := d.st.CardHomeByPath(row.Path)
		if removed, kept, _ := d.reclaimHome(task, got, false); removed || !strings.Contains(kept, "uncommitted") {
			t.Fatalf("dirty home: removed=%v kept=%q", removed, kept)
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices, want one", n)
	}

	cullGit(t, dd.Work, "add", "wip.txt")
	cullGit(t, dd.Work, "commit", "-q", "-m", "wip")
	got, _, _ := d.st.CardHomeByPath(row.Path)
	if removed, kept, _ := d.reclaimHome(task, got, false); removed || !strings.Contains(kept, "not landed") {
		t.Fatalf("unlanded home: removed=%v kept=%q", removed, kept)
	}
	if _, err := os.Stat(filepath.Join(dd.Work, "wip.txt")); err != nil {
		t.Fatal("the work was touched")
	}

	cullGit(t, repo, "branch", "-f", "claude/main", "claude/fix-the-thing")
	got, _, _ = d.st.CardHomeByPath(row.Path)
	if removed, kept, _ := d.reclaimHome(task, got, false); !removed {
		t.Fatalf("a landed home was kept: %q", kept)
	}
}

func TestACardWithNoHomeIsNeverTouched(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	dir := t.TempDir()
	task := cullCard(t, d, dir)
	if _, ok := d.homeOf(task.ID); ok {
		t.Fatal("a card with no row has a home")
	}
	if err := d.sweepHomes(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the folder went")
	}
}

func TestAHomeOutsideTheRootIsNotRemoved(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	row, err := d.st.CardHomeNew(store.CardHome{Path: filepath.ToSlash(outside), Kind: store.CardHomeScratch})
	if err != nil {
		t.Fatal(err)
	}
	if removed, _, _ := d.reclaimHome(nil, row, true); removed {
		t.Fatal("a folder outside the card root was removed")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("its contents went")
	}
}

func TestRemovalDoesNotFollowALink(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "precious.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dd.Work, "link")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	if removed, kept, _ := d.reclaimHome(nil, row, false); !removed {
		t.Fatalf("kept: %q", kept)
	}
	if _, err := os.Stat(filepath.Join(target, "precious.txt")); err != nil {
		t.Fatal("removal followed the link")
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
}

func aHomeThrowaway(t *testing.T, d *Daemon) (*store.Task, store.CardHome, homeDirs) {
	t.Helper()
	req := LaunchRequest{Title: "tryout", Throwaway: true}
	row, dd, err := d.launchHome(store.CardHomeThrowaway, &req)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "tryout", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return task, row, dd
}

func TestAThrowawayHomeGoesWhenItsRunnerExits(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := aHomeThrowaway(t, d)
	d.endThrowaway(task.ID)
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived its home")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeRemoved {
		t.Fatalf("row %+v", got)
	}
}

func TestAPromotedThrowawayHomeIsKeptWhereItIs(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := aHomeThrowaway(t, d)
	if err := d.st.SetPromoteTo(task.ID, filepath.ToSlash(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	d.endThrowaway(task.ID)
	if _, err := os.Stat(dd.Work); err != nil {
		t.Fatal("a promoted home was removed")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeKept {
		t.Fatalf("row %+v", got)
	}
}
