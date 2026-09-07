package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/dovholuknf/atrium/internal/api"
)

// A SHELL BESIDE A WEDGED AGENT.
//
// Atrium owns a terminal per supervised card and that terminal belongs to the
// runner. When the agent stops answering and the question is `git status`, or
// what is holding that lock, there is nowhere else to type it, and the only
// answer left is to walk to the machine.
//
// So: a card may hold TWO terminals. The runner's, and one shell in the same
// directory.
//
// TWO, NOT N, and the difference is the whole design. A list of terminals needs
// naming, ordering, closing and a picker, and none of that is the thing being
// solved. One shell is a tool for looking at something. Three shells is a
// terminal multiplexer, and there is a good one already installed.
//
// WHAT A SHELL IS NOT:
//
//   - It is not a runner. `supervisor.get` and `supervisor.all` keep meaning
//     the runner, which every caller already assumes: the reaper, the park,
//     shelving, actions, messages and the exit recorder all mean the process
//     doing the work. A shell lives in a second map so that not one of them
//     has to learn a distinction it has no use for.
//   - It has no card and no status. Nothing it does moves the work along, so
//     recording it as activity would be a lie told in the operator's own event
//     log.
//   - It is not resumed, not retried, and not restarted. A shell that exits is
//     a shell somebody typed `exit` into.
//   - It does not survive the daemon, and it does not survive its card.

// shellIdleness is how long a shell may sit with nobody attached before it is
// closed.
//
// Not a resource limit: one idle pty costs almost nothing. It is about what the
// operator believes is running on their machine. A shell opened to check one
// thing, looked at, and navigated away from would otherwise stay open with a
// process in it forever, and nothing on the board would say so. Long enough
// that stepping away from the keyboard does not lose your place.
const shellIdleness = 30 * time.Minute

// shellHangup is how long a shell gets to notice its terminal closing before it
// is killed.
//
// A shell that is going to take the hint takes it immediately: there is no work
// to finish and nothing to flush. Windows `cmd.exe` frequently does not take it
// at all, so this is the delay paid on every close, and it is paid in front of
// somebody who just deleted a card. One second is long enough for a shell that
// responds and short enough not to read as the board hanging.
const shellHangup = time.Second

// SettingShellCommand names the shell to open.
//
// Empty means work it out. Configurable because the thing wanted here is the
// operator's own shell, and the one the operating system reports is frequently
// not it: on Windows `COMSPEC` is `cmd.exe` on a machine where everything else
// is PowerShell.
const SettingShellCommand = "shell_command"

// shellFor works out what to run.
//
// The setting, then the environment's own answer, then a floor that exists on
// every installation of the platform. The floor matters: a shell that cannot
// start is reported as an error the operator can read, but only if there was
// something to try.
func (d *Daemon) shellFor() (string, []string) {
	if v, err := d.st.Setting(SettingShellCommand); err == nil && strings.TrimSpace(v) != "" {
		fields := strings.Fields(strings.TrimSpace(v))
		return fields[0], fields[1:]
	}
	if runtime.GOOS == "windows" {
		if v := strings.TrimSpace(os.Getenv("COMSPEC")); v != "" {
			return v, nil
		}
		return "cmd.exe", nil
	}
	if v := strings.TrimSpace(os.Getenv("SHELL")); v != "" {
		return v, nil
	}
	return "/bin/sh", nil
}

// getShell, addShell and the rest are `get`, `add` and `remove` over the second
// map. Deliberately not folded into the first: see the header.
func (s *supervisor) getShell(taskID string) *runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shells[taskID]
}

func (s *supervisor) addShell(r *runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shells[r.taskID] = r
}

func (s *supervisor) removeShell(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.shells, taskID)
}

func (s *supervisor) allShells() []*runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*runner, 0, len(s.shells))
	for _, r := range s.shells {
		out = append(out, r)
	}
	return out
}

// EnsureShell starts a shell for a card if there is not already a live one.
//
// IDEMPOTENT ON PURPOSE. The board asks for a shell every time somebody presses
// the button, because the alternative is the board tracking which cards have
// one and being wrong about it after a restart. Asking twice returns the same
// terminal, which is what the operator meant both times.
func (d *Daemon) EnsureShell(taskID string) error {
	if r := d.sup.getShell(taskID); r != nil {
		select {
		case <-r.done:
			// Exited but not yet swept. Fall through and start another rather
			// than handing back a terminal that is already closed.
		default:
			return nil
		}
	}

	t, err := d.st.Get(taskID)
	if err != nil {
		return fmt.Errorf("no such card: %w", err)
	}
	// THE CARD'S DIRECTORY, which is the entire point. A shell that opens
	// wherever the daemon was started answers no question anybody had.
	cwd := strings.TrimSpace(t.Worktree)
	if cwd == "" {
		return fmt.Errorf("this card has no working directory, so there is nowhere to open a shell")
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return fmt.Errorf("%s is not there any more, so a shell cannot open in it", cwd)
	}

	cmdName, args := d.shellFor()
	return d.spawnShell(taskID, cmdName, args, cwd)
}

func (d *Daemon) spawnShell(taskID, cmdName string, args []string, cwd string) error {
	resolved, err := exec.LookPath(cmdName)
	if err != nil {
		return fmt.Errorf("%s is not on PATH. set a shell under the runners page, "+
			"or install that one: %w", cmdName, err)
	}

	p, err := pty.New()
	if err != nil {
		return fmt.Errorf("could not open a pseudo terminal: %w", err)
	}
	// The same launch size the runner's terminal gets, because its ring buffer
	// files those first bytes under the same width. A shell opened at the
	// platform default and a buffer saying it was 120 columns wide is a
	// mislabelled prompt banner on the first attach.
	sizeAtLaunch(p)
	c := p.Command(resolved, args...)
	c.Dir = cwd
	c.Env = d.shellEnv(taskID)
	if err := c.Start(); err != nil {
		p.Close()
		return fmt.Errorf("could not start %s: %w", cmdName, err)
	}

	r := &runner{
		taskID: taskID, pty: p, cmd: c, started: time.Now(),
		buf:      newRing(api.ScrollbackBytes(d.st), launchCols),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	r.touch()
	d.sup.addShell(r)

	go func() {
		chunk := make([]byte, 8192)
		for {
			n, err := p.Read(chunk)
			if n > 0 {
				_, _ = r.buf.Write(chunk[:n])
				r.fanout(chunk[:n])
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[atrium] shell for %s ended: %v", taskID, err)
				}
				return
			}
		}
	}()

	go d.awaitShellExit(r)
	go d.sweepIdleShell(r)

	// NOT WRITTEN TO THE CARD'S EVENT LOG, and the reason is two reasons.
	//
	// The event kind is a CHECK constraint, and `schema.go` says at length that
	// widening it means rebuilding the largest table in the database: "a fair
	// price once and a bad habit". This is not worth that price.
	//
	// And the log is a record of what a SESSION was allowed to do. A shell is
	// the operator, at their own machine, on the terminal they already own.
	// It is not gated, it never will be, and an entry saying a human opened a
	// terminal would be the only line in that log that answers to nobody.
	//
	// The daemon's own log gets it, which is where facts about processes go.
	log.Printf("[atrium] shell for %s: %s in %s", taskID, resolved, cwd)
	return nil
}

// shellEnv is the daemon's environment plus the one variable that says which
// card this is.
//
// DELIBERATELY WITHOUT `ATRIUM_AGENT_NAME`. Every runner gets both, and every
// hook reads them to say who is doing what. A shell that carried the agent name
// would have anything started from it, including a second claude, filing its
// activity against the card as though the runner had done it. Which card it is
// in is useful and true; who is doing it is neither.
func (d *Daemon) shellEnv(taskID string) []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "ATRIUM_AGENT_NAME=") || strings.HasPrefix(kv, "ATRIUM_TASK_ID=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "ATRIUM_TASK_ID="+taskID)
}

// awaitShellExit is `awaitExit` with everything about the card taken out.
//
// The difference between the two IS the design. That one records an exit event,
// reads the last output to explain a startup failure, retries a stale resume,
// and moves the card to `dead`. A shell exiting means somebody typed `exit`, so
// none of it applies, and any of it would file a card as dead because its
// operator closed a terminal.
func (d *Daemon) awaitShellExit(r *runner) {
	_ = r.cmd.Wait()
	r.exitOnce.Do(func() { close(r.done) })
	r.closeWatchers()
	r.closePTY()
	d.sup.removeShell(r.taskID)
	log.Printf("[atrium] shell for %s closed", r.taskID)
}

// CloseShell ends a card's shell now, if it has one.
//
// Called when the card goes: archived, deleted, or swept. A pty holding a
// working directory open after its card is gone is a process nothing on the
// board accounts for, and on Windows it also locks the directory against the
// removal that is usually the next thing to happen.
// IT WAITS FOR THE PROCESS TO ACTUALLY GO, and that is not tidiness.
//
// Closing the terminal is a hangup, not a kill, so the shell runs on for a
// moment with the card's directory as its working directory. On Windows that
// directory cannot be removed while any process sits in it, and deleting a card
// is often followed by removing the tree. Returning early makes that failure
// land somewhere else, worded as a permission error, with nothing connecting it
// to the shell.
//
// Escalation, in the same order and for the same reason as `windDown`: hang up,
// wait, then kill. The wait is shorter because a shell has no work in flight.
func (d *Daemon) CloseShell(taskID string) {
	r := d.sup.getShell(taskID)
	if r == nil {
		return
	}
	d.sup.removeShell(taskID)
	r.closePTY()

	select {
	case <-r.done:
	case <-time.After(shellHangup):
		if r.cmd != nil && r.cmd.Process != nil {
			log.Printf("[atrium] shell for %s did not close, killing it", taskID)
			_ = r.cmd.Process.Kill()
		}
		// Waited on again, because a kill is a request too and the handle is
		// not released until the process has actually gone.
		select {
		case <-r.done:
		case <-time.After(shellHangup):
			log.Printf("[atrium] shell for %s is still holding its directory", taskID)
		}
	}

	r.exitOnce.Do(func() { close(r.done) })
	r.closeWatchers()
}

// stopShells closes every shell, for shutdown.
func (d *Daemon) stopShells() {
	live := d.sup.allShells()
	if len(live) == 0 {
		return
	}
	log.Printf("[atrium] closing %d shell(s)", len(live))
	for _, r := range live {
		d.CloseShell(r.taskID)
	}
}

// sweepIdleShell closes a shell nobody has looked at for a while.
//
// The clock is on ATTACHMENT, not on output: a shell running a long build with
// a browser watching it is being used, and a shell sitting at a prompt with
// nothing attached is not. `touch` is called when somebody attaches and when
// they leave, so the window starts from the last time it was closed rather than
// from when it was opened.
func (d *Daemon) sweepIdleShell(r *runner) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-tick.C:
			// THE CARD GOING IS CHECKED BEFORE IDLENESS, and it is checked
			// here rather than only at the delete endpoint.
			//
			// A single card deleted from the board closes its shell at once,
			// because the operator is watching and a process that outlives
			// what it belonged to is a surprise. But cards also leave in bulk:
			// the prune timer, the archive sweep, `POST /v1/tasks/prune`.
			// Threading a list of ids out of each of those and into the
			// supervisor is three call sites that can be forgotten. Asking
			// whether the card is still there is one, and it cannot be.
			if _, err := d.st.Get(r.taskID); err != nil {
				log.Printf("[atrium] shell for %s closed: its card is gone", r.taskID)
				d.CloseShell(r.taskID)
				return
			}
			if r.watching() {
				continue
			}
			if time.Since(r.lastSeen()) < shellIdleness {
				continue
			}
			log.Printf("[atrium] shell for %s closed after %s with nobody attached",
				r.taskID, shellIdleness)
			d.CloseShell(r.taskID)
			return
		}
	}
}
