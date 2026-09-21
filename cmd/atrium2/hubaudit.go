package main

import (
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// auditLog is the operational audit log the hub serves, over the hub's own
// store. It is what keeps `internal/link` from ever learning the hub has a
// database: the proxy holds the interface, this holds the SQLite.
type auditLog struct{ store *hubstore.Store }

// Record writes one operational line. A room name is resolved to the hub's
// record so the row carries its id, and a name that no longer resolves still
// gets a named line rather than being dropped, which is the detach-for-a-room-
// forced-out case this log exists for.
func (a auditLog) Record(room, kind, detail string) {
	var r *hubstore.Room
	if room != "" {
		if got, err := a.store.ByName(room); err == nil {
			r = got
		} else {
			r = &hubstore.Room{Name: room}
		}
	}
	a.store.Log(r, kind, detail)
}

// Recent reads the log newest first, filtered by room name and kind, and maps
// the store's rows onto the shape the board reads.
func (a auditLog) Recent(limit int, room, kind string) ([]link.AuditEntry, error) {
	entries, err := a.store.AuditWhere(limit, room, kind)
	if err != nil {
		return nil, err
	}
	out := make([]link.AuditEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, link.AuditEntry{
			ID: e.ID, At: e.At, Room: e.RoomName, Kind: e.Kind, Detail: e.Detail,
		})
	}
	return out, nil
}
