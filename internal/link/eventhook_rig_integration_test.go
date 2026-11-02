//go:build integration

package link

import (
	"encoding/json"
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

func TestEventsThroughAShareOrAProxyAre403(t *testing.T) {
	x := newEventRig(t)
	p := NewProxy(NewHub(Timings{}), nil, "", nil)
	p.SetEvents(x.hook)
	post := func(remote, host string, hdr map[string]string) int {
		r := httptest.NewRequest(http.MethodPost, "/_hub/events", strings.NewReader(landBodyJSON("stopped", "", "", 0, host+remote)))
		r.RemoteAddr, r.Host = remote, host
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w.Code
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", nil); c != http.StatusAccepted {
		t.Fatalf("local: %d", c)
	}
	if c := post("127.0.0.1:5000", "hub.example.com", nil); c != http.StatusForbidden {
		t.Fatalf("a proxied host: %d", c)
	}
	if c := post("127.0.0.1:5000", "127.0.0.1:7778", map[string]string{"X-Forwarded-For": "1.2.3.4"}); c != http.StatusForbidden {
		t.Fatalf("a forwarded request: %d", c)
	}
	if c := post("10.0.0.5:5000", "127.0.0.1:7778", nil); c != http.StatusForbidden {
		t.Fatalf("a remote caller: %d", c)
	}
	r := httptest.NewRequest(http.MethodPost, "/_hub/events/subs", strings.NewReader(`{"action":"list"}`))
	r.RemoteAddr, r.Host = "10.0.0.5:5000", "127.0.0.1:7778"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("subscriptions from a remote caller: %d", w.Code)
	}
}

func TestARoomsRelayedSubscribeIsStampedFromTheLink(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	x.proxy.SetEvents(forgepkg.NewEventHook(st, x.proxy.Deliverer(), nil))
	ask := func(path, body string) RelayAnswer {
		ans, err := x.miniR.Relay(relayCtx(t), wire.RelayRequest{Op: RelayHub, From: "sa1", Method: "POST", Path: path, Body: json.RawMessage(body)})
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"m1"}`); !ans.OK {
		t.Fatalf("own card: %+v", ans)
	}
	if rows, _ := st.EventSubsOf("m1mini~m1"); len(rows) != 1 || rows[0].CreatedBy != "sa1@m1mini" {
		t.Fatalf("rows %+v", rows)
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"sg4~s1"}`); ans.OK || ans.Code != http.StatusForbidden {
		t.Fatalf("another room's card: %+v", ans)
	}
	if ans := ask(hubSubsPath, `{"action":"subscribe","kind":"land","card":"m1","room":"sg4"}`); !ans.OK {
		t.Fatalf("a room named in the body: %+v", ans)
	}
	if rows, _ := st.EventSubsOf("sg4~m1"); len(rows) != 0 {
		t.Fatalf("a room's own say of its name wrote %+v", rows)
	}
	if ans := ask("/_hub/events", landBodyJSON("stopped", "", "", 0, "")); ans.OK {
		t.Fatalf("a room posted an event: %+v", ans)
	}
}

func TestTheProxyDeliversAnEventToACardOnARoom(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hook := x.proxy.NewEventHook(st, "land@testhub", nil)
	x.proxy.SetEvents(hook)
	post := func(path, body string) *http.Response {
		res, err := http.Post(x.board+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := post("/_hub/events/subs", `{"action":"subscribe","kind":"land","card":"sg4~s1","wake":true}`); res.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}
	if res := post("/_hub/events", landBodyJSON("pushed", landFrom, landTo, 14, "")); res.StatusCode != http.StatusAccepted {
		t.Fatalf("event: %d", res.StatusCode)
	}
	hook.Pass(relayCtx(t))
	got := x.sg4.messages()
	if len(got) != 1 || got[0]["id"] != "s1" || got[0]["from"] != "land@testhub" || got[0]["kind"] != "fyi" ||
		!strings.HasPrefix(got[0]["text"], "Landed: o/r main") {
		t.Fatalf("sg4 got %+v", got)
	}
}
