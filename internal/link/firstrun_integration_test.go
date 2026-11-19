//go:build integration

package link

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeFirstRun struct{ made, joined string }

func (f *fakeFirstRun) Facts() FirstRunFacts {
	return FirstRunFacts{Machine: "box", User: "pat", Room: "box", OS: "linux"}
}
func (f *fakeFirstRun) LocalRoom(name string) error {
	if name == "taken" {
		return errors.New("this machine's room already answers")
	}
	f.made = name
	return nil
}
func (f *fakeFirstRun) JoinString(name string) (string, error) {
	f.joined = name
	return "atr1_x_" + name, nil
}

func firstRunCall(p *Proxy, method, sub, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/_hub/"+sub, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Host = "127.0.0.1:7778"
	p.serveFirstRun(rec, r, sub)
	return rec
}

func TestFirstRunFactsRoomAndJoinString(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	f := &fakeFirstRun{}
	p.SetFirstRun(f)
	if rec := firstRunCall(p, http.MethodGet, "firstrun", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"user":"pat"`) {
		t.Fatalf("facts: %d %s", rec.Code, rec.Body)
	}
	if rec := firstRunCall(p, http.MethodPost, "firstrun/room", `{"name":" box "}`); rec.Code != 200 || f.made != "box" {
		t.Fatalf("room: %d %s made=%q", rec.Code, rec.Body, f.made)
	}
	if rec := firstRunCall(p, http.MethodPost, "firstrun/join-string", `{"name":"other"}`); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), "atr1_x_other") {
		t.Fatalf("join string: %d %s", rec.Code, rec.Body)
	}
}

func TestFirstRunRefusesWhatItCannotDoAndSaysWhatToDo(t *testing.T) {
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	if rec := firstRunCall(p, http.MethodGet, "firstrun", ""); rec.Code != 404 || !strings.Contains(rec.Body.String(), "atrium rooms add") {
		t.Fatalf("a hub with no first-run: %d %s", rec.Code, rec.Body)
	}
	p.SetFirstRun(&fakeFirstRun{})
	if rec := firstRunCall(p, http.MethodPost, "firstrun/room", `{"name":""}`); rec.Code != 400 {
		t.Fatalf("empty name: %d %s", rec.Code, rec.Body)
	}
	if rec := firstRunCall(p, http.MethodPost, "firstrun/room", `{"name":"taken"}`); rec.Code != 409 {
		t.Fatalf("room already there: %d %s", rec.Code, rec.Body)
	}
	if rec := firstRunCall(p, http.MethodGet, "firstrun/room", ""); rec.Code != 405 {
		t.Fatalf("a GET on a write: %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/_hub/firstrun/room", strings.NewReader(`{"name":"x"}`))
	r.RemoteAddr = "203.0.113.9:50000"
	p.serveFirstRun(rec, r, "firstrun/room")
	if rec.Code != 403 {
		t.Fatalf("from off the hub's machine: %d %s", rec.Code, rec.Body)
	}
}
