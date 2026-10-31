package conn

import (
	"context"
	"net"
	"net/http"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// Rooms is what the link's features ask of the hub about its rooms. The hooks a hub is wired with (Relay, Forge,
// PRClaim, RoomSpec, LastSeen) are fields, and stay on Hub.
type Rooms interface {
	Rooms() []wire.Attached
	Has(room string) bool
	Only() string
	Dial(ctx context.Context, room string) (net.Conn, error)
	Transport(room string) http.RoundTripper
	OpenStream(ctx context.Context, room, caller, claim string) (*Stream, error)
	Listeners() []ListenerState
	Changes() <-chan struct{}
	AskRestart(room string, ask RestartAsk) error
}

var _ Rooms = (*Hub)(nil)
