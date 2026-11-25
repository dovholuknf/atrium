//go:build integration

package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func cruftRig(t *testing.T) (*Proxy, *hubstore.Store) {
	t.Helper()
	p := NewProxy(NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1}), nil, "", nil)
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	t.Cleanup(func() { p.WaitWork(10 * time.Second) })
	if _, err := st.Add("alpha", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	p.SetBacklog(st)
	p.SetGrowler(NewGrowler(testGrowlStore{st}))
	return p, st
}

func cruftSend(p *Proxy, method, body string) (int, string) {
	req := httptest.NewRequest(method, "http://127.0.0.1:7778/_hub/cruft", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5000"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func cruftGrowls(t *testing.T, st *hubstore.Store) []hubstore.Growl {
	t.Helper()
	live, err := st.GrowlLive()
	if err != nil {
		t.Fatal(err)
	}
	return live
}

func TestTheHubKeepsTheLatestCruftReportPerRoom(t *testing.T) {
	t.Parallel()
	p, _ := cruftRig(t)
	if code, _ := cruftSend(p, http.MethodPost, `{"room":"alpha","freed_count":1,"kept":[]}`); code != http.StatusOK {
		t.Fatalf("first: %d", code)
	}
	if code, _ := cruftSend(p, http.MethodPost, `{"room":"alpha","freed_count":7,"kept":[]}`); code != http.StatusOK {
		t.Fatalf("second: %d", code)
	}
	_, body := cruftSend(p, http.MethodGet, "")
	var got struct {
		Reports map[string]struct {
			Freed int `json:"freed_count"`
		} `json:"reports"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Reports) != 1 || got.Reports["alpha"].Freed != 7 {
		t.Fatalf("reports %s", body)
	}
	if code, _ := cruftSend(p, http.MethodPost, `{"freed_count":1}`); code != http.StatusBadRequest {
		t.Fatalf("a report naming no room: %d", code)
	}
}

func TestOnlyAKeptItemNeedingACallRaisesAGrowlerAndTheNextReportEndsIt(t *testing.T) {
	t.Parallel()
	p, st := cruftRig(t)
	cruftSend(p, http.MethodPost, `{"room":"alpha","freed_count":3,"freed_bytes":99,
		"kept":[{"kind":"folder","ref":"/c/x","why":"a launch holds its card"}]}`)
	if n := len(cruftGrowls(t, st)); n != 0 {
		t.Fatalf("an informational report raised %d growlers", n)
	}
	needs := `{"room":"alpha","kept":[{"kind":"branch","ref":"/r claude/x","why":"2 commits nowhere else","needs":true}]}`
	cruftSend(p, http.MethodPost, needs)
	cruftSend(p, http.MethodPost, needs)
	live := cruftGrowls(t, st)
	if len(live) != 1 || !strings.Contains(live[0].Title, "1 item kept need your call") {
		t.Fatalf("growlers %+v", live)
	}
	cruftSend(p, http.MethodPost, `{"room":"alpha","kept":[]}`)
	if n := len(cruftGrowls(t, st)); n != 0 {
		t.Fatalf("%d growlers after the items went", n)
	}
}
