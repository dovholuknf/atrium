package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeAudit is an in-memory AuditLog, so the endpoint can be tested without a
// store. It records the last read's filters, which is how the passthrough is
// checked.
type fakeAudit struct {
	entries    []AuditEntry
	gotLimit   int
	gotRoom    string
	gotKind    string
	recordedTo []string
	records    []recorded
}

type recorded struct{ room, kind, detail string }

func (f *fakeAudit) Record(room, kind, detail string) {
	f.recordedTo = append(f.recordedTo, room+"/"+kind)
	f.records = append(f.records, recorded{room, kind, detail})
}

func (f *fakeAudit) Recent(limit int, room, kind string) ([]AuditEntry, error) {
	f.gotLimit, f.gotRoom, f.gotKind = limit, room, kind
	return f.entries, nil
}

func TestAuditEndpointReturnsEntriesAndPassesFilters(t *testing.T) {
	fa := &fakeAudit{entries: []AuditEntry{
		{ID: "1", At: time.Now(), Kind: "hub-started", Detail: "up"},
		{ID: "2", At: time.Now(), Room: "alpha", Kind: "room-attached"},
	}}
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetAuditLog(fa)

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/_hub/audit?limit=50&room=alpha&kind=room-attached", nil)
	p.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Events []AuditEntry `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 2 {
		t.Fatalf("want 2 events, got %d", len(body.Events))
	}
	if fa.gotLimit != 50 || fa.gotRoom != "alpha" || fa.gotKind != "room-attached" {
		t.Fatalf("filters not passed through: limit=%d room=%q kind=%q",
			fa.gotLimit, fa.gotRoom, fa.gotKind)
	}
}

// A hub with no store keeps no log. The endpoint answers an empty list rather
// than an error, so the pane draws "nothing yet".
func TestAuditEndpointEmptyWithoutAStore(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/_hub/audit", nil)
	p.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Events []AuditEntry `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 0 {
		t.Fatalf("want no events, got %d", len(body.Events))
	}
}

// auditFromRelay turns the handful of operational relay kinds into audit lines
// and leaves the rest alone. This is what makes a room's permission and session
// lifecycle events show up in the hub's feed without the hub knowing the room's
// storage.
func TestAuditFromRelay(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		data       string
		wantKind   string
		wantDetail string
		wantSkip   bool
	}{
		{name: "going-down", kind: "going-down", data: `{}`,
			wantKind: "room-going-down", wantDetail: "the room says it is winding down"},
		{name: "permission requested", kind: "permission",
			data:     `{"id":"p1","tool":"Bash","command":"ls -la"}`,
			wantKind: "permission-requested", wantDetail: "Bash: ls -la"},
		{name: "permission decided", kind: "permission",
			data:     `{"id":"p1","tool":"Bash","decision":"approve","decided_by":"you","decided_at":"2026-09-19T12:00:00Z"}`,
			wantKind: "permission-decided", wantDetail: "approve by you for Bash"},
		{name: "permission cancel is skipped", kind: "permission",
			data: `{"canceled":1,"task":"t1"}`, wantSkip: true},
		{name: "session lifecycle", kind: "lifecycle",
			data:     `{"kind":"session-exit","detail":"card exited with code 0 after 3s"}`,
			wantKind: "session-exit", wantDetail: "card exited with code 0 after 3s"},
		{name: "unknown lifecycle kind is skipped", kind: "lifecycle",
			data: `{"kind":"whatever","detail":"nope"}`, wantSkip: true},
		{name: "per-card kind is left alone", kind: "activity",
			data: `{"task_id":"t1"}`, wantSkip: true},
		{name: "bad payload is skipped not recorded", kind: "permission",
			data: `not json`, wantSkip: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fa := &fakeAudit{}
			p := NewProxy(NewHub(Timings{}), nil, "", nil)
			p.SetAuditLog(fa)
			p.auditFromRelay("alpha", c.kind, []byte(c.data))
			if c.wantSkip {
				if len(fa.records) != 0 {
					t.Fatalf("wanted nothing recorded, got %v", fa.records)
				}
				return
			}
			if len(fa.records) != 1 {
				t.Fatalf("wanted one line, got %v", fa.records)
			}
			got := fa.records[0]
			if got.room != "alpha" || got.kind != c.wantKind || got.detail != c.wantDetail {
				t.Fatalf("wanted alpha/%s/%q, got %s/%s/%q",
					c.wantKind, c.wantDetail, got.room, got.kind, got.detail)
			}
		})
	}
}

// RecordAudit persists through the wired log. The stream nudge is best effort
// and goes nowhere with no subscriber, which must not panic.
func TestRecordAuditWritesAndDoesNotBlock(t *testing.T) {
	fa := &fakeAudit{}
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetAuditLog(fa)
	p.RecordAudit("alpha", "room-attached", "detail")
	if len(fa.recordedTo) != 1 || fa.recordedTo[0] != "alpha/room-attached" {
		t.Fatalf("record did not reach the log: %v", fa.recordedTo)
	}
}
