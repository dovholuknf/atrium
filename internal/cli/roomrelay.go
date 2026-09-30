package cli

import (
	"context"
	"errors"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
)

// linkRelay is the daemon's Relay over a room's link to its hub. Here because
// internal/daemon and internal/link do not import each other, and this is the
// one place that holds both. See docs/fabric/cross-room-say-design.md.
type linkRelay struct{ room *link.Room }

func (l linkRelay) Say(ctx context.Context, s daemon.RelaySay) (daemon.RelayResult, error) {
	ans, err := l.room.Relay(ctx, link.RelayRequest{
		Op: link.RelaySay, From: s.From, Room: s.Room, To: s.To, Text: s.Text, When: s.When,
	})
	if err != nil {
		return daemon.RelayResult{}, relayErr(err)
	}
	return daemon.RelayResult{
		OK: ans.OK, Code: ans.Code, Error: ans.Error, Unreachable: ans.Unreachable, Unconfirmed: ans.Unconfirmed,
		Delivered: ans.Delivered, When: ans.When, Warning: ans.Warning, To: ans.To, Card: ans.Card,
	}, nil
}

func (l linkRelay) Peers(ctx context.Context, all bool) ([]daemon.RemotePeer, string, error) {
	ans, err := l.room.Relay(ctx, link.RelayRequest{Op: link.RelayPeers, All: all})
	if err != nil {
		return nil, "", relayErr(err)
	}
	if !ans.OK {
		return nil, "", errors.New(ans.Error)
	}
	out := make([]daemon.RemotePeer, 0, len(ans.Peers))
	for _, p := range ans.Peers {
		out = append(out, daemon.RemotePeer{
			Handle: p.Handle, Alias: p.Alias, Card: p.Card, Room: p.Room, Title: p.Title, Status: p.Status,
			Doing: p.Doing, Where: p.Where, Waiting: p.Waiting, Owned: p.Owned,
		})
	}
	return out, ans.Warning, nil
}

func (l linkRelay) Card(ctx context.Context, room, to string, events bool) (daemon.RelayResult, error) {
	return l.reach(ctx, link.RelayRequest{Op: link.RelayCard, Room: room, To: to, Events: events})
}

func (l linkRelay) Exit(ctx context.Context, room, to string) (daemon.RelayResult, error) {
	return l.reach(ctx, link.RelayRequest{Op: link.RelayExit, Room: room, To: to})
}

// reach relays one card or exit request, and carries the card back.
func (l linkRelay) reach(ctx context.Context, req link.RelayRequest) (daemon.RelayResult, error) {
	ans, err := l.room.Relay(ctx, req)
	if err != nil {
		return daemon.RelayResult{}, relayErr(err)
	}
	res := daemon.RelayResult{
		OK: ans.OK, Code: ans.Code, Error: ans.Error, Unreachable: ans.Unreachable, Unconfirmed: ans.Unconfirmed,
		To: ans.To, Card: ans.Card,
	}
	if t := ans.Task; t != nil {
		res.Task = &daemon.RemoteTask{
			Card: t.Card, Handle: t.Handle, Title: t.Title, Status: t.Status, Doing: t.Doing, Where: t.Where,
			Why: t.Why, Idle: t.Idle, Waiting: t.Waiting, Owned: t.Owned,
		}
		for _, e := range t.Events {
			res.Task.Events = append(res.Task.Events, daemon.RemoteEvent{At: e.At, Kind: e.Kind})
		}
	}
	return res, nil
}

// relayErr maps the link's three failures onto the daemon's, keeping the
// link's sentence.
func relayErr(err error) error {
	switch {
	case errors.Is(err, link.ErrRelayOld):
		return daemon.ErrRelayOld
	case errors.Is(err, link.ErrRelayUnconfirmed):
		return relayFailure{msg: err.Error(), is: daemon.ErrRelayUnconfirmed}
	default:
		return relayFailure{msg: err.Error(), is: daemon.ErrRelayDown}
	}
}

// relayFailure reads as the link's sentence and is the daemon's sentinel.
type relayFailure struct {
	msg string
	is  error
}

func (f relayFailure) Error() string { return f.msg }
func (f relayFailure) Unwrap() error { return f.is }
