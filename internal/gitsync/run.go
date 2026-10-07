// Package gitsync moves code between a hub and its rooms by FETCH, and by nothing else.
// See docs/fabric/git-sync-design.md. Nobody here ever receives a push: no receive-pack runs
// on either side, so no ref can be moved by what the other end sends.
//
// This package is git plumbing. It knows nothing about the link, which carries its bytes.
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CommandBound is how long any one git command may run. A clone of atrium from scratch is
// minutes on a bad overlay, so this is generous, and it is also the age past which a lock
// file left behind is not a command still running. See CleanLocks.
const CommandBound = 10 * time.Minute

// Runner runs git. Every command is bounded, killed at the bound, and cancelled together
// on Stop, so a hub or room that is shutting down does not leave a git child holding a
// ref lock behind it.
type Runner struct {
	// Bound overrides CommandBound. Zero takes it. Tests only.
	Bound time.Duration

	mu       sync.Mutex
	stopped  bool
	stop     context.CancelFunc
	root     context.Context
	children sync.WaitGroup
}

// NewRunner makes a runner that can be stopped.
func NewRunner() *Runner {
	r := &Runner{}
	r.root, r.stop = context.WithCancel(context.Background())
	return r
}

// Default is the runner the daemon uses.
var Default = NewRunner()

// Stop cancels every git child and waits for them, for at most `wait`. It reports whether
// they all exited in time. A runner that has been stopped refuses further commands.
func (r *Runner) Stop(wait time.Duration) bool {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	r.stop()
	done := make(chan struct{})
	go func() { r.children.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}

// ErrOutputCap is what a capped command answers when its output went past the cap.
var ErrOutputCap = errors.New("git output went past the bound and git was stopped")

// capWriter keeps what it is given up to max (0 means no cap) and, past it, stops the command.
type capWriter struct {
	buf  *bytes.Buffer
	max  int
	stop context.CancelFunc
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.max <= 0 {
		return w.buf.Write(p)
	}
	room := w.max - w.buf.Len()
	if room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	if len(p) > room && !w.over {
		w.over = true
		w.stop()
	}
	return len(p), nil
}

// ErrStopped is what a command answers once the runner has been stopped.
var ErrStopped = errors.New("git sync is shutting down")

// Error is a git command that failed, in git's own words.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if s := e.First(); s != "" {
		return s
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// First is the first line git said, which is what `detail` carries.
func (e *Error) First() string {
	for _, l := range strings.Split(e.Stderr, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// CleanEnv is the environment for a git child: os.Environ() with EVERY GIT_* variable
// removed, then what is asked for added.
//
// LOAD BEARING. On sg3 a command run with GIT_DIR, GIT_WORK_TREE or GIT_INDEX_FILE set
// acted on the real repository and deleted its work tree. Anything atrium runs may itself
// be running inside a hook, so its environment cannot be trusted to be clean.
func CleanEnv(extra ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if len(kv) >= 4 && strings.EqualFold(kv[:4], "GIT_") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	return append(out, extra...)
}

// hardening is on every command. No credential helper and no proxy variable can interfere, and the maintenance a
// fetch or a commit starts on its own runs inside that command rather than detached, so no git outlives the one the
// runner waits for.
var hardening = []string{"-c", "credential.helper=", "-c", "http.proxy=",
	"-c", "maintenance.autoDetach=false", "-c", "gc.autoDetach=false"}

// Git runs one git command in `dir` (or in no directory when empty) and answers its stdout.
// A failure is an *Error carrying git's stderr.
func (r *Runner) Git(ctx context.Context, dir string, args ...string) (string, error) {
	return r.git(ctx, dir, nil, args...)
}

// GitEnv is Git with extra environment, applied after every GIT_* variable is stripped. For a
// setting that has to hold for one command, such as GIT_LITERAL_PATHSPECS=1.
func (r *Runner) GitEnv(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	return r.git(ctx, dir, extraEnv, args...)
}

// GitCapped is GitEnv that stops reading at `max` bytes of output and kills git, so a very large
// diff is never held whole. What was read so far comes back with an *Error wrapping ErrOutputCap.
func (r *Runner) GitCapped(ctx context.Context, dir string, extraEnv []string, max int, args ...string) (string, error) {
	return r.gitWith(ctx, dir, extraEnv, nil, max, args...)
}

// GitInput is Git with `stdin` fed to the command, for a batch read such as
// `cat-file --batch` that takes its list on standard input.
func (r *Runner) GitInput(ctx context.Context, dir string, stdin []byte, args ...string) (string, error) {
	return r.gitWith(ctx, dir, nil, stdin, 0, args...)
}

func (r *Runner) git(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	return r.gitWith(ctx, dir, extraEnv, nil, 0, args...)
}

func (r *Runner) gitWith(ctx context.Context, dir string, extraEnv []string, stdin []byte, maxOut int, args ...string) (string, error) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return "", ErrStopped
	}
	r.children.Add(1)
	r.mu.Unlock()
	defer r.children.Done()

	bound := r.Bound
	if bound <= 0 {
		bound = CommandBound
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	// Cancelled with the runner as well as with the caller.
	stop := context.AfterFunc(r.root, cancel)
	defer stop()

	cmd := gitCommand(ctx, dir, extraEnv, args)
	var stdout, stderr bytes.Buffer
	capped := &capWriter{buf: &stdout, max: maxOut, stop: cancel}
	cmd.Stdout, cmd.Stderr = capped, &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.WaitDelay = 5 * time.Second
	// A cancel ends the child's whole tree, not the child alone. See tree_windows.go.
	var tree atomic.Pointer[procTree]
	cmd.Cancel = func() error {
		if t := tree.Load(); t != nil {
			return t.kill()
		}
		return cmd.Process.Kill()
	}
	err := cmd.Start()
	if err == nil {
		t := startTree(cmd)
		tree.Store(t)
		err = cmd.Wait()
		if t != nil {
			t.close()
		}
	}
	if capped.over {
		return stdout.String(), &Error{Args: args, Stderr: stderr.String(), Err: ErrOutputCap}
	}
	if err != nil {
		if ctx.Err() != nil {
			switch {
			case r.root.Err() != nil:
				err = ErrStopped
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				err = errors.New("git did not finish in " + bound.String() + " and was killed")
			}
		}
		return stdout.String(), &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// gitCommand is the git child gitWith runs, before its streams and its cancel are set: the
// hardening flags, the clean environment, and prepareTree, which on Windows also keeps it
// from opening a console window.
func gitCommand(ctx context.Context, dir string, extraEnv, args []string) *exec.Cmd {
	full := append(append([]string{}, hardening...), args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = CleanEnv(extraEnv...)
	prepareTree(cmd)
	return cmd
}
