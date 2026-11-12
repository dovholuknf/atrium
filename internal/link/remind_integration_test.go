//go:build integration

package link

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

type remindRig struct {
	t    *testing.T
	path string
	st   *hubstore.Store
	now  time.Time
	h    *Reminders
	sent []string
	// answer is what the fake say path answers a wake with.
	answer string
}

type remindDeliverer struct{ x *remindRig }

func (d remindDeliverer) Resolve(context.Context, string) (CITarget, CIAnswer) {
	return CITarget{}, CIAnswer{}
}

func (d remindDeliverer) Send(_ context.Context, t CITarget, _, text, _ string, wake bool) CIAnswer {
	if d.x.answer == hubstore.CIPending {
		return CIAnswer{State: hubstore.CIPending}
	}
	d.x.sent = append(d.x.sent, t.Card+"|"+text)
	return CIAnswer{State: hubstore.CIDelivered}
}

func newRemindRig(t *testing.T) *remindRig {
	t.Helper()
	x := &remindRig{t: t, path: filepath.Join(t.TempDir(), "hub.db"), now: time.Now().UTC().Truncate(time.Second),
		answer: hubstore.CIDelivered}
	x.open()
	if _, err := x.st.Add("alpha", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	return x
}

// open is the hub starting: a new store on the same file and a new loop.
func (x *remindRig) open() {
	st, err := hubstore.Open(x.path)
	if err != nil {
		x.t.Fatal(err)
	}
	st.SetClock(func() time.Time { return x.now })
	x.t.Cleanup(func() { st.Close() })
	x.st = st
	g := NewGrowler(testGrowlStore{st})
	x.h = &Reminders{Store: st, Growler: func() *Growler { return g }, Deliver: remindDeliverer{x}, Now: func() time.Time { return x.now }}
}

func (x *remindRig) post(body map[string]any) (int, map[string]any) {
	x.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_hub/remind", bytes.NewReader(raw))
	req.RemoteAddr, req.Host = "127.0.0.1:5000", "127.0.0.1:7778"
	rec := httptest.NewRecorder()
	x.h.Serve(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (x *remindRig) live() []hubstore.Growl {
	x.t.Helper()
	rows, err := x.st.GrowlLive()
	if err != nil {
		x.t.Fatal(err)
	}
	return rows
}

func TestAReminderComesDueAsAGrowlerOnItsCard(t *testing.T) {
	x := newRemindRig(t)
	code, out := x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "3d", "text": "delete the 21 GB folder"})
	if code != http.StatusOK {
		t.Fatalf("set: %d %v", code, out)
	}
	x.h.Pass(context.Background())
	if len(x.live()) != 0 {
		t.Fatalf("a growler before the time: %+v", x.live())
	}
	x.now = x.now.Add(3*24*time.Hour + time.Minute)
	x.h.Pass(context.Background())
	x.h.Pass(context.Background())
	rows := x.live()
	if len(rows) != 1 {
		t.Fatalf("live %+v, want one", rows)
	}
	g := rows[0]
	if g.Reason != hubstore.GrowlReminder || g.CardID != "c1" || g.RoomName != "alpha" || g.State != hubstore.GrowlOpen ||
		!strings.Contains(g.Title, "delete the 21 GB folder") || g.Body != "delete the 21 GB folder" {
		t.Fatalf("bell row %+v", g)
	}
	if len(x.sent) != 0 {
		t.Fatalf("a reminder without wake typed into the card: %v", x.sent)
	}
	if pending, _ := x.st.ReminderPendingOf("alpha~c1"); len(pending) != 0 {
		t.Fatalf("still pending after it fired: %+v", pending)
	}
	if _, err := x.st.GrowlAct(g.ID, hubstore.GrowlSnoozed, x.now.Add(time.Hour), "board", ""); err != nil {
		t.Fatalf("remind me on the bell row: %v", err)
	}
}

func TestAReminderSurvivesAHubRestart(t *testing.T) {
	x := newRemindRig(t)
	if code, out := x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "4h", "text": "check the deploy"}); code != 200 {
		t.Fatalf("set: %d %v", code, out)
	}
	x.st.Close()
	x.now = x.now.Add(5 * time.Hour)
	x.open()
	x.h.Pass(context.Background())
	if rows := x.live(); len(rows) != 1 || rows[0].Body != "check the deploy" {
		t.Fatalf("after a restart: %+v", rows)
	}
}

func TestACancelledReminderNeverRings(t *testing.T) {
	x := newRemindRig(t)
	_, a := x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "1h", "text": "one"})
	x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "1h", "text": "two"})
	_, list := x.post(map[string]any{"action": "list", "room": "alpha", "card": "c1"})
	if rs, _ := list["reminders"].([]any); len(rs) != 2 {
		t.Fatalf("list %v", list)
	}
	id, _ := a["reminder"].(map[string]any)["id"].(string)
	if _, out := x.post(map[string]any{"action": "cancel", "room": "alpha", "card": "c1", "id": id}); out["removed"] != float64(1) {
		t.Fatalf("cancel one: %v", out)
	}
	// another room's card cannot cancel it, and a room cannot name another room's card
	if code, _ := x.post(map[string]any{"action": "cancel", "room": "alpha", "card": "beta~c1"}); code != http.StatusForbidden {
		t.Fatalf("a room named another room's card: %d", code)
	}
	if _, out := x.post(map[string]any{"action": "cancel", "room": "alpha", "card": "c1"}); out["removed"] != float64(1) {
		t.Fatalf("cancel the rest: %v", out)
	}
	x.now = x.now.Add(2 * time.Hour)
	x.h.Pass(context.Background())
	if rows := x.live(); len(rows) != 0 {
		t.Fatalf("a cancelled reminder rang: %+v", rows)
	}
}

func TestAReminderIsRefusedPastNinetyDaysInThePastOrBare(t *testing.T) {
	x := newRemindRig(t)
	for name, body := range map[string]map[string]any{
		"91 days":  {"action": "set", "room": "alpha", "card": "c1", "in": "91d", "text": "x"},
		"the past": {"action": "set", "room": "alpha", "card": "c1", "at": x.now.Add(-time.Hour).Format(time.RFC3339), "text": "x"},
		"no time":  {"action": "set", "room": "alpha", "card": "c1", "text": "x"},
		"both":     {"action": "set", "room": "alpha", "card": "c1", "in": "1h", "at": x.now.Add(time.Hour).Format(time.RFC3339), "text": "x"},
		"no text":  {"action": "set", "room": "alpha", "card": "c1", "in": "1h"},
		"nonsense": {"action": "set", "room": "alpha", "card": "c1", "in": "soon", "text": "x"},
	} {
		if code, out := x.post(body); code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
	if code, out := x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "90d", "text": "x"}); code != 200 {
		t.Fatalf("90 days: %d %v", code, out)
	}
	if code, out := x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "1d12h", "text": "x"}); code != 200 {
		t.Fatalf("1d12h: %d %v", code, out)
	}
}

func TestAWakeReminderTypesIntoTheCardAndRetriesWhileItCannotBeReached(t *testing.T) {
	x := newRemindRig(t)
	x.post(map[string]any{"action": "set", "room": "alpha", "card": "c1", "in": "1h", "text": "run the cleanup", "wake": true})
	x.now = x.now.Add(2 * time.Hour)
	x.answer = hubstore.CIPending
	x.h.Pass(context.Background())
	if len(x.live()) != 1 || len(x.sent) != 0 {
		t.Fatalf("growler %d, sent %v", len(x.live()), x.sent)
	}
	if pending, _ := x.st.ReminderPendingOf("alpha~c1"); len(pending) != 1 {
		t.Fatal("a wake that could not be said was dropped")
	}
	x.answer = hubstore.CIDelivered
	x.h.Pass(context.Background())
	if len(x.sent) != 1 || !strings.Contains(x.sent[0], "alpha~c1|") || !strings.Contains(x.sent[0], "run the cleanup") {
		t.Fatalf("sent %v", x.sent)
	}
	if len(x.live()) != 1 {
		t.Fatalf("the retry raised the growler again: %+v", x.live())
	}
	if pending, _ := x.st.ReminderPendingOf("alpha~c1"); len(pending) != 0 {
		t.Fatal("still pending after the wake was said")
	}
}
