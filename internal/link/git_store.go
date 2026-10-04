package link

import (
	"context"
	"errors"
	"net"
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
	store, pass := strings.HasPrefix(r.URL.Path, gitsync.StorePrefix), strings.HasPrefix(r.URL.Path, gitsync.PassPrefix)
	if g == nil || (!store && !pass) {
		http.NotFound(w, r)
		return
	}
	reach, ok := gitReach(w, r)
	if !ok {
		return
	}
	// A FETCH PASSED THROUGH TO A ROOM, for the operator. A card on a room is not served here (it would come in
	// on the link's git kind, which does not route /git/room/), so there is no card to name and none is read.
	if pass {
		ctx := gitsync.WithReader(r.Context(), gitsync.Reader{Reach: reach, Key: readerKey(reach, r.RemoteAddr)})
		g.PassHandler().ServeHTTP(w, r.WithContext(ctx))
		return
	}
	ctx := gitsync.WithCaller(r.Context(), gitsync.Caller{Kind: gitsync.CallerOperator})
	g.StoreHandler().ServeHTTP(w, r.WithContext(ctx))
}

// gitReach says which of the hub's git reaches a request came in on, and writes the refusal when it may not use any:
// a zrok public share is 404 whatever else is true, an overlay or a zrok private share is the operator's, and anything
// else has to be loopback on this machine. The pass-through, the store and the lookup (git_url.go) share this rule.
func gitReach(w http.ResponseWriter, r *http.Request) (reach string, ok bool) {
	switch edge.ReachOf(r) {
	case edge.ReachZrokPublic:
		// The same answer as a path that is not there: a public share does not say it is a git server.
		http.NotFound(w, r)
		return "", false
	case edge.ReachOverlay:
		return "overlay", true
	case edge.ReachZrokPrivate:
		return "zrok-private", true
	}
	if !edge.LocalOperator(r) {
		http.Error(w, "the hub's git store is reached from the machine the hub runs on, or over the board's overlay"+
			edge.ProxyNote(r), http.StatusForbidden)
		return "", false
	}
	return "loopback", true
}

// readerKey is who the rate and the budget count a reader as: the reach and the peer's address.
//
// IT IS PER PEER ONLY WHERE THE PEER HAS A REAL ADDRESS. Loopback does, and so does a listener that hands out TCP
// connections. A ziti service listener (and a zrok share, which rides ziti) does not: the SDK's RemoteAddr is
// `ziti-edge-router connId=<n>, logical=<router>`, one per CONNECTION and naming nobody. Keyed on that, every new
// connection would be a new reader and the rate could be walked around by opening another. So an address that is not
// an IP is counted as the whole reach: every reader on the overlay or a private share shares one budget, which
// is a fairness cost (one busy phone can use up another's six fetches a minute) and not a hole, and every reader
// there is the operator anyway. The rate is per peer address on loopback, and per reach on the overlays.
func readerKey(reach, remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil || net.ParseIP(host) == nil {
		return reach
	}
	return reach + ":" + host
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
