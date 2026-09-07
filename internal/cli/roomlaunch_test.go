package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where a dispatched launch runs, and everything it refuses.
//
// This is the part of remote launch that atrium deliberately does not solve. A
// cloud box has no `D:/worktrees/...` and atrium does not make one, so the only
// two acceptable outcomes are "a directory that is really there" and "a refusal
// somebody can read". A third outcome, starting somewhere plausible, is the
// failure these tests exist to make impossible.

func tempRoot(t *testing.T) string {
	t.Helper()
	// Resolved, because `safepath` resolves both sides and a temp dir on macOS
	// is a symlink. Comparing against the unresolved form is the classic way to
	// write a containment test that passes for the wrong reason.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestARoomWithNoWorkspaceRefusesAnyDirectoryTheHubNames(t *testing.T) {
	root := tempRoot(t)
	_, err := resolveHandoutCwd(roomOpts{}, handout{Harness: "claude", Cwd: root}, "")
	if err == nil {
		t.Fatal("a hub named a directory on a room that never said it could, and it was honoured")
	}
	// The refusal has to name the way out, or the operator is reading a log on
	// a machine they are not sitting at.
	if !strings.Contains(err.Error(), "--workspace") {
		t.Fatalf("the refusal should name the flag that would allow it, got: %v", err)
	}
}

func TestADirectoryOutsideTheWorkspaceIsRefused(t *testing.T) {
	root := tempRoot(t)
	inside := filepath.Join(root, "work")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := tempRoot(t)

	o := roomOpts{Workspace: inside}
	if _, err := resolveHandoutCwd(o, handout{Harness: "claude", Cwd: outside}, ""); err == nil {
		t.Fatal("a directory outside the workspace was accepted")
	}
	// The sibling case, which a plain prefix test gets wrong.
	sibling := filepath.Join(root, "work-elsewhere")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveHandoutCwd(o, handout{Harness: "claude", Cwd: sibling}, ""); err == nil {
		t.Fatal("a sibling sharing the workspace's name prefix was treated as inside it")
	}
	// And climbing out of it.
	if _, err := resolveHandoutCwd(o, handout{Harness: "claude", Cwd: ".."}, ""); err == nil {
		t.Fatal("a relative path climbing out of the workspace was accepted")
	}
}

func TestADirectoryInsideTheWorkspaceIsUsed(t *testing.T) {
	root := tempRoot(t)
	repo := filepath.Join(root, "atrium")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveHandoutCwd(roomOpts{Workspace: root},
		handout{Harness: "claude", Cwd: repo}, "")
	if err != nil {
		t.Fatalf("a directory inside the workspace was refused: %v", err)
	}
	if !strings.EqualFold(got, repo) {
		t.Fatalf("resolved to %q, wanted %q", got, repo)
	}
}

// The whole point. cdaws has no `D:/worktrees/...` and never will.
func TestADirectoryThatIsNotThereIsRefusedBeforeAnythingStarts(t *testing.T) {
	root := tempRoot(t)
	missing := filepath.Join(root, "a-worktree-nobody-made")

	_, err := resolveHandoutCwd(roomOpts{Workspace: root},
		handout{Harness: "claude", Cwd: missing}, "")
	if err == nil {
		t.Fatal("a launch was allowed to start in a directory that does not exist")
	}
	if !strings.Contains(err.Error(), "does not create worktrees") {
		t.Fatalf("the refusal should say atrium is not going to make it, got: %v", err)
	}
	// Forward slashes, like every path atrium carries, because the board that
	// draws this may not be on the platform the room is.
	if !strings.Contains(err.Error(), filepath.ToSlash(missing)) {
		t.Fatalf("the refusal should name the directory, got: %v", err)
	}
}

// A file is not a directory, and `os.Stat` succeeding is not the check.
func TestAFileWhereADirectoryWasExpectedIsRefused(t *testing.T) {
	root := tempRoot(t)
	f := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveHandoutCwd(roomOpts{Workspace: root},
		handout{Harness: "claude", Cwd: f}, ""); err == nil {
		t.Fatal("a plain file was accepted as a working directory")
	}
}

// The ordinary dispatch names nothing and the room's own runner answers. This
// is the case that makes the feature usable without the hub ever learning a
// path on another machine.
func TestNoDirectoryFallsBackToTheRoomsOwnRunner(t *testing.T) {
	root := tempRoot(t)
	got, err := resolveHandoutCwd(roomOpts{}, handout{Harness: "claude"}, root)
	if err != nil {
		t.Fatalf("the runner's own working directory was refused: %v", err)
	}
	if !strings.EqualFold(got, root) {
		t.Fatalf("resolved to %q, wanted %q", got, root)
	}
}

// The room's own configuration is the room's business. Running it through the
// workspace check would refuse the ordinary setup, where the one checkout on
// that machine is nowhere near whatever directory was nominated for
// hub-supplied paths.
func TestTheRunnersOwnDirectoryIsNotCheckedAgainstTheWorkspace(t *testing.T) {
	workspace := tempRoot(t)
	elsewhere := tempRoot(t)
	got, err := resolveHandoutCwd(roomOpts{Workspace: workspace},
		handout{Harness: "claude"}, elsewhere)
	if err != nil {
		t.Fatalf("this room's own runner configuration was refused: %v", err)
	}
	if !strings.EqualFold(got, elsewhere) {
		t.Fatalf("resolved to %q, wanted %q", got, elsewhere)
	}
}

// The runner's directory still has to exist. A room configured against a
// checkout somebody deleted must refuse rather than start.
func TestTheRunnersOwnDirectoryStillHasToBeThere(t *testing.T) {
	gone := filepath.Join(tempRoot(t), "deleted")
	if _, err := resolveHandoutCwd(roomOpts{}, handout{Harness: "claude"}, gone); err == nil {
		t.Fatal("a runner pointed at a directory that is gone was launched anyway")
	}
}

// The failure this file exists to prevent. `daemon.Launch` falls back to
// `os.Getwd()` for a local launch, and doing that here would start a session
// wherever the room process happens to be sitting: a plausible directory, the
// wrong one, on a machine nobody is watching.
func TestNothingAtAllIsRefusedRatherThanRunningWhereverTheRoomIs(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveHandoutCwd(roomOpts{}, handout{Harness: "claude"}, "")
	if err == nil {
		t.Fatalf("a launch with no directory anywhere resolved to %q instead of refusing", got)
	}
	if strings.Contains(got, here) {
		t.Fatal("it fell back to the room process's own working directory")
	}
	if !strings.Contains(err.Error(), "will not guess") {
		t.Fatalf("the refusal should say it is not guessing, got: %v", err)
	}
	// Named, because the operator reading this is looking at four machines and
	// has to know which runner on which one to fix.
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("the refusal should name the runner, got: %v", err)
	}
}

func TestARoomStartedWithNoLaunchRefusesWorkAndSaysSo(t *testing.T) {
	res := runOneHandout(t.Context(), "http://127.0.0.1:1", roomOpts{NoLaunch: true},
		handout{ID: "x", Harness: "claude"})
	if res.OK {
		t.Fatal("a room started with --no-launch ran something")
	}
	if !strings.Contains(res.Err, "--no-launch") {
		t.Fatalf("the refusal should name the flag, got: %q", res.Err)
	}
}

func TestAnItemWithNoRunnerIsRefusedWithoutAskingTheDaemon(t *testing.T) {
	// The address is deliberately unreachable. Refusing before any request
	// means this comes back as a refusal about the item, not about the daemon.
	res := runOneHandout(t.Context(), "http://127.0.0.1:1", roomOpts{},
		handout{ID: "x"})
	if res.OK || !strings.Contains(res.Err, "which runner") {
		t.Fatalf("wanted a refusal naming the missing runner, got: %+v", res)
	}
}

// A card on this machine, since that is where its terminal is and always will
// be. The hub draws this link and never tries to serve the card itself.
func TestTheCardLinkPointsAtTheRoomsOwnBoard(t *testing.T) {
	if got := cardURL("http://cdaws:7778/", "abc"); got != "http://cdaws:7778/#card=abc" {
		t.Fatalf("card link: %q", got)
	}
	// A room that never said where its board is has no link to offer, and an
	// empty string is drawn as nothing rather than as a broken anchor.
	if got := cardURL("", "abc"); got != "" {
		t.Fatalf("a room with no board produced %q", got)
	}
	if got := cardURL("http://cdaws:7778", ""); got != "" {
		t.Fatalf("a launch with no card produced %q", got)
	}
}
