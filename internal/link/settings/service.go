package settings

import (
	"encoding/json"
	"net/http"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// Calls is every call out of the fan-out settings into the rest of the hub.
type Calls struct {
	// Store is the hub's store of settings, nil when the hub has none.
	Store func() Store
	// Rooms lists the attached rooms.
	Rooms func() []wire.Attached
	// RoomClient is the client that reaches one room, and HostFor the host to address it by.
	RoomClient func(room string) *http.Client
	HostFor    func(room string) string
}

// Service is the settings the hub owns and passes to every room.
type Service struct{ Calls }

// New is a Service over those calls.
func New(c Calls) *Service { return &Service{c} }

func (p *Service) store() Store {
	if p.Store == nil {
		return nil
	}
	return p.Store()
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
