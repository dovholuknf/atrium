package api

import (
	"context"
	"log"
	"time"
)

// WHAT AN OPEN IS DOING, said while it does it. A pasted link takes as long as a fetch and a worktree take, and a
// step that is slow must never be silent. The board sends an id with the open, and each step is broadcast as an
// `open-progress` event carrying that id and a line, which the board shows in a toast-sized strip instead of a dialog.
// Every step is also logged with its seconds, which is how a slow one is found afterwards.

// openProgressEvent is the event kind a step is broadcast as.
const openProgressEvent = "open-progress"

type progressKey struct{}

// opSteps says the steps of one open, and times them.
type opSteps struct {
	id    string
	label string
	say   func(step string)
	last  time.Time
	name  string
}

func (s *Server) newOpSteps(id, label string) *opSteps {
	o := &opSteps{id: id, label: label, last: time.Now()}
	if id != "" {
		o.say = func(step string) { s.Broadcast(openProgressEvent, map[string]any{"id": id, "step": step}) }
	}
	return o
}

// begin ends the step that was running, logging its seconds, and says the next one.
func (o *opSteps) begin(name, line string) {
	if o == nil {
		return
	}
	o.end()
	o.name, o.last = name, time.Now()
	if o.say != nil && line != "" {
		o.say(line)
	}
}

// end logs the running step's seconds.
func (o *opSteps) end() {
	if o == nil || o.name == "" {
		return
	}
	log.Printf("[atrium open] %s: %s took %.2fs", o.label, o.name, time.Since(o.last).Seconds())
	o.name = ""
}

func withOpSteps(ctx context.Context, o *opSteps) context.Context {
	return context.WithValue(ctx, progressKey{}, o)
}

// stepsOf is the steps recorder a context carries, or nil, which every method takes.
func stepsOf(ctx context.Context) *opSteps {
	o, _ := ctx.Value(progressKey{}).(*opSteps)
	return o
}
