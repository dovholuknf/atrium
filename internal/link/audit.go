package link

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The operational audit log, as the hub serves it.
//
// ── WHY AN INTERFACE, AND NOT THE STORE ─────────────────
//
// The same rule `Inventory` and the control server follow: keeping the log means
// a database, and this package holds none and must not learn to. The daemon
// implements this over `internal/hubstore` and wires it in, so `internal/link`
// stays a transport that knows nothing about SQLite.
//
// A hub that never wires one answers `/_hub/audit` empty rather than erroring,
// which is what a hub with no store can honestly say.

// AuditEntry is one operational event, shaped for the board.
//
// Room is a name, empty for a hub-level line. It is the room's own field rather
// than tagged, because the audit pane is a hub view and the board reads the name
// straight.
type AuditEntry struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Room   string    `json:"room,omitempty"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
}

// AuditLog is what the proxy needs to record and read operational events.
type AuditLog interface {
	// Record writes one line. room is a name, empty for a hub-level event. Best
	// effort: it must never block or fail the thing being recorded.
	Record(room, kind, detail string)
	// Recent reads the log newest first, optionally filtered by room name and
	// kind. Empty filters match everything.
	Recent(limit int, room, kind string) ([]AuditEntry, error)
}

// SetAuditLog wires the operational audit log up. Optional.
func (p *Proxy) SetAuditLog(a AuditLog) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.audit = a
}

func (p *Proxy) auditLog() AuditLog {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.audit
}

// RecordAudit persists one operational event and nudges any watching board.
//
// TWO THINGS, AND THE SECOND IS A DELTA, NOT STATE. The line is written to the
// log, and an `audit` event is emitted on the hub stream carrying only that
// something happened. The pane re-fetches on that delta, and again on reconnect,
// which is the same self-healing the rest of this stream rests on. See
// events.go. A board with the pane closed pays nothing.
//
// Best effort throughout: a hub with no audit wiring records nothing and the
// nudge is dropped by `emit` like any other event nobody is watching.
func (p *Proxy) RecordAudit(room, kind, detail string) {
	if a := p.auditLog(); a != nil {
		a.Record(room, kind, detail)
	}
	payload, err := json.Marshal(map[string]string{"room": room, "kind": kind})
	if err != nil {
		return
	}
	p.feeds.emit(Event{Kind: "audit", Data: payload})
}

// serveAudit answers GET /_hub/audit, newest first, filterable by room and kind.
func (p *Proxy) serveAudit(w http.ResponseWriter, r *http.Request) {
	a := p.auditLog()
	if a == nil {
		// A hub with no store keeps no operational log. An empty list rather than
		// an error, so the pane draws "nothing yet" instead of a failure.
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []AuditEntry{}})
		return
	}
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	entries, err := a.Recent(limit, r.URL.Query().Get("room"), r.URL.Query().Get("kind"))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"events": entries})
}
