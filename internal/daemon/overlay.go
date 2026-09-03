package daemon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Reaching the board from somewhere that is not this machine.
//
// Atrium listens on loopback and has no authentication, on purpose: an auth
// layer invented here would be a worse one than the overlays that already
// exist. That decision only holds while there IS a way to reach it from
// elsewhere, and until now the answer was "set one up yourself".
//
// So atrium drives one instead of becoming one. zrok and OpenZiti both already
// know how to publish a loopback address to somebody who is allowed to see it.
// Atrium keeps the configuration, starts the process, and reports what it
// says. It never handles an identity, never proxies traffic, and has no opinion
// about who may connect: that is the overlay's job and it stays there.

// overlayKind names a way out. Both are configured and run the same way, and
// only the command line differs.
type overlayKind string

const (
	OverlayZrok overlayKind = "zrok"
	OverlayZiti overlayKind = "ziti"
)

// overlayStartGrace bounds how long a share is given to say something useful
// before the board stops waiting and shows whatever it has.
const overlayStartGrace = 12 * time.Second

// keepLines is how much of a share's output is kept. Enough to show why it
// failed, bounded so a chatty process cannot grow without limit.
const keepLines = 200

// OverlayState is what the board shows for one overlay.
type OverlayState struct {
	Kind overlayKind `json:"kind"`
	// Found is the resolved path of the command, empty when it is not
	// installed. Everything else is moot when this is empty.
	Found string `json:"found"`
	// Running is whether atrium is holding a share open right now.
	Running bool `json:"running"`
	// Since is when it started, RFC3339, empty when it is not running.
	Since string `json:"since"`
	// Address is what the overlay published, when it said one. zrok prints a
	// URL; ziti does not, so this stays empty there and the service name is
	// what identifies it.
	Address string `json:"address,omitempty"`
	// Output is the tail of what the process wrote, which is the only place a
	// failure explains itself.
	Output []string `json:"output"`
	// Err is why the last start failed, when it did.
	Err string `json:"err,omitempty"`
}

// overlay is one running share.
type overlay struct {
	mu   sync.Mutex
	kind overlayKind
	cmd  *exec.Cmd
	// starting is held from the moment a start is accepted until the process
	// is recorded, which is a window the mutex alone does not cover: starting
	// a process is slow and cannot be done under a lock. Without it two
	// clicks, or two tabs, each pass the "is one running" check and launch a
	// share, and the second overwrites the first. The first then keeps
	// publishing the board with nothing tracking it.
	starting bool
	cancel   context.CancelFunc
	since    time.Time
	lines    []string
	address  string
	err      string
}

// overlays holds both, whether running or not.
type overlays struct {
	mu sync.Mutex
	by map[overlayKind]*overlay
}

func newOverlays() *overlays {
	return &overlays{by: map[overlayKind]*overlay{}}
}

func (o *overlays) get(kind overlayKind) *overlay {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.by[kind] == nil {
		o.by[kind] = &overlay{kind: kind}
	}
	return o.by[kind]
}

// state reports what to show, without starting or stopping anything.
func (v *overlay) state(found string) OverlayState {
	v.mu.Lock()
	defer v.mu.Unlock()
	st := OverlayState{
		Kind: v.kind, Found: found,
		Running: v.cmd != nil || v.starting, Address: v.address, Err: v.err,
		Output:  append([]string(nil), v.lines...),
	}
	if v.cmd != nil {
		st.Since = v.since.Format(time.RFC3339)
	}
	return st
}

// start runs the share and watches its output.
//
// The process is held rather than waited on: a share runs until it is stopped,
// so there is no exit to report and the useful signal is what it prints on the
// way up.
func (v *overlay) start(name string, args []string, env []string) error {
	v.mu.Lock()
	if v.cmd != nil || v.starting {
		v.mu.Unlock()
		return fmt.Errorf("a %s share is already running", v.kind)
	}
	v.starting = true
	v.mu.Unlock()

	// Cleared on every path out of here, or a failed start would leave the
	// overlay permanently refusing to start again.
	defer func() {
		v.mu.Lock()
		v.starting = false
		v.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	// Both streams, because a share that refuses says so on stderr and a
	// share that works prints its address on stdout.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}

	v.mu.Lock()
	v.cmd, v.cancel, v.since = cmd, cancel, time.Now()
	v.lines, v.address, v.err = nil, "", ""
	v.mu.Unlock()

	go v.read(stdout)
	go v.read(stderr)
	// The process ending on its own is a failure, since a share is meant to
	// stay up. Noticing means the board stops claiming it is running.
	go func() {
		err := cmd.Wait()
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.cmd != cmd {
			// Already replaced by a later start.
			return
		}
		v.cmd, v.cancel = nil, nil
		if err != nil && v.err == "" {
			v.err = err.Error()
		}
		log.Printf("[atrium] the %s share ended: %v", v.kind, err)
	}()
	return nil
}

// read keeps the tail of one stream and picks an address out of it.
func (v *overlay) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	// A share can print a long line, and the default limit is small enough
	// that a URL inside JSON would be cut in half.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		v.mu.Lock()
		v.lines = append(v.lines, line)
		if len(v.lines) > keepLines {
			v.lines = v.lines[len(v.lines)-keepLines:]
		}
		if v.address == "" {
			if u := findURL(line); u != "" {
				v.address = u
			}
		}
		v.mu.Unlock()
	}
	// A read that gave up is worth saying. Silently capturing nothing from
	// here on would hide the one place a share explains why it refused.
	if err := sc.Err(); err != nil {
		v.mu.Lock()
		if v.err == "" {
			v.err = "stopped reading its output: " + err.Error()
		}
		v.mu.Unlock()
	}
}

// stop ends the share. Stopping one that is not running is not an error: the
// board can ask for a state it is already in.
func (v *overlay) stop() {
	v.mu.Lock()
	cancel := v.cancel
	v.cancel = nil
	v.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// findURL pulls the first https address out of a line.
//
// Matched rather than parsed, because what a share prints is not a documented
// format: zrok has printed a bare URL, a boxed one and a JSON field at
// different times, and none of those is a contract atrium should depend on.
// Finding a URL if there is one and showing the raw output either way survives
// all three.
func findURL(line string) string {
	i := strings.Index(line, "https://")
	if i < 0 {
		return ""
	}
	rest := line[i:]
	end := strings.IndexAny(rest, " \t\"'<>)]},")
	if end > 0 {
		rest = rest[:end]
	}
	return strings.TrimRight(rest, ".,")
}
