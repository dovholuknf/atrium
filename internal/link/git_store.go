package link

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub's git store on the board: `/git/hub/<host>/<owner>/<repo>.git/...`, for the operator. See
// docs/rnd/hub-forge-design.md 3.4. A room reaches the same store on the link's git kind (git.go), as a room.
//
// WHO IS THE OPERATOR HERE:
//
//	loopback on the hub's machine   edge.LocalOperator, the board's own rule (a loopback address, a loopback
//	                                Host and no forwarding header)
//	an OpenZiti service             the network's policy decides who may reach it
//	a zrok PRIVATE share            the share token decides
//	a zrok PUBLIC share             404, whatever else is true
//	anything else                   403
//
// The card headers are never read on this path. They are dropped from the request, and the push is the
// operator's, with no room and no card.

// serveGitStore answers anything under /git/ on the board's listeners.
func (p *Proxy) serveGitStore(w http.ResponseWriter, r *http.Request) {
	g := p.git()
	if g == nil || !strings.HasPrefix(r.URL.Path, gitsync.StorePrefix) {
		http.NotFound(w, r)
		return
	}
	switch edge.ReachOf(r) {
	case edge.ReachZrokPublic:
		// The same answer as a path that is not there: a public share does not say it is a git server.
		http.NotFound(w, r)
		return
	case edge.ReachOverlay, edge.ReachZrokPrivate:
	default:
		if !edge.LocalOperator(r) {
			http.Error(w, "the hub's git store is reached from the machine the hub runs on, or over the board's overlay"+
				edge.ProxyNote(r), http.StatusForbidden)
			return
		}
	}
	ctx := gitsync.WithCaller(r.Context(), gitsync.Caller{Kind: gitsync.CallerOperator})
	g.StoreHandler().ServeHTTP(w, r.WithContext(ctx))
}

// GitCards answers gitsync.CardLookup through the hub's own relay, which asks the room the way a card on
// another room does. A room that is not attached, or does not answer, cannot be asked. A room that answers 404
// has no such card, which is a card that was culled.
func (p *Proxy) GitCards(ctx context.Context, room, card string) (gitsync.CardState, error) {
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if c == nil || p.hub == nil || !p.hub.Has(room) {
		return gitsync.CardState{}, gitsync.ErrRoomUnreachable
	}
	ans := c.relay(ctx, "", RelayRequest{Op: RelayCard, Room: room, To: card})
	switch {
	case ans.OK && ans.Task != nil:
		return gitsync.CardState{Status: ans.Task.Status, IdleSeconds: ans.Task.Idle}, nil
	case ans.Unreachable:
		return gitsync.CardState{}, gitsync.ErrRoomUnreachable
	case ans.Code == http.StatusNotFound:
		return gitsync.CardState{}, gitsync.ErrCardGone
	}
	return gitsync.CardState{}, errors.New(ans.Error)
}
