package daemon

import "time"

// activityForCycle gives the activity tracker the exported names the cycle asks for.
type activityForCycle struct{ *activityTracker }

func (a activityForCycle) DialogOpen(id string) bool         { return a.dialogOpen(id) }
func (a activityForCycle) TurnsBegun(id string) int          { return a.turnsBegun(id) }
func (a activityForCycle) MidTurn(id string) bool            { return a.midTurn(id) }
func (a activityForCycle) SinceBusy(id string) time.Duration { return a.sinceBusy(id) }
func (a activityForCycle) OnSubagents(id string) bool        { return a.onSubagents(id) }
func (a activityForCycle) QuietFor(id string) time.Duration  { return a.quietFor(id) }
func (a activityForCycle) LooksIdle(id string) bool {
	_, on := a.looksIdleMark(id)
	return on
}
func (a activityForCycle) What(id string) string {
	if cur := a.get(id); cur != nil {
		return cur.What
	}
	return ""
}
func (a activityForCycle) Describe(id string, now time.Time) (string, string) {
	return a.describe(id, now)
}
