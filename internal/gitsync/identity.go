package gitsync

import (
	"context"
	"net/http"
	"strings"
)

// Who is on the other end of a request to the hub's store. See docs/fabric/hub-forge-design.md 3.4.
//
// THE CALLER IS SET BY THE LISTENER THE REQUEST CAME IN ON, never by anything the request says.
// internal/link puts a Caller on the context: the link's `git` kind says Room (from the room's
// hub-signed certificate), the board's loopback and overlay reaches say Operator, and every other
// reach says nothing, which is refused. The card headers are read ONLY from a Room caller. On any
// other reach they are dropped without being looked at.

// Header names a room's stable forwarder adds on the link's git kind. The card is the one whose token
// the forwarder checked. The chain is that card then its `moved_to` predecessors, nearest first.
const (
	HeaderCard  = "X-Atrium-Card"
	HeaderChain = "X-Atrium-Card-Chain"
	// HeaderLaunchBranch is the branch the card was launched on and HeaderKin is its launcher's id, then the earlier
	// cards that launcher launched on that branch, nearest first. See kin in receive_rules.go.
	HeaderLaunchBranch = "X-Atrium-Card-Branch"
	HeaderKin          = "X-Atrium-Card-Kin"
)

// MaxChain is how many ids a chain may hold, the card included.
const MaxChain = 8

// CallerKind says which reach a request came in on.
type CallerKind int

const (
	// CallerNone is a request with no identity. It may do nothing.
	CallerNone CallerKind = iota
	// CallerRoom is a room on the link's `git` kind. Name is the room.
	CallerRoom
	// CallerOperator is the operator, on the board's loopback or an overlay reach that is not a public share.
	CallerOperator
)

// Caller is who a request is from.
type Caller struct {
	Kind CallerKind
	// Room is the room's name, from its certificate. Set for CallerRoom only.
	Room string
}

type callerKey struct{}

// WithCaller puts the caller on a context.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom is the caller a listener put on the context, or CallerNone.
func CallerFrom(ctx context.Context) Caller {
	c, _ := ctx.Value(callerKey{}).(Caller)
	return c
}

// Pusher is who a push is from, decided by the hub.
type Pusher struct {
	// Operator is the operator. Room and Card are empty then.
	Operator bool
	Room     string
	Card     string
	// Chain is Card then its predecessors, nearest first. Honoured only against an owner on the same room.
	Chain []string
	// LaunchBranch and Kin are what the room said about the branch the card was launched on and the cards that handed
	// it over. Honoured only against an owner on the same room, and only for LaunchBranch.
	LaunchBranch string
	Kin          []string
}

// Label is how a refusal or an audit line names the pusher.
func (p Pusher) Label() string {
	if p.Operator {
		return "the operator"
	}
	return p.Room + "'s " + p.Card
}

// ValidCardID says whether s is the shape of a card id: letters, digits, dash and underscore, up to 128.
// The room mints them, so this is a whitelist for what the hub is willing to put in a log, a header and a
// sentence, and not a check that the card exists.
func ValidCardID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// pusherFrom reads the pusher off a request, or says why there is none. The headers are read from a
// room caller only. The returned reason is a sentence for the user.
func pusherFrom(r *http.Request, c Caller) (Pusher, string) {
	switch c.Kind {
	case CallerOperator:
		return Pusher{Operator: true}, ""
	case CallerRoom:
		if c.Room == "" {
			return Pusher{}, "the hub does not know which room this is"
		}
		// ONE VALUE EACH. The forwarder sets these, and a client that sent its own and had them added to rather than
		// replaced must fail loudly, not have the first value win.
		if len(r.Header.Values(HeaderCard)) > 1 || len(r.Header.Values(HeaderChain)) > 1 ||
			len(r.Header.Values(HeaderLaunchBranch)) > 1 || len(r.Header.Values(HeaderKin)) > 1 {
			return Pusher{}, "the request names its card more than once, which only a forwarder that adds to the card headers does. it has to set them"
		}
		card := strings.TrimSpace(r.Header.Get(HeaderCard))
		if card == "" {
			return Pusher{}, "a push has to come from a card, and this one named none. push from a card with a token"
		}
		if !ValidCardID(card) {
			return Pusher{}, "that is not a card id"
		}
		chain := []string{card}
		if raw := strings.TrimSpace(r.Header.Get(HeaderChain)); raw != "" {
			parts := strings.Split(raw, ",")
			if len(parts) > MaxChain {
				return Pusher{}, "the card's history is too long to follow"
			}
			for i, p := range parts {
				p = strings.TrimSpace(p)
				if !ValidCardID(p) {
					return Pusher{}, "the card's history holds something that is not a card id"
				}
				// The chain starts with the card itself. A chain that does not is the room's mistake, and
				// is refused rather than guessed at.
				if i == 0 {
					if p != card {
						return Pusher{}, "the card's history does not start with the card"
					}
					continue
				}
				chain = append(chain, p)
			}
		}
		p := Pusher{Room: c.Room, Card: card, Chain: chain}
		// A branch that is not a good ref name is ignored, and so is a kin list that holds a bad id: neither refuses
		// the push, they only grant nothing.
		if b := strings.TrimSpace(r.Header.Get(HeaderLaunchBranch)); b != "" && checkRefName(headsPrefix+b) == "" {
			var kin []string
			for _, k := range strings.Split(r.Header.Get(HeaderKin), ",") {
				if k = strings.TrimSpace(k); k != "" {
					kin = append(kin, k)
				}
			}
			ok := len(kin) <= MaxChain
			for _, k := range kin {
				ok = ok && ValidCardID(k)
			}
			if ok {
				p.LaunchBranch, p.Kin = b, kin
			}
		}
		return p, ""
	}
	return Pusher{}, "the hub cannot tell who is pushing. push through a room's hub remote, or as the operator on the hub"
}
