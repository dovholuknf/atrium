package daemon

import "time"

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

// NewContexts is what the rest of the room asks of the context cycle.
type NewContexts interface {
	get(taskID string) *newContext
	claim(taskID string, c *newContext) (uint64, bool)
	sessionStarted(taskID, conv string) bool
	mine(taskID string, gen uint64) bool
	advance(taskID string, gen uint64, step string) bool
	kick(taskID string)
	ack(taskID string) bool
	waitingForAck(taskID string) bool
	notePrompt(taskID string, gen uint64, turn int, at time.Time) bool
	noteCleared(taskID string, gen uint64)
	fail(taskID string, gen uint64, reason string) bool
	finish(taskID string, gen uint64) bool
	finishRecorded(taskID string, gen uint64, record func()) bool
	clear(taskID string) bool
	holding(taskID string) bool
	holdsInput(taskID string) bool
	beginCaptureOnly(taskID, file, conv string) (uint64, bool)
	typingHeld(taskID string) bool
	typingNow(taskID string) bool
	arm(taskID string, gen uint64) (armed, changed bool)
	setWaiting(taskID string, gen uint64, why string) bool
	setTyping(taskID string, gen uint64, on bool) bool
	seedFailed(taskID string, row ncJournalRow, reason string)
	busyCard() string
	stopAll()
}

var (
	_ ActivityTracker = (*activityTracker)(nil)
	_ NewContexts     = (*newContexts)(nil)
)
