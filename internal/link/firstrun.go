package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
)

// FirstRun is what the board's first-run panel asks of the hub: who this machine is, a room for this machine, and a
// join string for another one. The cli supplies it, since it owns the room's launch and the keys.
type FirstRun interface {
	Facts() FirstRunFacts
	LocalRoom(name string) error
	JoinString(name string) (string, error)
}

// FirstRunFacts is what the connect panel prefills. User is who the room runs as.
type FirstRunFacts struct {
	Machine string `json:"machine"`
	User    string `json:"user"`
	Room    string `json:"room"`
	OS      string `json:"os"`
}

// SetFirstRun wires the first-run answers. A hub run with `--no-room` and no way to start one never calls it.
func (p *Proxy) SetFirstRun(f FirstRun) {
	p.mu.Lock()
	p.firstRun = f
	p.mu.Unlock()
}

// serveFirstRun answers GET /_hub/firstrun, POST /_hub/firstrun/room and POST /_hub/firstrun/join-string. Both writes
// are for the machine the hub runs on, since one starts a process there and the other mints a credential.
func (p *Proxy) serveFirstRun(w http.ResponseWriter, r *http.Request, sub string) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	p.mu.Lock()
	f := p.firstRun
	p.mu.Unlock()
	if f == nil {
		fail(http.StatusNotFound, "this hub cannot make rooms from the board. run `atrium rooms add <name>` on the hub's machine")
		return
	}
	if sub == "firstrun" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		_ = json.NewEncoder(w).Encode(f.Facts())
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(http.StatusMethodNotAllowed, "that has to be a POST")
		return
	}
	if !edge.LocalOperator(r) {
		fail(http.StatusForbidden, "rooms are made only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, "could not read that: send {\"name\": \"room-name\"}")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		fail(http.StatusBadRequest, "a room needs a name. type one, such as the name of this machine")
		return
	}
	switch sub {
	case "firstrun/room":
		if err := f.LocalRoom(name); err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.RecordAudit(name, "room-made-from-board", "this machine's room")
		_ = json.NewEncoder(w).Encode(map[string]string{"room": name})
	case "firstrun/join-string":
		line, err := f.JoinString(name)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.RecordAudit(name, "join-string-made-from-board", "for another machine")
		_ = json.NewEncoder(w).Encode(map[string]string{"room": name, "line": line})
	default:
		fail(http.StatusNotFound, "no such first-run call")
	}
}
