package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
)

// A launch on another room, for this room's atrium_launch with `room`. See
// docs/fabric/cross-room-say-design.md.
//
// CARRIED, NEVER HELD. A say that cannot be sent is kept and sent when the hub answers.
// A launch is not: sent late it starts a session nobody is waiting for, and sent twice it
// starts two. So a launch the hub or the target cannot take is refused now, and one that
// may have started is said to be unconfirmed.
//
// NOTHING HERE TOUCHES THIS ROOM'S DISK. The brief rides in the request and the target
// room writes BRIEF.md on its own disk, and `cwd` is a path on the other machine, so it is
// not looked at here.

// RelayLaunch is one launch for another room. From is the launcher's handle here, with
// no room: the hub adds this room's name from its certificate.
type RelayLaunch struct {
	From, Room string
	// The launch itself, as the hub's atrium_launch takes it.
	Cwd, Title, Why, Prompt, Brief, Runner string
	Tags                                   []string
	Model, Effort                          string
	Args                                   []string
	Env                                    map[string]string
	LeanAgents, LeanSkills                 []string
}

// roomLaunchIn is the body of `POST /v1/peers/launch`: the launch the stdio atrium_launch
// was given, the room it is for, and the session asking.
type roomLaunchIn struct {
	Room       string            `json:"room"`
	From       string            `json:"from"`
	Cwd        string            `json:"cwd"`
	Title      string            `json:"title"`
	Why        string            `json:"why"`
	Prompt     string            `json:"prompt"`
	Brief      string            `json:"brief"`
	Runner     string            `json:"runner"`
	Tags       []string          `json:"tags"`
	Model      string            `json:"model"`
	Effort     string            `json:"effort"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	LeanAgents []string          `json:"lean_agents"`
	LeanSkills []string          `json:"lean_skills"`
}

// handleRoomLaunch is `POST /v1/peers/launch`: start a session on another room, through
// the hub. A room that is this one answers `{"local": true}` and starts nothing, and the
// caller launches here the way it always has.
func (d *Daemon) handleRoomLaunch(w http.ResponseWriter, r *http.Request) {
	var in roomLaunchIn
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	other := d.otherRoom(in.Room)
	if other == "" {
		writeJSONCode(w, http.StatusOK, map[string]any{"local": true})
		return
	}
	if strings.TrimSpace(in.Cwd) == "" {
		writeJSONCode(w, http.StatusBadRequest, errBody("say where to run it. atrium does not create the directory"))
		return
	}
	rl := d.relay()
	if rl == nil {
		writeJSONCode(w, http.StatusServiceUnavailable, errBody("this atrium is not a room linked to a hub, so it "+
			"cannot launch on room "+other))
		return
	}
	// The launcher's handle as this room knows it, which is what the hub is told.
	from := strings.TrimSpace(in.From)
	if sender, _ := d.st.GetByWireName(d.st.Qualify(from)); sender != nil {
		from = sender.WireName
	}
	cctx, cancel := context.WithTimeout(r.Context(), relayWait)
	defer cancel()
	res, err := rl.Launch(cctx, RelayLaunch{
		From: from, Room: other, Cwd: in.Cwd, Title: in.Title, Why: in.Why, Prompt: in.Prompt, Brief: in.Brief,
		Runner: in.Runner, Tags: in.Tags, Model: in.Model, Effort: in.Effort, Args: in.Args, Env: in.Env,
		LeanAgents: in.LeanAgents, LeanSkills: in.LeanSkills,
	})
	code, body := launchAnswer(other, res, err)
	if code >= 400 {
		log.Printf("[atrium] %s's launch on %s was not made (%d): %v", from, other, code, body["error"])
	} else {
		log.Printf("[atrium] %s launched %v on %s", from, body["card"], other)
	}
	writeJSONCode(w, code, body)
}

// launchAnswer turns the hub's answer into this room's. Every failure is a refusal now,
// because nothing is held for a launch.
func launchAnswer(room string, res RelayResult, err error) (int, map[string]any) {
	switch {
	case errors.Is(err, ErrRelayOld):
		return http.StatusBadGateway, errBody("the hub is older than launching on another room, so it cannot " +
			"carry this. update the hub")
	case errors.Is(err, ErrRelayUnconfirmed), err == nil && res.Unconfirmed:
		why := res.Error
		if err != nil {
			why = err.Error()
		}
		return http.StatusGatewayTimeout, errBody("it may or may not have started on " + room + " (" + why +
			"). it is not retried, so look at the peers on that room before launching again")
	case errors.Is(err, ErrRelayDown):
		return http.StatusServiceUnavailable, errBody("the hub is not answering (" + err.Error() +
			"), so nothing was started on " + room + ". nothing is held: launch again when it answers")
	case err != nil:
		return http.StatusBadGateway, errBody(err.Error())
	case !res.OK && strings.Contains(res.Error, "does not know the relay op"):
		// A HUB OLDER THAN THIS. It carries a say and a card, not a launch.
		return http.StatusBadGateway, errBody("the hub is older than launching on another room, so it cannot " +
			"carry this. update the hub")
	case !res.OK && res.Unreachable:
		return http.StatusServiceUnavailable, errBody("room " + room + " is not answering (" + res.Error +
			"), so nothing was started. nothing is held: launch again when it answers")
	case !res.OK:
		code := res.Code
		if code < 400 {
			code = http.StatusBadGateway
		}
		return code, errBody(res.Error)
	}
	out := map[string]any{"card": res.Card, "handle": res.To, "watch": res.Watch, "brief": res.Brief,
		"model": res.Model, "effort": res.Effort, "warning": res.Warning}
	if t := res.Task; t != nil {
		out["title"], out["status"] = t.Title, t.Status
	}
	return http.StatusOK, out
}
