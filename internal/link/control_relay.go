package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The hub's half of a message between rooms. See docs/fabric/cross-room-say-design.md.
//
// Everything here goes through the hub's OWN board over loopback, scoped with
// `X-Atrium-Room` to the target, exactly as a scoped board would. So a target
// room resolves the name and delivers the message the way it delivers any peer
// message, and this file learns nothing about how.

// relay answers one relay request from the room `from`.
func (c *controlMCP) relay(ctx context.Context, from string, req RelayRequest) RelayAnswer {
	switch req.Op {
	case RelaySay:
		sender := strings.TrimSpace(req.From)
		if sender == "" {
			// RULE 3, said out loud. An unnamed message is the operator's, and a
			// message from another room is never the operator's.
			return RelayAnswer{Code: http.StatusBadRequest,
				Error: "a message to another room needs the session sending it, or it would be typed as the operator"}
		}
		target := strings.TrimSpace(req.Room)
		if target == "" || equalFold(target, from) {
			return RelayAnswer{Code: http.StatusBadRequest,
				Error: "the relay carries messages to another room. this one names the room it came from"}
		}
		if ans, ok := c.reachable(ctx, target); !ok {
			return ans
		}
		return c.deliverAcross(ctx, target, req.To, sender+"@"+from, req.Text, req.When)
	case RelayCard, RelayExit:
		target := strings.TrimSpace(req.Room)
		if target == "" || equalFold(target, from) {
			return RelayAnswer{Code: http.StatusBadRequest,
				Error: "the relay reaches a card on another room. this one names the room it came from"}
		}
		if ans, ok := c.reachable(ctx, target); !ok {
			return ans
		}
		if req.Op == RelayExit {
			return c.exitAcross(ctx, target, req.To)
		}
		return c.cardAcross(ctx, target, req.To, req.Events)
	case RelayLaunch:
		return c.launchAcross(ctx, from, req)
	case RelayFind:
		card, code, err := c.hub.lookupEverywhere(from, req.To)
		if err != nil {
			return RelayAnswer{Code: code, Error: err.Error()}
		}
		return RelayAnswer{OK: true, To: card.Wire + "@" + card.Room, Card: tagFor(card.Room, card.ID)}
	case RelayPeers:
		if req.Everywhere && c.hub != nil {
			// ONLY THE CARDS TAGGED atrium:everywhere, from the index, with no
			// question put to any room.
			peers := []RelayPeer{}
			for _, e := range c.hub.every.all(from) {
				p := e.asPeer()
				peers = append(peers, RelayPeer{Handle: p.Handle, Alias: p.Alias, Card: p.Card, Room: p.Room,
					Title: p.Title, Status: p.Status, Everywhere: true})
			}
			return RelayAnswer{OK: true, Peers: peers}
		}
		peers, quiet := c.peersElsewhere(ctx, from, req.All)
		ans := RelayAnswer{OK: true, Peers: peers}
		if len(quiet) > 0 {
			ans.Warning = "not answering: " + strings.Join(quiet, ", ")
		}
		return ans
	}
	return RelayAnswer{Code: http.StatusBadRequest, Error: fmt.Sprintf("this hub does not know the relay op %q", req.Op)}
}

// reachable is whether a room is attached, and when it is not, whether it is a
// room this hub knows at all. A room it knows is worth holding a message for.
// A name it has never heard of is a typo, and holding that for a day helps
// nobody.
func (c *controlMCP) reachable(ctx context.Context, room string) (RelayAnswer, bool) {
	if c.hub == nil || c.hub.Has(room) {
		return RelayAnswer{}, true
	}
	down := RelayAnswer{Unreachable: true, Code: http.StatusServiceUnavailable,
		Error: "the room " + room + " is not attached to the hub right now"}
	var inv struct {
		Durable bool `json:"durable"`
		Rooms   []struct {
			Name string `json:"name"`
		} `json:"rooms"`
	}
	if err := c.ask(ctx, http.MethodGet, "/_hub/inventory", "", nil, &inv); err != nil {
		return down, false
	}
	names := make([]string, 0, len(inv.Rooms))
	for _, r := range inv.Rooms {
		if equalFold(r.Name, room) {
			return down, false
		}
		names = append(names, r.Name)
	}
	msg := fmt.Sprintf("no room called %q is attached", room)
	if inv.Durable {
		msg = fmt.Sprintf("this hub knows no room called %q", room)
	}
	if len(names) > 0 {
		msg += ". rooms: " + strings.Join(names, ", ")
	}
	return RelayAnswer{Code: http.StatusNotFound, Error: msg}, false
}

// deliverAcross resolves `to` on `room` and posts the message there, from
// `fromWire`, which is always `handle@room`.
func (c *controlMCP) deliverAcross(ctx context.Context, room, to, fromWire, text, when string) RelayAnswer {
	id, handle, err := c.resolvePeer(ctx, room, to)
	if err != nil {
		return refusal(err)
	}
	body := map[string]string{"text": text, "from": fromWire}
	if w := strings.TrimSpace(when); w != "" {
		body["when"] = w
	}
	var res struct {
		Delivered string `json:"delivered"`
		Warning   string `json:"warning"`
		When      string `json:"when"`
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message", room, body, &res); err != nil {
		// PAST THE POINT OF NO RETURN. The post may have landed before the
		// answer was lost, so a room that is not answering here is unconfirmed,
		// not unreachable, and the sender's room must not send it again.
		ans := refusal(err)
		if ans.Unreachable {
			ans.Unreachable, ans.Unconfirmed = false, true
		}
		return ans
	}
	return RelayAnswer{OK: true, Delivered: res.Delivered, When: res.When, Warning: res.Warning,
		To: handle + "@" + room, Card: tagFor(room, id)}
}

// cardAcross reads `to` on `room`, named across, for a room's atrium_task.
func (c *controlMCP) cardAcross(ctx context.Context, room, to string, withEvents bool) RelayAnswer {
	id, _, err := c.resolvePeer(ctx, room, to)
	if err != nil {
		return refusal(err)
	}
	t, events, _, err := c.readCard(ctx, room, id, withEvents, false)
	if err != nil {
		return refusal(err)
	}
	card, handle := namedFrom("", room, t.ID, t.Wire)
	task := &RelayTask{Card: card, Handle: handle, Title: t.Title, Status: t.Status, Doing: t.Activity.What,
		Where: t.Worktree, Why: t.Why, Idle: t.Idle, Waiting: t.Wait, Owned: t.Superv}
	for _, e := range events {
		task.Events = append(task.Events, RelayEvent{At: e.At, Kind: e.Kind})
	}
	return RelayAnswer{OK: true, To: handle, Card: card, Task: task}
}

// exitAcross asks `to` on `room` to leave, for a room's atrium_exit. Asking
// twice asks the same session to leave twice, which is harmless, so a failure
// here is just what it is and nothing is held.
func (c *controlMCP) exitAcross(ctx context.Context, room, to string) RelayAnswer {
	id, handle, err := c.resolvePeer(ctx, room, to)
	if err != nil {
		return refusal(err)
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/exit", room, nil, nil); err != nil {
		return refusal(err)
	}
	card, wire := namedFrom("", room, id, handle)
	return RelayAnswer{OK: true, To: wire, Card: card}
}

// refusal turns a failed call into an answer. A 502, 503 or 504 is a room that
// is not answering, and worth holding for. Anything else will be refused again.
// An error that is not the board's is resolvePeer's own not-found sentence.
func refusal(err error) RelayAnswer {
	var be *boardError
	if errors.As(err, &be) {
		switch be.code {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return RelayAnswer{Unreachable: true, Code: be.code, Error: be.msg}
		}
		return RelayAnswer{Code: be.code, Error: be.msg}
	}
	return RelayAnswer{Code: http.StatusNotFound, Error: err.Error()}
}

// peersElsewhere lists the sessions on every attached room but `besides`, each
// asked on its own, and names the rooms that did not answer.
func (c *controlMCP) peersElsewhere(ctx context.Context, besides string, all bool) ([]RelayPeer, []string) {
	out := []RelayPeer{}
	var quiet []string
	if c.hub == nil {
		return out, nil
	}
	for _, a := range c.hub.Rooms() {
		if equalFold(a.Name, besides) {
			continue
		}
		var body struct {
			Tasks []ctlCard `json:"tasks"`
		}
		if err := c.ask(ctx, http.MethodGet, "/v1/tasks", a.Name, nil, &body); err != nil {
			quiet = append(quiet, a.Name)
			continue
		}
		for _, t := range body.Tasks {
			live := t.Status != "done" && t.Status != "dead" && t.Status != "shelved"
			if (!all && !live) || t.Wire == "" {
				continue
			}
			// The alias named across too, since a bare one means the caller's
			// own room.
			alias := ""
			if t.Alias != "" {
				alias = t.Alias + "@" + a.Name
			}
			out = append(out, RelayPeer{
				Handle: t.Wire + "@" + a.Name, Alias: alias, Card: tagFor(a.Name, t.ID), Room: a.Name,
				Title: t.Title, Status: t.Status, Doing: t.Activity.What, Where: t.Worktree,
				Waiting: t.Wait, Owned: t.Superv,
			})
		}
	}
	return out, quiet
}

// sayAcross is a hub-side atrium_say to another room.
//
// FORWARDED TO THE SENDER'S ROOM, not delivered from here. The sender's room
// keeps the work ledger and knows whether this is a worker reporting to its
// launcher, so it has to see the message, and it then relays it back through
// the link exactly as it relays one from its own sessions. One path, one
// record.
func (c *controlMCP) sayAcross(ctx context.Context, req *mcp.CallToolRequest, room, name, target string,
	in sayInput) (*mcp.CallToolResult, sayOutput, error) {

	out := sayOutput{}
	me := agentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("a message to another room needs to say which session sent it, or it " +
			"would be typed as the operator. this call carries no " + AgentHeader)
	}
	body := map[string]string{"from": me, "to": name + "@" + target, "text": in.Text}
	if w := strings.TrimSpace(in.When); w != "" {
		body["when"] = w
	}
	var res struct {
		Delivered string `json:"delivered"`
		To        string `json:"to"`
		Card      string `json:"card"`
		When      string `json:"when"`
		Warning   string `json:"warning"`
		Note      string `json:"note"`
	}
	err := c.ask(ctx, http.MethodPost, "/v1/say", room, body, &res)
	var be *boardError
	if errors.As(err, &be) && be.code == http.StatusNotFound && be.bare {
		// THE SENDER'S ROOM IS OLDER THAN THIS. Delivered from here instead, so
		// the message still goes, and the sender is told what that cost.
		if ans, ok := c.reachable(ctx, target); !ok {
			return nil, out, errors.New(ans.Error)
		}
		ans := c.deliverAcross(ctx, target, name, me+"@"+room, in.Text, in.When)
		if !ans.OK {
			return nil, out, errors.New(ans.Error)
		}
		out.Delivered, out.To, out.Card, out.When = ans.Delivered, ans.To, ans.Card, ans.When
		out.Note = "your room is older than cross-room say, so the hub delivered this itself and your " +
			"room's work ledger has no record of it."
		if ans.Warning != "" {
			out.Note = ans.Warning + " " + out.Note
		}
		return nil, out, nil
	}
	if errors.As(err, &be) {
		switch be.code {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			// THE SENDER'S ROOM TOOK IT AND DID NOT ANSWER IN TIME. It may still
			// relay it, so this is not a failure to try again.
			out.Delivered, out.To = "unconfirmed", name+"@"+target
			out.Note = "your room did not answer in time (" + be.msg + "). it may still deliver this, so ask " +
				"whether it arrived before sending it again."
			return nil, out, nil
		}
	}
	if err != nil {
		return nil, out, err
	}
	out.Delivered, out.To, out.Card, out.When = res.Delivered, res.To, res.Card, res.When
	out.Note = res.Note
	if res.Warning != "" {
		out.Note = res.Warning
	}
	return nil, out, nil
}
