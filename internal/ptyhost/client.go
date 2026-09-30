package ptyhost

// THE CLIENT API. This is the contract the daemon's supervisor seam is written against. Keep it small.
//
//	Dial(addr) (*Client, error)                      connect, retrying briefly while the host comes up
//	DialTimeout(addr, d) (*Client, error)            the same with your own patience
//	(*Client).Probe() (Probe, error)                 read-only: proto, build, host pid, daemon connected, in_job
//	(*Client).Hello(build, takeover) (Probe, error)  become THE daemon. takeover replaces a connected one
//	(*Client).Spawn(SpawnArgs) (Spawned, error)      start a pty and its runner, get {Pid, RunID}
//	(*Client).List() ([]PtyInfo, error)              every pty the host holds, exited ones included
//	(*Client).Attach(runID, from) (*Attachment, error)  replay from an absolute offset, then follow live
//	(*Client).Write(runID, data) error               bytes to the pty, in the order this connection sent them
//	(*Client).Resize(runID, cols, rows) (Cut, error) records the cut, then resizes
//	(*Client).Signal(runID, SigTerm|SigKill) error
//	(*Client).Collect(runID) error                   acknowledge an exit. Refused while alive. Forgets the pty
//	(*Client).Close() error                          idempotent, safe from any goroutine
//	(*Client).Done() <-chan struct{}                 closed when the connection is gone, Err() says why
//
// Every verb after Spawn names the pty by RunID alone, and an unknown RunID is refused with a *RemoteError. A card's
// `id` is only an attribute.
//
// Only a connection that has said Hello may spawn, list, attach, write, resize, signal or collect. Probe needs no
// hello, evicts nobody, and is what to call to look at a host. A second Hello without takeover fails with "a
// daemon is already connected".
//
// An Attachment carries the replay bytes with their size cuts and a `Truncated` flag (the `from` asked for was older
// than the ring, so the replay starts at the ring's start instead, and `From` says where), then Events: live output
// at absolute offsets and finally one exit event, `Exited` true and `ExitCode` set, never before the last output
// bytes. Events is closed when the connection ends or the pty is collected. The offset to ask for after a
// reconnect is the end of the last output event seen, or From+len(Replay) if there was none. A slow consumer never
// blocks the connection: it is buffered in memory on this side, while the HOST closes a client that falls more than
// 4 MB behind (G4), and the way back is Attach again from that offset.
//
// Replies are matched by a sequence number, so calls may be made from several goroutines. Writes and resizes made
// from one goroutine reach the pty in that order.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// DefaultDialTimeout is how long Dial keeps trying. A host that has just been started is not listening yet.
const DefaultDialTimeout = 3 * time.Second

// RemoteError is a refusal from the host.
type RemoteError struct{ Msg string }

func (e *RemoteError) Error() string { return e.Msg }

// Probe is what probe and hello answer.
type Probe struct {
	Proto  int
	Build  string
	Pid    int
	Daemon bool // a daemon is connected (probe), always true after a hello
	InJob  bool // the host is inside a job object that refused breakaway (Windows), false elsewhere
	Ptys   int
	// TookOver is set on a hello that replaced a connected daemon.
	TookOver bool
}

// SpawnArgs is one spawn. Env is the whole environment of the runner, nil meaning the host's own.
type SpawnArgs struct {
	ID         string
	Kind       string // KindRunner or KindShell
	Argv       []string
	Env        []string
	Cwd        string
	Cols, Rows int
	Ring       int // bytes of output the host retains
}

// Spawned is what spawn answers.
type Spawned struct {
	Pid   int
	RunID string
}

// Attachment is one attach. Replay starts at absolute offset From.
type Attachment struct {
	Info      PtyInfo
	From      int64
	Truncated bool
	Cuts      []Cut
	Replay    []byte
	Events    <-chan Event
}

// Client is one connection to a host.
type Client struct {
	conn   net.Conn
	closer *closeOnce
	done   chan struct{}

	wmu sync.Mutex
	enc *json.Encoder

	mu      sync.Mutex
	seq     int64
	pending map[int64]chan Reply
	atts    map[string]*evq
	err     error
}

// Dial connects to the host at addr, retrying for DefaultDialTimeout.
func Dial(addr string) (*Client, error) { return DialTimeout(addr, DefaultDialTimeout) }

// DialTimeout connects to the host at addr, retrying until d has passed.
func DialTimeout(addr string, d time.Duration) (*Client, error) {
	deadline := time.Now().Add(d)
	for {
		conn, err := dialChannel(addr, 500*time.Millisecond)
		if err == nil {
			return newClientConn(conn), nil
		}
		var ne net.Error
		retry := errNoHost(err) || (errors.As(err, &ne) && ne.Timeout())
		if !retry || time.Now().After(deadline) {
			return nil, fmt.Errorf("dial pty host: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func newClientConn(conn net.Conn) *Client {
	c := &Client{conn: conn, done: make(chan struct{}), enc: json.NewEncoder(conn),
		pending: map[int64]chan Reply{}, atts: map[string]*evq{}}
	c.closer = newCloser(c.teardown)
	go c.readLoop()
	return c
}

// Close ends the connection. Any number of callers, any goroutine.
func (c *Client) Close() error { return c.closer.Close() }

// Done is closed when the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err says why the connection ended, or nil while it is up.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Client) teardown() error {
	err := c.conn.Close()
	c.mu.Lock()
	if c.err == nil {
		c.err = io.ErrClosedPipe
	}
	atts := c.atts
	c.atts = map[string]*evq{}
	c.mu.Unlock()
	close(c.done)
	for _, q := range atts {
		q.finish()
	}
	return err
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	_ = c.Close()
}

func (c *Client) readLoop() {
	r := bufio.NewReaderSize(c.conn, 1<<16)
	dec := json.NewDecoder(r)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			c.fail(err)
			return
		}
		var head struct {
			Ev string `json:"ev"`
		}
		_ = json.Unmarshal(raw, &head)
		if head.Ev != "" {
			var ev Event
			if json.Unmarshal(raw, &ev) != nil {
				continue
			}
			c.mu.Lock()
			q := c.atts[ev.RunID]
			c.mu.Unlock()
			if q != nil {
				q.put(ev)
			}
			continue
		}
		var rp Reply
		if json.Unmarshal(raw, &rp) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[rp.Seq]
		delete(c.pending, rp.Seq)
		c.mu.Unlock()
		if ch != nil {
			ch <- rp
		}
	}
}

func (c *Client) call(q Request) (Reply, error) {
	c.mu.Lock()
	c.seq++
	q.Seq = c.seq
	ch := make(chan Reply, 1)
	c.pending[q.Seq] = ch
	c.mu.Unlock()
	c.wmu.Lock()
	err := c.enc.Encode(&q)
	c.wmu.Unlock()
	if err != nil {
		c.fail(err)
		return Reply{}, err
	}
	select {
	case r := <-ch:
		if !r.OK {
			return r, &RemoteError{Msg: r.Err}
		}
		return r, nil
	case <-c.done:
		if err := c.Err(); err != nil {
			return Reply{}, err
		}
		return Reply{}, io.ErrClosedPipe
	}
}

func probeOf(r Reply) Probe {
	return Probe{Proto: r.Proto, Build: r.Build, Pid: r.Pid, Daemon: r.Daemon, InJob: r.InJob, Ptys: r.Ptys,
		TookOver: r.Took}
}

// Probe asks the host about itself and changes nothing.
func (c *Client) Probe() (Probe, error) {
	r, err := c.call(Request{V: VProbe})
	return probeOf(r), err
}

// Hello makes this connection the daemon. Without takeover it is refused while another daemon is connected.
func (c *Client) Hello(build string, takeover bool) (Probe, error) {
	r, err := c.call(Request{V: VHello, Proto: Proto, Build: build, Takeover: takeover})
	p := probeOf(r)
	p.Daemon = err == nil
	return p, err
}

// Spawn starts a pty.
func (c *Client) Spawn(a SpawnArgs) (Spawned, error) {
	r, err := c.call(Request{V: VSpawn, ID: a.ID, Kind: a.Kind, Argv: a.Argv, Env: a.Env, Cwd: a.Cwd,
		Cols: a.Cols, Rows: a.Rows, Ring: a.Ring})
	return Spawned{Pid: r.Pid, RunID: r.RunID}, err
}

// List returns every pty the host holds.
func (c *Client) List() ([]PtyInfo, error) {
	r, err := c.call(Request{V: VList})
	return r.List, err
}

// Attach replays the ring from absolute offset `from` and then follows the pty live. Only one attachment per
// run_id per connection.
func (c *Client) Attach(runID string, from int64) (*Attachment, error) {
	q := newEvq()
	c.mu.Lock()
	if _, dup := c.atts[runID]; dup {
		c.mu.Unlock()
		return nil, errors.New("already attached to " + runID + " on this connection")
	}
	// Registered BEFORE the request goes out: events follow the reply immediately.
	c.atts[runID] = q
	c.mu.Unlock()
	r, err := c.call(Request{V: VAttach, RunID: runID, From: from})
	if err != nil {
		c.mu.Lock()
		if c.atts[runID] == q {
			delete(c.atts, runID)
		}
		c.mu.Unlock()
		q.finish()
		return nil, err
	}
	a := &Attachment{From: r.From, Truncated: r.Truncated, Cuts: r.Cuts, Replay: r.Data, Events: q.out}
	if r.Info != nil {
		a.Info = *r.Info
	}
	go q.pump()
	return a, nil
}

// Write sends bytes to the pty.
func (c *Client) Write(runID string, data []byte) error {
	_, err := c.call(Request{V: VWrite, RunID: runID, Data: data})
	return err
}

// Resize records a cut at the current output offset and then resizes the pty.
func (c *Client) Resize(runID string, cols, rows int) (Cut, error) {
	r, err := c.call(Request{V: VResize, RunID: runID, Cols: cols, Rows: rows})
	if r.Cut != nil {
		return *r.Cut, err
	}
	return Cut{}, err
}

// Signal sends SigTerm or SigKill to the runner.
func (c *Client) Signal(runID, sig string) error {
	_, err := c.call(Request{V: VSignal, RunID: runID, Sig: sig})
	return err
}

// Collect acknowledges an exit and makes the host forget that pty and its ring.
func (c *Client) Collect(runID string) error {
	_, err := c.call(Request{V: VCollect, RunID: runID})
	if err == nil {
		c.mu.Lock()
		if q := c.atts[runID]; q != nil {
			delete(c.atts, runID)
			q.finish()
		}
		c.mu.Unlock()
	}
	return err
}

// evq is an unbounded queue in front of an Attachment's Events channel, so a consumer that is slow never stalls
// the connection's read loop, which also carries the replies to every other call.
type evq struct {
	mu   sync.Mutex
	buf  []Event
	fin  bool
	wake chan struct{}
	out  chan Event
}

func newEvq() *evq { return &evq{wake: make(chan struct{}, 1), out: make(chan Event)} }

func (q *evq) put(e Event) {
	q.mu.Lock()
	q.buf = append(q.buf, e)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *evq) finish() {
	q.mu.Lock()
	q.fin = true
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pump delivers in order and closes out after the last event of a finished queue.
func (q *evq) pump() {
	defer close(q.out)
	for {
		q.mu.Lock()
		batch, fin := q.buf, q.fin
		q.buf = nil
		q.mu.Unlock()
		for _, e := range batch {
			q.out <- e
		}
		if len(batch) > 0 {
			continue
		}
		if fin {
			return
		}
		<-q.wake
	}
}
