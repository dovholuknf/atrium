package gitsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Pushed and Reachable on a real bare repository made by real pushes, and RoomBranch against a room that serves
// real git.

type pushedFix struct {
	*recvFix
	card who
}

// pushedFixture is a hub whose fix/x is at `tip`, pushed by sg4's C1, with these commits on it:
//
//	first   the branch's first commit, the hub's tip
//	later   a commit on fix/x the hub has under another name (fix/ahead), so it holds it and fix/x is behind it
//	rival   a commit off main the hub holds under fix/rival, which is not on fix/x
//	local   a commit on fix/x that was never pushed anywhere
func pushedFixture(t *testing.T) (*pushedFix, map[string]string) {
	t.Helper()
	x := &pushedFix{recvFix: newRecv(t), card: roomCard("sg4", "C1")}
	x.seedMain(hubRepo)
	sha := map[string]string{}
	sha["first"] = x.branch("fix/x", "x.txt")
	x.must(x.push(x.card, hubRepo, "fix/x:refs/heads/fix/x"))
	sha["later"] = x.grow("fix/x", "y.txt")
	// The hub takes the later commit under another name, then fix/x goes back in the work repository so the
	// following commits are built on `first` again.
	x.must(x.push(x.card, hubRepo, "fix/x:refs/heads/fix/ahead"))
	git(t, x.work, "branch", "-q", "-f", "fix/x", sha["first"])
	sha["rival"] = x.branch("fix/rival", "r.txt")
	x.must(x.push(x.card, hubRepo, "fix/rival:refs/heads/fix/rival"))
	sha["local"] = x.grow("fix/x", "z.txt")
	return x, sha
}

func TestPushedSaysMatchesBehindAheadDivergedAndNotPushed(t *testing.T) {
	x, sha := pushedFixture(t)
	st := x.h.Store()
	at := func(branch string) string { return x.refOn(hubRepo, "refs/heads/"+branch) }
	if at("fix/x") != sha["first"] {
		t.Fatalf("fixture: fix/x is %s, want %s", at("fix/x"), sha["first"])
	}
	main := x.refOn(hubRepo, MainRef)

	cases := []struct {
		name, branch, head, want string
	}{
		{"the hub's tip is the head", "fix/x", sha["first"], PushedMatches},
		{"a head the hub has never seen", "fix/x", sha["local"], PushedBehind},
		{"a head built on the hub's tip, which the hub holds", "fix/x", sha["later"], PushedBehind},
		{"a head the hub's tip is built on", "fix/ahead", sha["first"], PushedAhead},
		{"a head the hub's tip is built on, as main", "fix/x", main, PushedAhead},
		{"both moved, the head is held", "fix/x", sha["rival"], PushedDiverged},
		{"no such branch", "fix/nowhere", sha["first"], PushedNotPushed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := st.Pushed(bg, hubRepo, c.branch, c.head)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != c.want {
				t.Fatalf("state %q, want %q: %+v", got.State, c.want, got)
			}
			if c.want == PushedNotPushed {
				if got != (Pushed{State: PushedNotPushed}) {
					t.Fatalf("not-pushed carries a value: %+v", got)
				}
				return
			}
			if got.HubSHA != at(c.branch) {
				t.Fatalf("hub_sha %q, the hub has %q", got.HubSHA, at(c.branch))
			}
		})
	}
}

func TestPushedCarriesTheOwnerAndTheTimeOfTheLastPush(t *testing.T) {
	x, sha := pushedFixture(t)
	got, err := x.h.Store().Pushed(bg, hubRepo, "fix/x", sha["first"])
	if err != nil {
		t.Fatal(err)
	}
	if got.Room != "sg4" || got.Card != "C1" || got.Released {
		t.Fatalf("owner: %+v", got)
	}
	when, err := time.Parse(time.RFC3339, got.At)
	if err != nil || time.Since(when) > time.Hour {
		t.Fatalf("at %q (%v)", got.At, err)
	}

	// The operator's branch has no owner, and a released one says so.
	got, err = x.h.Store().Pushed(bg, hubRepo, "main", sha["first"])
	if err != nil {
		t.Fatal(err)
	}
	if got.Room != "" || got.Card != "" {
		t.Fatalf("main has an owner: %+v", got)
	}
	if _, err := x.h.ReleaseBranch(bg, hubRepo, "fix/x"); err != nil {
		t.Fatal(err)
	}
	got, _ = x.h.Store().Pushed(bg, hubRepo, "fix/x", sha["first"])
	if !got.Released {
		t.Fatalf("a released branch is not said to be: %+v", got)
	}
}

func TestPushedOfARepositoryTheHubLacksIsNotPushed(t *testing.T) {
	x, sha := pushedFixture(t)
	got, err := x.h.Store().Pushed(bg, "github/o/absent", "fix/x", sha["first"])
	if err != nil || got.State != PushedNotPushed {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPushedRefusesWhatIsNotANameOrACommitId(t *testing.T) {
	x, sha := pushedFixture(t)
	st := x.h.Store()
	for _, head := range []string{"", "main", "HEAD", sha["first"][:12], sha["first"] + "0", strings.ToUpper(sha["first"]),
		"--all", sha["first"] + "^", "-" + sha["first"][1:]} {
		if _, err := st.Pushed(bg, hubRepo, "fix/x", head); err == nil {
			t.Errorf("head %q was taken", head)
		}
		if _, err := st.Reachable(bg, hubRepo, "main", head); err == nil {
			t.Errorf("reachable of %q was taken", head)
		}
	}
	for _, branch := range []string{"", "-x", "a..b", "a b", "x\ny", "refs/heads/../x", "x.lock", "@{u}"} {
		if _, err := st.Pushed(bg, hubRepo, branch, sha["first"]); err == nil {
			t.Errorf("branch %q was taken", branch)
		}
		if _, _, err := st.BranchSHA(bg, hubRepo, branch); err == nil {
			t.Errorf("branch sha of %q was taken", branch)
		}
	}
	for _, repo := range []string{"", "github/../r", "github/o/r/../../x", "-x/o/r"} {
		if _, err := st.Pushed(bg, repo, "fix/x", sha["first"]); err == nil {
			t.Errorf("repo %q was taken", repo)
		}
	}
}

func TestPushedCancelledIsAnErrorNotAState(t *testing.T) {
	x, sha := pushedFixture(t)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	got, err := x.h.Store().Pushed(ctx, hubRepo, "fix/x", sha["local"])
	if err == nil {
		t.Fatalf("a cancelled read answered %+v", got)
	}
}

func TestReachableIsTheTipOrWhatItIsBuiltOn(t *testing.T) {
	x, sha := pushedFixture(t)
	st := x.h.Store()
	main := x.refOn(hubRepo, MainRef)
	for _, c := range []struct {
		name, branch, sha string
		want              bool
	}{
		{"the tip itself", "main", main, true},
		{"a commit the tip is built on", "fix/ahead", sha["first"], true},
		{"a commit the tip is built on, from main", "fix/x", main, true},
		{"a commit on another branch", "main", sha["first"], false},
		{"a commit past the tip", "fix/x", sha["later"], false},
		{"a commit the hub never saw", "main", sha["local"], false},
		{"a branch the hub lacks", "fix/nowhere", main, false},
	} {
		got, err := st.Reachable(bg, hubRepo, c.branch, c.sha)
		if err != nil || got != c.want {
			t.Errorf("%s: %v, %v", c.name, got, err)
		}
	}
	if got, err := st.Reachable(bg, "github/o/absent", "main", main); err != nil || got {
		t.Errorf("an absent repository: %v, %v", got, err)
	}
}

func TestBranchSHAIsTheHubsTipOrNotFound(t *testing.T) {
	x, sha := pushedFixture(t)
	got, ok, err := x.h.Store().BranchSHA(bg, hubRepo, "fix/x")
	if err != nil || !ok || got != sha["first"] {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	if got, ok, err = x.h.Store().BranchSHA(bg, hubRepo, "fix/nowhere"); err != nil || ok || got != "" {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	// A prefix of a name is not the name: fix/x must not find fix/xy, and fix does not find fix/x.
	if _, ok, _ := x.h.Store().BranchSHA(bg, hubRepo, "fix"); ok {
		t.Fatal("fix found a branch")
	}
}

// ── a room's branch ─────────────────────────────────────

func TestRoomBranchIsTheTipTheRoomAdvertises(t *testing.T) {
	x := newHubFixture(t, true)
	tip := git(t, x.roomWork, "rev-parse", "claude/main")
	git(t, x.roomWork, "switch", "-q", "-c", "claude/x")
	want := commit(t, x.roomWork, "x.txt", "x")

	got, found, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/x")
	if err != nil || !found || got != want {
		t.Fatalf("%q %v %v, want %q", got, found, err, want)
	}
	got, found, err = x.h.RoomBranch(bg, "SG3", "github/o/r", "claude/main")
	if err != nil || !found || got != strings.TrimSpace(tip) {
		t.Fatalf("claude/main: %q %v %v", got, found, err)
	}
	if _, found, err = x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/nope"); err != nil || found {
		t.Fatalf("a branch the room lacks: %v %v", found, err)
	}
	// A longer name that starts with the asked one is not it.
	if _, found, _ = x.h.RoomBranch(bg, "sg3", "github/o/r", "claude"); found {
		t.Fatal("a prefix found a branch")
	}
}

func TestRoomBranchOfARepositoryTheRoomDoesNotServeIsNotFound(t *testing.T) {
	x := newHubFixture(t, true)
	if _, found, err := x.h.RoomBranch(bg, "sg3", "github/o/other", "claude/main"); err != nil || found {
		t.Fatalf("%v %v", found, err)
	}
}

func TestRoomBranchSaysWhyARoomCannotBeAsked(t *testing.T) {
	x := newHubFixture(t, true)
	x.rooms.up = false
	if _, _, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/main"); !errors.Is(err, ErrRoomUnreachable) ||
		!strings.Contains(err.Error(), "not connected") {
		t.Fatalf("an unattached room: %v", err)
	}

	x = newHubFixture(t, false)
	before := x.requests.Load()
	if _, _, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/main"); !errors.Is(err, ErrRoomUnreachable) {
		t.Fatalf("a room that does not serve git: %v", err)
	}
	if x.requests.Load() != before {
		t.Fatal("a room that did not say git was dialled")
	}

	x = newHubFixture(t, true)
	x.rooms.srv.Close()
	if _, _, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/main"); !errors.Is(err, ErrRoomUnreachable) {
		t.Fatalf("a room that is gone: %v", err)
	}

	if _, _, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "a..b"); err == nil {
		t.Fatal("a bad branch name was taken")
	}
	if _, _, err := x.h.RoomBranch(bg, "sg3", "nonsense", "claude/main"); err == nil {
		t.Fatal("a bad repository name was taken")
	}
}

func TestRoomBranchOfARoomThatAnswersBadlyIsUnreachableNotAbsent(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"a server error": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusInternalServerError) },
		"not a git advertisement": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>hello</html>"))
		},
		"a cut-off advertisement": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("001e# serv")) },
	} {
		t.Run(name, func(t *testing.T) {
			x := newHubFixture(t, true)
			bad := httptest.NewServer(h)
			t.Cleanup(bad.Close)
			x.rooms.srv = bad
			if sha, found, err := x.h.RoomBranch(bg, "sg3", "github/o/r", "claude/main"); !errors.Is(err, ErrRoomUnreachable) {
				t.Fatalf("%q %v %v", sha, found, err)
			}
		})
	}
}

// Only git's exit status 1 is "no". Any other failure is the read failing, and is never taken for an answer.
func TestAGitFailureIsAnErrorAndNotANo(t *testing.T) {
	x, sha := pushedFixture(t)
	notARepo := t.TempDir()
	if ok, err := x.h.Store().hasCommit(bg, notARepo, sha["first"]); err == nil || ok {
		t.Fatalf("hasCommit in a directory that is no repository: %v, %v", ok, err)
	}
	if ok, err := x.h.Store().isAncestor(bg, notARepo, sha["first"], sha["later"]); err == nil || ok {
		t.Fatalf("isAncestor in a directory that is no repository: %v, %v", ok, err)
	}
}
