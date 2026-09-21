package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"strings"
)

// EventSink is where a card's history goes. Two questions hide in the event
// log, and a sink answers one or both of them:
//
//   - HOT: "what are this card's last N events", asked every time a card opens.
//     A hot sink serves Recent and is read on the board's critical path.
//   - COLD: "everything that ever happened, kept somewhere durable", which
//     nothing on the board reads. A cold sink is write-only and does not serve
//     Recent.
//
// Sinks compose rather than replace: a store writes every event to its hot sink
// and fans the same event out to any cold sinks. See docs/backlog-2.md.
type EventSink interface {
	// Append records one event. A hot sink's Append is synchronous and on the
	// halt path, because the board depends on the read it feeds. A cold sink's
	// Append is best effort and must never fail the caller.
	Append(taskID string, e *Event) error
	// Recent returns the newest limit events, oldest first. A cold sink that
	// does not serve reads returns ErrRecentUnsupported.
	Recent(taskID string, limit int) ([]*Event, error)
}

// ErrRecentUnsupported is returned by Recent on a write-only cold sink.
var ErrRecentUnsupported = errors.New("event sink does not serve Recent")

// configureSinks wires the hot and cold sinks from the event_sink setting. It is
// called once at Open, after migrate, with the db path so a file sink can put
// its logs beside the database.
//
// The default is left untouched: an unset or `db` setting keeps the db table as
// the hot sink and adds no cold sinks, so every existing install behaves exactly
// as before. Only an explicit setting adds anything.
//
// Nothing here is fatal. A setting this build cannot honour is logged and
// skipped, because a cold trail is a durability convenience and must never be
// the reason the daemon refuses to start.
func (s *Store) configureSinks(dbPath string) {
	s.configureHotWindow()

	raw, err := s.Setting(SettingEventSink)
	if err != nil {
		log.Printf("event sink: reading %s: %v; using db only", SettingEventSink, err)
		return
	}
	names := splitSinkNames(raw)
	if len(names) == 0 {
		return // default: hot db, no cold, already set in Open
	}

	// The first name is the hot sink. Only `db` can serve Recent in this phase,
	// so anything else there is refused and the default db hot sink stands.
	if names[0] != "db" {
		log.Printf("event sink: hot sink %q is not supported (only db serves reads); using db", names[0])
	}

	logsDir := filepath.Join(filepath.Dir(dbPath), "events")
	for _, name := range names[1:] {
		switch name {
		case "db":
			// The db is the hot sink; naming it again as a cold sink would write
			// every event to the table twice. Ignore rather than double-write.
			log.Printf("event sink: %q is the hot sink and cannot also be a cold sink; ignoring", name)
		case "file":
			fs, err := newFileSink(logsDir)
			if err != nil {
				log.Printf("event sink: file sink disabled: %v", err)
				continue
			}
			s.cold = append(s.cold, fs)
		default:
			log.Printf("event sink: unknown sink %q; ignoring", name)
		}
	}
}

// configureHotWindow reads the byte bound and sets it on the db hot sink. An
// unset, empty, non-numeric, or non-positive value leaves the sink unbounded,
// which is the default: the db keeps every event and nothing rolls off. Only a
// positive value opts a card's history into the rolling window.
//
// The hot sink is always the db sink in this phase, set in Open before this
// runs, so the type assertion holds. A bad value is logged and ignored rather
// than fatal, the same posture the rest of sink configuration takes.
func (s *Store) configureHotWindow() {
	raw, err := s.Setting(SettingEventWindowBytes)
	if err != nil {
		log.Printf("event sink: reading %s: %v; hot window left unbounded", SettingEventWindowBytes, err)
		return
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return // default: unbounded
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		log.Printf("event sink: %s = %q is not a positive number of bytes; hot window left unbounded",
			SettingEventWindowBytes, raw)
		return
	}
	if db, ok := s.hot.(*dbSink); ok {
		db.windowBytes = n
	}
}

// splitSinkNames turns the comma-separated setting into a trimmed, lower-cased
// list, dropping blanks. An empty result means "use the default".
func splitSinkNames(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// dbSink is the event table, wrapped as a sink. This is the default hot sink and
// what every install used before sinks existed, so its behaviour is exactly the
// INSERT and the query that lived in tasks.go.
//
// It borrows the store's *sql.DB rather than owning it: the store opens and
// closes the connection, and every call here runs inside the store's guard, so
// contention retries and a hard failure halts, the same as before.
type dbSink struct {
	db *sql.DB

	// windowBytes is the per-card hot-window bound in bytes of payload. Zero
	// means unbounded, which is the default and phase-1 behaviour: nothing rolls
	// off. A positive value rolls the oldest events off a card after each append
	// so the table holds only a recent window. Set once at Open by configureSinks
	// before any Append, so the writer never races the caller for it.
	windowBytes int64
}

func newDBSink(db *sql.DB) *dbSink { return &dbSink{db: db} }

// Append writes one row to the event table. The id, timestamp and payload are
// already resolved on the Event, so two sinks fed the same event agree on all
// three.
//
// When a window is set, the oldest events for this card roll off after the
// insert, so the db holds only the recent window. This is what actually shrinks
// the operational database over time; incremental auto_vacuum hands the freed
// pages back to disk.
func (d *dbSink) Append(taskID string, e *Event) error {
	if _, err := d.db.Exec(`INSERT INTO event (id, task_id, at, kind, payload) VALUES (?,?,?,?,?)`,
		e.ID, taskID, ts(e.At), e.Kind, string(e.Payload)); err != nil {
		return err
	}
	if d.windowBytes > 0 {
		return d.rollOff(taskID, d.windowBytes)
	}
	return nil
}

// rollOff deletes the oldest events for a card until the retained payload bytes
// fit the window. It always keeps the NEWEST event, even one larger than the
// whole window, so a card never loses the thing that just happened; only older
// events roll off.
//
// A running total over the newest-first order picks the cut: an event is dropped
// once every event at least as new as it already fills the window. Payload bytes
// are counted as a blob so multi-byte JSON is measured in bytes, not runes,
// matching the byte the setting names.
func (d *dbSink) rollOff(taskID string, windowBytes int64) error {
	_, err := d.db.Exec(
		`DELETE FROM event
		 WHERE task_id = ?1
		   AND id IN (
		     SELECT id FROM (
		       SELECT id, SUM(LENGTH(CAST(payload AS BLOB))) OVER (
		                    ORDER BY at DESC, id DESC
		                    ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
		                  ) AS running
		       FROM event WHERE task_id = ?1
		     ) WHERE running > ?2
		   )
		   AND id <> (SELECT id FROM event WHERE task_id = ?1 ORDER BY at DESC, id DESC LIMIT 1)`,
		taskID, windowBytes)
	return err
}

// Recent returns the NEWEST limit events, oldest first within that window.
//
// The two halves pull opposite ways and both belong here: selecting `at ASC`
// with a LIMIT takes the oldest N, which on a busy card is the day it was
// created rather than what just happened. So the limit applies to the newest
// end and the window is reversed before it is returned, which keeps the wire
// oldest-first for the timeline.
//
// `id` breaks ties in the same direction as `at` in both clauses. These
// timestamps have millisecond resolution and a hook can write two events inside
// one.
func (d *dbSink) Recent(taskID string, limit int) ([]*Event, error) {
	rows, err := d.db.Query(
		`SELECT id, task_id, at, kind, payload FROM event
		 WHERE task_id = ? ORDER BY at DESC, id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var (
			e       Event
			at, pay string
		)
		if err := rows.Scan(&e.ID, &e.TaskID, &at, &e.Kind, &pay); err != nil {
			return nil, err
		}
		if e.At, err = parseTS(at); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(pay)
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Newest-first came back, oldest-first goes out. In place, since the slice
	// is bounded by `limit` and is nobody else's yet.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
