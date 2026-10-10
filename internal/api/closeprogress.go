package api

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// WHAT A CLOSE IS DOING, said while it does it, the way an open does (openprogress.go). A close can take a while
// (a stash pushes to the hub, a worktree with node_modules is a long delete), so each step of closeCard in its own
// order is broadcast as a `close-progress` event carrying the id the board sent with the close. A step says it is
// running, then ok or fail with what git said. Every step is logged with its seconds, like the open's.

// closeProgressEvent is the event kind a step is broadcast as.
const closeProgressEvent = "close-progress"

// closeSteps says the steps of one close, and times them. A nil one says nothing and every method takes it.
type closeSteps struct {
	id    string
	label string
	say   func(payload map[string]any)
	key   string
	line  string
	last  time.Time
	bad   []string
}

func (s *Server) newCloseSteps(id, label string) *closeSteps {
	c := &closeSteps{id: id, label: label}
	if id != "" {
		c.say = func(p map[string]any) { s.Broadcast(closeProgressEvent, p) }
	}
	return c
}

// begin ends the step that was running and says the next one is.
func (c *closeSteps) begin(key, line string) {
	if c == nil {
		return
	}
	c.end("")
	c.key, c.line, c.last, c.bad = key, line, time.Now(), nil
	c.emit("run", "")
}

// problem records what the running step could not do. The step ends as failed, and the close carries on.
func (c *closeSteps) problem(format string, a ...any) {
	if c == nil || c.key == "" {
		return
	}
	c.bad = append(c.bad, strings.TrimSpace(fmt.Sprintf(format, a...)))
}

// end closes the running step: ok, with detail, or fail with what it recorded. It logs the step's seconds.
func (c *closeSteps) end(detail string) {
	if c == nil || c.key == "" {
		return
	}
	log.Printf("[atrium close] %s: %s took %.2fs", c.label, c.key, time.Since(c.last).Seconds())
	if len(c.bad) > 0 {
		c.emit("fail", strings.Join(c.bad, ". "))
	} else {
		c.emit("ok", detail)
	}
	c.key = ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// sizeNote is " (26 MB)" for a size worth saying, and nothing for none.
func sizeNote(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n >= 1<<30:
		return fmt.Sprintf(" (%.2f GB)", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf(" (%d MB)", n>>20)
	}
	return fmt.Sprintf(" (%d KB)", max(n>>10, 1))
}

func (c *closeSteps) emit(state, detail string) {
	if c.say == nil {
		return
	}
	c.say(map[string]any{"id": c.id, "key": c.key, "line": c.line, "state": state, "detail": detail,
		"seconds": time.Since(c.last).Seconds()})
}
