package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Running the commands that find work.
//
// Atrium holds an argv and an interval, runs it, and reads its stdout as
// intake items. It never learns what GitHub is, and there is nowhere in the
// source table to put a credential: `gh` already has one, in the keyring it
// already uses. See docs/intake-design.md, layer 2.
//
// Everything here obeys the rule the resilience section states for hooks, for
// the same reason: intake is a suggestion, not durable state. A source that
// fails is reported on its row and retried on its next tick, and nothing it
// does can halt anything.

const (
	// sourcePoll is how often the scheduler looks for work to do. Not how
	// often a source runs, which is the source's own interval. This is only
	// the resolution at which "is it due yet" is asked.
	sourcePoll = 20 * time.Second

	// sourceOutputLimit bounds what one run may say.
	//
	// A source returning more than this is reporting a repository rather than
	// a work queue, and reading forty megabytes into memory to find that out
	// is the failure it is meant to prevent.
	sourceOutputLimit = 1 << 20

	// sourceTimeout bounds one run.
	//
	// A source is a network call wearing a shell script, and the thing network
	// calls do is hang. Two minutes is generous for `gh issue list` and short
	// enough that a wedged source is noticed on the same afternoon.
	sourceTimeout = 2 * time.Minute
)

// sourceLoop asks whether anything is due, on a fixed short tick.
//
// The tick is the resolution of the question, not the rate anything runs at.
// A source with a fifteen minute interval is checked every twenty seconds and
// runs every fifteen minutes, which costs one query and keeps the interval on
// the row where an operator can change it and see it take effect.
func (d *Daemon) sourceLoop(ctx context.Context) {
	tick := time.NewTicker(sourcePoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		d.pollSources(ctx)
	}
}

// pollSources runs every source that is due.
//
// Sequential rather than concurrent. There are a handful of these, they are
// each a child process making network calls, and running them one at a time
// means a slow source delays the next tick rather than competing with four
// others for the machine.
func (d *Daemon) pollSources(ctx context.Context) {
	due, err := d.st.DueSources(time.Now().UTC())
	if err != nil {
		log.Printf("[atrium] checking sources: %v", err)
		return
	}
	for _, s := range due {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.runSource(ctx, s)
	}
}

// runSource runs one source and records what happened.
//
// The outcome is always written, success or failure, because a row that says
// when it last ran and what it said is the whole reporting surface. Returns
// how many items were new, for the caller that asked for a run by hand.
func (d *Daemon) runSource(ctx context.Context, s *store.Source) (created int, err error) {
	items, runErr := d.readSource(ctx, s)
	if runErr == nil {
		for _, item := range items {
			task, err := func() (*store.Task, error) {
				t, isNew, err := d.st.Offer(item)
				if err != nil {
					return nil, err
				}
				if isNew {
					created++
				}
				return t, nil
			}()
			if err != nil {
				// An item that fails to land FAILS THE RUN.
				//
				// Every item was checked before any was offered, so reaching
				// here is a write failing rather than a bad item, and a write
				// failing is the store's problem and not this batch's.
				//
				// Items already offered stay. That is safe rather than untidy:
				// offering is keyed on the pair, so the next tick sees them as
				// known and reports nothing new. What must NOT happen is the
				// row saying the run succeeded, because then nothing ever
				// reconciles and the inbox is quietly short.
				runErr = fmt.Errorf("could not offer %s: %w", item.ExternalID, err)
				break
			}
			d.publishTask(task.ID)
		}
	}

	disabled, recErr := d.st.SourceRan(s.ID, len(items), runErr)
	if recErr != nil {
		log.Printf("[atrium] recording the run of source %s: %v", s.ID, recErr)
	}
	switch {
	case disabled:
		log.Printf("[atrium] source %s failed %d times and is switched off: %v",
			s.ID, store.MaxSourceFailures, runErr)
	case runErr != nil:
		log.Printf("[atrium] source %s: %v", s.ID, runErr)
	case created > 0:
		log.Printf("[atrium] source %s offered %d new item(s) of %d reported",
			s.ID, created, len(items))
	}
	return created, runErr
}

// limitedWriter passes bytes through until a budget runs out, then discards
// the rest.
//
// Not an error on overflow: returning one makes the child's next write fail,
// which for a shell script means a broken pipe and a confusing exit status
// instead of the clear "printed too much" the caller wants to report. The
// caller checks how much arrived and decides.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.left <= 0 {
		return len(p), nil
	}
	take := p
	if len(take) > l.left {
		take = take[:l.left]
	}
	n, err := l.w.Write(take)
	l.left -= n
	if err != nil {
		return n, err
	}
	// The child is told everything landed, so it is not the one that finds out
	// about the limit.
	return len(p), nil
}

// RunSourceNow runs one source outside its interval.
//
// A source is a script somebody just wrote, and the question they have is
// whether it works. Waiting fifteen minutes to find out that a path was wrong
// is how a feature goes unused.
//
// A disabled source still runs when asked. Pressing the button IS the operator
// saying to run it, and refusing on the grounds that it is switched off would
// leave no way to test one before turning it on.
func (d *Daemon) RunSourceNow(id string) (int, error) {
	s, err := d.st.SourceByID(id)
	if err != nil {
		return 0, err
	}
	return d.runSource(context.Background(), s)
}

// readSource runs the command and parses what it printed.
func (d *Daemon) readSource(ctx context.Context, s *store.Source) ([]store.IntakeItem, error) {
	if strings.TrimSpace(s.Cmd) == "" {
		return nil, errors.New("no command configured")
	}
	runCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, s.Cmd, s.Args...)
	cmd.Dir = s.Cwd
	// The daemon's own environment, minus the markers that would make a child
	// think it is inside the session atrium was started from. Exactly what a
	// launched runner gets, and for the same reason.
	cmd.Env = childEnv(nil, nil)

	// Bounded WHILE reading, not after.
	//
	// `cmd.Output()` buffers everything the child prints and hands it over,
	// so checking the length afterwards is a check made once the memory has
	// already been allocated. A source that dumps a repository would be
	// refused, having first been read in full. The whole point of the bound is
	// that it never happens.
	//
	// One byte over the limit is enough to know: the read stops there and the
	// child is left to be killed by the context.
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: sourceOutputLimit + 1}
	// Stderr is bounded too, and much smaller. It exists to put a readable
	// reason on the row, and a row is one line.
	cmd.Stderr = &limitedWriter{w: &errOut, left: 8 << 10}

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("took longer than %s and was stopped", sourceTimeout)
	}
	if out.Len() > sourceOutputLimit {
		return nil, fmt.Errorf("printed more than the %d byte limit and was stopped",
			sourceOutputLimit)
	}
	if err != nil {
		// A source that fails is usually a source whose command said why on
		// stderr, and the exit status alone is not worth reading.
		if errOut.Len() > 0 {
			return nil, fmt.Errorf("%v: %s", err, firstLine(errOut.String()))
		}
		return nil, err
	}

	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		// Nothing to report is a normal answer and not a failure. A queue with
		// nothing in it is the state you want.
		return nil, nil
	}

	var items []store.IntakeItem
	if trimmed[0] == '[' {
		if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
			return nil, fmt.Errorf("printed something that is not a list of items: %w", err)
		}
	} else {
		var one store.IntakeItem
		if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
			return nil, fmt.Errorf("printed something that is not an item: %w", err)
		}
		items = []store.IntakeItem{one}
	}

	// Every item is checked before any is offered, so a batch with one bad
	// entry is a source to fix rather than a partial import to reconcile.
	for i := range items {
		if err := items[i].Normalize(); err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}
	}
	return items, nil
}
