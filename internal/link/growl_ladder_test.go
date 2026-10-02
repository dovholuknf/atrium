package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// snoozeDue snoozes a growler as a click would, for a time already past, so the
// next tick wakes it. The store keeps the real clock, the rig's moves.
func (r *growlRig) snoozeDue(t *testing.T, id string) {
	t.Helper()
	row, err := r.g.st.Act(id, "snoozed", time.Now().Add(-time.Second), "board", "")
	if err != nil || row.State != "snoozed" {
		t.Fatalf("snooze: %+v %v", row, err)
	}
	// A woken growler is raised at the store's now, so the rig's clock catches up.
	r.mu.Lock()
	r.clock = time.Now().UTC()
	r.mu.Unlock()
}

func (r *growlRig) reminders(t *testing.T) int {
	t.Helper()
	n := 0
	for _, row := range r.live(t) {
		n += row.Reminders
	}
	return n
}

func (r *growlRig) reminded() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if ids, _ := e["remind"].([]any); len(ids) > 0 {
			n += len(ids)
		}
	}
	return n
}

func (r *growlRig) phones() []Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notice(nil), r.phoned...)
}

// A QUESTION AND A BLOCK RING ONCE: no reminder ever,
// however long they wait, and nothing for the phone beyond the notify raise.
func TestGrowlLadderOffNeverReminds(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"),
		growlReportCard("b", "blocked", "2026-09-30T10:00:00Z"))
	if len(rig.live(t)) != 2 {
		t.Fatalf("raised %+v, want a question and a block", rig.live(t))
	}
	for _, d := range []time.Duration{70 * time.Second, 10 * time.Minute, 5 * time.Hour} {
		rig.advance(d)
		rig.g.tick(context.Background())
	}
	if n := rig.reminded(); n != 0 {
		t.Fatalf("%d reminders for reasons off the ladder", n)
	}
	if n := rig.reminders(t); n != 0 {
		t.Fatalf("reminders counted %d", n)
	}
	if got := rig.phones(); len(got) != 0 {
		t.Fatalf("the phone heard %+v", got)
	}
}

// A HALT AND A STALLED DEPLOY HOLD BLOCK SOMETHING, so with nothing set they
// keep reminding on the backoff like a permission.
func TestGrowlLadderDefaultKeepsHaltAndHold(t *testing.T) {
	rig := newGrowlRig(t)
	rig.health = roomHealth{ok: true, halted: true, cause: "disk I/O error"}
	rig.hold = roomHold{ok: true, on: true, id: "h1", by: "merge", startedAt: rig.g.now().Add(-20 * time.Minute)}
	rig.g.tick(context.Background())
	if len(rig.live(t)) != 2 {
		t.Fatalf("raised %+v, want a halt and a hold", rig.live(t))
	}
	rig.advance(70 * time.Second)
	rig.g.tick(context.Background())
	if n := rig.reminded(); n != 2 {
		t.Fatalf("%d reminders a minute on, want one each for the halt and the hold", n)
	}
	// Turned off, they ring once like a question.
	if err := rig.st.SetSetting(settingGrowlLadder, `{"halt":false,"deploy-hold":false}`); err != nil {
		t.Fatal(err)
	}
	rig.advance(5 * time.Minute)
	rig.g.tick(context.Background())
	if n := rig.reminded(); n != 2 {
		t.Fatalf("%d reminders after the ladder was turned off for them", n)
	}
}

// A PERMISSION KEEPS ITS LADDER, step for step, to the phone as well.
func TestGrowlLadderOnPermissionFollowsTheBackoff(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlPermCard("a", rig.g.now().Add(-3*time.Minute)))
	rig.advance(30 * time.Second)
	rig.g.tick(context.Background())
	if rig.reminded() != 0 {
		t.Fatalf("a permission reminded before the first step")
	}
	for i, step := range growlBackoff {
		rig.advance(step)
		rig.g.tick(context.Background())
		if got := rig.reminders(t); got != i+1 {
			t.Fatalf("after step %d reminders %d, want %d", i, got, i+1)
		}
	}
	// Each tick jumped far enough to pass every step so far, so one reminder
	// each, not a burst.
	if n := rig.reminded(); n != len(growlBackoff) {
		t.Fatalf("%d reminders, want %d", n, len(growlBackoff))
	}
	if got := rig.phones(); len(got) != len(growlBackoff) || got[0].Reason != ReasonPermission {
		t.Fatalf("the phone heard %+v", got)
	}
}

// A WOKEN QUESTION RE-RAISES ONCE, PHONES ONCE, AND DOES NOT START A LADDER.
func TestGrowlWokenQuestionRaisesOnce(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"))
	id := rig.live(t)[0].ID
	rig.snoozeDue(t, id)
	rig.g.tick(context.Background())
	ev := rig.lastEvent(t)
	if ids, _ := ev["remind"].([]any); len(ids) != 1 || ids[0] != id {
		t.Fatalf("the wake said %v, want %s reminded", ev, id)
	}
	got := rig.phones()
	if len(got) != 1 || got[0].Reason != ReasonQuestion || got[0].Name != "worker q" ||
		got[0].Card != "sparta~q" || got[0].Room != "sparta" {
		t.Fatalf("the phone heard %+v, want the question once", got)
	}
	if row := rig.live(t)[0]; row.State != "open" || row.Reminders != 0 {
		t.Fatalf("the woken growler is %+v", row)
	}
	for _, d := range []time.Duration{70 * time.Second, 10 * time.Minute, 5 * time.Hour} {
		rig.advance(d)
		rig.g.tick(context.Background())
	}
	if n := rig.reminded(); n != 1 {
		t.Fatalf("%d reminders after the wake, want only the wake", n)
	}
	if len(rig.phones()) != 1 {
		t.Fatalf("the phone heard %+v after the wake", rig.phones())
	}
}

// A WOKEN PERMISSION PHONES ONCE AND ITS LADDER STARTS OVER FROM THE WAKE.
func TestGrowlWokenPermissionRestartsItsLadder(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlPermCard("a", rig.g.now().Add(-3*time.Minute)))
	id := rig.live(t)[0].ID
	rig.advance(11 * time.Minute)
	rig.g.tick(context.Background())
	if rig.reminders(t) != 4 {
		t.Fatalf("reminders %d, want the first four steps", rig.reminders(t))
	}
	rig.snoozeDue(t, id)
	before := len(rig.phones())
	rig.g.tick(context.Background())
	if row := rig.live(t)[0]; row.State != "open" || row.Reminders != 0 {
		t.Fatalf("the woken permission is %+v", row)
	}
	if got := rig.phones(); len(got) != before+1 || got[before].Reason != ReasonPermission {
		t.Fatalf("the phone heard %+v, want one more for the wake", got)
	}
	// The ladder runs from the wake: a minute on is its first step again.
	rig.advance(2 * time.Minute)
	rig.g.tick(context.Background())
	if row := rig.live(t)[0]; row.Reminders < 1 {
		t.Fatalf("the woken permission %+v never started its ladder", row)
	}
}

// THE SETTING TAKES EFFECT ON THE NEXT TICK, either way.
func TestGrowlLadderSettingChangeTakesEffect(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"))
	rig.advance(70 * time.Second)
	rig.g.tick(context.Background())
	if rig.reminded() != 0 {
		t.Fatalf("a question reminded with the default ladder")
	}
	if err := rig.st.SetSetting(settingGrowlLadder, `{"question":true}`); err != nil {
		t.Fatal(err)
	}
	rig.g.tick(context.Background())
	if rig.reminded() != 1 {
		t.Fatalf("a question did not remind with its ladder on")
	}
	// Naming one reason leaves the others at their default.
	if l := rig.g.ladder(); !l[ReasonPermission] || !l[ReasonQuestion] || l[reasonBlocked] {
		t.Fatalf("the ladder is %v", l)
	}
	if err := rig.st.SetSetting(settingGrowlLadder, `{"permission":false}`); err != nil {
		t.Fatal(err)
	}
	rig.advance(5 * time.Minute)
	rig.g.tick(context.Background())
	if rig.reminded() != 1 {
		t.Fatalf("a question reminded after its ladder was turned off")
	}
	// Rubbish is the default, not a silent permission.
	if err := rig.st.SetSetting(settingGrowlLadder, `not json`); err != nil {
		t.Fatal(err)
	}
	if l := rig.g.ladder(); !l[ReasonPermission] || l[ReasonQuestion] {
		t.Fatalf("an unreadable setting gave %v", l)
	}
}

func TestGrowlLadderEndpoint(t *testing.T) {
	rig := newGrowlRig(t)
	p := &Proxy{growl: rig.g}
	p.feeds = newFeeds(p)
	do := func(method, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, "http://127.0.0.1/_hub/growl-ladder", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:50000"
		w := httptest.NewRecorder()
		p.serveGrowlLadder(w, req)
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	code, m := do(http.MethodGet, "")
	if code != 200 || m["permission"] != true || m["question"] != false || m["blocked"] != false ||
		m["halt"] != true || m["deploy-hold"] != true {
		t.Fatalf("GET answered %d %v", code, m)
	}
	if code, m = do(http.MethodPut, `{"question":true}`); code != 200 || m["question"] != true || m["permission"] != true {
		t.Fatalf("PUT answered %d %v", code, m)
	}
	if !rig.g.ladder()[ReasonQuestion] {
		t.Fatalf("the PUT did not reach the ladder")
	}
	if code, _ = do(http.MethodPut, `{"ready":true}`); code != 400 {
		t.Fatalf("an unknown reason answered %d", code)
	}
	if code, _ = do(http.MethodPut, `nope`); code != 400 {
		t.Fatalf("garbage answered %d", code)
	}
	if code, _ = do(http.MethodDelete, ""); code != 405 {
		t.Fatalf("a DELETE answered %d", code)
	}
	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/_hub/growl-ladder", strings.NewReader(`{"question":false}`))
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	p.serveGrowlLadder(w, req)
	if w.Code != 403 || !rig.g.ladder()[ReasonQuestion] {
		t.Fatalf("a PUT through a proxy answered %d", w.Code)
	}
}

// A CARD WHOSE SESSION ENDED TAKES ITS GROWLERS WITH IT, open or snoozed.
func TestGrowlEndedCardEndsItsGrowlers(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"), growlQuestionCard("s"), growlQuestionCard("x"))
	var snoozed string
	for _, r := range rig.live(t) {
		if r.CardID == "s" {
			snoozed = r.ID
		}
	}
	if _, err := rig.g.st.Act(snoozed, "snoozed", time.Now().Add(time.Hour), "board", ""); err != nil {
		t.Fatal(err)
	}
	ended := func(id, status string) CardState {
		c := growlQuestionCard(id)
		var m map[string]any
		_ = json.Unmarshal(c.Payload, &m)
		m["status"] = status
		b, _ := json.Marshal(m)
		return CardState{ID: id, Status: status, Payload: b}
	}
	// One card done, one dead, one still waiting.
	rig.announce(t, ended("q", "done"), ended("s", "dead"), growlQuestionCard("x"))
	rows := rig.live(t)
	if len(rows) != 1 || rows[0].CardID != "x" {
		t.Fatalf("after the sessions ended the live set is %+v, want only x", rows)
	}
	rows, err := rig.g.st.Live()
	if err != nil || len(rows) != 1 {
		t.Fatalf("store live %+v %v", rows, err)
	}
	// An ended card does not come back raised on the next tick either.
	rig.g.tick(context.Background())
	if rows := rig.live(t); len(rows) != 1 {
		t.Fatalf("a tick raised %+v", rows)
	}
	if ev := rig.lastEvent(t); len(ev["growls"].([]any)) != 1 {
		t.Fatalf("the screens were told %v", ev)
	}
}
