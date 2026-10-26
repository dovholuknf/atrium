package wire

import (
	"strings"
	"time"
)

// Path is one way to reach a hub. Which fields mean anything depends on the kind.
type Path struct {
	Transport string `json:"transport"`
	// Addr is where a direct room dials, host:port.
	Addr string `json:"addr,omitempty"`
	// Service is the ziti service.
	Service string `json:"service,omitempty"`
	// Share is a zrok private share token. Room-local: the hub never hands one out.
	Share string `json:"share,omitempty"`
	// Identity is the ziti identity file. Room-local: it is this room's credential.
	Identity string `json:"identity,omitempty"`
	// From is where the path came from, "join", "hub" or "local", for showing.
	// Set by Candidates, never stored.
	From string `json:"from,omitempty"`
}

// MaskShare is a zrok share token as the board may show it: its first four characters, never the whole credential.
func MaskShare(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 4 {
		return "..."
	}
	return token[:4] + "..."
}

// PathState is one rung as a room sees it. Never a credential: Addr is the dial address, the ziti service name or a
// masked zrok share.
type PathState struct {
	Kind      string    `json:"kind"`
	Addr      string    `json:"addr,omitempty"`
	State     string    `json:"state"`
	Since     time.Time `json:"since,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	RTTMs     int64     `json:"rtt_ms,omitempty"`
	// Stale is true when RTTMs is the last answer and the probe now due has not returned yet.
	Stale bool `json:"stale,omitempty"`
	// Mutual is true when the rung really does TLS with a client certificate, which is what "mTLS" claims.
	Mutual bool   `json:"mtls,omitempty"`
	Source string `json:"source,omitempty"`
	// Hammering is true when dials on this path are failing fast. See dialgate.go.
	Hammering bool `json:"hammering,omitempty"`
}

// RemoteAccess is what a room said about its switch: observed, never trusted for granting.
type RemoteAccess struct {
	On     bool     `json:"on"`
	From   []string `json:"from,omitempty"`
	Addr   string   `json:"addr,omitempty"`
	Port   int      `json:"port,omitempty"`
	Open   int      `json:"open"`
	Forced bool     `json:"forced,omitempty"`
}

func (r *RemoteAccess) Allows(caller string) bool {
	if r == nil || !r.On {
		return false
	}
	for _, f := range r.From {
		if f == "*" || strings.EqualFold(f, caller) {
			return true
		}
	}
	return false
}
