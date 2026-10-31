package link

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The hub's half of a room's atrium_launch with `room`. See docs/fabric/cross-room-say-design.md.
//
// ONE LAUNCH, NOT TWO. This is the hub's own atrium_launch with `room` set (launchHandler),
// reached by a different door: both end in launchOnRoom, so the gate, the cap on the
// TARGET room, the origin and subagent tags, the lean default, the report line and the
// lineage are one piece of code. The only thing that differs is who the launcher is, and
// that comes from the connection here, not from a header.
//
// THE BRIEF TRAVELS IN THE BODY. /v1/launch carries the text and the target room's own
// daemon writes BRIEF.md on ITS disk. The launching room's disk is not touched, and its
// directory is never looked at: `cwd` is a path on the other machine.
//
// NEVER HELD. A launch is not idempotent, so a failure that may have come after the post is
// `unconfirmed` and the launching room says so rather than trying again. A room that is
// known and not attached is `unreachable` (reachable), found out before anything is posted.

// launchAcross starts the session `req.Launch` describes on `req.Room`, for the room `from`.
func (c *controlMCP) launchAcross(ctx context.Context, from string, req RelayRequest) RelayAnswer {
	target := strings.TrimSpace(req.Room)
	spec := req.Launch
	switch {
	case target == "" || equalFold(target, from):
		return RelayAnswer{Code: http.StatusBadRequest,
			Error: "the relay launches on another room. this one names the room it came from"}
	case spec == nil || strings.TrimSpace(spec.Cwd) == "":
		return RelayAnswer{Code: http.StatusBadRequest,
			Error: "say where to run it. atrium does not create the directory"}
	}
	// A room the hub knows nothing of is a 404 that lists the rooms it does know. One it
	// knows and cannot reach is unreachable. Either way nothing has been posted.
	if ans, ok := c.reachable(ctx, target); !ok {
		return ans
	}
	harness := strings.TrimSpace(spec.Runner)
	if harness == "" {
		harness = "claude"
	}
	// THE LAUNCHER, as the hub's own launch names it: `me@myroom`, and its card as
	// `myroom~id`, so the worker's reports and notices come back across to this card. The
	// room comes from the connection, never from the request, so a room cannot launch as
	// another room's card.
	spawnedBy, spawnedByID, spawnedByDir := strings.TrimSpace(req.From), "", ""
	if spawnedBy != "" {
		if id, _, err := c.resolvePeer(ctx, from, spawnedBy); err == nil {
			spawnedByID = wire.TagFor(from, id)
			spawnedByDir = c.launcherDir(ctx, from, id)
		}
		spawnedBy += "@" + from
	}
	in := launchInput{
		Cwd: spec.Cwd, Title: spec.Title, Why: spec.Why, Prompt: spec.Prompt, Brief: spec.Brief,
		Runner: spec.Runner, Tags: spec.Tags, Model: spec.Model, Effort: spec.Effort, Args: spec.Args,
		Env: spec.Env, LeanAgents: spec.LeanAgents, LeanSkills: spec.LeanSkills, Superlean: relaySuperlean(spec.Superlean), Lifetime: spec.Lifetime, Isolated: spec.Item, Room: target,
	}
	out, err := c.launchOnRoom(ctx, in, harness, target, from, spawnedBy, spawnedByID, spawnedByDir)
	c.auditLaunchAcross(target, from, req.From, harness, out, err)
	if err != nil {
		return launchRefusal(err)
	}
	return RelayAnswer{
		OK: true, To: out.Handle, Card: out.Card,
		Task:  &RelayTask{Card: out.Card, Handle: out.Handle, Title: out.Title, Status: out.Status},
		Watch: out.Watch, Brief: out.Brief, Model: out.Model, Effort: out.Effort,
		// The dropped-options sentence, without the note every launch ends on.
		Warning: strings.TrimSuffix(out.Note, LaunchStartedNote),
	}
}

// launchRefusal turns a failed launch into an answer. Unlike refusal, a gateway failure is
// UNCONFIRMED, never unreachable: the post may have started the session before the answer
// was lost, and a second launch would start a second one.
func launchRefusal(err error) RelayAnswer {
	var ref *ctl.RefusedError
	if errors.As(err, &ref) {
		return RelayAnswer{Code: http.StatusConflict, Error: ref.Msg}
	}
	var be *ctl.BoardError
	if errors.As(err, &be) {
		switch be.Code {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return RelayAnswer{Unconfirmed: true, Code: be.Code, Error: be.Msg}
		}
	}
	return refusal(err)
}

// auditLaunchAcross writes the line a hub-side atrium_launch would, on the target room, by
// the launcher on the asking room. Ids, handles and rooms only, never a prompt or a brief.
func (c *controlMCP) auditLaunchAcross(target, from, by, harness string, out launchOutput, err error) {
	if c.audit == nil {
		return
	}
	by = strings.TrimSpace(by)
	if by == "" {
		by = "unnamed"
	}
	what := "launch " + harness
	if out.Card != "" {
		what += " as " + out.Card
	}
	c.audit(target, "ctl-launch", "by "+by+"@"+from+" (claimed): "+what+", "+auditOutcome(err))
}

// relaySuperlean is a room's superlean flag as the hub's launchInput takes it: only true is a choice, false is absent.
func relaySuperlean(on bool) *bool {
	if !on {
		return nil
	}
	return &on
}
