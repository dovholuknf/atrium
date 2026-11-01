package daemon

import "github.com/dovholuknf/atrium/internal/daemon/hublink"

// What the hublink package declares and the CLI reaches as daemon.X.
type RelayLaunch = hublink.RelayLaunch
type RelayResult = hublink.RelayResult
type RelaySay = hublink.RelaySay
type RemoteEvent = hublink.RemoteEvent
type RemotePeer = hublink.RemotePeer
type RemoteTask = hublink.RemoteTask

var (
	ErrRelayDown        = hublink.ErrRelayDown
	ErrRelayOld         = hublink.ErrRelayOld
	ErrRelayUnconfirmed = hublink.ErrRelayUnconfirmed
)
