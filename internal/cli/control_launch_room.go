package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

// atrium_launch with `room`, on the stdio control server. See docs/fabric/cross-room-say-design.md.
//
// THE SAME ROAD AS atrium_exit AND atrium_task on another room: this room's daemon
// (POST /v1/peers/launch) asks its hub, and the hub launches on the target exactly as its
// own atrium_launch with `room` does. Nothing is written or looked at on THIS machine:
// the brief travels in the request and the target room writes BRIEF.md on its own disk,
// and `cwd` is a path over there.

// launchAcrossWait bounds the whole trip. Longer than the daemon's own bound on the hub, so
// the daemon's sentence reaches the caller rather than this giving up first.
const launchAcrossWait = 55 * time.Second

// launchAcrossRoom launches on `room` when it is not this room. done is false when the
// room turned out to be this one, and the caller then launches here as it always has.
func launchAcrossRoom(ctx context.Context, in LaunchInput, harness, room string) (done bool, out LaunchOutput, err error) {
	if strings.EqualFold(room, strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))) {
		return false, out, nil
	}
	req := map[string]any{
		"room": room, "from": strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")),
		"runner": harness, "cwd": in.Cwd, "title": in.Title, "why": in.Why,
		"prompt": strings.TrimSpace(in.Prompt), "brief": strings.TrimSpace(in.Brief), "tags": in.Tags,
		"model": in.Model, "effort": in.Effort, "args": in.Args, "env": in.Env,
		"lean_agents": in.LeanAgents, "lean_skills": in.LeanSkills,
	}
	var res struct {
		Local   bool   `json:"local"`
		Card    string `json:"card"`
		Handle  string `json:"handle"`
		Title   string `json:"title"`
		Status  string `json:"status"`
		Watch   string `json:"watch"`
		Brief   string `json:"brief"`
		Model   string `json:"model"`
		Effort  string `json:"effort"`
		Warning string `json:"warning"`
	}
	switch err := askFor(ctx, launchAcrossWait, http.MethodPost, "/v1/peers/launch", req, &res); {
	case olderRoom(err):
		return true, out, fmt.Errorf("this room is older than launching on another room, so it cannot launch on %s. "+
			"update this room", room)
	case err != nil:
		return true, out, err
	case res.Local:
		return false, out, nil
	}
	out.Card, out.Handle, out.Title, out.Status = res.Card, res.Handle, res.Title, res.Status
	out.Watch, out.Brief, out.Model, out.Effort = res.Watch, res.Brief, res.Model, res.Effort
	out.Note = res.Warning + link.LaunchStartedNote
	return true, out, nil
}
