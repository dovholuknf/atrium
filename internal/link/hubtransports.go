package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/zitictx"
)

// The hub's own ziti transports, each one a switch in the gear.
//
//	GET /_hub/transports   {"transports": [{"name": "board-ziti", "state": "bound", ...}]}
//	PUT /_hub/transports   {"name": "ci-hook", "on": false, "service": "...", "identity": "..."}
//
// Three things bind a ziti service on the hub: the board, the room link and the CI hook. Each used to be a command
// line flag, so the only way to stop the hub's ziti traffic during a controller storm was to stop the hub. Now each is
// an item here with a stored on/off, service and identity (hub_transport.<name>.on|service|identity), and the loop
// that keeps it bound is the CI hook's (CIBinder), which backs off from 30 seconds to a cap of 10 minutes and never
// spins on a controller that refuses.
//
// A FLAG IS A SEED. One given at start wins for that run and the view says which fields it set. The first change made
// here replaces it: the effective values are stored and the item stops reading as seeded, so the next start with no
// flag keeps what the operator chose.
//
// TURNING OFF closes the item's listener at once and ends its loop. When no other transport that is on uses the same
// identity, it also closes that identity's ziti context (zitictx.Close), which ends the router channels and the
// session refresh: that is what stops the traffic. Turning on reads the identity file afresh. A ziti context is
// shared by every item on one identity, so while another is still on it the context stays.
//
// THE ROOM LINK'S ZITI IS REFUSED while a room reaches the hub only over it: a room told over the link could not be
// told to turn it back on. The refusal names the rooms.
//
// SET FROM THE HUB'S MACHINE ONLY, like the hosts: it runs against files on the hub. Reading stays open.

// The names a transport goes by.
const (
	TransportBoardZiti = "board-ziti"
	TransportRoomZiti  = "room-ziti"
	TransportCIHook    = "ci-hook"
)

// ErrUnknownTransport is Set's answer for a name the hub has no such item for.
var ErrUnknownTransport = errors.New("no such transport")

const (
	transportStop    = 5 * time.Second
	transportOffWhy  = "turned off from the settings"
	transportNeedsID = "refused: no ziti identity is set. give one here or start the hub with --atrium-identity"
)

// TransportSeed is what the command line said for one item.
type TransportSeed struct {
	On       *bool
	Service  string
	Identity string
}

// TransportItem is one thing the hub binds.
type TransportItem struct {
	Name  string
	Label string
	// Kind is the hub's listener row this item reports into (Hub.SetListener).
	Kind           string
	DefaultService string
	// Listen binds the service with the identity.
	Listen func(service, identity string) (net.Listener, error)
	// Handler is served over the listener, unless Serve is set.
	Handler http.Handler
	Serve   func(ctx context.Context, ln net.Listener) error
	// Refuse says why this may not be turned off now, empty when it may. Said as a refusal.
	Refuse func() string
	// Locked says why this cannot be switched at all, empty when it can.
	Locked string
}

// TransportView is one item as the gear shows it.
type TransportView struct {
	Name           string    `json:"name"`
	Label          string    `json:"label"`
	Kind           string    `json:"kind"`
	On             bool      `json:"on"`
	State          string    `json:"state"`
	LastError      string    `json:"last_error,omitempty"`
	Since          time.Time `json:"since,omitempty"`
	Service        string    `json:"service"`
	DefaultService string    `json:"default_service"`
	Identity       string    `json:"identity"`
	IdentityUsed   string    `json:"identity_used"`
	Seeded         []string  `json:"seeded,omitempty"`
	Locked         string    `json:"locked,omitempty"`
	Switchable     bool      `json:"switchable"`
}

type htItem struct {
	TransportItem
	on       bool
	service  string
	identity string
	seeded   map[string]bool
	cancel   context.CancelFunc
	done     chan struct{}

	smu   sync.Mutex
	up    bool
	err   string
	since time.Time
}

// HubTransports keeps the hub's transports bound, or not, as the settings say.
type HubTransports struct {
	Store HubSettings
	// Hub receives each item's listener row. May be nil.
	Hub   *Hub
	Audit func(kind, detail string)
	// IdentityFallback is the identity an item with none of its own uses: the hub's --atrium-identity.
	IdentityFallback string
	// CheckIdentity is asked before an item is turned on or given an identity. Nil accepts anything.
	CheckIdentity func(path string) error
	// CloseIdentity ends an identity's ziti context. Nil is zitictx.Close.
	CloseIdentity func(identity string)
	// Wait, First and Cap are the bind loop's, see CIBinder.
	Wait  func(ctx context.Context, d time.Duration) bool
	First time.Duration
	Cap   time.Duration

	mu    sync.Mutex
	ctx   context.Context
	items map[string]*htItem
	order []string
}

func settingKey(name, field string) string { return "hub_transport." + name + "." + field }

func (t *HubTransports) stored(name, field string) string {
	if t.Store == nil {
		return ""
	}
	v, _ := t.Store.HubSetting(settingKey(name, field))
	return strings.TrimSpace(v)
}

func (t *HubTransports) audit(kind, detail string) {
	if t.Audit != nil {
		t.Audit(kind, detail)
	}
}

// Add registers an item with what the command line said, which wins over what is stored.
func (t *HubTransports) Add(it TransportItem, seed TransportSeed) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.items == nil {
		t.items = map[string]*htItem{}
	}
	h := &htItem{TransportItem: it, seeded: map[string]bool{}}
	h.on = t.stored(it.Name, "on") == "on"
	h.service = t.stored(it.Name, "service")
	h.identity = t.stored(it.Name, "identity")
	if seed.On != nil {
		h.on, h.seeded["on"] = *seed.On, true
	}
	if s := strings.TrimSpace(seed.Service); s != "" {
		h.service, h.seeded["service"] = s, true
	}
	if s := strings.TrimSpace(seed.Identity); s != "" {
		h.identity, h.seeded["identity"] = s, true
	}
	if h.service == "" {
		h.service = it.DefaultService
	}
	if _, ok := t.items[it.Name]; !ok {
		t.order = append(t.order, it.Name)
	}
	t.items[it.Name] = h
	if t.ctx != nil && h.on {
		t.start(h)
	}
}

// Start binds every item that is on, and keeps them bound until ctx ends.
func (t *HubTransports) Start(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx = ctx
	for _, n := range t.order {
		if h := t.items[n]; h.on && h.cancel == nil {
			t.start(h)
		}
	}
}

// identityFor is the identity an item binds with: its own, the hub's, or another item's.
func (t *HubTransports) identityFor(h *htItem) string {
	if h.identity != "" {
		return h.identity
	}
	if t.IdentityFallback != "" {
		return t.IdentityFallback
	}
	for _, n := range t.order {
		if o := t.items[n]; o != h && o.identity != "" {
			return o.identity
		}
	}
	return ""
}

func sameIdentity(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if x, err := filepath.Abs(a); err == nil {
		a = x
	}
	if x, err := filepath.Abs(b); err == nil {
		b = x
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// inUse says whether an item other than except, and on, binds with this identity.
func (t *HubTransports) inUse(identity string, except *htItem) bool {
	for _, n := range t.order {
		if o := t.items[n]; o != except && o.on && sameIdentity(t.identityFor(o), identity) {
			return true
		}
	}
	return false
}

func (t *HubTransports) setState(h *htItem, up bool, err error) {
	h.smu.Lock()
	if up != h.up || h.since.IsZero() {
		h.since = time.Now()
	}
	h.up, h.err = up, ""
	if err != nil {
		h.err = pathClip(err.Error(), 300)
	}
	h.smu.Unlock()
	if t.Hub != nil {
		t.Hub.SetListener(h.Kind, up, err)
	}
}

// start runs the bind loop for an item. t.mu is held.
func (t *HubTransports) start(h *htItem) {
	if t.ctx == nil || h.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(t.ctx)
	service, identity, listen := h.service, t.identityFor(h), h.Listen
	b := &CIBinder{
		Name:    h.Name,
		Listen:  func() (net.Listener, error) { return listen(service, identity) },
		Handler: h.Handler,
		Serve:   h.Serve,
		State:   func(up bool, err error) { t.setState(h, up, err) },
		Audit:   t.Audit,
		Wait:    t.Wait,
		First:   t.First,
		Cap:     t.Cap,
	}
	done := make(chan struct{})
	h.cancel, h.done = cancel, done
	t.setState(h, false, nil)
	go func() {
		b.Run(ctx)
		close(done)
	}()
}

// stop ends an item's loop and waits for its listener to close, for a bounded time. t.mu is held.
func (t *HubTransports) stop(h *htItem) {
	if h.cancel == nil {
		return
	}
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(transportStop):
	}
	h.cancel, h.done = nil, nil
	t.setState(h, false, errors.New(transportOffWhy))
}

func (t *HubTransports) view(h *htItem) TransportView {
	h.smu.Lock()
	up, why, since := h.up, h.err, h.since
	h.smu.Unlock()
	v := TransportView{Name: h.Name, Label: h.Label, Kind: h.Kind, On: h.on, Service: h.service,
		DefaultService: h.DefaultService, Identity: h.identity, IdentityUsed: t.identityFor(h), Locked: h.Locked,
		Switchable: h.Locked == "", Since: since}
	switch {
	case !h.on:
		v.State = "off"
	case up:
		v.State = "bound"
	case why != "" && why != transportOffWhy:
		v.State, v.LastError = "failing", why
	default:
		v.State = "binding"
	}
	for _, f := range []string{"on", "service", "identity"} {
		if h.seeded[f] {
			v.Seeded = append(v.Seeded, f)
		}
	}
	return v
}

// View is every item, in the order they were added.
func (t *HubTransports) View() []TransportView {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TransportView, 0, len(t.order))
	for _, n := range t.order {
		out = append(out, t.view(t.items[n]))
	}
	return out
}

// Set turns an item on or off and changes its service or identity, each only when given. Changing the service or the
// identity of one that is on binds it again. The values in force are stored, so a flag that seeded them stops winning.
// A refusal reads "refused: ...".
func (t *HubTransports) Set(name string, on *bool, service, identity *string) (TransportView, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.items[name]
	if h == nil {
		return TransportView{}, ErrUnknownTransport
	}
	if h.Locked != "" {
		return t.view(h), errors.New("refused: " + h.Locked)
	}
	newOn, newSvc, newID := h.on, h.service, h.identity
	if on != nil {
		newOn = *on
	}
	if service != nil {
		if newSvc = strings.TrimSpace(*service); newSvc == "" {
			newSvc = h.DefaultService
		}
		if len(newSvc) > 100 || strings.ContainsAny(newSvc, " \t\r\n\x00") {
			return t.view(h), errors.New("a ziti service name has no spaces and is at most 100 characters")
		}
	}
	if identity != nil {
		if newID = strings.TrimSpace(*identity); strings.ContainsAny(newID, "\r\n\x00") || len(newID) > 500 {
			return t.view(h), errors.New("that is not a path to an identity file")
		}
	}
	changedBind := newSvc != h.service || newID != h.identity
	if h.on && !newOn && h.Refuse != nil {
		if why := h.Refuse(); why != "" {
			return t.view(h), errors.New(why)
		}
	}
	if newOn && (!h.on || changedBind) {
		use := newID
		if use == "" {
			use = t.identityFor(h)
		}
		if use == "" {
			return t.view(h), errors.New(transportNeedsID)
		}
		if t.CheckIdentity != nil {
			if err := t.CheckIdentity(use); err != nil {
				return t.view(h), err
			}
		}
	}
	if t.Store != nil {
		v := "off"
		if newOn {
			v = "on"
		}
		for field, val := range map[string]string{"on": v, "service": newSvc, "identity": newID} {
			if err := t.Store.SetHubSetting(settingKey(name, field), val); err != nil {
				return t.view(h), fmt.Errorf("could not save that: %w", err)
			}
		}
	}
	wasOn, oldIdentity := h.on, t.identityFor(h)
	h.on, h.service, h.identity = newOn, newSvc, newID
	h.seeded = map[string]bool{}
	if wasOn && (!newOn || changedBind) {
		t.stop(h)
	}
	if newOn && (!wasOn || changedBind) {
		t.start(h)
	}
	if wasOn && (!newOn || !sameIdentity(oldIdentity, t.identityFor(h))) && !t.inUse(oldIdentity, h) {
		closeIdentity := t.CloseIdentity
		if closeIdentity == nil {
			closeIdentity = zitictx.Close
		}
		closeIdentity(oldIdentity)
	}
	switch {
	case wasOn && !newOn:
		t.audit("hub-transport-off", name+" turned off from the settings")
	case newOn && !wasOn:
		t.audit("hub-transport-on", name+" turned on from the settings, service "+newSvc)
	case changedBind:
		t.audit("hub-transport-set", name+" now binds service "+newSvc)
	}
	return t.view(h), nil
}

// Effective is whether an item is on and the service it binds. An item never added is off.
func (t *HubTransports) Effective(name string) (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.items[name]
	if h == nil {
		return false, ""
	}
	return h.on, h.service
}

// OnlyVia names the attached rooms that reach this hub over a kind and have no other path that answers. A room that has
// reported no paths is counted, since nothing says it has another.
func (h *Hub) OnlyVia(kind string) []string {
	var out []string
	for _, a := range h.Rooms() {
		if a.Via != kind {
			continue
		}
		other := false
		for _, p := range a.Paths {
			if p.Kind != kind && (p.State == PathUp || p.State == PathOn) {
				other = true
			}
		}
		if !other {
			out = append(out, a.Name)
		}
	}
	sort.Strings(out)
	return out
}

// SetTransports gives the proxy the hub's transports, answered at /_hub/transports.
func (p *Proxy) SetTransports(t *HubTransports) {
	p.mu.Lock()
	p.transports = t
	p.mu.Unlock()
}

type transportsAsk struct {
	Name     string  `json:"name"`
	On       *bool   `json:"on"`
	Service  *string `json:"service"`
	Identity *string `json:"identity"`
}

// serveTransports answers GET and PUT /_hub/transports.
func (p *Proxy) serveTransports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	p.mu.Lock()
	t := p.transports
	p.mu.Unlock()
	if t == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut, http.MethodPost:
		if !edge.LocalOperator(r) {
			fail(http.StatusForbidden, "the hub's transports are switched only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var ask transportsAsk
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&ask); err != nil {
			fail(http.StatusBadRequest, `send {"name":"ci-hook","on":false}`)
			return
		}
		if _, err := t.Set(ask.Name, ask.On, ask.Service, ask.Identity); err != nil {
			switch {
			case errors.Is(err, ErrUnknownTransport):
				fail(http.StatusNotFound, "no transport called "+ask.Name)
			case strings.HasPrefix(err.Error(), "refused"):
				fail(http.StatusConflict, err.Error())
			default:
				fail(http.StatusBadRequest, err.Error())
			}
			return
		}
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"transports": t.View()})
}
