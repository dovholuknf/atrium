package daemon

import (
	"fmt"
	"log"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// REPORT_TO: a launch that names who its reports go to. See
// docs/backlog/runtime/r-014.md.
//
// A card's launcher is who `atrium_report` goes to, who hears the silent-stop
// notice and who owed-report tracking counts against. A card started from the
// board or a script has none, so a PR review started by gwt reported to nobody.
// `report_to` names one. The name is KEPT AS GIVEN beside the resolved card and
// resolved again at every delivery, because a director's card id changes every
// time it is relaunched and a walk lasts hours.

// resolveReportTo turns a launch's `report_to` into a card on this room, or
// refuses with the handles that would have worked. Nothing is created yet.
func (d *Daemon) resolveReportTo(req LaunchRequest, name string) (*store.Task, error) {
	if hasTag(req.Tags, OriginAgentTag) {
		return nil, fmt.Errorf("report_to is for the board and `atrium launch --report-to`. " +
			"a session's launch reports to the session that made it")
	}
	if n, room, err := SplitAddress(name); err == nil && (room != "" || strings.Contains(name, "~")) {
		return nil, fmt.Errorf("%s is on another room (%s). report_to takes a card on this room for now, "+
			"because a launcher is a local card", n, room)
	}
	t := d.localTarget(name)
	if t == nil {
		return nil, fmt.Errorf("no session called %s to report to. handles that would have worked: %s. "+
			"no card was created", name, d.handleList())
	}
	return t, nil
}

// handleList is the handles on this room a report_to could have named.
func (d *Daemon) handleList() string {
	list, _ := d.peers("")
	handles := make([]string, 0, len(list))
	for _, p := range list {
		handles = append(handles, p.Handle)
	}
	if len(handles) == 0 {
		return "(none are running)"
	}
	return strings.Join(handles, ", ")
}

// reportsToLauncher is whether a card owes its launcher reports and stop
// notices: one an agent launched, or one told at launch who to report to.
func (d *Daemon) reportsToLauncher(t *store.Task) bool {
	return agentLaunched(t) || (t != nil && d.st.ReportTo(t.ID) != "")
}

// currentLauncher resolves a card's stored `report_to` again, and when it now
// names a different card than the one recorded, records that one so everything
// reading the card's launcher (the owed-report count, a report, a stop notice)
// agrees. Nil when the card has no report_to, or the name no longer resolves:
// the stored id is the fallback then.
func (d *Daemon) currentLauncher(worker *store.Task) *store.Task {
	if worker == nil {
		return nil
	}
	name := d.st.ReportTo(worker.ID)
	if name == "" {
		return nil
	}
	t := d.localTarget(name)
	if t == nil || t.ID == worker.ID {
		return nil
	}
	if worker.SpawnedByID != t.ID || worker.SpawnedBy != t.WireName {
		if err := d.st.SetLauncher(worker.ID, t.WireName, t.ID); err != nil {
			log.Printf("[atrium] could not point %s at its launcher %s: %v", worker.DisplayTitle(), name, err)
		} else {
			worker.SpawnedBy, worker.SpawnedByID = t.WireName, t.ID
		}
	}
	return t
}

// refreshLauncher re-resolves the launcher of the card with this id, for a door
// that reads the recorded launcher without going through launcherOf.
func (d *Daemon) refreshLauncher(id string) {
	if t, err := d.st.Get(id); err == nil {
		d.currentLauncher(t)
	}
}
