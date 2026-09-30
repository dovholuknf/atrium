package daemon

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sync"

	"github.com/aymanbagabas/go-pty"

	"github.com/dovholuknf/atrium/internal/store"
)

// term is a terminal atrium owns, as far as the supervisor may ask of it.
//
// It is the seam between the daemon's policy (the ring, the screen model, the
// typing gate, viewports, carryover, injection) and the thing that holds a
// process on a pseudo terminal. Today that is go-pty in this process. The
// design in docs/rnd/rolling-restart-design.md puts it in a separate host, and
// the interface is written so a host can satisfy it: output is a stream of
// bytes, an exit is a code delivered once, and there is nothing here that only
// makes sense inside one process (no handle, no *os.Process, no file
// descriptor).
type term interface {
	// Read is the output stream. It returns io.EOF or an error when the
	// terminal is gone.
	Read(p []byte) (int, error)
	// Write is input. It may take fewer bytes than given, like an io.Writer.
	Write(p []byte) (int, error)
	// Resize sets the terminal's size.
	Resize(cols, rows int) error
	// Pid is the process the terminal started. A reconnect hint, never an
	// identity.
	Pid() int
	// Wait blocks until the process has ended and returns its exit code, or -1
	// when there is none to give. It answers once per terminal: the code
	// belongs to the start, so a second call returns the same code.
	Wait() int
	// Kill ends the process without asking.
	Kill() error
	// Close hangs the terminal up, which most processes treat as a signal to
	// stop. Safe to call more than once. The runner still guards it with
	// closePTY, because a double close of a Windows handle is not a panic.
	Close() error
}

// localTerm is term over go-pty in the daemon's own process, doing exactly what
// the supervisor did before the seam existed.
type localTerm struct {
	pty pty.Pty
	cmd *pty.Cmd

	waitOnce sync.Once
	code     int
}

func newLocalTerm(p pty.Pty, c *pty.Cmd) *localTerm { return &localTerm{pty: p, cmd: c} }

func (t *localTerm) Read(p []byte) (int, error)  { return t.pty.Read(p) }
func (t *localTerm) Write(p []byte) (int, error) { return t.pty.Write(p) }
func (t *localTerm) Resize(cols, rows int) error { return t.pty.Resize(cols, rows) }
func (t *localTerm) Close() error                { return t.pty.Close() }

func (t *localTerm) Pid() int {
	if t.cmd == nil || t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

func (t *localTerm) Wait() int {
	t.waitOnce.Do(func() {
		if t.cmd == nil {
			t.code = -1
			return
		}
		err := t.cmd.Wait()
		if err == nil {
			return
		}
		t.code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.code = ee.ExitCode()
		}
	})
	return t.code
}

func (t *localTerm) Kill() error {
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	return t.cmd.Process.Kill()
}

// tm is the runner's terminal, or nil when it has none.
//
// `pty` and `cmd` on the runner are the in-process terminal's own parts and are
// only ever read here. A test that puts a fake pty on a runner gets it wrapped
// on first use, which is how every existing supervisor test keeps working
// unchanged.
func (r *runner) tm() term {
	r.termOnce.Do(func() {
		if r.t == nil && r.pty != nil {
			r.t = newLocalTerm(r.pty, r.cmd)
		}
	})
	return r.t
}

// adopt makes t the runner's terminal. For the in-process terminal it also
// keeps the parts `pty` and `cmd` point at, which is what a test reaches for.
func (r *runner) adopt(t term) {
	r.t = t
	if lt, ok := t.(*localTerm); ok {
		r.pty, r.cmd = lt.pty, lt.cmd
	}
	// Anything asking for the terminal from here on gets this one.
	r.termOnce.Do(func() {})
}

// startTerm opens a terminal and starts a process in it. THE ONE PLACE A
// TERMINAL IS SPAWNED, for runners and shells alike.
//
// `name` is what the operator typed, for the error, and `resolved` is what is
// run. env is final: the caller has already decided what the child is told.
//
// `kind` is store.RunKindRunner or RunKindShell and only the host path uses it.
// With `pty_host` on the terminal is asked of the host, see hostterm.go. A host
// that cannot be reached falls through to the in-process start below, because
// the setting must never be the reason a launch fails.
func (d *Daemon) startTerm(taskID, kind, name, resolved string, args []string, cwd string, env []string,
	cols, rows int) (term, error) {
	if d.st.PtyHostOn() {
		if t, ok, err := d.startHostTerm(taskID, kind, name, resolved, args, cwd, env, cols, rows); ok {
			return t, err
		}
	}

	raise := d.beginPTYRaise()
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("could not open a pseudo terminal: %w", err)
	}
	// Set BEFORE the process starts, so its first bytes are drawn at the size
	// the ring files them under.
	sizeAtLaunch(p, cols, rows)
	c := p.Command(resolved, args...)
	c.Dir = cwd
	c.Env = env
	if err := c.Start(); err != nil {
		p.Close()
		return nil, fmt.Errorf("could not start %s: %w", name, err)
	}
	// The operator types into this one too.
	raise.apply(c.Process.Pid)
	return newLocalTerm(p, c), nil
}

// recordRun writes the runner's start down. It is called after the process is
// up and BEFORE the runner is offered to anything, so a terminal a crashed
// daemon had just started is still claimed by its card when the next daemon
// finds it.
//
// Not fatal: the process is already running and a store that cannot write
// halts the daemon on its own account. An exit for a run with no row still
// files once, see store.FileExit.
func (d *Daemon) recordRun(r *runner, kind string) {
	// Empty: the terminal is in this process. A host puts its own address here.
	host := ""
	if ht := hostOf(r); ht != nil {
		host = ht.host
	}
	if err := d.st.RecordRun(store.PtyRun{
		RunID: r.runID, TaskID: r.taskID, Kind: kind,
		Host:    host,
		Started: r.started,
	}); err != nil {
		log.Printf("[atrium] record the start of run %s for %s: %v", r.runID, r.taskID, err)
	}
}
