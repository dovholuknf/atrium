package daemon

import "fmt"

// Operational lifecycle events a room emits for the hub's audit log.
//
// ── why the room and not the hub ─────────────────────────
//
// The hub can watch a card appear and later leave, but that is not the same
// thing as a session STARTING, a session saying it FINISHED, or a process
// EXITING with a reason. Only the room holds the reason: the exit code, how
// long it ran, whether an agent handed the work back or a runner fell over on
// startup. So the room says it, on its own event stream, and the hub turns the
// line into an audit row. See internal/link/events.go, which reads the
// `lifecycle` kind off the relay and records it, and docs/audit-design.md, which
// deferred exactly this to a room restart.
//
// ── the shape on the wire ────────────────────────────────
//
// One event kind, `lifecycle`, carrying the audit kind and a ready-made detail
// line. The room composes the wording because the room owns the facts, and the
// hub stays a dumb relay that records what it is told rather than guessing at a
// payload whose shape is the room's. The hub whitelists the kinds it will
// record, so a stray value cannot invent an audit kind.
//
// BEST EFFORT, like every other broadcast. A lifecycle line that does not go out
// must never fail the launch, the finish, or the exit it describes.

// emitLifecycle broadcasts one operational lifecycle event for the hub to
// record. The kind is one of the `session-*` values the hub whitelists.
func (d *Daemon) emitLifecycle(kind, detail string) {
	d.ap.Broadcast("lifecycle", map[string]any{"kind": kind, "detail": detail})
}

// taskTitle is a card's display title, or its id when it cannot be read. Used
// only to word a lifecycle line, so a lookup that fails is a plainer line rather
// than a dropped event.
func (d *Daemon) taskTitle(taskID string) string {
	if taskID == "" {
		return ""
	}
	if t, err := d.st.Get(taskID); err == nil {
		return t.DisplayTitle()
	}
	return taskID
}

// lifecycleStart words a session-start line: what card, on which runner, and
// whether it is resuming an earlier conversation.
func lifecycleStart(title, runner string, resumed bool) string {
	line := fmt.Sprintf("%s started", title)
	if runner != "" {
		line += " on " + runner
	}
	if resumed {
		line += ", resuming"
	}
	return line
}
