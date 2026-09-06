package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Who a restart has to wait for.
//
// Getting this wrong in the safe direction is not safe. A card wrongly counted
// as busy is one the restart waits for forever, and the wait is not passive:
// each attempt asks the session to stop, the session replies, replying is a
// turn, and a turn is activity. Asking it to stop is what kept it busy.

func boardWith(t *testing.T, cards ...map[string]any) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": cards})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// THE ONE THAT HUNG. A session that declared its work over is not mid-tool,
// and a `done` card never moves back to `needs-input` on its own, so counting
// it as busy meant a restart could never proceed.
func TestASessionThatSaidItIsDoneIsNotBusy(t *testing.T) {
	for _, status := range []string{"done", "dead", "shelved"} {
		board := boardWith(t, map[string]any{
			"id": "a", "display_title": "finished one", "status": status,
			"supervised": true, "idle_seconds": 3,
			"activity": map[string]any{"what": "thinking"},
		})
		busy, err := busyAgents(board)
		if err != nil {
			t.Fatal(err)
		}
		if len(busy) != 0 {
			t.Fatalf("a %q card counted as busy: %+v", status, busy)
		}
	}
}

// Activity is written when a tool STARTS and nothing writes when a turn ends,
// so a session that stopped an hour ago still reads as `thinking`.
func TestStaleActivityIsNotActivity(t *testing.T) {
	board := boardWith(t, map[string]any{
		"id": "a", "display_title": "long gone", "status": "running",
		"supervised": true, "idle_seconds": parkIdleAfter + 60,
		"activity": map[string]any{"what": "thinking"},
	})
	busy, err := busyAgents(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 0 {
		t.Fatalf("a session idle for %ds counted as busy: %+v", parkIdleAfter+60, busy)
	}
}

// And the case all of this exists for still works: something actually running
// is still worth refusing a restart over.
func TestASessionThatIsWorkingIsStillBusy(t *testing.T) {
	board := boardWith(t, map[string]any{
		"id": "a", "display_title": "busy one", "status": "running",
		"supervised": true, "idle_seconds": 2,
		"activity": map[string]any{"what": "running", "tool": "Bash"},
	})
	busy, err := busyAgents(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 1 {
		t.Fatalf("a working session was not counted: %+v", busy)
	}
	if busy[0].Doing != "running Bash" {
		t.Fatalf("what it is doing came back as %q", busy[0].Doing)
	}
}

// Waiting on a human is the definition of a safe moment.
func TestAWaitingSessionIsNotBusy(t *testing.T) {
	for _, status := range []string{"needs-input", "needs-permission"} {
		board := boardWith(t, map[string]any{
			"id": "a", "display_title": "waiting", "status": status,
			"supervised": true, "idle_seconds": 1,
		})
		busy, err := busyAgents(board)
		if err != nil {
			t.Fatal(err)
		}
		if len(busy) != 0 {
			t.Fatalf("a %q card counted as busy", status)
		}
	}
}

// A session atrium does not own is unaffected by a restart, so it is not
// something to wait for.
func TestAnUnsupervisedSessionIsNotBusy(t *testing.T) {
	board := boardWith(t, map[string]any{
		"id": "a", "display_title": "not ours", "status": "running",
		"supervised": false, "idle_seconds": 1,
		"activity": map[string]any{"what": "running"},
	})
	busy, err := busyAgents(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 0 {
		t.Fatalf("a session atrium does not own counted as busy: %+v", busy)
	}
}

// THE LOOP THAT WASTED AN HOUR.
//
// Telling a session to stop makes it think: it reads the message, writes a
// sentence saying it has stopped, and that turn is activity. So the more
// politely it obeys, the more it looks like it is working, and each retry sends
// another message and starts it again.
//
// After a session has been told, only work in flight counts.
func TestOnceToldThinkingStopsCounting(t *testing.T) {
	got := stillHoldingSomething([]busyCard{
		{ID: "a", Title: "obedient", Doing: "thinking"},
		{ID: "b", Title: "mid-tool", Doing: "tool Bash"},
		{ID: "c", Title: "silent", Doing: ""},
	})
	if len(got) != 2 {
		t.Fatalf("wanted the tool and the silent one, got %+v", got)
	}
	for _, b := range got {
		if b.Title == "obedient" {
			t.Fatal("a session that acknowledged the park message still counted as busy, " +
				"which is the loop this exists to break")
		}
	}
}

// A session mid-tool may have a file half written, and that is the whole reason
// any of this waits for anything.
func TestASessionMidToolStillCounts(t *testing.T) {
	got := stillHoldingSomething([]busyCard{{ID: "a", Title: "writing", Doing: "tool Edit"}})
	if len(got) != 1 {
		t.Fatal("a session mid-tool was treated as safe to interrupt")
	}
}

// A card with no activity at all and no terminal status is still counted, and
// that direction is deliberate: "I cannot tell" is not "it is safe".
func TestSilenceIsStillTreatedAsBusy(t *testing.T) {
	board := boardWith(t, map[string]any{
		"id": "a", "display_title": "quiet", "status": "running",
		"supervised": true, "idle_seconds": 5,
	})
	busy, err := busyAgents(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(busy) != 1 {
		t.Fatalf("a supervised running card with no activity was treated as safe: %+v", busy)
	}
}
