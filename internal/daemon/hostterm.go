package daemon

import (
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/ptyhost"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE PTY HOST, FROM THE DAEMON'S SIDE. See docs/rnd/rolling-restart-design.md and
// docs/terminal/ptyhost-protocol.md.
//
// With `pty_host` on, a terminal is asked of a separate process that keeps running when this one does not, and
// this file is everything the daemon does about that: finding or starting the host, a `term` that speaks to it,
// and reattaching to what it kept. The policy (the ring the board reads, the screen model, the typing gate)
// stays in the daemon exactly as it was, which is why the host path is a `term` and not a second supervisor.
//
// THE SETTING MUST NEVER FAIL A LAUNCH. Every way of not having a host ends in an in-process terminal and a log
// line saying why, because a session that could not start on a machine that turned an experiment on is a worse
// outcome than the experiment not running.

const (
	// hostRetry is how long a failure to reach the host is remembered, so a machine with no host does not pay
	// a dial and a start attempt on every launch.
	hostRetry = 30 * time.Second
	// hostDial is how long the first dial waits before a host is started, since a host that is up answers at once.
	hostDial = 250 * time.Millisecond
	// hostEOFWait caps how long Wait lets the reader catch up after the exit event, so the tail it files is complete
	// without a stuck reader holding the exit back.
	hostEOFWait = 2 * time.Second
	// hostMaxRing is the largest ring the host accepts.
	hostMaxRing = 512 << 20
)

// hostLink is this daemon's one connection to the host.
type hostLink struct {
	mu     sync.Mutex
	cl     *ptyhost.Client
	had    bool      // this daemon has held a link, so a later one may take over from its own ghost
	failAt time.Time // when the last attempt failed
	// opts is how Start launches a host. A test points it at a re-exec of its own binary or at nothing.
	opts ptyhost.StartOptions
}

func (l *hostLink) close() {
	l.mu.Lock()
	cl := l.cl
	l.cl = nil
	l.mu.Unlock()
	if cl != nil {
		_ = cl.Close()
	}
}

func (d *Daemon) hostStateDir() string { return filepath.Dir(d.opts.DBPath) }

// hostClient returns the connection, making it when there is none.
//
// takeover says a connected daemon may be replaced. It is only ever true at startup (the previous daemon is gone
// by definition) or after this daemon already held a link, so a runtime attempt that finds somebody else
// connected refuses and falls back rather than evicting a live daemon. start says a missing host may be started.
func (d *Daemon) hostClient(takeover, start bool) (*ptyhost.Client, string, error) {
	l := &d.ph
	l.mu.Lock()
	defer l.mu.Unlock()
	dir := d.hostStateDir()
	addr := ptyhost.Address(dir)
	if l.cl != nil {
		select {
		case <-l.cl.Done():
			l.cl = nil
		default:
			return l.cl, addr, nil
		}
	}
	if !l.failAt.IsZero() && time.Since(l.failAt) < hostRetry {
		return nil, addr, errors.New("the pty host was unreachable a moment ago")
	}
	cl, err := d.connectHost(dir, addr, takeover || l.had, start, l.opts)
	if err != nil {
		l.failAt = time.Now()
		return nil, addr, err
	}
	l.failAt = time.Time{}
	l.cl, l.had = cl, true
	return cl, addr, nil
}

func (d *Daemon) connectHost(dir, addr string, takeover, start bool, so ptyhost.StartOptions) (*ptyhost.Client, error) {
	cl, err := ptyhost.DialTimeout(addr, hostDial)
	if err != nil {
		if !start {
			return nil, err
		}
		if _, serr := ptyhost.Start(dir, so); serr != nil {
			return nil, fmt.Errorf("start the pty host: %w", serr)
		}
		if cl, err = ptyhost.Dial(addr); err != nil {
			return nil, err
		}
	}
	// A probe first, which evicts nobody.
	pr, err := cl.Probe()
	if err != nil {
		_ = cl.Close()
		return nil, err
	}
	if pr.Proto != ptyhost.Proto {
		_ = cl.Close()
		return nil, fmt.Errorf("the pty host speaks protocol %d and this daemon speaks %d", pr.Proto, ptyhost.Proto)
	}
	if pr.Daemon && !takeover {
		_ = cl.Close()
		return nil, errors.New("another daemon is connected to the pty host")
	}
	if _, err := cl.Hello("atrium-daemon", pr.Daemon); err != nil {
		_ = cl.Close()
		return nil, err
	}
	if pr.Daemon {
		log.Printf("[atrium] pty host at %s already had a daemon connected. this one is starting, so the "+
			"previous one is gone, and the host now answers to this one", addr)
	}
	return cl, nil
}

// startHostTerm asks the host for a terminal. ok is false when there is no host to ask, and the caller falls back
// to an in-process terminal. An error with ok true is the host refusing the start, which a local start would only
// repeat.
func (d *Daemon) startHostTerm(taskID, kind, name, resolved string, args []string, cwd string, env []string,
	cols, rows int) (t term, ok bool, err error) {
	cl, addr, err := d.hostClient(false, true)
	if err != nil {
		log.Printf("[atrium] pty_host is on but there is no host (%v), starting %s in this process", err, name)
		return nil, false, nil
	}
	ring := min(api.ScrollbackBytes(d.st), hostMaxRing)
	sp, err := cl.Spawn(ptyhost.SpawnArgs{
		ID: taskID, Kind: kind, Argv: append([]string{resolved}, args...), Env: env, Cwd: cwd,
		Cols: cols, Rows: rows, Ring: ring,
	})
	if err != nil {
		var re *ptyhost.RemoteError
		if errors.As(err, &re) {
			return nil, true, fmt.Errorf("could not start %s: %w", name, err)
		}
		log.Printf("[atrium] the pty host went away starting %s (%v), starting it in this process", name, err)
		return nil, false, nil
	}
	att, err := cl.Attach(sp.RunID, 0)
	if err != nil {
		// Started and unreachable. Ended here so the fallback is not a second copy of the same session.
		_ = cl.Signal(sp.RunID, ptyhost.SigKill)
		log.Printf("[atrium] could not attach to %s in the pty host (%v), starting it in this process", name, err)
		return nil, false, nil
	}
	return newHostTerm(cl, addr, sp.RunID, sp.Pid, att, true), true, nil
}

// hostTerm is `term` over one run in the host.
type hostTerm struct {
	cl    *ptyhost.Client
	host  string
	runID string
	pid   int

	mu   sync.Mutex
	cond *sync.Cond
	buf  []byte
	fin  bool

	exited chan struct{}
	eof    chan struct{}
	code   int
	lost   bool
	once   sync.Once
	eofOne sync.Once
}

// newHostTerm follows att. feedReplay puts the replay in front of the first Read, which is what a fresh spawn
// wants. A reattach has already put it in a ring of its own and passes false.
func newHostTerm(cl *ptyhost.Client, host, runID string, pid int, att *ptyhost.Attachment, feedReplay bool) *hostTerm {
	t := &hostTerm{cl: cl, host: host, runID: runID, pid: pid,
		exited: make(chan struct{}), eof: make(chan struct{})}
	t.cond = sync.NewCond(&t.mu)
	if feedReplay {
		t.buf = append(t.buf, att.Replay...)
	}
	go t.follow(att.Events)
	return t
}

func (t *hostTerm) follow(ev <-chan ptyhost.Event) {
	gotExit := false
	for e := range ev {
		switch e.Ev {
		case ptyhost.EvOut:
			if gotExit {
				continue
			}
			t.mu.Lock()
			t.buf = append(t.buf, e.Data...)
			t.mu.Unlock()
			t.cond.Broadcast()
		case ptyhost.EvExit:
			if gotExit {
				continue
			}
			gotExit = true
			t.code = e.ExitCode
			t.mu.Lock()
			t.fin = true
			t.mu.Unlock()
			t.cond.Broadcast()
			t.once.Do(func() { close(t.exited) })
		}
	}
	// Events closed with no exit: the link went, and nothing here knows whether the process did.
	if !gotExit {
		t.lost = true
		t.mu.Lock()
		t.fin = true
		t.mu.Unlock()
		t.cond.Broadcast()
		t.once.Do(func() { close(t.exited) })
	}
}

func (t *hostTerm) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.buf) == 0 && !t.fin {
		t.cond.Wait()
	}
	if len(t.buf) == 0 {
		t.eofOne.Do(func() { close(t.eof) })
		return 0, io.EOF
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}

func (t *hostTerm) Write(p []byte) (int, error) {
	if err := t.cl.Write(t.runID, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *hostTerm) Resize(cols, rows int) error {
	// The host records the cut. The daemon's own ring lays its own, from the same decision.
	_, err := t.cl.Resize(t.runID, cols, rows)
	return err
}

func (t *hostTerm) Pid() int { return t.pid }

// Wait is the host's exit event, then the reader catching up so the tail the caller reads is whole.
func (t *hostTerm) Wait() int {
	<-t.exited
	if t.lost {
		return -1
	}
	select {
	case <-t.eof:
	case <-time.After(hostEOFWait):
	}
	return t.code
}

func (t *hostTerm) Kill() error { return t.cl.Signal(t.runID, ptyhost.SigKill) }

// Close is a hangup. The pty may already have ended, which is not an error worth carrying.
func (t *hostTerm) Close() error {
	_ = t.cl.Signal(t.runID, ptyhost.SigTerm)
	return nil
}

// Lost says the link ended and no exit was seen: nothing is known to have exited.
func (t *hostTerm) Lost() bool { return t.lost }

// Collect acknowledges an exit that has been filed, and only then.
func (t *hostTerm) Collect() error { return t.cl.Collect(t.runID) }

// hostOf is the host term under a runner, or nil.
func hostOf(r *runner) *hostTerm {
	ht, _ := r.tm().(*hostTerm)
	return ht
}

// removeRunner and removeShellIf take an entry out only if it is still THIS runner. A late exit of an old start
// must never remove the newer one filed under the same card.
func (s *supervisor) removeRunner(r *runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if same(s.runners[r.taskID], r) {
		delete(s.runners, r.taskID)
	}
}

func (s *supervisor) removeShellIf(r *runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if same(s.shells[r.taskID], r) {
		delete(s.shells, r.taskID)
	}
}

func same(cur, r *runner) bool {
	return cur != nil && (cur == r || (r.runID != "" && cur.runID == r.runID))
}

// reattachRuns picks up every terminal the host kept while no daemon was here. Called once at startup, before
// fixtures, and sequential on purpose: nothing exiting is ever visible as live.
//
// The order is the design's (3.2). Every live run is registered first and the exited ones are filed after, so a
// card that has an old exited run and a newer live one is not marked dead for the old one. A run with no row, or
// whose card is gone, is logged and left alone: guessing whose it is would file an exit against the wrong card.
func (d *Daemon) reattachRuns() {
	cl, addr, err := d.hostClient(true, false)
	if err != nil {
		log.Printf("[atrium] pty_host is on and no host answered (%v), nothing to reattach", err)
		return
	}
	list, err := cl.List()
	if err != nil {
		log.Printf("[atrium] list the pty host: %v", err)
		return
	}
	type gone struct {
		row  *store.PtyRun
		info ptyhost.PtyInfo
		att  *ptyhost.Attachment
		buf  *ringBuffer
	}
	var exited []gone
	for _, info := range list {
		row, err := d.st.Run(info.RunID)
		if err != nil {
			log.Printf("[atrium] the pty host holds run %s (%s) and there is no record of it, leaving it alone",
				info.RunID, info.ID)
			continue
		}
		task, err := d.st.Get(row.TaskID)
		if err != nil {
			log.Printf("[atrium] the pty host holds run %s for %s and that card is gone, leaving it alone",
				info.RunID, row.TaskID)
			continue
		}
		if row.Filed && !info.Exited {
			log.Printf("[atrium] run %s for %s is live but its exit is already filed, leaving it alone",
				info.RunID, row.TaskID)
			continue
		}
		if cur := d.currentOf(row); cur != nil && cur.runID == info.RunID {
			continue
		}
		att, err := cl.Attach(info.RunID, 0)
		if err != nil {
			log.Printf("[atrium] attach to run %s for %s: %v", info.RunID, row.TaskID, err)
			continue
		}
		buf := ringFromAttach(att, api.ScrollbackBytes(d.st))
		if att.Info.Exited {
			exited = append(exited, gone{row, att.Info, att, buf})
			continue
		}
		d.adoptLive(cl, addr, row, task, att, buf)
	}
	for _, g := range exited {
		d.fileGone(cl, g.row, g.info, g.buf)
	}
}

func (d *Daemon) currentOf(row *store.PtyRun) *runner {
	if row.Kind == store.RunKindShell {
		return d.sup.getShell(row.TaskID)
	}
	return d.sup.get(row.TaskID)
}

// ringFromAttach rebuilds a ring from an attach reply: the replay laid in segments, each at the size the host
// says it was written at, so the screen and scrollback come back as they were.
func ringFromAttach(att *ptyhost.Attachment, size int) *ringBuffer {
	cols, rows := att.Info.Cols, att.Info.Rows
	if len(att.Cuts) > 0 {
		cols, rows = att.Cuts[0].Cols, att.Cuts[0].Rows
	}
	rb := newRingSized(size, cols, rows)
	seg := func(from, to int64) {
		if from < 0 {
			from = 0
		}
		if n := int64(len(att.Replay)); to > n {
			to = n
		}
		if to > from {
			_, _ = rb.Write(att.Replay[from:to])
		}
	}
	if len(att.Cuts) == 0 {
		seg(0, int64(len(att.Replay)))
	}
	for i, c := range att.Cuts {
		rb.SetSize(c.Cols, c.Rows)
		end := int64(len(att.Replay))
		if i+1 < len(att.Cuts) {
			end = att.Cuts[i+1].Off - att.From
		}
		start := c.Off - att.From
		if i == 0 {
			start = 0
		}
		seg(start, end)
	}
	rb.SetSize(att.Info.Cols, att.Info.Rows)
	return rb
}

func startedOf(info ptyhost.PtyInfo) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, info.Started); err == nil {
		return t
	}
	return time.Now()
}

// adoptLive makes a runner of a terminal that is still running, following it from the end of its replay.
func (d *Daemon) adoptLive(cl *ptyhost.Client, addr string, row *store.PtyRun, task *store.Task,
	att *ptyhost.Attachment, buf *ringBuffer) {
	shell := row.Kind == store.RunKindShell
	r := &runner{
		taskID: row.TaskID, runID: row.RunID, started: startedOf(att.Info), pid: att.Info.Pid,
		dir: task.Worktree, buf: buf,
		watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	if !shell {
		r.onSized = d.noteRoomSize
		r.line.escClears = clearsOnEsc(task.Runner)
	}
	t := newHostTerm(cl, addr, row.RunID, att.Info.Pid, att, false)
	r.adopt(t)
	if shell {
		r.touch()
		d.sup.addShell(r)
	} else {
		d.sup.add(r)
		d.EnsureCardShare(row.TaskID)
	}
	go func() {
		chunk := make([]byte, 8192)
		for {
			n, err := t.Read(chunk)
			if n > 0 {
				r.deliverOutput(chunk[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	if shell {
		go d.awaitShellExit(r)
		go d.sweepIdleShell(r)
	} else {
		go d.awaitExit(r)
	}
	log.Printf("[atrium] reattached %s for %s (run %s, pid %d)", row.Kind, row.TaskID, row.RunID, att.Info.Pid)
}

// fileGone files the exit of a terminal that ended while no daemon was connected, then collects it, and only if
// the filing committed: a crash between the two leaves the exit listed for the next daemon, which files nothing.
func (d *Daemon) fileGone(cl *ptyhost.Client, row *store.PtyRun, info ptyhost.PtyInfo, buf *ringBuffer) {
	var err error
	if row.Kind == store.RunKindShell {
		_, err = d.st.FileExit(store.ExitFiling{TaskID: row.TaskID, RunID: row.RunID, Kind: store.RunKindShell})
	} else {
		started := startedOf(info)
		lived := time.Since(started)
		if t, perr := time.Parse(time.RFC3339Nano, info.ExitedAt); perr == nil {
			lived = t.Sub(started)
		}
		cur := d.sup.get(row.TaskID)
		_, err = d.fileExitChecked(runExit{
			taskID: row.TaskID, runID: row.RunID, code: info.ExitCode,
			tail: lastOutput(buf.Tail(tailBytes), 12), lived: lived,
			// A newer live run holds this card, so this old end is history and nothing more.
			superseded: cur != nil && cur.runID != row.RunID,
		})
	}
	if err != nil {
		log.Printf("[atrium] file the exit of run %s for %s: %v. it stays in the host for the next daemon",
			row.RunID, row.TaskID, err)
		return
	}
	if err := cl.Collect(row.RunID); err != nil {
		log.Printf("[atrium] collect run %s: %v", row.RunID, err)
	}
}
