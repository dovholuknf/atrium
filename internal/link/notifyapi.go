package link

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// The hub's notify endpoints, under /_hub/ like audit and nudge.
//
//	GET  /_hub/notify        the setting, the failure state and the counters
//	PUT  /_hub/notify        {enabled, command}
//	POST /_hub/notify/test   run the command once, now
//	POST /_hub/presence      {visible, tab}, from the desktop board
//
// THESE ARE THE HUB'S OWN AND ARE NOT PROXIED TO A ROOM. Setting the command or
// running the test runs a program on the hub's machine, so the question is who
// can reach `/_hub/*`. A lent session is served by a room daemon's guest
// listener, an allowlist that has no `/_hub/` in it, wrapping that room's own
// board and not this hub's, and this hub serves no guest listener at all (see
// the guests note in events.go). So there is no guest to refuse here. If a hub
// ever serves one, refuse PUT and test for it in this file before that listener
// exists.
//
// PUT AND TEST ARE LOOPBACK ONLY (f-024). This header used to say the board is
// trusted here the way it is for launching a session. But a board reached over
// an overlay, the phone for one, is not the operator at this machine, and a
// command the hub runs is exactly what must not be settable from there. GET and
// presence stay open: the phone reads the setting and says it is visible.

// SetNotify wires the notifier. Optional: a hub without one answers the notify
// routes 404 and takes presence and ignores it.
func (p *Proxy) SetNotify(n *Notifier) {
	p.mu.Lock()
	p.notify = n
	p.mu.Unlock()
}

func (p *Proxy) notifier() *Notifier {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.notify
}

// serveNotify answers the three notify routes.
func (p *Proxy) serveNotify(w http.ResponseWriter, r *http.Request, sub string) {
	n := p.notifier()
	if n == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	// SETTING THE COMMAND AND RUNNING IT ARE LOOPBACK ONLY, like /_hub/mcp. A
	// PUT names a program the hub runs and the test runs it, so neither may be
	// reachable over an overlay, however the board got there. Reading the
	// setting is harmless and stays open. See the note at the top of this file.
	if (sub == "notify" && r.Method == http.MethodPut) || sub == "notify/test" {
		if !loopbackRemote(r.RemoteAddr) {
			fail(http.StatusForbidden, "the notify command is set and tested only from the machine the hub "+
				"runs on. it is not reachable over an overlay.")
			return
		}
	}
	switch sub {
	case "notify":
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(n.Status())
		case http.MethodPut:
			var body struct {
				Enabled bool     `json:"enabled"`
				Command []string `json:"command"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
				fail(http.StatusBadRequest, "could not read that: "+err.Error())
				return
			}
			if err := n.Configure(body.Enabled, body.Command); err != nil {
				fail(http.StatusBadRequest, err.Error())
				return
			}
			p.RecordAudit("", "notify-configured", fmt.Sprintf("enabled=%v", body.Enabled))
			_ = json.NewEncoder(w).Encode(n.Status())
		default:
			fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		}
	case "notify/test":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "that has to be a POST")
			return
		}
		res, err := n.Test(r.Context())
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

// servePresence is a desktop tab saying it is visible or hidden.
//
// THE BODY IS READ AS JSON WHATEVER ITS CONTENT TYPE, because a page closing
// sends its `visible: false` with sendBeacon, which posts text/plain. Always
// answered ok: a tab does not care.
func (p *Proxy) servePresence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, `{"error":%q}`, "that has to be a POST")
		return
	}
	n := p.notifier()
	if n != nil {
		var body struct {
			Visible bool   `json:"visible"`
			Tab     string `json:"tab"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err == nil {
			n.pres.Set(body.Tab, body.Visible)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
