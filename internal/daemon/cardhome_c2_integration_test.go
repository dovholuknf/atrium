//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func fillHome(t *testing.T, dir string, files int, size int) {
	t.Helper()
	for i := 0; i < files; i++ {
		sub := filepath.Join(dir, "d"+string(rune('a'+i%3)))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "f"+string(rune('a'+i))), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheSizeWalkStopsAtItsBudgetAndResumes(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	fillHome(t, dd.Dir, 6, 100)

	if d.walkHome(row.Path, time.Now().Add(-time.Second)) {
		t.Fatal("a walk past its deadline said it was done")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.MeasuredAt != "" {
		t.Fatalf("an unfinished walk wrote a size: %+v", got)
	}
	if !d.walkHome(row.Path, time.Now().Add(time.Minute)) {
		t.Fatal("the walk did not finish with time")
	}
	got, _, _ := d.st.CardHomeByPath(row.Path)
	if got.Files < 6 || got.Bytes < 600 || got.MeasuredAt == "" {
		t.Fatalf("size not written: %+v", got)
	}
}

func TestTheSizeWalkDoesNotFollowALink(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	fillHome(t, outside, 3, 5000)
	if err := os.Symlink(outside, filepath.Join(dd.Work, "link")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	if !d.walkHome(row.Path, time.Now().Add(time.Minute)) {
		t.Fatal("the walk did not finish")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.Bytes >= 5000 {
		t.Fatalf("the walk counted what a link points at: %d bytes", got.Bytes)
	}
}

func TestTheDiskViewListsHomesBySizeAndWhatIsNotAtriums(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	small, ds, err := d.makeHome(store.CardHomeScratch, "small", "w")
	if err != nil {
		t.Fatal(err)
	}
	big, db, err := d.makeHome(store.CardHomeScratch, "big", "w")
	if err != nil {
		t.Fatal(err)
	}
	fillHome(t, ds.Dir, 1, 10)
	fillHome(t, db.Dir, 3, 4000)
	d.walkHome(small.Path, time.Now().Add(time.Minute))
	d.walkHome(big.Path, time.Now().Add(time.Minute))
	if err := os.MkdirAll(filepath.Join(root, "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	outCard, _, err := d.st.Register(store.Observed{WireName: "elsewhere", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	any, err := d.DiskView()
	if err != nil {
		t.Fatal(err)
	}
	v := any.(DiskView)
	if len(v.Homes) != 2 || v.Homes[0].Path != big.Path || v.Homes[0].Bytes < v.Homes[1].Bytes {
		t.Fatalf("homes not by size: %+v", v.Homes)
	}
	if len(v.NotAtriums) != 1 || !strings.HasSuffix(v.NotAtriums[0], "/stray") {
		t.Fatalf("not atrium's: %v", v.NotAtriums)
	}
	if len(v.Outside) != 1 || v.Outside[0].Card != outCard.ID {
		t.Fatalf("outside: %+v", v.Outside)
	}
	if v.Bytes != v.Homes[0].Bytes+v.Homes[1].Bytes {
		t.Fatalf("total %d", v.Bytes)
	}
}

func TestRemoveHomeRefusesALiveCardAndRemovesAKeptOrphan(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "w", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	if _, err := d.RemoveHome(row.ID); err == nil {
		t.Fatal("a live card's home was removed")
	}
	if _, err := os.Stat(dd.Dir); err != nil {
		t.Fatal("the refused removal touched the home")
	}
	if _, err := d.RemoveHome("no-such-id"); err == nil {
		t.Fatal("an unknown id was accepted")
	}

	if err := d.st.CardHomeSet(row.Path, store.CardHomeKept, "dirty", ""); err != nil {
		t.Fatal(err)
	}
	res, err := d.RemoveHome(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(HomeRemoved); !r.Removed {
		t.Fatalf("a kept home stayed: %+v", r)
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
}

func TestCullKeepsTheBranchWhenTheHubDoesNotHaveIt(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	if err := os.WriteFile(filepath.Join(dd.Work, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cullGit(t, dd.Work, "add", "a.txt")
	cullGit(t, dd.Work, "commit", "-q", "-m", "a")
	tip := strings.TrimSpace(cullGit(t, dd.Work, "rev-parse", "HEAD"))
	cullGit(t, repo, "checkout", "-q", "claude/main")
	cullGit(t, repo, "cherry-pick", tip)
	cullGit(t, repo, "checkout", "-q", "main")

	res, err := d.cullHome(task, DefaultCullInto, row)
	if err != nil {
		t.Fatal(err)
	}
	if !res.WorktreeRemoved || res.Kept != "" {
		t.Fatalf("a landed home was not removed: %+v", res)
	}
	if res.BranchDeleted {
		t.Fatalf("BranchDeleted claimed a branch the hub never had: %+v", res)
	}
	if out := cullGit(t, repo, "branch", "--list", "claude/fix-the-thing"); strings.TrimSpace(out) == "" {
		t.Fatal("the branch was deleted")
	}
}

func TestALongSayAndTheHandoffGoInTheHomeForACardWithOne(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.makeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "homed", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}

	say := launcherSayBody(d.saysDir(task.ID), task.ID, "line one\nline two")
	want := filepath.Join(dd.Meta, "says", task.ID+".md")
	if say != "read "+want+" and do what it says." {
		t.Fatalf("say %q, want a file at %s", say, want)
	}
	if b, err := os.ReadFile(want); err != nil || string(b) != "line one\nline two" {
		t.Fatalf("say file: %q %v", b, err)
	}
	if got := d.handoffPath(task); got != filepath.Join(dd.Meta, "handoff.md") {
		t.Fatalf("handoff at %s", got)
	}
	path, err := d.prepareHandoff(task)
	if err != nil || filepath.Dir(path) != dd.Meta || !regexp.MustCompile(`^handoff-\d{14}\.md$`).MatchString(filepath.Base(path)) {
		t.Fatalf("prepare: %q %v", path, err)
	}
}

func TestALongSayAndTheHandoffPlacesForACardWithNoHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, _, err := d.st.Register(store.Observed{WireName: "plain", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if dir := d.saysDir(task.ID); dir != "" {
		t.Fatalf("a card with no home has a says dir %q", dir)
	}
	say := launcherSayBody(d.saysDir(task.ID), task.ID, "line one\nline two")
	if want := filepath.Join(os.TempDir(), "atrium-says", task.ID+".md"); !strings.Contains(say, want) {
		t.Fatalf("say %q, want the old place %s", say, want)
	}
	// The handoff is never in the system temp folder: beside the room's database instead.
	if got, want := d.handoffPath(task), filepath.Join(filepath.Dir(d.opts.DBPath), handoffDirName, task.ID+".md"); got != want {
		t.Fatalf("handoff at %s, want %s", got, want)
	}
}

func requiredRoom(t *testing.T, on bool) (*Daemon, string) {
	t.Helper()
	d, root := homeRoom(t)
	d.hk.settleWindow = 30 * time.Second
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "quick", Label: "quick", Enabled: true, LaunchMode: store.LaunchPTY,
		Cmd: os.Args[0], Args: []string{"-test.run=TestHelperSourceProcess"},
		PromptArgs: []string{"-test.v={prompt}"},
	}); err != nil {
		t.Fatal(err)
	}
	if on {
		if err := d.st.SetSetting(store.SettingCardRootRequired, "on"); err != nil {
			t.Fatal(err)
		}
	}
	return d, root
}

func launchOutside(d *Daemon, cwd string, tags []string, scratch bool) error {
	_, err := d.Launch(LaunchRequest{Harness: "quick", Cwd: cwd, Title: "outside", Prompt: "go", Tags: tags, Scratch: scratch})
	return err
}

func refusedForRoot(err error) bool {
	return err != nil && strings.Contains(err.Error(), "card root") && strings.Contains(err.Error(), "repo")
}

func TestAnAgentLaunchOutsideTheCardRootIsRefusedWhenRequired(t *testing.T) {
	t.Parallel()
	d, _ := requiredRoom(t, true)
	err := launchOutside(d, t.TempDir(), []string{OriginAgentTag}, false)
	if !refusedForRoot(err) {
		t.Fatalf("an agent launch outside the root was not refused with the repo and branch sentence: %v", err)
	}
	if !strings.Contains(err.Error(), "branch") || !strings.Contains(err.Error(), "scratch") {
		t.Fatalf("the refusal does not name branch and scratch: %v", err)
	}
}

func TestAnAgentLaunchOutsideTheCardRootIsAllowedWhenNotRequired(t *testing.T) {
	t.Parallel()
	d, _ := requiredRoom(t, false)
	if err := launchOutside(d, t.TempDir(), []string{OriginAgentTag}, true); refusedForRoot(err) {
		t.Fatalf("refused with the setting off: %v", err)
	}
}

func TestScratchAndAHumanLaunchAreNeverRefusedForTheCardRoot(t *testing.T) {
	t.Parallel()
	d, _ := requiredRoom(t, true)
	if err := launchOutside(d, t.TempDir(), []string{OriginAgentTag}, true); refusedForRoot(err) {
		t.Fatalf("scratch was refused: %v", err)
	}
	if err := launchOutside(d, t.TempDir(), nil, false); refusedForRoot(err) {
		t.Fatalf("a human launch was refused: %v", err)
	}
}
