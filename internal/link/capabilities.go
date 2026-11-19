package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// What each room can do, read from the rooms and kept. See docs/design/room-capabilities.md.
//
// The rooms probe themselves. The hub only ASKS, over the link a room dialled, for `GET /v1/capabilities` (the declared
// list and the newest result of each) and stores what it hears. A run is `POST /v1/capabilities/run` on one named room,
// and the room decides what runs: the hub names ids from the room's own declared list and never a command.
//
// A room that does not answer is "not answering". Its cells go stale and are never turned to fail on that evidence.

// CapResults is the hub's store of results.
type CapResults interface {
	CapabilityPut(rows []hubstore.CapabilityRow) error
	CapabilityLatest() ([]hubstore.CapabilityRow, error)
}

const (
	capEvery = 30 * time.Second
	capBound = 10 * time.Second
)

type capRoom struct {
	Running []string
	Checked time.Time
	Since   time.Time
	Ver     int
}

type capWatch struct {
	mu    sync.Mutex
	store CapResults
	caps  map[string]json.RawMessage
	order []string
	rooms map[string]*capRoom
}

// SetCapabilities hands the proxy the store results are kept in.
func (p *Proxy) SetCapabilities(st CapResults) {
	p.cap.mu.Lock()
	defer p.cap.mu.Unlock()
	p.cap.store = st
}

type roomCapSnapshot struct {
	Version int               `json:"list_ver"`
	Caps    []json.RawMessage `json:"caps"`
	Results []struct {
		Cap     string `json:"cap"`
		State   string `json:"state"`
		Version string `json:"version"`
		Reason  string `json:"reason"`
		At      string `json:"at"`
		MS      int64  `json:"ms"`
	} `json:"results"`
	Running []string `json:"running"`
}

// RunCapabilityWatch reads attached rooms' capabilities until ctx ends.
func (p *Proxy) RunCapabilityWatch(ctx context.Context) {
	t := time.NewTicker(capEvery)
	defer t.Stop()
	for {
		p.capSweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *Proxy) capSweep(ctx context.Context) {
	var wg sync.WaitGroup
	for _, a := range p.hub.Rooms() {
		wg.Add(1)
		go func(a wire.Attached) {
			defer wg.Done()
			p.readCaps(ctx, a)
		}(a)
	}
	wg.Wait()
}

// readCaps asks one room and stores what it said. A room that fails is left as it was.
func (p *Proxy) readCaps(ctx context.Context, a wire.Attached) bool {
	ctx, cancel := context.WithTimeout(ctx, capBound)
	defer cancel()
	var snap roomCapSnapshot
	if p.setupGet(ctx, a.Name, "/v1/capabilities", &snap) != nil {
		return false
	}
	p.cap.mu.Lock()
	st := p.cap.store
	if p.cap.caps == nil {
		p.cap.caps, p.cap.rooms = map[string]json.RawMessage{}, map[string]*capRoom{}
	}
	for _, raw := range snap.Caps {
		var id struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &id) != nil || id.ID == "" {
			continue
		}
		if _, ok := p.cap.caps[id.ID]; !ok {
			p.cap.order = append(p.cap.order, id.ID)
		}
		p.cap.caps[id.ID] = raw
	}
	p.cap.rooms[wire.KeyOf(a.Name)] = &capRoom{Running: snap.Running, Checked: time.Now().UTC(), Since: a.Since, Ver: snap.Version}
	p.cap.mu.Unlock()
	if st == nil {
		return true
	}
	rows := make([]hubstore.CapabilityRow, 0, len(snap.Results))
	for _, r := range snap.Results {
		rows = append(rows, hubstore.CapabilityRow{Room: a.Name, Cap: r.Cap, ListVer: snap.Version, At: r.At,
			State: r.State, Version: r.Version, Reason: r.Reason, MS: r.MS})
	}
	_ = st.CapabilityPut(rows)
	return true
}

// CapabilityView is what the board draws, and what `atrium_capabilities` answers from.
type CapabilityView struct {
	Caps    []json.RawMessage        `json:"caps"`
	Rooms   []CapabilityRoom         `json:"rooms"`
	Results []hubstore.CapabilityRow `json:"results"`
}

// CapabilityRoom is one room's column.
type CapabilityRoom struct {
	Name    string   `json:"name"`
	Online  bool     `json:"online"`
	Checked string   `json:"checked,omitempty"`
	Running []string `json:"running,omitempty"`
}

func (p *Proxy) capabilityView() (CapabilityView, error) {
	v := CapabilityView{Caps: []json.RawMessage{}, Rooms: []CapabilityRoom{}, Results: []hubstore.CapabilityRow{}}
	p.cap.mu.Lock()
	st := p.cap.store
	for _, id := range p.cap.order {
		v.Caps = append(v.Caps, p.cap.caps[id])
	}
	known := map[string]CapabilityRoom{}
	for k, r := range p.cap.rooms {
		known[k] = CapabilityRoom{Name: k, Checked: r.Checked.Format(time.RFC3339), Running: r.Running}
	}
	p.cap.mu.Unlock()
	if st != nil {
		rows, err := st.CapabilityLatest()
		if err != nil {
			return v, err
		}
		v.Results = append(v.Results, rows...)
		for _, r := range rows {
			if _, ok := known[wire.KeyOf(r.Room)]; !ok {
				known[wire.KeyOf(r.Room)] = CapabilityRoom{Name: r.Room}
			}
		}
	}
	for _, a := range p.hub.Rooms() {
		c := known[wire.KeyOf(a.Name)]
		c.Name, c.Online = a.Name, true
		known[wire.KeyOf(a.Name)] = c
	}
	for _, c := range known {
		v.Rooms = append(v.Rooms, c)
	}
	sort.Slice(v.Rooms, func(i, j int) bool { return v.Rooms[i].Name < v.Rooms[j].Name })
	return v, nil
}

// serveCapabilities is `GET /_hub/capabilities`, and `POST /_hub/capabilities?room=<name>&cap=<id>` to run: one named
// room, never an unnamed write. `room=all` asks every attached room.
func (p *Proxy) serveCapabilities(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		v, err := p.capabilityView()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"the hub could not read capability results"}`))
			return
		}
		for _, c := range v.Rooms {
			if len(c.Running) > 0 {
				for _, a := range p.hub.Rooms() {
					if wire.KeyOf(a.Name) == wire.KeyOf(c.Name) {
						go p.readCaps(context.Background(), a)
					}
				}
			}
		}
		_ = json.NewEncoder(w).Encode(v)
	case http.MethodPost:
		p.runCapabilities(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Proxy) runCapabilities(w http.ResponseWriter, r *http.Request) {
	room := strings.TrimSpace(r.URL.Query().Get("room"))
	if room == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"name the room to run on, or room=all"}`))
		return
	}
	var ids []string
	if c := strings.TrimSpace(r.URL.Query().Get("cap")); c != "" {
		ids = strings.Split(c, ",")
	}
	body, _ := json.Marshal(map[string]any{"caps": ids})
	var targets []wire.Attached
	for _, a := range p.hub.Rooms() {
		if room == "all" || wire.KeyOf(a.Name) == wire.KeyOf(room) {
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"that room is not attached, so it cannot run its probes"}`))
		return
	}
	asked, unreached, busy := []string{}, map[string]string{}, 0
	for _, a := range targets {
		ctx, cancel := context.WithTimeout(r.Context(), capBound)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+hostFor(a.Name)+"/v1/capabilities/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := p.roomClient(a.Name).Do(req)
		switch {
		case err != nil:
			unreached[a.Name] = err.Error()
		default:
			msg, _ := io.ReadAll(io.LimitReader(res.Body, 1<<12))
			res.Body.Close()
			if res.StatusCode == http.StatusAccepted {
				asked = append(asked, a.Name)
			} else {
				if res.StatusCode == http.StatusConflict {
					busy++
				}
				unreached[a.Name] = strings.TrimSpace(string(msg))
			}
		}
		cancel()
		go p.readCaps(context.Background(), a)
	}
	code := http.StatusAccepted
	switch {
	case len(asked) > 0:
	case busy == len(targets):
		// A room still on its first sweep refuses a second run. It was reached, so this is no gateway failure.
		code = http.StatusConflict
	default:
		code = http.StatusBadGateway
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"asked": asked, "unreached": unreached})
}
