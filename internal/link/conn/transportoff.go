package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Turning a transport off and on, without restarting the room.
//
// A room keeps its rungs and a set of kinds that are OFF. An off rung is never dialled, never probed and never climbed
// onto, and its dialer is closed, which for ziti ends the context and every goroutine it started. The choice is written
// to transports.json (`off`), so a restart keeps it. Turning on clears it and the next climb or probe finds the rung
// again: the identity is read afresh, because a ziti dialer builds its context on its first dial.
//
// THE LAST WORKING PATH IS NEVER TURNED OFF. A room is told over the link, and a room with no other way back to its hub
// could not be told to turn anything on again. The room checks this itself with a fresh probe rather than trusting a
// beat that may be a minute old.

// canSwitch is the kinds. Direct has no overlay to close, and a room that could drop it is a room somebody cannot reach.
func canSwitch(kind string) bool { return kind == "ziti" || kind == "zrok" }

// offer is a Dialer whose transports can be turned off. `Ladder` is the one there is.
type offer interface {
	SetOff(kind string, off bool) bool
	Off() []string
	IsOff(kind string) bool
	WorkingElsewhere(ctx context.Context, kind string, wait time.Duration) bool
	OnKind() string
	Hammering() map[string]bool
}

type hammerer interface{ Hammering() bool }

// SetOff turns a kind off or on and says whether that changed anything. Turning off closes the dialers of that kind.
func (l *Ladder) SetOff(kind string, off bool) bool {
	l.mu.Lock()
	if l.off == nil {
		l.off = map[string]bool{}
	}
	if l.off[kind] == off {
		l.mu.Unlock()
		return false
	}
	if off {
		l.off[kind] = true
	} else {
		delete(l.off, kind)
	}
	var ds []Dialer
	if off {
		for _, r := range l.rungs {
			if r.Path.Transport == kind {
				ds = append(ds, r.Dial)
			}
		}
	}
	l.mu.Unlock()
	closeDialers(ds)
	return true
}

// Off is the kinds that are off, sorted.
func (l *Ladder) Off() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for k := range l.off {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsOff says whether a kind is off.
func (l *Ladder) IsOff(kind string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.off[kind]
}

// OnKind is the kind of the rung the ladder stands on, "" when it has none.
func (l *Ladder) OnKind() string {
	r, _, ok := l.current()
	if !ok {
		return ""
	}
	return r.Path.Transport
}

// live is the rungs that are not off.
func (l *Ladder) live() []Rung {
	var out []Rung
	for _, r := range l.Rungs() {
		if !l.IsOff(r.Path.Transport) {
			out = append(out, r)
		}
	}
	return out
}

// WorkingElsewhere says whether some rung of another kind, not off, answers a dial right now. Closes what it opens.
func (l *Ladder) WorkingElsewhere(ctx context.Context, kind string, wait time.Duration) bool {
	for _, r := range l.live() {
		if r.Path.Transport == kind {
			continue
		}
		if c, err := dialWithin(ctx, r.Dial, wait); err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// Hammering is which kinds are failing dials fast enough to flag.
func (l *Ladder) Hammering() map[string]bool {
	out := map[string]bool{}
	for _, r := range l.Rungs() {
		d := r.Dial
		if p, ok := d.(Proven); ok {
			d = p.Dialer
		}
		if h, ok := d.(hammerer); ok && h.Hammering() {
			out[r.Path.Transport] = true
		}
	}
	return out
}

// errTransportOff is what a dial on an off rung answers, which only a race can reach.
var errTransportOff = errors.New("that transport is turned off")

// ── the room's side ──────────────────────────────────────

// TransportAsk is the body of POST /v1/link/transports.
type TransportAsk struct {
	Transport string `json:"transport"`
	On        bool   `json:"on"`
}

// TransportState is one kind on the room, for GET and for the answer to a POST.
type TransportState struct {
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Off        bool   `json:"off,omitempty"`
	Hammering  bool   `json:"hammering,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	Switchable bool   `json:"switchable,omitempty"`
}

// ErrLastPath is the refusal for turning off the way a room reaches its hub with no other left.
func ErrLastPath(kind string) error {
	return fmt.Errorf("refused: %s is this room's last working path to the hub, so it stays on", kind)
}

// SetTransport turns one transport off or on. It refuses the last working path, writes the choice down through
// PersistOff, and when the link is standing on the kind it lets go so the room climbs onto another.
func (r *Room) SetTransport(ctx context.Context, kind string, on bool) error {
	l, ok := r.Dial.(offer)
	if !ok {
		return errors.New("this room has one transport and nothing to switch")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "openziti" {
		kind = "ziti"
	}
	if !canSwitch(kind) {
		return fmt.Errorf("%q cannot be turned off here. one of: ziti, zrok", kind)
	}
	r.tmu.Lock()
	defer r.tmu.Unlock()
	if on == !l.IsOff(kind) {
		return nil
	}
	if !on {
		wait := r.T.DialWait
		if wait <= 0 {
			wait = DefaultTimings().DialWait
		}
		if !l.WorkingElsewhere(ctx, kind, wait) {
			return ErrLastPath(kind)
		}
	}
	l.SetOff(kind, !on)
	if r.PersistOff != nil {
		if err := r.PersistOff(l.Off()); err != nil {
			log.Printf("[link] could not write the transport choice down: %v", err)
		}
	}
	if on {
		log.Printf("[link] transport %s turned on", kind)
		return nil
	}
	log.Printf("[link] transport %s turned off", kind)
	if l.OnKind() == kind {
		r.mu.Lock()
		drop := r.dropLink
		r.mu.Unlock()
		if drop != nil {
			r.forceMove.Store(true)
			drop()
		}
	}
	return nil
}

// Transports is the state of every kind on this room.
func (r *Room) Transports() []TransportState {
	var out []TransportState
	for _, p := range r.pathsNow() {
		out = append(out, TransportState{Kind: p.Kind, State: p.State, Off: p.State == PathOff,
			Hammering: p.Hammering, LastError: p.LastError, Switchable: canSwitch(p.Kind)})
	}
	return out
}

// transportsHandler answers GET and POST /v1/link/transports, on the link only.
func (r *Room) transportsHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case http.MethodGet:
	case http.MethodPost:
		var ask TransportAsk
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4<<10)).Decode(&ask); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "send {\"transport\":\"ziti\",\"on\":false}"})
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
		defer cancel()
		if err := r.SetTransport(ctx, ask.Transport, ask.On); err != nil {
			status := http.StatusBadRequest
			if strings.HasPrefix(err.Error(), "refused") {
				status = http.StatusConflict
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var off []string
	if l, ok := r.Dial.(offer); ok {
		off = l.Off()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"transports": r.Transports(), "off": off})
}
