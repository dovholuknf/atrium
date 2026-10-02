package edge

import (
	"context"
	"net/http"
)

// Reach is which of the board's listeners a request came in on, when it is not the main one. The main
// listener (loopback, or wherever the board is bound) is the empty Reach: its requests are told apart by
// their address, with LocalOperator.
//
// A MARK IS SET BY THE LISTENER'S OWN HANDLER, never read from the request, so nothing a caller sends can
// change it. The hub's git store uses it to refuse a zrok public share and to take the overlay's reaches as
// the operator's. See docs/rnd/hub-forge-design.md 3.4.
type Reach string

const (
	// ReachZrokPublic is a zrok public share: the internet behind the share's login.
	ReachZrokPublic Reach = "zrok-public"
	// ReachZrokPrivate is a zrok private share: only a peer holding the share token.
	ReachZrokPrivate Reach = "zrok-private"
	// ReachOverlay is an OpenZiti service: who may reach it is a policy on that network.
	ReachOverlay Reach = "overlay"
)

type reachKey struct{}

// MarkReach wraps the handler of a second listener, so ReachOf can tell its requests from the main one's.
func MarkReach(h http.Handler, r Reach) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), reachKey{}, r)))
	})
}

// ReachOf is the mark the listener put on a request, or "".
func ReachOf(r *http.Request) Reach {
	v, _ := r.Context().Value(reachKey{}).(Reach)
	return v
}
