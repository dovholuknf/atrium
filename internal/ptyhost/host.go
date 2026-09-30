package ptyhost

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

// Defaults.
const (
	// DefaultIdleExit is how long a host with no pty and no daemon waits before it exits (design 3.3).
	DefaultIdleExit = 10 * time.Minute
	// DefaultClientQueue is the bound on what one slow client may have queued, in bytes (design G4).
	DefaultClientQueue = 4 << 20

	defaultRing = 1 << 20
	maxRing     = 512 << 20
	defaultCols = 100
	defaultRows = 30
)

// Options for a Host. The zero value is usable.
type Options struct {
	// Build is what the host reports as its build, shown on the board.
	Build string
	// IdleExit is DefaultIdleExit when zero. A test passes something short.
	IdleExit time.Duration
	// ClientQueue is DefaultClientQueue when zero.
	ClientQueue int
	// Logf is where the host narrates. Nil logs to the standard logger.
	Logf func(format string, args ...any)
}

// Host owns the ptys. It serves one daemon at a time, and any number of read-only probes.
//
// Everything a daemon cares about is here: spawning under a pty, draining the output ALWAYS (a pty nobody reads
// fills its pipe and the runner blocks on a write, and that is the one thing that must never happen during a daemon
// gap), the ring and its cuts, input, resize, signal, and the exit status kept until it is collected. Nothing that
// is policy is here. The host knows a card only as the opaque `id` it was handed.
type Host struct {
	opts  Options
	logf  func(string, ...any)
	inJob bool

	spawnMu sync.Mutex // serialises spawns, so a priority raise can tell whose console host is whose

	// verbMu is read-held while a verb runs and write-held for an instant by a takeover, so the old daemon's last
	// in-flight verb finishes before the new daemon's first one is answered.
	verbMu sync.RWMutex

	mu         sync.Mutex
	ptys       map[string]*hpty
	clients    map[*client]struct{}
	daemon     *client
	lastDaemon time.Time
	ln         net.Listener
	lnCloser   *closeOnce

	closing atomic.Bool
	done    chan struct{}
}

// NewHost makes a host that has not started listening.
func NewHost(o Options) *Host {
	if o.IdleExit <= 0 {
		o.IdleExit = DefaultIdleExit
	}
	if o.ClientQueue <= 0 {
		o.ClientQueue = DefaultClientQueue
	}
	h := &Host{opts: o, logf: o.Logf, ptys: map[string]*hpty{}, clients: map[*client]struct{}{},
		lastDaemon: time.Now(), done: make(chan struct{})}
	if h.logf == nil {
		h.logf = func(f string, a ...any) { log.Printf("[atrium ptyhost] "+f, a...) }
	}
	var detail string
	h.inJob, detail = inJob()
	h.logf("job object: %s", detail)
	return h
}

// hpty is one pty, the runner under it, and its output.
type hpty struct {
	runID, id, kind string
	pid             int
	started         time.Time
	p               pty.Pty
	cmd             *pty.Cmd
	closer          *closeOnce

	lastWrite atomic.Int64 // unix nanos of the last drained chunk
	drained   chan struct{}

	mu       sync.Mutex // guards everything below, and is what makes snapshot-and-subscribe atomic
	rg       *ring
	subs     map[*client]struct{}
	exited   bool
	exitCode int
}

func (p *hpty) infoLocked() PtyInfo {
	cols, rows := p.rg.size()
	return PtyInfo{RunID: p.runID, ID: p.id, Kind: p.kind, Pid: p.pid, Cols: cols, Rows: rows,
		Started: p.started.UTC().Format(time.RFC3339Nano), Exited: p.exited, ExitCode: p.exitCode,
		RingStart: p.rg.start(), OutOffset: p.rg.total}
}

// Serve accepts connections on ln until the host closes or goes idle. It returns nil for both.
func (h *Host) Serve(ln net.Listener) error {
	h.mu.Lock()
	h.ln = ln
	h.lnCloser = newCloser(ln.Close)
	h.mu.Unlock()
	go h.idleLoop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if h.closing.Load() {
				return nil
			}
			return err
		}
		h.newClient(conn)
	}
}

// Done is closed when the host has stopped: closed, or idle.
func (h *Host) Done() <-chan struct{} { return h.done }

// idleLoop ends the host when it holds no pty and no daemon has been connected for the idle time. It never ends a
// host with a pty in it, exited or not: an exit nobody has collected is still somebody's answer.
func (h *Host) idleLoop() {
	tick := min(max(h.opts.IdleExit/10, 5*time.Millisecond), 5*time.Second)
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
		}
		h.mu.Lock()
		idle := h.daemon == nil && len(h.ptys) == 0 && time.Since(h.lastDaemon) >= h.opts.IdleExit
		h.mu.Unlock()
		if idle {
			h.logf("idle for %s with no pty and no daemon, exiting", h.opts.IdleExit)
			h.Close()
			return
		}
	}
}

// Close stops listening, drops every client and closes every pty, which ends the runners under them. It is the
// end of the host and safe to call from anywhere, any number of times, concurrently.
func (h *Host) Close() {
	if !h.closing.CompareAndSwap(false, true) {
		<-h.done
		return
	}
	h.mu.Lock()
	lc := h.lnCloser
	var cs []*client
	for c := range h.clients {
		cs = append(cs, c)
	}
	var ps []*hpty
	for _, p := range h.ptys {
		ps = append(ps, p)
	}
	h.mu.Unlock()
	if lc != nil {
		_ = lc.Close()
	}
	for _, c := range cs {
		c.close("host closing")
	}
	for _, p := range ps {
		_ = p.closer.Close()
	}
	close(h.done)
}

// ---- clients ----

type outItem struct {
	v any // *Reply or *Event
	n int // bytes counted against the bound, zero for a reply
}

// client is one connection. Everything sent to it goes through a bounded queue and one writer goroutine, so the
// pty drain never waits on a socket.
type client struct {
	h      *Host
	conn   net.Conn
	closer *closeOnce
	done   chan struct{}

	mu     sync.Mutex
	q      []outItem
	qBytes int
	dead   bool
	why    string
	wake   chan struct{}

	attached map[string]*hpty // guarded by h.mu
}

func (h *Host) newClient(conn net.Conn) *client {
	c := &client{h: h, conn: conn, done: make(chan struct{}), wake: make(chan struct{}, 1),
		attached: map[string]*hpty{}}
	c.closer = newCloser(c.teardown)
	h.mu.Lock()
	if h.closing.Load() {
		h.mu.Unlock()
		_ = c.closer.Close()
		return c
	}
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	go c.writeLoop()
	go c.readLoop()
	return c
}

// push queues a frame. counted frames (live output) are bounded and a false answer means the queue is full: the
// caller closes this client. A reply is never counted, since it answers a request this client made, and an attach
// replay is legitimately bigger than the bound.
func (c *client) push(v any, n int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead {
		return true // nothing to send to, and not a reason to close it again
	}
	if n > 0 && c.qBytes+n > c.h.opts.ClientQueue {
		return false
	}
	c.q = append(c.q, outItem{v: v, n: n})
	c.qBytes += n
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return true
}

func (c *client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			batch := c.q
			c.q = nil
			c.mu.Unlock()
			if len(batch) == 0 {
				break
			}
			for _, it := range batch {
				b, err := json.Marshal(it.v)
				if err == nil {
					_, err = c.conn.Write(append(b, '\n'))
				}
				c.mu.Lock()
				c.qBytes -= it.n
				c.mu.Unlock()
				if err != nil {
					c.close("write: " + err.Error())
					return
				}
			}
		}
	}
}

// close is the one way a client ends. Idempotent, and callable from the drain, the reader, the writer and a
// takeover at once.
func (c *client) close(why string) {
	c.mu.Lock()
	if c.why == "" {
		c.why = why
	}
	c.mu.Unlock()
	_ = c.closer.Close()
}

// teardown runs once, behind the closer.
func (c *client) teardown() error {
	c.mu.Lock()
	c.dead = true
	c.q, c.qBytes = nil, 0
	why := c.why
	c.mu.Unlock()
	close(c.done)
	err := c.conn.Close()

	h := c.h
	h.mu.Lock()
	delete(h.clients, c)
	if h.daemon == c {
		h.daemon = nil
		h.lastDaemon = time.Now()
	}
	var ps []*hpty
	for _, p := range c.attached {
		ps = append(ps, p)
	}
	c.attached = map[string]*hpty{}
	h.mu.Unlock()
	for _, p := range ps {
		p.mu.Lock()
		delete(p.subs, c)
		p.mu.Unlock()
	}
	h.logf("client closed: %s", why)
	return err
}

func (c *client) readLoop() {
	dec := json.NewDecoder(c.conn)
	for {
		var q Request
		if err := dec.Decode(&q); err != nil {
			why := "connection ended"
			if !errors.Is(err, io.EOF) {
				why = "read: " + err.Error()
			}
			c.close(why)
			return
		}
		c.handle(&q)
	}
}

func (c *client) reply(q *Request, r Reply) {
	r.Seq = q.Seq
	c.push(&r, 0)
}

func (c *client) fail(q *Request, format string, a ...any) {
	c.reply(q, Reply{Err: fmt.Sprintf(format, a...)})
}

func (c *client) isDaemon() bool {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	return c.h.daemon == c
}

// handle answers one frame.
func (c *client) handle(q *Request) {
	h := c.h
	switch q.V {
	case VProbe:
		// Read-only. It never evicts and changes nothing.
		h.mu.Lock()
		r := Reply{OK: true, Proto: Proto, Build: h.opts.Build, Pid: os.Getpid(), Daemon: h.daemon != nil,
			InJob: h.inJob, Ptys: len(h.ptys)}
		h.mu.Unlock()
		c.reply(q, r)
		return
	case VHello:
		c.hello(q)
		return
	}
	h.verbMu.RLock()
	defer h.verbMu.RUnlock()
	if !c.isDaemon() {
		c.fail(q, "say hello first")
		return
	}
	switch q.V {
	case VSpawn:
		c.spawn(q)
	case VList:
		h.mu.Lock()
		var ps []*hpty
		for _, p := range h.ptys {
			ps = append(ps, p)
		}
		h.mu.Unlock()
		list := make([]PtyInfo, 0, len(ps))
		for _, p := range ps {
			p.mu.Lock()
			list = append(list, p.infoLocked())
			p.mu.Unlock()
		}
		sort.Slice(list, func(i, j int) bool { return list[i].RunID < list[j].RunID })
		c.reply(q, Reply{OK: true, List: list})
	case VAttach, VWrite, VResize, VSignal, VCollect:
		h.mu.Lock()
		p := h.ptys[q.RunID]
		h.mu.Unlock()
		if p == nil {
			// The address rule: a verb naming a run_id the host does not hold is refused. An empty one is too.
			c.fail(q, "unknown run_id")
			return
		}
		switch q.V {
		case VAttach:
			c.attach(q, p)
		case VWrite:
			c.write(q, p)
		case VResize:
			c.resize(q, p)
		case VSignal:
			c.signal(q, p)
		case VCollect:
			c.collect(q, p)
		}
	default:
		c.fail(q, "unknown verb %q", q.V)
	}
}

func (c *client) hello(q *Request) {
	h := c.h
	h.mu.Lock()
	cur := h.daemon
	if cur != nil && cur != c && !q.Takeover {
		h.mu.Unlock()
		c.fail(q, "a daemon is already connected")
		return
	}
	h.daemon = c
	h.mu.Unlock()
	took := cur != nil && cur != c
	if took {
		h.logf("takeover: a daemon (build %q, proto %d) replaced the connected one", q.Build, q.Proto)
		cur.close("taken over")
		// A barrier: wait for the old daemon's in-flight verb, so it finishes before this daemon's first is answered.
		h.verbMu.Lock()
		h.verbMu.Unlock() //nolint:staticcheck // the point is the wait
	} else if cur == nil {
		h.logf("daemon connected (build %q, proto %d)", q.Build, q.Proto)
	}
	c.reply(q, Reply{OK: true, Proto: Proto, Build: h.opts.Build, Pid: os.Getpid(), InJob: h.inJob, Took: took})
}

// ---- verbs ----

func (c *client) spawn(q *Request) {
	h := c.h
	if q.Kind != KindRunner && q.Kind != KindShell {
		c.fail(q, "kind must be %q or %q", KindRunner, KindShell)
		return
	}
	if len(q.Argv) == 0 {
		c.fail(q, "spawn needs an argv")
		return
	}
	cols, rows := q.Cols, q.Rows
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	ringN := q.Ring
	if ringN <= 0 {
		ringN = defaultRing
	}
	ringN = min(ringN, maxRing)
	// Resolved on the host's PATH BEFORE the working directory is set: go-pty resolves relative to Dir, so a bare
	// `claude` would be looked for inside the repository being worked in.
	resolved, err := exec.LookPath(q.Argv[0])
	if err != nil {
		c.fail(q, "%s is not on PATH: %v", q.Argv[0], err)
		return
	}
	h.spawnMu.Lock()
	defer h.spawnMu.Unlock()
	rz := beginRaise()
	pt, err := pty.New()
	if err != nil {
		c.fail(q, "could not open a pseudo terminal: %v", err)
		return
	}
	if err := pt.Resize(cols, rows); err != nil {
		h.logf("could not set the launch size: %v", err)
	}
	cmd := pt.Command(resolved, q.Argv[1:]...)
	cmd.Dir = q.Cwd
	if q.Env != nil {
		cmd.Env = q.Env
	} else {
		cmd.Env = os.Environ()
	}
	if err := cmd.Start(); err != nil {
		_ = pt.Close()
		c.fail(q, "could not start %s: %v", q.Argv[0], err)
		return
	}
	if err := rz.apply(cmd.Process.Pid); err != nil {
		h.logf("could not raise a runner's priority: %v", err)
	}
	p := &hpty{runID: newRunID(), id: q.ID, kind: q.Kind, pid: cmd.Process.Pid, started: time.Now(),
		p: pt, cmd: cmd, drained: make(chan struct{}), rg: newRing(ringN, cols, rows), subs: map[*client]struct{}{}}
	p.closer = newCloser(pt.Close)
	p.lastWrite.Store(time.Now().UnixNano())
	h.mu.Lock()
	h.ptys[p.runID] = p
	h.mu.Unlock()
	h.logf("spawned %s id=%s kind=%s pid=%d %dx%d ring=%d", p.runID, p.id, p.kind, p.pid, cols, rows, ringN)
	go h.drain(p)
	go h.await(p)
	c.reply(q, Reply{OK: true, Pid: p.pid, RunID: p.runID})
}

// drain reads the pty for as long as it lives, whether or not a client is connected. It never waits on a client.
func (h *Host) drain(p *hpty) {
	defer close(p.drained)
	buf := make([]byte, 32<<10)
	for {
		n, err := p.p.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			var slow []*client
			p.mu.Lock()
			off := p.rg.total
			p.rg.write(chunk)
			p.lastWrite.Store(time.Now().UnixNano())
			ev := &Event{Ev: EvOut, RunID: p.runID, Off: off, Data: chunk}
			for c := range p.subs {
				if !c.push(ev, n+128) {
					slow = append(slow, c)
				}
			}
			p.mu.Unlock()
			for _, c := range slow {
				h.logf("client too slow on %s, more than %d bytes queued: closing it, it reattaches from its offset",
					p.runID, h.opts.ClientQueue)
				c.close("slow reader")
			}
		}
		if err != nil {
			return
		}
	}
}

// await notes the runner's exit. The exit is published only after the ring has drained: to every attached client
// it arrives on the same queue as the output, behind the last bytes.
//
// "Drained" is the read ending, or, where the read does not end when the runner does (a ConPTY stays open until it
// is closed), no bytes for a short quiet spell, bounded. A byte that still arrives after that is kept in the ring
// like any other and reaches a later attach.
func (h *Host) await(p *hpty) {
	err := p.cmd.Wait()
	code := 0
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	} else if err != nil {
		code = -1
	}
	begin := time.Now()
wait:
	for {
		select {
		case <-p.drained:
			break wait
		case <-time.After(20 * time.Millisecond):
		}
		quiet := time.Since(time.Unix(0, p.lastWrite.Load()))
		if quiet >= 200*time.Millisecond || time.Since(begin) >= 2*time.Second {
			break
		}
	}
	p.mu.Lock()
	p.exited, p.exitCode = true, code
	ev := &Event{Ev: EvExit, RunID: p.runID, Exited: true, ExitCode: code}
	for c := range p.subs {
		c.push(ev, 0)
	}
	p.mu.Unlock()
	h.logf("%s exited code=%d", p.runID, code)
}

func (c *client) attach(q *Request, p *hpty) {
	h := c.h
	h.mu.Lock()
	if _, ok := c.attached[p.runID]; ok {
		h.mu.Unlock()
		c.fail(q, "already attached to %s on this connection", p.runID)
		return
	}
	c.attached[p.runID] = p
	h.mu.Unlock()
	// Snapshot, subscribe and queue the reply under the pty's lock, so the reply is ahead of every event in the
	// queue and no byte is missed or seen twice between the replay and the live stream.
	p.mu.Lock()
	data, from, trunc, cuts := p.rg.snapshot(q.From)
	info := p.infoLocked()
	p.subs[c] = struct{}{}
	c.reply(q, Reply{OK: true, Info: &info, From: from, Truncated: trunc, Cuts: cuts, Data: data})
	if p.exited {
		c.push(&Event{Ev: EvExit, RunID: p.runID, Exited: true, ExitCode: p.exitCode}, 0)
	}
	p.mu.Unlock()
}

func (c *client) write(q *Request, p *hpty) {
	p.mu.Lock()
	gone := p.exited
	p.mu.Unlock()
	if gone {
		c.fail(q, "%s has exited", p.runID)
		return
	}
	if _, err := p.p.Write(q.Data); err != nil {
		c.fail(q, "write: %v", err)
		return
	}
	c.reply(q, Reply{OK: true})
}

func (c *client) resize(q *Request, p *hpty) {
	if q.Cols <= 0 || q.Rows <= 0 {
		c.fail(q, "resize needs cols and rows")
		return
	}
	// The cut is recorded BEFORE the resize is applied, so it is never behind a byte produced at the new size (G3:
	// never recorded AFTER such a byte. A byte written in the instant between the two may sit on either side, which
	// is the same race the supervisor has today).
	p.mu.Lock()
	oldC, oldR := p.rg.size()
	if oldC == q.Cols && oldR == q.Rows {
		cut := Cut{Off: p.rg.total, Cols: oldC, Rows: oldR}
		p.mu.Unlock()
		c.reply(q, Reply{OK: true, Cut: &cut})
		return
	}
	cut := p.rg.cut(q.Cols, q.Rows)
	p.mu.Unlock()
	if err := p.p.Resize(q.Cols, q.Rows); err != nil {
		p.mu.Lock()
		p.rg.cut(oldC, oldR) // the size did not change after all
		p.mu.Unlock()
		c.fail(q, "resize: %v", err)
		return
	}
	c.reply(q, Reply{OK: true, Cut: &cut})
}

func (c *client) signal(q *Request, p *hpty) {
	p.mu.Lock()
	gone := p.exited
	p.mu.Unlock()
	if gone {
		c.fail(q, "%s has exited", p.runID)
		return
	}
	var err error
	switch q.Sig {
	case SigTerm:
		err = termProcess(p.cmd.Process)
	case SigKill:
		err = p.cmd.Process.Kill()
	default:
		c.fail(q, "sig must be %q or %q", SigTerm, SigKill)
		return
	}
	if err != nil {
		c.fail(q, "signal: %v", err)
		return
	}
	c.reply(q, Reply{OK: true})
}

// collect acknowledges an exit, and only then does the host forget the pty and its ring. It is refused while the
// runner is alive.
func (c *client) collect(q *Request, p *hpty) {
	h := c.h
	p.mu.Lock()
	if !p.exited {
		p.mu.Unlock()
		c.fail(q, "%s is still running", p.runID)
		return
	}
	subs := make([]*client, 0, len(p.subs))
	for s := range p.subs {
		subs = append(subs, s)
	}
	p.subs = map[*client]struct{}{}
	p.mu.Unlock()
	h.mu.Lock()
	delete(h.ptys, p.runID)
	for _, s := range subs {
		delete(s.attached, p.runID)
	}
	h.mu.Unlock()
	_ = p.closer.Close()
	h.logf("%s collected", p.runID)
	c.reply(q, Reply{OK: true})
}

// ---- ids ----

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newRunID is a random 128-bit ULID-style id: 26 Crockford base32 characters, a 48-bit millisecond clock then 80
// random bits. It is unique across every host and every host lifetime, never a counter a restarted host could
// repeat.
func newRunID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (8 * (5 - i)))
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	// 128 bits as 26 groups of 5, the first carrying 3.
	var out [26]byte
	var acc uint
	var bits, oi int
	// Encode from the least significant end.
	oi = 25
	for i := 15; i >= 0; i-- {
		acc |= uint(b[i]) << bits
		bits += 8
		for bits >= 5 && oi >= 0 {
			out[oi] = crockford[acc&31]
			acc >>= 5
			bits -= 5
			oi--
		}
	}
	if oi >= 0 {
		out[oi] = crockford[acc&31]
	}
	return string(out[:])
}
