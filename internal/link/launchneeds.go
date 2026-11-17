package link

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// launchNeedsFresh is how recent a pass has to be for a launch's needs.
const launchNeedsFresh = 24 * time.Hour

// roomForNeeds never falls back to a room that fails a need, and keeps the launcher's own room when it qualifies.
func roomForNeeds(v CapabilityView, needs []string, room string, named bool, now time.Time) (string, error) {
	var ok []string
	for _, m := range CapabilityWhich(v, needs, launchNeedsFresh, now) {
		if m.OK {
			ok = append(ok, m.Room)
		}
	}
	for _, r := range ok {
		if room != "" && wire.EqualFold(r, room) {
			return r, nil
		}
	}
	if len(ok) > 0 && !named {
		return ok[0], nil
	}
	return "", &ctl.RefusedError{Msg: "no room can take this launch: " + needsWhy(v, needs, room, named, now)}
}

// RoomForNeeds is roomForNeeds for the stdio server, which places an unnamed launch before it asks the hub.
func RoomForNeeds(v CapabilityView, needs []string, room string, named bool, now time.Time) (string, error) {
	return roomForNeeds(v, needs, room, named, now)
}

func needsWhy(v CapabilityView, needs []string, room string, named bool, now time.Time) string {
	var out []string
	for _, rm := range v.Rooms {
		if named && !wire.EqualFold(rm.Name, room) {
			continue
		}
		why := "not attached"
		if rm.Online {
			why = ""
			for _, m := range CapabilityWhich(v, needs, launchNeedsFresh, now) {
				if wire.EqualFold(m.Room, rm.Name) && m.Stale {
					why = "only stale passes"
				}
			}
			if why == "" {
				why = firstUnmet(v, needs, rm.Name, now)
			}
		}
		out = append(out, rm.Name+": "+why)
	}
	if len(out) == 0 {
		return "no room is known"
	}
	sort.Strings(out)
	return strings.Join(out, ". ")
}

func firstUnmet(v CapabilityView, needs []string, room string, now time.Time) string {
	for _, n := range needs {
		found := false
		for _, r := range v.Results {
			if wire.EqualFold(r.Room, room) && r.Cap == n {
				found = true
				if r.State != "pass" {
					why := n + " " + r.State
					if r.Reason != "" {
						why += ": " + r.Reason
					}
					return why
				}
			}
		}
		if !found {
			return n + " has no result"
		}
	}
	return "a need is not met"
}

func (c *controlMCP) needsRoom(ctx context.Context, in launchInput, room string, named bool) (string, error) {
	if len(in.Needs) == 0 {
		return room, nil
	}
	var v CapabilityView
	if err := c.Ask(ctx, http.MethodGet, "/_hub/capabilities", "", nil, &v); err != nil {
		return "", fmt.Errorf("could not read the rooms' capabilities to place this launch: %w", err)
	}
	return roomForNeeds(v, in.Needs, room, named, time.Now())
}
