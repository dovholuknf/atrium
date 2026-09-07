package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// The outward path, tested where it actually lives: the reply to a check-in.
//
// The hub never dials a room, so if work does not ride the reply it never
// leaves this machine. Everything here is about that one body.

func checkIn(t *testing.T, d *Daemon, rep RoomReport) map[string]any {
	t.Helper()
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/rooms", bytes.NewReader(body))
	d.handleRoomCheckIn(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("check-in answered %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("could not read the reply: %v", err)
	}
	return out
}

func launchesIn(t *testing.T, reply map[string]any) []map[string]any {
	t.Helper()
	raw, ok := reply["launches"]
	if !ok {
		t.Fatal("the reply to a check-in carries no launches field at all, so nothing " +
			"can ever be sent to a room")
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("launches is %T", raw)
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("a launch is %T", item)
		}
		out = append(out, m)
	}
	return out
}

func TestQueuedWorkRidesTheReplyToACheckIn(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.QueueDispatch(store.Dispatch{
		Room: "cdaws", Harness: "claude", Prompt: "backlog 12",
	}); err != nil {
		t.Fatal(err)
	}

	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true}))
	if len(got) != 1 {
		t.Fatalf("a room checked in with work waiting and was handed %d", len(got))
	}
	if got[0]["harness"] != "claude" || got[0]["prompt"] != "backlog 12" {
		t.Fatalf("the instruction did not survive the reply: %+v", got[0])
	}
	// Without a token the room cannot report back, and the item would sit in
	// `claimed` until the lease ran out.
	if got[0]["token"] == "" || got[0]["token"] == nil {
		t.Fatal("a handout with no token")
	}

	// And the next check-in gets nothing, because it was claimed.
	if again := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true})); len(again) != 0 {
		t.Fatalf("the same item was handed out again on the next heartbeat: %d", len(again))
	}
}

func TestOneRoomIsNeverHandedAnothersWork(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdzrok", Launches: true}))
	if len(got) != 0 {
		t.Fatalf("cdzrok was handed %d item(s) queued for cdaws", len(got))
	}
}

// A room that has decided not to run anything is handed nothing, so its queue
// stays visible on the board instead of being spent on refusals.
func TestARoomThatRefusesWorkIsHandedNoneOfIt(t *testing.T) {
	d := testDaemon(t)
	item, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws"})); len(got) != 0 {
		t.Fatalf("a room reporting launches=false was handed %d item(s)", len(got))
	}
	after, err := d.st.Dispatch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != store.DispatchQueued {
		t.Fatalf("the item moved to %q instead of waiting for a room that will run it",
			after.State)
	}
	if after.Attempts != 0 {
		t.Fatalf("an attempt was spent on a room that was never going to run it: %d",
			after.Attempts)
	}
}

// A room part way through the last batch is handed nothing either. Two batches
// end to end would outlast the lease on the first, and the hub would take back
// an item that room is still starting.
func TestARoomStartingThingsIsHandedNoMore(t *testing.T) {
	d := testDaemon(t)
	item, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true, Busy: true}))
	if len(got) != 0 {
		t.Fatalf("a busy room was handed %d more item(s)", len(got))
	}
	after, _ := d.st.Dispatch(item.ID)
	if after.State != store.DispatchQueued || after.Attempts != 0 {
		t.Fatalf("an attempt was spent on a room that was mid-batch: %+v", after)
	}
	// And it collects on the check-in after it is free again.
	if free := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true})); len(free) != 1 {
		t.Fatalf("the item was not handed over once the room was free: %d", len(free))
	}
}

// The stored room is keyed on the trimmed name. If the handout looked work up
// under the untrimmed one, a room checking in as " cdaws " would be drawn on
// the board and never collect anything queued for it.
func TestWhitespaceAroundARoomNameDoesNotSplitItsQueue(t *testing.T) {
	d := testDaemon(t)
	if _, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"}); err != nil {
		t.Fatal(err)
	}
	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "  cdaws  ", Launches: true}))
	if len(got) != 1 {
		t.Fatalf("a room whose name arrived padded collected %d of its own items", len(got))
	}
}

func TestARoomReportsWhatHappenedAndOnlyWithItsToken(t *testing.T) {
	d := testDaemon(t)
	item, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true}))
	token, _ := got[0]["token"].(string)

	// Somebody else's answer is refused, and the status says the world moved
	// rather than that the request was malformed.
	if code, _ := result(t, d, item.ID, `{"token":"nope","ok":true,"card_id":"x"}`); code != http.StatusConflict {
		t.Fatalf("a result with the wrong token answered %d", code)
	}

	body := `{"token":"` + token + `","ok":false,"error":"D:/worktrees/x is not a directory on this machine"}`
	if code, _ := result(t, d, item.ID, body); code != http.StatusOK {
		t.Fatalf("the holder's result answered %d", code)
	}
	after, err := d.st.Dispatch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != store.DispatchFailed {
		t.Fatalf("state after a refusal: %q", after.State)
	}
	// THE REASON IS THE POINT. A card that fails with nothing written on it is
	// the outcome this whole path exists to avoid.
	if after.Error == "" {
		t.Fatal("a refused item carries no reason, so the board has nothing to draw")
	}
}

func result(t *testing.T, d *Daemon, id, body string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/dispatch/"+id+"/result",
		bytes.NewReader([]byte(body)))
	r.SetPathValue("id", id)
	d.handleDispatchResult(w, r)
	return w.Code, w.Body.String()
}

// A room saying it started something ends this hub's interest in the item. The
// work is then visible the way every other remote card is: in that room's own
// check-in, on that room's own board. A durable copy here is what federation
// rules out.
func TestAStartedItemPointsAtTheRoomAndKeepsNoCopy(t *testing.T) {
	d := testDaemon(t)
	item, err := d.st.QueueDispatch(store.Dispatch{Room: "cdaws", Harness: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	got := launchesIn(t, checkIn(t, d, RoomReport{Name: "cdaws", Launches: true}))
	token, _ := got[0]["token"].(string)

	body := `{"token":"` + token + `","ok":true,"card_id":"card-9",` +
		`"card_url":"http://cdaws:7778/#card=card-9"}`
	if code, out := result(t, d, item.ID, body); code != http.StatusOK {
		t.Fatalf("result answered %d: %s", code, out)
	}
	after, _ := d.st.Dispatch(item.ID)
	if after.State != store.DispatchRunning {
		t.Fatalf("state: %q", after.State)
	}
	if after.CardURL == "" {
		t.Fatal("nothing points at the card, so the operator has no way to reach it")
	}
	// No task was made here. The card is on cdaws.
	tasks, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("this hub made %d card(s) for work running somewhere else", len(tasks))
	}
}
