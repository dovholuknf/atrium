// Package gitsync moves code between a hub and its rooms by FETCH, and by nothing else.
// See docs/rnd/git-sync-design.md. Nobody here ever receives a push: no receive-pack runs
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

// hardening is on every command. No credential helper and no proxy variable can interfere.
var hardening = []string{"-c", "credential.helper=", "-c", "http.proxy="}

// Git runs one git command in `dir` (or in no directory when empty) and answers its stdout.
// A failure is an *Error carrying git's stderr.
func (r *Runner) Git(ctx context.Context, dir string, args ...string) (string, error) {
	return r.git(ctx, dir, nil, args...)
}

func (r *Runner) git(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
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

	full := append(append([]string{}, hardening...), args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = CleanEnv(extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
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
