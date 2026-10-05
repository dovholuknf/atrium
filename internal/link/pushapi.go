package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The hub's Web Push endpoints, under /_hub/push, JSON.
//
//	GET    /_hub/push                       the switch, the contact and the subscriptions   operator
//	PUT    /_hub/push                       {enabled, contact, rotate}                       operator
//	POST   /_hub/push/test                  a test push to every subscription                operator
//	DELETE /_hub/push/subscriptions/<id>    remove one                                       operator
//	GET    /_hub/push/key                   the VAPID public key, 404 while push is off      anyone who reaches /_hub
//	POST   /_hub/push/subscriptions         {endpoint, keys, label, origin}                  the share, while push is on
//	DELETE /_hub/push/subscriptions         {endpoint}                                       the share, its own browser
//
// WHO MAY DO WHAT is docs/rnd/web-push-design.md "LP1 and the overlay rule". Turning push on, rotating the key,
// changing the contact, the test push to everybody, listing and removing by id are the OPERATOR's: edge.LocalOperator,
// the same test the notify command's PUT uses, so a request that came through zrok or any proxy is a 403. The share
// may subscribe and unsubscribe its own browser. A subscription is far less power than approving a permission, which
// anybody past the share's password can already do, and the hub POSTs only to an allowlisted push host.
//
// THE ENDPOINT IS THE PROOF OF OWNERSHIP. A removal from the share names the endpoint, which only the browser that
// subscribed knows. The id is not a secret and is refused there. Neither the endpoint nor a key is ever in an answer:
// the list says the push service's host and nothing more of the endpoint.

// SetPush wires Web Push. Optional: a hub without it answers the push routes 404.
func (p *Proxy) SetPush(x *Push) {
	x.announce = p.pushNewDevice
	p.mu.Lock()
	p.push = x
	p.mu.Unlock()
}

func (p *Proxy) pusher() *Push {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.push
}

// pushNewDevice puts a new device in front of the operator: the audit feed, and a growler on the desktop board, so a
// stranger who has the share's password and subscribes is seen.
func (p *Proxy) pushNewDevice(id, label, origin string) {
	line := "a new device subscribed to alerts: " + label + ", from " + origin
	p.RecordAudit("", "push-subscribed", line)
	g := p.growler()
	if g == nil {
		return
	}
	rs, ok := g.st.(growlRaiser)
	if !ok {
		return
	}
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		return
	}
	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	raised, err := rs.Raise(names[0], GrowlRow{ID: "push|" + id, Reason: ReasonQuestion, Subject: id,
		Title: line, Body: "Remove it in the desktop gear if it is not yours."})
	if err == nil && raised {
		g.publish(nil)
	}
}

// pushSubView is a subscription as the gear reads it.
type pushSubView struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Origin         string `json:"origin"`
	Service        string `json:"service"`
	CreatedAt      string `json:"created_at"`
	Failures       int    `json:"failures"`
	DisabledReason string `json:"disabled_reason"`
}

func pushViewOf(s hubstore.PushSub) pushSubView {
	svc := ""
	if u, err := url.Parse(s.Endpoint); err == nil {
		svc = u.Hostname()
	}
	return pushSubView{ID: s.ID, Label: s.Label, Origin: s.Origin, Service: svc, CreatedAt: s.CreatedAt,
		Failures: s.Failures, DisabledReason: s.DisabledReason}
}

func (p *Proxy) servePush(w http.ResponseWriter, r *http.Request, rest string) {
	x := p.pusher()
	if x == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	// A page on another origin cannot write here, as the documents routes do it. A browser on the operator's machine
	// that opens somebody's page must not be made to turn push on.
	if write {
		if err := docsCrossOrigin.Check(r); err != nil {
			fail(http.StatusForbidden, "a page on another origin cannot write push settings here")
			return
		}
	}
	operator := func() bool {
		if edge.LocalOperator(r) {
			return true
		}
		fail(http.StatusForbidden, "push is turned on, set and listed only from the machine the hub runs on. "+
			"it is not reachable over an overlay"+edge.ProxyNote(r))
		return false
	}
	switch {
	case rest == "key":
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		key, ok := x.PublicKey()
		if !ok {
			fail(http.StatusNotFound, "push is not turned on at this hub")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"key": key})
	case rest == "":
		if !operator() {
			return
		}
		switch r.Method {
		case http.MethodGet:
			p.pushStatus(w, x)
		case http.MethodPut:
			p.pushConfigure(w, r, x, fail)
		default:
			fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		}
	case rest == "test":
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "that has to be a POST")
			return
		}
		if !operator() {
			return
		}
		if !x.On() {
			fail(http.StatusConflict, "push is not turned on")
			return
		}
		results := x.TestAll(r.Context())
		ok := 0
		first := ""
		for _, res := range results {
			if res.OK {
				ok++
			} else if first == "" {
				first = res.Err
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": len(results), "ok": ok, "error": first})
	case rest == "subscriptions":
		switch r.Method {
		case http.MethodPost:
			p.pushSubscribe(w, r, x, fail)
		case http.MethodDelete:
			p.pushUnsubscribe(w, r, x, fail)
		default:
			fail(http.StatusMethodNotAllowed, "that has to be a POST or a DELETE")
		}
	case strings.HasPrefix(rest, "subscriptions/") && !strings.Contains(strings.TrimPrefix(rest, "subscriptions/"), "/"):
		if r.Method != http.MethodDelete {
			fail(http.StatusMethodNotAllowed, "that has to be a DELETE")
			return
		}
		if !operator() {
			return
		}
		id := strings.TrimPrefix(rest, "subscriptions/")
		if err := x.st.PushRemoveID(id); err != nil {
			if errors.Is(err, hubstore.ErrNoPushSub) {
				fail(http.StatusNotFound, "no subscription has that id")
				return
			}
			fail(http.StatusInternalServerError, "could not remove it")
			return
		}
		x.dropped(id)
		p.RecordAudit("", "push-removed", "a device was removed from alerts")
		if g := p.growler(); g != nil {
			if rs, ok := g.st.(growlRaiser); ok {
				if changed, _ := rs.End("push|" + id); changed {
					g.publish(nil)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

func (p *Proxy) pushStatus(w http.ResponseWriter, x *Push) {
	subs, err := x.st.PushSubs()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":%q}`, "could not read the subscriptions")
		return
	}
	views := make([]pushSubView, 0, len(subs))
	for _, s := range subs {
		views = append(views, pushViewOf(s))
	}
	out := map[string]any{"enabled": x.On(), "contact": x.Contact(), "max": hubstore.PushMax, "subscriptions": views}
	if key, ok := x.PublicKey(); ok {
		out["key"] = key
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (p *Proxy) pushConfigure(w http.ResponseWriter, r *http.Request, x *Push, fail func(int, string)) {
	var body struct {
		Enabled *bool   `json:"enabled"`
		Contact *string `json:"contact"`
		Rotate  bool    `json:"rotate"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&body); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if body.Contact != nil {
		if err := x.SetContact(*body.Contact); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
	}
	if body.Rotate {
		n, err := x.Rotate()
		if err != nil {
			fail(http.StatusInternalServerError, "could not make a new key")
			return
		}
		p.RecordAudit("", "push-key-rotated", fmt.Sprintf("%d subscriptions were removed with the old key", n))
	}
	if body.Enabled != nil {
		if err := x.SetEnabled(*body.Enabled); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		p.RecordAudit("", "push-configured", fmt.Sprintf("enabled=%v", *body.Enabled))
	}
	p.pushStatus(w, x)
}

func (p *Proxy) pushSubscribe(w http.ResponseWriter, r *http.Request, x *Push, fail func(int, string)) {
	if !x.On() {
		fail(http.StatusConflict, "push is not turned on at this hub")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Label  string `json:"label"`
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&body); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	id, _, err := x.Subscribe(hubstore.PushSub{Endpoint: body.Endpoint, P256dh: body.Keys.P256dh, Auth: body.Keys.Auth,
		Label: body.Label, Origin: body.Origin})
	switch {
	case errors.Is(err, hubstore.ErrPushFull):
		fail(http.StatusConflict, err.Error())
		return
	case err != nil:
		fail(http.StatusBadRequest, err.Error())
		return
	}
	// THE ONE TEST PUSH, after the 201 is decided and not before it is answered: a slow push service must not hold the
	// phone's switch.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*pushSendTimeout)
		defer cancel()
		x.Test(ctx, id)
	}()
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (p *Proxy) pushUnsubscribe(w http.ResponseWriter, r *http.Request, x *Push, fail func(int, string)) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil || body.Endpoint == "" {
		fail(http.StatusBadRequest, "name the endpoint to remove in the body")
		return
	}
	subs, _ := x.st.PushSubs()
	id := ""
	for _, s := range subs {
		if s.Endpoint == body.Endpoint {
			id = s.ID
		}
	}
	if err := x.st.PushRemoveEndpoint(body.Endpoint); err != nil {
		if errors.Is(err, hubstore.ErrNoPushSub) {
			fail(http.StatusNotFound, "that subscription is already gone")
			return
		}
		fail(http.StatusInternalServerError, "could not remove it")
		return
	}
	x.dropped(id)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
