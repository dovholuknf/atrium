package daemon

import (
	"time"
)

// ActivityTracker is what the rest of the room asks of the activity tracker. The features that leave the package take
// this and never the struct.
type ActivityTracker interface {
	get(taskID string) *Activity
	set(taskID, what, tool string)
	forget(taskID string)
	dialogOpen(taskID string) bool
	dialogRaised(taskID string)
	midTurn(taskID string) bool
	turnsBegun(taskID string) int
	turnSince(taskID string) (time.Time, bool)
	toolSince(taskID string) (string, time.Time, bool)
	promptSeen(taskID string)
	quietFor(taskID string) time.Duration
	sinceBusy(taskID string) time.Duration
	callsLately(taskID string) int
	onSubagents(taskID string) bool
	subagentStarted(taskID, id, kind string)
	subagentStopped(taskID, id string)
	setBackground(taskID string, n int)
	setBackgroundWork(taskID string, n int)
	backgroundWork(taskID string) (int, time.Time)
	setHeld(taskID string, r heldPeer) bool
	markHeldStuck(taskID string) bool
	clearHeld(taskID string)
	setTelemetry(taskID string, t Telemetry)
	telemetry(taskID string) *Telemetry
	telemetryAllowed(key string) bool
	describe(taskID string, now time.Time) (string, string)
	turned(taskID string) bool
	heard(taskID string) bool
	sessionSpoke(taskID string)
	markLooksIdle(taskID string, m idleMark) bool
	clearLooksIdle(taskID string) (idleMark, bool)
	looksIdleMark(taskID string) (idleMark, bool)
}

var _ ActivityTracker = (*activityTracker)(nil)
