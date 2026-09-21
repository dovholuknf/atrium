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

// auditFromRelay turns a relayed room event into an operational audit line, when
// the event is one the audit log keeps.
//
// MOST RELAYED EVENTS ARE LEFT ALONE. The stream carries a room's per-card
// history, which has its own home in the event table, and the audit log is not a
// second copy of it. Only the handful of kinds that answer "what happened to
// atrium" are recorded here.
//
// Best effort, like the rest of this path: a payload that does not parse, or a
// cancel message that carries no permission, is skipped rather than written as a
// noise line.
func (p *Proxy) auditFromRelay(room, kind string, data []byte) {
	switch kind {
	case "going-down":
		// A room announcing it is winding down, which the hub already sees on the
		// relay. Only this kind of the raw relay is operational.
		p.RecordAudit(room, "room-going-down", "the room says it is winding down")
	case "permission":
		p.auditPermission(room, data)
	case "lifecycle":
		p.auditLifecycle(room, data)
	}
}

// relayPermission is the part of a room's permission event the audit log reads.
// A subset of internal/store's Permission, so internal/link still learns nothing
// about the room's storage: it reads a payload, not a table.
type relayPermission struct {
	ID        string  `json:"id"`
	Tool      string  `json:"tool"`
	Command   string  `json:"command"`
	Decision  string  `json:"decision"`
	DecidedAt *string `json:"decided_at"`
	DecidedBy string  `json:"decided_by"`
}

// auditPermission derives a requested-or-decided line from a relayed permission.
//
// The room broadcasts the same permission twice in its life: once when it is
// raised with no decision, and once when it is answered. A decided-at with a
// decision is the second, everything else is the first. The cancel message
// carries no id and is not a permission at all, so it is skipped.
func (p *Proxy) auditPermission(room string, data []byte) {
	var pm relayPermission
	if err := json.Unmarshal(data, &pm); err != nil || pm.ID == "" {
		return
	}
	if pm.DecidedAt != nil && pm.Decision != "" {
		detail := pm.Decision
		if pm.DecidedBy != "" {
			detail += " by " + pm.DecidedBy
		}
		if tool := strings.TrimSpace(pm.Tool); tool != "" {
			detail += " for " + tool
		}
		p.RecordAudit(room, "permission-decided", detail)
		return
	}
	detail := strings.TrimSpace(pm.Tool)
	if cmd := strings.TrimSpace(pm.Command); cmd != "" {
		if detail != "" {
			detail += ": "
		}
		detail += cmd
	}
	p.RecordAudit(room, "permission-requested", detail)
}

// lifecycleKinds is the set of operational kinds a room may record through the
// lifecycle relay. A whitelist, so a room cannot mint an arbitrary audit kind
// and the board's kind filter stays a known list.
var lifecycleKinds = map[string]bool{
	"session-start":  true,
	"session-finish": true,
	"session-exit":   true,
}

// auditLifecycle records a session lifecycle line a room composed. The room owns
// the wording because it owns the facts, so the hub records what it is told once
// the kind is one it recognises.
func (p *Proxy) auditLifecycle(room string, data []byte) {
	var lc struct {
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(data, &lc); err != nil || !lifecycleKinds[lc.Kind] {
		return
	}
	p.RecordAudit(room, lc.Kind, lc.Detail)
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
