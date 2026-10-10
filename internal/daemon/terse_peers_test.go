package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestPeerFramingIsShort(t *testing.T) {
	msgs := []*store.Message{{FromPeer: "agent2", Text: "i am done"}}
	got := messageBanner(msgs, false)
	if !strings.HasPrefix(got, "@agent2 says (a session, not the human)") || !strings.HasSuffix(got, ":\n\n\"i am done\"") {
		t.Fatalf("peer banner: %q", got)
	}
	if strings.Contains(got, "sent through atrium") || strings.Contains(got, "Treat it as") {
		t.Fatalf("the old banner is back: %q", got)
	}
	if peerBanner("agent1") != atriumLabel("@agent1 says:") {
		t.Fatalf("typed label: %q", peerBanner("agent1"))
	}
}

func TestReportBodyIsShort(t *testing.T) {
	task := &store.Task{WireName: "w", ID: "card-1", Worktree: "/some/path"}
	sha := "b550e917aabbccdd"
	if got, want := reportBody(task, "done", sha, false, "shipped it\nmore detail"), "@w done b550e917: shipped it"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got, want := reportBody(task, "done", sha, true, ""), "@w done b550e917 unverified"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := reportBody(task, "blocked", "", false, "need a token"); got != "@w blocked: need a token" {
		t.Fatalf("got %q", got)
	}
}
