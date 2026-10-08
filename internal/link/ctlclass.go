package link

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Which control tools a caller is shown.
//
// THIS IS NOT A BOUNDARY. The class comes from `X-Atrium-Agent`, a header anything
// on loopback can set, and a lookup that fails serves the FULL set. Every handler
// keeps every check it has, so a worker that reached a director tool by lying about
// who it is meets the same refusals it always did. What this buys is tidiness and
// context: a worker no longer carries five long tool descriptions it must not use.
//
// A tool the caller's server lacks answers with the SDK's unknown-tool error. That
// is the whole enforcement, and nothing else checks the class.
//
// FIXED AT THE FIRST REQUEST. MCP clients read `tools/list` once, at initialize, so
// the class that counts is the one the session had when it first connected. A retag
// lands at the next session start, and nothing sends `list_changed`.

// ctlClass is which set of control tools a caller gets.
type ctlClass int

const (
	// classFull is every tool. The default, and what any doubt resolves to.
	classFull ctlClass = iota
	// classWorker is the tools a worker uses to talk and to report.
	classWorker
)

// workerTools is the whole of the worker set, by name.
//
// A TOOL IS IN THE WORKER SET ONLY WHEN IT IS NAMED HERE. `addTool` filters on
// this list and not on a list of exclusions, so a tool added later, such as the
// git tools a `registerGit` adds, lands in `full` alone with no further change.
var workerTools = map[string]bool{
	"atrium_status": true,
	"atrium_peers":  true,
	"atrium_say":    true,
	"atrium_report": true,
	// How a worker ends, in one line. See ctl_end.go.
	"atrium_done":    true,
	"atrium_blocked": true,
	"atrium_task":    true,
	"atrium_alias":   true,
	// Workers write the reports, so a worker publishes. See docs_mcp.go.
	"atrium_publish": true,
	// The cards that push to the hub are workers. See git_hub.go.
	"atrium_git_push": true,
	// And the ones that read code that is not in their cwd. See git_url.go.
	"atrium_git_url": true,
	// And the ones that need a build machine. See resources_mcp.go.
	"atrium_resources": true,
}

// inClass reports whether a tool is served to a class.
func inClass(name string, class ctlClass) bool {
	return class == classFull || workerTools[name]
}

// addTool is `mcp.AddTool` for the class being built: it registers the tool when
// the class includes it and does nothing otherwise.
func addTool[In, Out any](s *mcp.Server, class ctlClass, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if inClass(t.Name, class) {
		mcp.AddTool(s, t, h)
	}
}

const (
	// classTTL is how long a lookup is remembered, so a session's tools/list and
	// its calls do not each cost a trip to the board.
	classTTL = time.Minute
	// classCacheMax bounds the cache. Past it the oldest entries are dropped, and
	// dropping one only costs a lookup.
	classCacheMax = 512
)

// classEntry is one remembered answer.
type classEntry struct {
	class   ctlClass
	expires time.Time
}

// classOf is the class of the session that sent a request.
func (c *controlMCP) classOf(r *http.Request) ctlClass {
	agent := strings.TrimSpace(r.Header.Get(AgentHeader))
	room := strings.TrimSpace(r.Header.Get(RoomHeader))
	if agent == "" {
		// Not one of atrium's sessions, so nothing to look up.
		return classFull
	}
	key := strings.ToLower(room) + "\x00" + agent
	now := c.classNow()

	c.classMu.Lock()
	e, ok := c.classes[key]
	c.classMu.Unlock()
	if ok && now.Before(e.expires) {
		return e.class
	}

	class, found := c.lookupClass(r.Context(), room, agent)
	if !found {
		// FAIL OPEN, and do not remember it: a card not on the board yet at the
		// session's first connect must not keep the full set for a minute once it is.
		return classFull
	}

	c.classMu.Lock()
	defer c.classMu.Unlock()
	if c.classes == nil {
		c.classes = map[string]classEntry{}
	}
	for k, v := range c.classes {
		if !now.Before(v.expires) {
			delete(c.classes, k)
		}
	}
	for len(c.classes) >= classCacheMax {
		// Every entry lives the same TTL, so the soonest to expire is the oldest.
		oldest, at := "", time.Time{}
		for k, v := range c.classes {
			if oldest == "" || v.expires.Before(at) {
				oldest, at = k, v.expires
			}
		}
		delete(c.classes, oldest)
	}
	c.classes[key] = classEntry{class: class, expires: now.Add(classTTL)}
	return class
}

// classNow is the clock, injectable so a test can pass the TTL.
func (c *controlMCP) classNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// lookupClass finds the calling card on its room and reads its tags. found is
// false when the board could not be asked or no card answers to that name.
//
// A WORKER is tagged SubagentTag and not DirectorTag. Everything else is full:
// directors, the resident merger, and a session a human started, which carries no
// tags at all.
func (c *controlMCP) lookupClass(ctx context.Context, room, agent string) (ctlClass, bool) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return classFull, false
	}
	best := pickCard(body.Tasks, agent)
	if best == nil {
		return classFull, false
	}
	if hasTag(best.Tags, SubagentTag) && !hasTag(best.Tags, DirectorTag) {
		return classWorker, true
	}
	return classFull, true
}

// pickCard is the card a wire name belongs to. A wire name can be reused by a card that has
// since died, so a live one is preferred, then the newest, which is how resolvePeer orders an
// alias. Nil when no card answers to it.
func pickCard(tasks []ctlCard, agent string) *ctlCard {
	var best *ctlCard
	for i := range tasks {
		t := &tasks[i]
		if t.Wire != agent {
			continue
		}
		switch {
		case best == nil, best.Status == "dead" && t.Status != "dead":
			best = t
		case (best.Status == "dead") == (t.Status == "dead") && t.Created > best.Created:
			best = t
		}
	}
	return best
}
