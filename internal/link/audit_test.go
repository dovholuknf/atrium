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
}

func (f *fakeAudit) Record(room, kind, detail string) {
	f.recordedTo = append(f.recordedTo, room+"/"+kind)
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
