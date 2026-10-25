//go:build integration

package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newWorkHarness is an open harness whose checkout of o/r was cloned from a BARE origin, plus a second clone of that
// origin to push from, so what the room fetches is something it has not seen.
type newWorkHarness struct {
	*openHarness
	bare, checkout, pusher string
}

func newNewWorkHarness(t *testing.T) *newWorkHarness {
	t.Helper()
	oh := newOpenHarness(t, &fakeForge{})
	tmp := realSlash(t.TempDir())
	nh := &newWorkHarness{openHarness: oh, bare: tmp + "/origin.git", pusher: tmp + "/pusher"}
	git(t, tmp, "clone", "-q", "--bare", oh.up, nh.bare)
	git(t, tmp, "clone", "-q", nh.bare, nh.pusher)
	nh.checkout = filepath.ToSlash(filepath.Join(oh.root, "o", "r"))
	mkDir(t, filepath.Dir(filepath.FromSlash(nh.checkout)))
	git(t, tmp, "clone", "-q", nh.bare, nh.checkout)
	// a home with no hooks, or the room would run the ones the machine has
	if err := oh.srv.st.SetSetting(SettingWorktreeHooks, filepath.ToSlash(filepath.Join(tmp, "hooks"))); err != nil {
		t.Fatal(err)
	}
	return nh
}

// pushBranch puts a branch with one new commit on the origin, off its main.
func (nh *newWorkHarness) pushBranch(t *testing.T, branch string) string {
	t.Helper()
	git(t, nh.pusher, "checkout", "-q", "-B", branch, "origin/main")
	git(t, nh.pusher, "commit", "-q", "--allow-empty", "-m", "on "+branch)
	git(t, nh.pusher, "push", "-q", "origin", branch)
	return git(t, nh.pusher, "rev-parse", "HEAD")
}

func (nh *newWorkHarness) newWork(t *testing.T, body map[string]any) (int, map[string]any) {
	t.Helper()
	if body["repo"] == nil {
		body["repo"] = "github.com/o/r"
	}
	rec := post(t, nh.h, "/v1/new", body)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// THE BASE IS THE REMOTE'S DEFAULT BRANCH AS IT IS NOW, not the clone's stale one, and the operator's clone is not
// touched: its main stays where it was.
func TestNewWorkForksFromOriginsDefaultAfterFetchingAndLeavesTheCloneAlone(t *testing.T) {
	nh := newNewWorkHarness(t)
	before := git(t, nh.checkout, "rev-parse", "main")
	git(t, nh.pusher, "checkout", "-q", "main")
	git(t, nh.pusher, "commit", "-q", "--allow-empty", "-m", "newer")
	git(t, nh.pusher, "push", "-q", "origin", "main")
	newest := git(t, nh.pusher, "rev-parse", "HEAD")

	code, out := nh.newWork(t, map[string]any{"name": "some.new.work", "prompt": "go"})
	if code != 201 || out["created"] != true || out["existed"] != false || out["repo"] != "github.com/o/r" {
		t.Fatalf("%d %v", code, out)
	}
	path := out["worktree"].(string)
	if !strings.HasSuffix(path, "/o/r/some.new.work") {
		t.Fatalf("worktree %q", path)
	}
	if got := git(t, path, "rev-parse", "HEAD"); got != newest {
		t.Errorf("the branch is at %s, origin's main is %s", got, newest)
	}
	if got := git(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "some.new.work" {
		t.Errorf("the worktree is on %q", got)
	}
	if got := git(t, nh.checkout, "rev-parse", "main"); got != before {
		t.Errorf("the operator's main moved from %s to %s", before, got)
	}
	if got := git(t, nh.checkout, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("the operator's clone is on %q", got)
	}
	l := lastLaunch(t, nh.openHarness)
	if l.Cwd != path || l.Branch != "some.new.work" || l.Repo != "r" || l.Org != "o" || l.Prompt != "go" {
		t.Errorf("the launch was %+v", l)
	}
	if got := inventory(t, nh.srv.st, out["card"].(string)); got != "worktree "+path+",branch some.new.work" {
		t.Errorf("the inventory is %s", got)
	}
}

// A NAME THE REMOTE ALREADY HAS IS TRACKED: the branch starts at origin/<name> and follows it.
func TestNewWorkTracksABranchTheRemoteHas(t *testing.T) {
	nh := newNewWorkHarness(t)
	tip := nh.pushBranch(t, "work-b")
	code, out := nh.newWork(t, map[string]any{"name": "work-b"})
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	path := out["worktree"].(string)
	if got := git(t, path, "rev-parse", "HEAD"); got != tip {
		t.Errorf("the branch is at %s, origin/work-b is %s", got, tip)
	}
	if got := git(t, path, "rev-parse", "--abbrev-ref", "work-b@{upstream}"); got != "origin/work-b" {
		t.Errorf("the branch tracks %q", got)
	}
}

// --from WINS over a tracked name and over the default, and a ref that is nowhere is refused before anything is made.
func TestNewWorkFromForksFromTheRefAskedAndRefusesOneThatIsNowhere(t *testing.T) {
	nh := newNewWorkHarness(t)
	rel := nh.pushBranch(t, "release")
	nh.pushBranch(t, "x") // the name is tracked too, and from still wins
	code, out := nh.newWork(t, map[string]any{"name": "x", "from": "release"})
	if code != 201 {
		t.Fatalf("%d %v", code, out)
	}
	if got := git(t, out["worktree"].(string), "rev-parse", "HEAD"); got != rel {
		t.Errorf("the branch is at %s, release is %s", got, rel)
	}
	if out["base"] != "refs/remotes/origin/release" {
		t.Errorf("the base was %v", out["base"])
	}

	n := len(nh.launched)
	code, out = nh.newWork(t, map[string]any{"name": "y", "from": "no-such-ref"})
	if code != 400 || !strings.Contains(out["error"].(string), "no-such-ref") || len(nh.launched) != n {
		t.Errorf("a ref nowhere: %d %v", code, out)
	}
	if exists(filepath.ToSlash(filepath.Join(nh.root, "o", "r", "y"))) {
		t.Error("a refused ref left a worktree behind")
	}
}

// A NAME THAT ALREADY HAS A WORKTREE answers that worktree and its live card. Nothing starts twice.
func TestNewWorkWithAnExistingNameAnswersTheWorktreeAndItsLiveCard(t *testing.T) {
	nh := newNewWorkHarness(t)
	code, first := nh.newWork(t, map[string]any{"name": "again"})
	if code != 201 {
		t.Fatalf("%d %v", code, first)
	}
	code, second := nh.newWork(t, map[string]any{"name": "again"})
	if code != 200 || second["existed"] != true || second["created"] != false || second["card"] != first["card"] ||
		second["worktree"] != first["worktree"] || len(nh.launched) != 1 {
		t.Errorf("the second ask: %d %v, launched %d", code, second, len(nh.launched))
	}
}

// THE REPO'S SETUP HOOK RUNS in the new worktree, once. One that fails is reported and does not fail the work.
func TestNewWorkRunsTheSetupHookOnceAndReportsOneThatFails(t *testing.T) {
	nh := newNewWorkHarness(t)
	dir, _ := nh.srv.st.Setting(SettingWorktreeHooks)
	hookDir := filepath.Join(filepath.FromSlash(dir), "github.com", "o", "r")
	mkDir(t, hookDir)
	write := func(fail bool) {
		var file, body string
		if runtime.GOOS == "windows" {
			file = "worktree.ps1"
			body = "param($WorktreePath,$Org,$Repo,$RemoteHost)\nSet-Content -Path (Join-Path $WorktreePath 'hooked.txt') -Value \"$Org/$Repo@$RemoteHost\"\n"
			if fail {
				body += "Write-Output 'hook says no'\nexit 3\n"
			}
		} else {
			file = "worktree.sh"
			body = "echo \"$2/$3@$4\" > \"$1/hooked.txt\"\n"
			if fail {
				body += "echo 'hook says no'\nexit 3\n"
			}
		}
		if err := os.WriteFile(filepath.Join(hookDir, file), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(false)
	code, out := nh.newWork(t, map[string]any{"name": "hooked"})
	if code != 201 || out["hook"] == nil || out["hook_error"] != nil {
		t.Fatalf("%d %v", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(out["worktree"].(string)), "hooked.txt"))
	if err != nil || !strings.Contains(string(raw), "o/r@github.com") {
		t.Errorf("the hook did not run in the worktree: %v %q", err, raw)
	}
	// the second ask finds the worktree, so the hook is not run again
	if err := os.Remove(filepath.Join(filepath.FromSlash(out["worktree"].(string)), "hooked.txt")); err != nil {
		t.Fatal(err)
	}
	_, again := nh.newWork(t, map[string]any{"name": "hooked"})
	if again["hook"] != nil || exists(out["worktree"].(string)+"/hooked.txt") {
		t.Errorf("the hook ran again: %v", again)
	}

	write(true)
	code, out = nh.newWork(t, map[string]any{"name": "hook-fails"})
	if code != 201 || !strings.Contains(out["hook_error"].(string), "hook says no") {
		t.Errorf("a failing hook: %d %v", code, out)
	}
}

func TestNewWorkWarnsWhenTheHooksFolderIsMissing(t *testing.T) {
	nh := newNewWorkHarness(t)
	dir, _ := nh.srv.st.Setting(SettingWorktreeHooks)
	code, out := nh.newWork(t, map[string]any{"name": "no-hooks"})
	if code != 201 || !strings.Contains(fmt.Sprint(out["hook_warning"]), dir) {
		t.Fatalf("%d %v", code, out)
	}
	mkDir(t, filepath.FromSlash(dir))
	code, out = nh.newWork(t, map[string]any{"name": "hooks-there"})
	if code != 201 || out["hook_warning"] != nil {
		t.Errorf("a hooks folder that is there: %d %v", code, out)
	}
}

func TestNewWorkRefusesWhatIsNotARepoOrHasNoName(t *testing.T) {
	nh := newNewWorkHarness(t)
	for _, body := range []map[string]any{
		{"repo": "o/r", "name": "a"},
		{"repo": "github.com/o/r", "name": ""},
		{"repo": "github.com/o/r", "name": "-x"},
		{"repo": "github.com/o/r", "name": "a..b"},
	} {
		if code, out := nh.newWork(t, body); code != 400 || len(nh.launched) != 0 {
			t.Errorf("%v: %d %v", body, code, out)
		}
	}
}

// PREPARE FINDS THE CLONE AND MAKES NOTHING ELSE: no worktree, no card, no hook. A pasted repo nobody here has seen stops
// after it for a confirm, and only the second call, with a name, starts anything.
func TestNewWorkPrepareMakesNoWorktreeAndNoCard(t *testing.T) {
	nh := newNewWorkHarness(t)
	before := len(nh.launched)
	code, out := nh.newWork(t, map[string]any{"prepare": true})
	if code != 200 || out["card"] != nil && out["card"] != "" || out["checkout"] != nh.checkout {
		t.Fatalf("%d %v", code, out)
	}
	if got := len(nh.launched); got != before {
		t.Errorf("prepare started %d launches", got-before)
	}
	if got := git(t, nh.checkout, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Errorf("prepare made a worktree: %s", got)
	}
	// a claude/ name lands in a flat folder, as every claude/* name does
	code, out = nh.newWork(t, map[string]any{"name": "claude/work-261016-1432", "title": "o/r"})
	if code != 201 || !strings.HasSuffix(out["worktree"].(string), "/o/r/claude-work-261016-1432") {
		t.Fatalf("%d %v", code, out)
	}
}
