package transport

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
)

// Serves says what an entry carries: the board, the room link, or both.
const (
	ServesBoard = "board"
	ServesLink  = "link"
	ServesBoth  = "both"
)

// Entry is one row of the Accessing Atrium list. Any number may share a kind.
type Entry struct {
	// ID is stable and unique across the list.
	ID string `json:"id"`
	// Name is what the operator calls it.
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Serves is one of ServesBoard, ServesLink, ServesBoth. Empty is both.
	Serves string `json:"serves,omitempty"`
	// Settings holds the values of the kind's Fields. A secret is held as a file path or a reference, never inline.
	Settings map[string]string `json:"settings,omitempty"`
}

// AccessField is one input of a kind's form, so the board draws it and the CLI takes it as a flag.
type AccessField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// AccessKind is one plug-in: a way in. Adding a kind is one Register call and the screen does not change.
type AccessKind interface {
	// Kind is the stable key: direct, ziti, zrok, spiffe.
	Kind() string
	// Fields is the one form of the kind.
	Fields() []AccessField
	// Hub builds the hub's half of one entry. It may return nil for a kind that only dials.
	Hub(Entry) (HubAccess, error)
	// Room builds the room's half of one entry. It may return nil for a kind that only listens.
	Room(Entry) (RoomAccess, error)
}

// HubAccess is the listening half.
type HubAccess interface {
	Listen(ctx context.Context) (net.Listener, error)
	// Join is what `atrium room join` is given for this entry, a string or empty when the kind needs none.
	Join(room string) (string, error)
	// Name is who a connection is from its credential alone. Empty is unproven.
	Name(c net.Conn) string
	// Key is stable across credential rotation, so a redial is not read as a takeover.
	Key(c net.Conn) string
	// Revoke stops the entry from answering and releases what it holds.
	Revoke(ctx context.Context) error
	// State is a live one-line state for the list.
	State() string
}

// RoomAccess is the dialling half.
type RoomAccess interface {
	Dial(ctx context.Context) (net.Conn, error)
	Describe() string
	// Mutual says the dial proves the room, as Direct.Mutual does.
	Mutual() bool
}

var (
	accessMu    sync.RWMutex
	accessKinds = map[string]AccessKind{}
)

// RegisterAccess adds a kind. A second kind of the same key is a programming error.
func RegisterAccess(k AccessKind) {
	accessMu.Lock()
	defer accessMu.Unlock()
	if _, dup := accessKinds[k.Kind()]; dup {
		panic("transport: access kind registered twice: " + k.Kind())
	}
	accessKinds[k.Kind()] = k
}

// AccessKindFor returns the kind of a key.
func AccessKindFor(kind string) (AccessKind, bool) {
	accessMu.RLock()
	defer accessMu.RUnlock()
	k, ok := accessKinds[kind]
	return k, ok
}

// AccessKinds lists the registered keys, sorted.
func AccessKinds() []string {
	accessMu.RLock()
	defer accessMu.RUnlock()
	out := make([]string, 0, len(accessKinds))
	for k := range accessKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Validate checks an entry against its kind's form.
func (e Entry) Validate() error {
	if e.ID == "" || e.Name == "" {
		return fmt.Errorf("an access entry needs an id and a name")
	}
	k, ok := AccessKindFor(e.Kind)
	if !ok {
		return fmt.Errorf("no access kind called %q. one of: %v", e.Kind, AccessKinds())
	}
	switch e.Serves {
	case "", ServesBoard, ServesLink, ServesBoth:
	default:
		return fmt.Errorf("serves is board, link or both, not %q", e.Serves)
	}
	for _, f := range k.Fields() {
		if f.Required && e.Settings[f.Name] == "" {
			return fmt.Errorf("%s: %s is required", e.Kind, f.Label)
		}
	}
	return nil
}

// ValidateEntries checks a whole list: each entry, and no id used twice.
func ValidateEntries(list []Entry) error {
	seen := map[string]bool{}
	for _, e := range list {
		if err := e.Validate(); err != nil {
			return err
		}
		if seen[e.ID] {
			return fmt.Errorf("access id %q is used twice", e.ID)
		}
		seen[e.ID] = true
	}
	return nil
}
