package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// testGrowlStore is internal/cli's growlStore, again, because link cannot
// import cli. It maps and nothing else.
type testGrowlStore struct{ s *hubstore.Store }

func (g testGrowlStore) id(room string) (string, error) {
	r, err := g.s.ByName(room)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func toHub(r GrowlRow) hubstore.Growl {
	return hubstore.Growl{ID: r.ID, CardID: r.CardID, Reason: r.Reason, Title: r.Title, Body: r.Body, Subject: r.Subject}
}

func fromHub(g hubstore.Growl) GrowlRow {
	r := GrowlRow{ID: g.ID, Room: g.RoomName, CardID: g.CardID, Reason: g.Reason, Title: g.Title, Body: g.Body,
		Subject: g.Subject, RaisedAt: g.RaisedAt, State: g.State, Reminders: g.Reminders, ChangedAt: g.ChangedAt,
		ChangedVia: g.ChangedVia, ChangedTab: g.ChangedTab}
	if g.Until != nil {
		r.Until = g.Until.Format(time.RFC3339Nano)
	}
	return r
}

func (g testGrowlStore) Sync(room string, reasons []string, want []GrowlRow, present map[string]bool) ([]string, bool, error) {
	id, err := g.id(room)
	if err != nil {
		return nil, false, err
	}
	var out []hubstore.Growl
	for _, w := range want {
		out = append(out, toHub(w))
	}
	return g.s.GrowlSync(id, reasons, out, present)
}

func (g testGrowlStore) Room(room, reason string, on bool, row GrowlRow) (bool, bool, error) {
	id, err := g.id(room)
	if err != nil {
		return false, false, err
	}
	return g.s.GrowlRoom(id, reason, on, toHub(row))
}

func (g testGrowlStore) Fill(id, subject, body string) (bool, error) {
	return g.s.GrowlFill(id, subject, body)
}

func (g testGrowlStore) Live() ([]GrowlRow, error) {
	rows, err := g.s.GrowlLive()
	var out []GrowlRow
	for _, r := range rows {
		out = append(out, fromHub(r))
	}
	return out, err
}

func (g testGrowlStore) Act(id, state string, until time.Time, via, tab string) (GrowlRow, error) {
	row, err := g.s.GrowlAct(id, state, until, via, tab)
	switch {
	case errors.Is(err, hubstore.ErrGrowlNotFound):
		return GrowlRow{}, ErrGrowlNotFound
	case errors.Is(err, hubstore.ErrGrowlStale):
		return fromHub(row), ErrGrowlStale
	}
	return fromHub(row), err
}

func (g testGrowlStore) Wake() ([]string, error)          { return g.s.GrowlWake() }
func (g testGrowlStore) Reminded(id string, n int) error  { return g.s.GrowlReminded(id, n) }
func (g testGrowlStore) Prune() error                     { _, err := g.s.GrowlPrune(); return err }
func (g testGrowlStore) Setting(n string) (string, error) { return g.s.Setting(n) }

func (g testGrowlStore) Rooms() ([]string, error) {
	rooms, err := g.s.Rooms()
	var out []string
	for _, r := range rooms {
		out = append(out, r.Name)
	}
	return out, err
}

func (g testGrowlStore) Cards(room string) ([]CardState, error) {
	id, err := g.id(room)
	if err != nil {
		return nil, err
	}
	cards, err := g.s.Cards(id)
	var out []CardState
	for _, c := range cards {
		out = append(out, CardState{ID: c.ID, Status: c.Status, Payload: json.RawMessage(c.Payload)})
	}
	return out, err
}

// growlRig is a growler over a real hub store, with a room called sparta, a
// clock the test moves, and every event it says kept.
type growlRig struct {
	g      *Growler
	st     *hubstore.Store
	clock  time.Time
	online bool
	mu     sync.Mutex
	events []map[string]any
	phoned []Notice
	health roomHealth
	hold   roomHold
	perms  []pendingPerm
}

func newGrowlRig(t *testing.T) *growlRig {
	t.Helper()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Add("sparta", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	rig := &growlRig{st: st, clock: time.Now().UTC(), online: true}
	g := NewGrowler(testGrowlStore{st})
	g.now = func() time.Time {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return rig.clock
	}
	g.attached = func(string) bool {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return rig.online
	}
	g.say = func(e Event) {
		var m map[string]any
		_ = json.Unmarshal(e.Data, &m)
		rig.mu.Lock()
		rig.events = append(rig.events, m)
		rig.mu.Unlock()
	}
	g.phone = func(n Notice) {
		rig.mu.Lock()
		rig.phoned = append(rig.phoned, n)
		rig.mu.Unlock()
	}
	g.health = func(context.Context, string) roomHealth {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return rig.health
	}
	g.hold = func(context.Context, string) roomHold {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return rig.hold
	}
	g.pending = func(context.Context, string) []pendingPerm {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		return rig.perms
	}
	rig.g = g
	return rig
}

// announce is what the hub does: the cache first, then the growler.
func (r *growlRig) announce(t *testing.T, cards ...CardState) {
	t.Helper()
	out := make([]hubstore.Card, 0, len(cards))
	for _, c := range cards {
		out = append(out, hubstore.Card{ID: c.ID, Status: c.Status, Payload: c.Payload})
	}
	if _, err := r.st.Announce(mustRoomID(t, r.st), out); err != nil {
		t.Fatal(err)
	}
	r.g.Announced("sparta", cards)
}

func (r *growlRig) advance(d time.Duration) {
	r.mu.Lock()
	r.clock = r.clock.Add(d)
	r.mu.Unlock()
}

func (r *growlRig) lastEvent(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		t.Fatal("no growls event was said")
	}
	return r.events[len(r.events)-1]
}

func (r *growlRig) live(t *testing.T) []GrowlRow {
	t.Helper()
	rows, err := r.g.view()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func growlCard(id, body string) CardState {
	return CardState{ID: id, Payload: json.RawMessage(body)}
}

func growlPermCard(id string, since time.Time, tags ...string) CardState {
	b, _ := json.Marshal(map[string]any{"title": "worker " + id, "status": "needs-permission",
		"waiting_since": since.Format(time.RFC3339Nano), "tags": tags, "seen": map[string]any{}})
	return growlCard(id, string(b))
}

func growlQuestionCard(id string, tags ...string) CardState {
	b, _ := json.Marshal(map[string]any{"title": "worker " + id, "status": "needs-input", "tags": tags,
		"seen": map[string]any{"questions_at": "2026-09-30T10:00:00Z", "open_questions": []string{"Which?\nmore"},
			"turn_ended_at": "2026-09-30T10:00:00Z"}})
	return growlCard(id, string(b))
}

// WHAT EARNS ONE. A permission after two minutes, an agent's permission too, a
// question at once but never an agent's, and a plain waiting card never.
func TestGrowlDerivation(t *testing.T) {
	rig := newGrowlRig(t)
	now := rig.g.now()
	rig.g.Announced("sparta", []CardState{
		growlPermCard("young", now.Add(-time.Minute)),
		growlPermCard("old", now.Add(-3*time.Minute)),
		growlPermCard("agent", now.Add(-3*time.Minute), "origin:agent"),
		growlQuestionCard("q"),
		growlQuestionCard("aq", "origin:agent"),
		growlCard("idle", `{"status":"needs-input","seen":{"turn_ended_at":"2026-09-30T10:00:00Z"}}`),
	})
	got := map[string]GrowlRow{}
	for _, r := range rig.live(t) {
		got[r.CardID] = r
	}
	if len(got) != 3 || got["old"].ID == "" || got["agent"].ID == "" || got["q"].ID == "" {
		t.Fatalf("growlers for %v, want old, agent and q", got)
	}
	if !strings.HasPrefix(got["old"].ID, "sparta|old|permission|") {
		t.Fatalf("the id is %q, want the room in front of the notify identity", got["old"].ID)
	}
	if q := got["q"]; q.Body != "Which?" || q.Card != "sparta~q" || q.Urgency != 4 {
		t.Fatalf("the question growler is %+v", q)
	}
	// The ordering: permissions first, then the question.
	rows := rig.live(t)
	if rows[len(rows)-1].Reason != ReasonQuestion {
		t.Fatalf("a question sorted above a permission: %+v", rows)
	}
	// Two minutes on, the young permission is promoted by the ticker alone, with
	// no announcement at all.
	rig.advance(90 * time.Second)
	if _, err := rig.st.Announce(mustRoomID(t, rig.st), []hubstore.Card{
		{ID: "young", Status: "needs-permission", Payload: growlPermCard("young", now.Add(-time.Minute)).Payload},
	}); err != nil {
		t.Fatal(err)
	}
	rig.g.tick(context.Background())
	found := false
	for _, r := range rig.live(t) {
		found = found || r.CardID == "young"
	}
	if !found {
		t.Fatalf("the ticker did not promote a permission past two minutes")
	}
}

func mustRoomID(t *testing.T, st *hubstore.Store) string {
	t.Helper()
	r, err := st.ByName("sparta")
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

// A PERMISSION LEARNS ITS REQUEST from the room, so the board can approve it.
func TestGrowlPermissionIsFilledFromTheRoom(t *testing.T) {
	rig := newGrowlRig(t)
	now := rig.g.now()
	rig.perms = []pendingPerm{
		{ID: "p2", TaskID: "a", Command: "later", RequestedAt: now.Add(-time.Minute)},
		{ID: "p1", TaskID: "a", Command: "go test ./...\nsecond line", RequestedAt: now.Add(-5 * time.Minute)},
	}
	rig.announce(t, growlPermCard("a", now.Add(-5*time.Minute)))
	// The fill runs off the announcement's goroutine.
	var rows []GrowlRow
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if rows = rig.live(t); len(rows) == 1 && rows[0].Subject != "" {
			break
		}
	}
	if len(rows) != 1 || rows[0].Subject != "p1" || rows[0].Body != "go test ./..." {
		t.Fatalf("the permission growler is %+v, want the oldest request's id and first line", rows)
	}
}

// REMINDERS FOLLOW THE BACKOFF, reach the phone, and stop after two hours.
func TestGrowlReminders(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"))
	rig.advance(70 * time.Second)
	rig.g.tick(context.Background())
	ev := rig.lastEvent(t)
	remind, _ := ev["remind"].([]any)
	if len(remind) != 1 {
		t.Fatalf("a minute on, the event was %v, want one reminder", ev)
	}
	if len(rig.phoned) != 1 || rig.phoned[0].Reason != ReasonQuestion || rig.phoned[0].Name != "worker q" {
		t.Fatalf("the phone was told %+v", rig.phoned)
	}
	// Nothing new due: no event.
	n := len(rig.events)
	rig.g.tick(context.Background())
	if len(rig.events) != n {
		t.Fatalf("a tick with nothing due said %v", rig.events[len(rig.events)-1])
	}
	// Far past the last step: the count catches up once and then stops.
	rig.advance(5 * time.Hour)
	rig.g.tick(context.Background())
	if r := rig.live(t)[0]; r.Reminders != len(growlBackoff) {
		t.Fatalf("reminders %d, want %d", r.Reminders, len(growlBackoff))
	}
	n = len(rig.events)
	rig.advance(5 * time.Hour)
	rig.g.tick(context.Background())
	if len(rig.events) != n {
		t.Fatalf("a growler past the two hour step rang again")
	}
}

// A ROOM GOING OFFLINE DOES NOT RESOLVE. The growler stays open and says so.
func TestGrowlRoomOfflineKeepsItOpen(t *testing.T) {
	rig := newGrowlRig(t)
	rig.announce(t, growlQuestionCard("q"))
	rig.mu.Lock()
	rig.online = false
	rig.mu.Unlock()
	rig.g.tick(context.Background())
	rows := rig.live(t)
	if len(rows) != 1 || rows[0].State != "open" || !rows[0].RoomOffline {
		t.Fatalf("an offline room's growler is %+v, want open and marked offline", rows)
	}
}

// A HALT COMES FROM THE ROOM'S OWN HEALTH, and a room that does not answer is
// neither halted nor healthy.
func TestGrowlHaltFromHealth(t *testing.T) {
	rig := newGrowlRig(t)
	rig.health = roomHealth{ok: true, halted: true, cause: "disk I/O error"}
	rig.g.tick(context.Background())
	rows := rig.live(t)
	if len(rows) != 1 || rows[0].Reason != "halt" || rows[0].Body != "disk I/O error" || rows[0].Urgency != 2 {
		t.Fatalf("a halted room's growlers are %+v", rows)
	}
	rig.health = roomHealth{}
	rig.g.tick(context.Background())
	if len(rig.live(t)) != 1 {
		t.Fatalf("a room that did not answer ended its halt")
	}
	rig.health = roomHealth{ok: true}
	rig.g.tick(context.Background())
	if len(rig.live(t)) != 0 {
		t.Fatalf("a healthy room kept its halt")
	}
}

// THE ENDPOINTS. The live set on GET, an action on POST, a stale one 409 with
// the row, an unknown one 404, a bad one 400, and every change said as an event.
func TestGrowlEndpoints(t *testing.T) {
	rig := newGrowlRig(t)
	p := &Proxy{growl: rig.g}
	rig.announce(t, growlQuestionCard("q"))
	id := rig.live(t)[0].ID

	do := func(method, path, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		p.serveGrowls(w, req, strings.TrimPrefix(req.URL.Path, "/_hub/"))
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	path := "/_hub/growls/" + url.PathEscape(id)

	code, body := do(http.MethodGet, "/_hub/growls", "")
	if rows, _ := body["growls"].([]any); code != 200 || len(rows) != 1 || body["perm_after_seconds"] != float64(120) {
		t.Fatalf("GET answered %d %v", code, body)
	}
	if code, _ := do(http.MethodPost, path, `{"do":"shout","via":"board"}`); code != 400 {
		t.Fatalf("a bad do answered %d", code)
	}
	if code, _ := do(http.MethodPost, path, `{"do":"snooze","minutes":0,"via":"board"}`); code != 400 {
		t.Fatalf("a zero minute snooze answered %d", code)
	}
	if code, _ := do(http.MethodPost, "/_hub/growls/nope", `{"do":"dismiss","via":"board"}`); code != 404 {
		t.Fatalf("an unknown id answered %d", code)
	}
	n := len(rig.events)
	code, body = do(http.MethodPost, path, `{"do":"dismiss","via":"phone","tab":"t-1"}`)
	row, _ := body["growl"].(map[string]any)
	if code != 200 || row["state"] != "dismissed" || row["changed_via"] != "phone" {
		t.Fatalf("dismiss answered %d %v", code, body)
	}
	if len(rig.events) != n+1 {
		t.Fatalf("a dismiss said no event")
	}
	if rows, _ := rig.lastEvent(t)["growls"].([]any); len(rows) != 0 {
		t.Fatalf("after a dismiss the event carried %v", rows)
	}
	code, body = do(http.MethodPost, path, `{"do":"snooze","minutes":15,"via":"board"}`)
	if row, _ := body["growl"].(map[string]any); code != 409 || row["state"] != "dismissed" {
		t.Fatalf("an action on a dismissed growler answered %d %v", code, body)
	}
	code, body = do(http.MethodPost, path, `{"do":"undismiss","via":"board"}`)
	if row, _ := body["growl"].(map[string]any); code != 200 || row["state"] != "open" {
		t.Fatalf("undismiss answered %d %v", code, body)
	}
	if rows, _ := rig.lastEvent(t)["growls"].([]any); len(rows) != 1 {
		t.Fatalf("after an undismiss the event carried %v", rows)
	}
	// reopen is the design doc's name for the same action.
	_, _ = do(http.MethodPost, path, `{"do":"dismiss","via":"board"}`)
	if code, _ := do(http.MethodPost, path, `{"do":"reopen","via":"board"}`); code != 200 {
		t.Fatalf("reopen answered %d", code)
	}
}

func growlReportCard(id, report, since string, tags ...string) CardState {
	b, _ := json.Marshal(map[string]any{"title": "worker " + id, "status": "needs-input", "tags": tags,
		"waiting_since": since, "waiting_reason": report, "recap": "did a thing\n\nneeds: the prod token",
		"seen": map[string]any{}})
	return growlCard(id, string(b))
}

// A REPORT GROWLS. blocked and a report's question at once, never an agent's,
// and the growler ends when the card stops waiting.
func TestGrowlFromAReport(t *testing.T) {
	rig := newGrowlRig(t)
	since := "2026-09-30T10:00:00.000Z"
	rig.announce(t, growlReportCard("b", "blocked", since), growlReportCard("q", "question", since),
		growlReportCard("ab", "blocked", since, "origin:agent"))
	got := map[string]GrowlRow{}
	for _, r := range rig.live(t) {
		got[r.CardID] = r
	}
	if len(got) != 2 {
		t.Fatalf("growlers for %v, want b and q", got)
	}
	if b := got["b"]; b.Reason != "blocked" || b.Urgency != 3 || b.Body != "the prod token" ||
		b.ID != "sparta|b|blocked|"+since || b.Title != "worker b is blocked" {
		t.Fatalf("the blocked growler is %+v", b)
	}
	if q := got["q"]; q.Reason != ReasonQuestion || q.Title != "worker q has a question" {
		t.Fatalf("the report question growler is %+v", q)
	}
	// Replied to: the card runs again, and both end.
	running := growlCard("b", `{"status":"running","seen":{}}`)
	rig.announce(t, running, growlCard("q", `{"status":"running","seen":{}}`))
	if rows := rig.live(t); len(rows) != 0 {
		t.Fatalf("a card that stopped waiting kept %+v", rows)
	}
	// And the notifier still says input for a reported card, even one whose first
	// turn has not ended.
	nc, ok := NotifyIdentity("b", growlReportCard("b", "blocked", since).Payload)
	if !ok || nc.Reason != ReasonInput {
		t.Fatalf("a reported card notifies %+v %v, want input", nc, ok)
	}
}

// A DEPLOY HOLD GROWLS ONCE IT OUTLIVES ITS WINDOW, and ends when the room says
// it lifted. A room that does not answer changes nothing.
func TestGrowlDeployHold(t *testing.T) {
	rig := newGrowlRig(t)
	now := rig.g.now()
	rig.hold = roomHold{ok: true, on: true, id: "h1", by: "merge", whys: []string{"r-growler"},
		startedAt: now.Add(-5 * time.Minute)}
	rig.g.tick(context.Background())
	if rows := rig.live(t); len(rows) != 0 {
		t.Fatalf("a five minute hold growled: %+v", rows)
	}
	rig.advance(11 * time.Minute)
	rig.g.tick(context.Background())
	rows := rig.live(t)
	if len(rows) != 1 || rows[0].Reason != "deploy-hold" || rows[0].Subject != "h1" || rows[0].Urgency != 5 ||
		rows[0].Body != "held by merge: r-growler" || rows[0].CardID != "" {
		t.Fatalf("a sixteen minute hold's growlers are %+v", rows)
	}
	rig.hold = roomHold{}
	rig.g.tick(context.Background())
	if len(rig.live(t)) != 1 {
		t.Fatalf("a room that did not answer ended its hold")
	}
	rig.hold = roomHold{ok: true}
	rig.g.tick(context.Background())
	if len(rig.live(t)) != 0 {
		t.Fatalf("a lifted hold kept its growler")
	}
}
