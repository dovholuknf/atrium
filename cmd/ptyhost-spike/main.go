// ptyhost-spike is stage 0 of f-011, a THROWAWAY. It proves or disproves the pty host of
// docs/rnd/rolling-restart-design.md section 3 and is not part of the product. It is not wired into the atrium
// binary, the supervisor or the cobra root, on purpose.
//
//	ptyhost-spike host   -name N [-cols C -rows R -ring BYTES] -- cmd args...   own ONE pty, serve the pipe
//	ptyhost-spike start  -name N [same flags] -- cmd args...                    start a host detached
//	ptyhost-spike parent -name N [-hold D] -- cmd args...                       start a host detached, then sit
//	ptyhost-spike client -name N <verb> [args]                                  one connection, one verb
//
// Verbs: hello, list, attach, write, resize, signal, collect. Framing is one JSON object per line, and bytes
// travel as base64 (encoding/json does that for []byte).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const proto = 1

// extraModes are platform-specific spike modes (jobtest on Windows).
var extraModes = map[string]func([]string){}

type cut struct {
	Off  int64 `json:"off"`
	Cols int   `json:"cols"`
	Rows int   `json:"rows"`
}

// ring keeps the last `cap` bytes and the absolute offset of the next byte. total never resets.
type ring struct {
	mu    sync.Mutex
	buf   []byte
	cap   int
	total int64
	cuts  []cut
	subs  map[chan frame]struct{}
}

type frame struct {
	Out  int64  `json:"out,omitempty"`
	Data []byte `json:"data,omitempty"`
	Exit *int   `json:"exit,omitempty"`
}

func (r *ring) start() int64 { return max(0, r.total-int64(len(r.buf))) }

func (r *ring) write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	off := r.total
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.cap:]...)
	}
	r.total += int64(len(p))
	for c := range r.subs {
		select {
		case c <- frame{Out: off, Data: append([]byte(nil), p...)}:
		default:
			// a slow client never blocks the drain: drop it, it reattaches from its offset.
			close(c)
			delete(r.subs, c)
		}
	}
}

// snapshot returns bytes from `from`, the effective from, whether it was truncated, and the cuts in force
// from there. With sub true it also subscribes, atomically with the snapshot.
func (r *ring) snapshot(from int64, sub bool) ([]byte, int64, bool, []cut, chan frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trunc := false
	if from < r.start() {
		from, trunc = r.start(), true
	}
	if from > r.total {
		from = r.total
	}
	data := append([]byte(nil), r.buf[from-r.start():]...)
	var cuts []cut
	for i, c := range r.cuts {
		if c.Off <= from {
			cuts = []cut{{Off: from, Cols: c.Cols, Rows: c.Rows}}
			continue
		}
		cuts = append(cuts, r.cuts[i])
	}
	var ch chan frame
	if sub {
		ch = make(chan frame, 4096)
		r.subs[ch] = struct{}{}
	}
	return data, from, trunc, cuts, ch
}

type host struct {
	name string
	kind string
	r    *ring
	p    pty.Pty
	c    *pty.Cmd
	pid  int

	mu       sync.Mutex
	cols     int
	rows     int
	started  time.Time
	exited   bool
	exitCode int
	runID    string
	conn     io.Closer // the one daemon being served
	collect  chan struct{}
}

type req struct {
	V     string `json:"v"`
	Proto int    `json:"proto,omitempty"`
	RunID string `json:"run_id,omitempty"`
	From  int64  `json:"from,omitempty"`
	Data  []byte `json:"data,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Sig   string `json:"sig,omitempty"`
}

type resp struct {
	OK        bool     `json:"ok"`
	Err       string   `json:"err,omitempty"`
	Proto     int      `json:"proto,omitempty"`
	Build     string   `json:"build,omitempty"`
	Pid       int      `json:"pid,omitempty"`
	RunID     string   `json:"run_id,omitempty"`
	Cols      int      `json:"cols,omitempty"`
	Rows      int      `json:"rows,omitempty"`
	Started   string   `json:"started,omitempty"`
	Exited    bool     `json:"exited,omitempty"`
	ExitCode  int      `json:"exit_code,omitempty"`
	RingStart int64    `json:"ring_start"`
	OutOffset int64    `json:"out_offset"`
	From      int64    `json:"from,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Cuts      []cut    `json:"cuts,omitempty"`
	Tail      []byte   `json:"tail,omitempty"`
	Frame     *frame   `json:"frame,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Ptys      []resp   `json:"ptys,omitempty"`
	Extra     []string `json:"extra,omitempty"`
}

func newRunID() string {
	// spike: time plus pid is enough. The design asks for 128 random bits.
	return fmt.Sprintf("%016x-%08x", time.Now().UnixNano(), os.Getpid())
}

func (h *host) info() resp {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return resp{OK: true, RunID: h.runID, Pid: h.pid, Cols: h.cols, Rows: h.rows, Kind: h.kind,
		Started: h.started.Format(time.RFC3339Nano), Exited: h.exited, ExitCode: h.exitCode,
		RingStart: h.r.start(), OutOffset: h.r.total}
}

func runHost(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	name := fs.String("name", "", "pipe or socket name")
	cols := fs.Int("cols", 100, "")
	rows := fs.Int("rows", 30, "")
	ringN := fs.Int("ring", 1<<20, "ring bytes")
	kind := fs.String("kind", "runner", "")
	dir := fs.String("dir", "", "cwd")
	_ = fs.Parse(args)
	argv := fs.Args()
	if *name == "" || len(argv) == 0 {
		fatal("usage: host -name N -- cmd args")
	}

	ln, err := listen(*name)
	if err != nil {
		fatal("listen: %v", err)
	}
	p, err := pty.New()
	if err != nil {
		fatal("pty: %v", err)
	}
	if err := p.Resize(*cols, *rows); err != nil {
		fatal("resize: %v", err)
	}
	exe, err := exec.LookPath(argv[0])
	if err != nil {
		fatal("lookpath: %v", err)
	}
	c := p.Command(exe, argv[1:]...)
	c.Dir = *dir
	c.Env = os.Environ()
	if err := c.Start(); err != nil {
		fatal("start: %v", err)
	}
	h := &host{name: *name, kind: *kind, p: p, c: c, pid: c.Process.Pid, cols: *cols, rows: *rows,
		started: time.Now(), runID: newRunID(), collect: make(chan struct{})}
	h.r = &ring{cap: *ringN, subs: map[chan frame]struct{}{}, cuts: []cut{{0, *cols, *rows}}}
	logf("host up name=%s pid=%d runner=%d run_id=%s", *name, os.Getpid(), h.pid, h.runID)

	drained := make(chan struct{})
	go func() { // ALWAYS drains, whether or not a client is connected
		defer close(drained)
		b := make([]byte, 8192)
		for {
			n, err := p.Read(b)
			if n > 0 {
				h.r.write(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := c.Wait()
		code := 0
		if c.ProcessState != nil {
			code = c.ProcessState.ExitCode()
		} else if err != nil {
			code = -1
		}
		select { // let the drain finish the tail, bounded
		case <-drained:
		case <-time.After(1500 * time.Millisecond):
		}
		h.mu.Lock()
		h.exited, h.exitCode = true, code
		h.mu.Unlock()
		h.r.mu.Lock()
		for ch := range h.r.subs {
			ch <- frame{Exit: &code}
		}
		h.r.mu.Unlock()
		logf("runner exited code=%d", code)
	}()
	go func() { // no client and collected: the spike host exits
		<-h.collect
		time.Sleep(200 * time.Millisecond)
		logf("collected, host exits")
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			fatal("accept: %v", err)
		}
		// serves one daemon at a time: a new connection closes the older one first
		h.mu.Lock()
		if h.conn != nil {
			h.conn.Close()
		}
		h.conn = conn
		h.mu.Unlock()
		go h.serve(conn)
	}
}

func (h *host) serve(conn io.ReadWriteCloser) {
	defer conn.Close()
	var wmu sync.Mutex
	send := func(v any) error {
		b, _ := json.Marshal(v)
		wmu.Lock()
		defer wmu.Unlock()
		_, err := conn.Write(append(b, '\n'))
		return err
	}
	rd := bufio.NewReaderSize(conn, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var q req
		if json.Unmarshal(line, &q) != nil {
			_ = send(resp{Err: "bad frame"})
			continue
		}
		if q.RunID != "" && q.RunID != h.runID {
			_ = send(resp{Err: "unknown run_id"}) // the address rule: a verb naming another run is refused
			continue
		}
		switch q.V {
		case "hello":
			_ = send(resp{OK: true, Proto: proto, Build: "spike"})
		case "list":
			_ = send(resp{OK: true, Ptys: []resp{h.info()}})
		case "attach":
			data, from, trunc, cuts, ch := h.r.snapshot(q.From, true)
			i := h.info()
			i.From, i.Truncated, i.Cuts, i.Tail = from, trunc, cuts, data
			_ = send(i)
			h.mu.Lock()
			ex, code := h.exited, h.exitCode
			h.mu.Unlock()
			if ex {
				_ = send(resp{OK: true, Frame: &frame{Exit: &code}})
			}
			go func() {
				for f := range ch {
					f := f
					if send(resp{OK: true, Frame: &f}) != nil {
						return
					}
				}
			}()
		case "write":
			_, err := h.p.Write(q.Data)
			_ = send(resp{OK: err == nil, Err: errs(err)})
		case "resize":
			// the cut is recorded BEFORE the pty resize, at the current offset (design 3.2)
			h.r.mu.Lock()
			h.r.cuts = append(h.r.cuts, cut{h.r.total, q.Cols, q.Rows})
			off := h.r.total
			h.r.mu.Unlock()
			err := h.p.Resize(q.Cols, q.Rows)
			h.mu.Lock()
			h.cols, h.rows = q.Cols, q.Rows
			h.mu.Unlock()
			_ = send(resp{OK: err == nil, Err: errs(err), OutOffset: off})
		case "signal":
			var err error
			if h.c.Process != nil {
				err = h.c.Process.Kill() // spike: term and kill are the same on Windows
			}
			_ = send(resp{OK: err == nil, Err: errs(err)})
		case "collect":
			h.mu.Lock()
			ex := h.exited
			h.mu.Unlock()
			if !ex {
				_ = send(resp{Err: "not exited"})
				continue
			}
			_ = send(resp{OK: true})
			close(h.collect)
			return
		default:
			_ = send(resp{Err: "unknown verb " + q.V})
		}
	}
}

func errs(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func logf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s [ptyhost-spike] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(f, a...))
}

func fatal(f string, a ...any) {
	logf(f, a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: host|start|parent|client")
	}
	switch os.Args[1] {
	case "host":
		runHost(os.Args[2:])
	case "start":
		startHost(os.Args[2:])
	case "parent":
		fs := flag.NewFlagSet("parent", flag.ExitOnError)
		hold := fs.Duration("hold", time.Hour, "")
		name := fs.String("name", "", "")
		fs.String("kind", "runner", "")
		_ = name
		// everything after the flags is the host command; reuse startHost with the same args minus -hold
		var rest []string
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "-log" { // the parent and its host both log here, since a task has no terminal
				os.Setenv("SPIKE_HOSTLOG", os.Args[i+1])
				if f, err := os.OpenFile(os.Args[i+1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
					os.Stderr = f
				}
				i++
				continue
			}
			if os.Args[i] == "-nobreak" { // control run: the host does not ask to leave the job
				os.Setenv("SPIKE_NOBREAKAWAY", "1")
				continue
			}
			if os.Args[i] == "-hold" || os.Args[i] == "--hold" {
				d, _ := time.ParseDuration(os.Args[i+1])
				*hold = d
				i++
				continue
			}
			rest = append(rest, os.Args[i])
		}
		logf("parent %d: %s", os.Getpid(), jobReport())
		startHost(rest)
		logf("parent %d holding for %s", os.Getpid(), *hold)
		time.Sleep(*hold)
	case "client":
		runClient(os.Args[2:])
	default:
		if f, ok := extraModes[os.Args[1]]; ok {
			f(os.Args[2:])
			return
		}
		fatal("unknown mode %s", os.Args[1])
	}
}

func startHost(args []string) {
	exe, _ := os.Executable()
	out, _ := os.OpenFile(os.Getenv("SPIKE_HOSTLOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	pr, err := startDetached(exe, append([]string{"host"}, args...), out)
	if err != nil {
		fatal("start detached: %v", err)
	}
	logf("started host pid=%d", pr.Pid)
}

// ---- client ----

func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	name := fs.String("name", "", "")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fatal("client verb")
	}
	conn, err := dial(*name)
	if err != nil {
		fatal("dial: %v", err)
	}
	defer conn.Close()
	rd := bufio.NewReaderSize(conn, 1<<20)
	call := func(q req) resp {
		b, _ := json.Marshal(q)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			fatal("write: %v", err)
		}
		line, err := rd.ReadBytes('\n')
		if err != nil {
			fatal("read: %v", err)
		}
		var r resp
		_ = json.Unmarshal(line, &r)
		return r
	}
	show := func(r resp) {
		r.Tail = nil
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
	}
	sub := flag.NewFlagSet("verb", flag.ExitOnError)
	switch rest[0] {
	case "hello":
		show(call(req{V: "hello", Proto: proto}))
	case "list":
		show(call(req{V: "list"}))
	case "write": // write [-raw] text... ; \r is appended unless -noenter
		noenter := sub.Bool("noenter", false, "")
		id := sub.String("run", "", "")
		_ = sub.Parse(rest[1:])
		t := strings.Join(sub.Args(), " ")
		if !*noenter {
			t += "\r"
		}
		show(call(req{V: "write", RunID: *id, Data: []byte(t)}))
	case "resize":
		var c, r int
		fmt.Sscan(rest[1], &c)
		fmt.Sscan(rest[2], &r)
		show(call(req{V: "resize", Cols: c, Rows: r}))
	case "signal":
		show(call(req{V: "signal", Sig: "kill"}))
	case "collect":
		show(call(req{V: "collect"}))
	case "attach":
		from := sub.Int64("from", 0, "")
		dur := sub.Duration("for", 5*time.Second, "")
		outf := sub.String("out", "", "file for the raw bytes")
		id := sub.String("run", "", "")
		script := sub.String("script", "", "file of commands sent on this connection after attach")
		_ = sub.Parse(rest[1:])
		r := call(req{V: "attach", From: *from, RunID: *id})
		if !r.OK {
			show(r)
			return
		}
		var f *os.File
		if *outf != "" {
			f, _ = os.Create(*outf)
			defer f.Close()
		}
		if f != nil {
			f.Write(r.Tail)
		}
		next := r.From + int64(len(r.Tail))
		hdr := r
		hdr.Tail = nil
		b, _ := json.Marshal(hdr)
		fmt.Printf("attach header: %s\nreplayed %d bytes from %d, next=%d\n", b, len(r.Tail), r.From, next)
		if *script != "" {
			// commands on THIS connection, since the host serves one at a time: "sleep MS", "write TEXT"
			// (with Enter), "type TEXT" (without), "resize C R", "list", "sig", "collect", "hold"
			go func() {
				sc, _ := os.ReadFile(*script)
				for _, ln := range strings.Split(strings.ReplaceAll(string(sc), "\r", ""), "\n") {
					verb, arg, _ := strings.Cut(ln, " ")
					var q req
					switch verb {
					case "sleep":
						var ms int
						fmt.Sscan(arg, &ms)
						time.Sleep(time.Duration(ms) * time.Millisecond)
						continue
					case "write":
						// text, a pause, then Enter as its own frame: an Enter in the same write as the text
						// is taken by claude's input box as part of a paste and does not submit.
						b, _ := json.Marshal(req{V: "write", Data: []byte(arg)})
						_, _ = conn.Write(append(b, '\n'))
						time.Sleep(500 * time.Millisecond)
						q = req{V: "write", Data: []byte("\r")}
					case "type": // \e is ESC, \r is Enter
						arg = strings.NewReplacer(`\e`, "\x1b", `\r`, "\r").Replace(arg)
						q = req{V: "write", Data: []byte(arg)}
					case "resize":
						q = req{V: "resize"}
						fmt.Sscan(arg, &q.Cols, &q.Rows)
					case "list", "collect":
						q = req{V: verb}
					case "sig":
						q = req{V: "signal", Sig: "kill"}
					default:
						continue
					}
					b, _ := json.Marshal(q)
					_, _ = conn.Write(append(b, '\n'))
					logf("client sent %s %s", verb, arg)
				}
			}()
		}
		deadline := time.After(*dur)
		lines := make(chan resp)
		go func() {
			for {
				line, err := rd.ReadBytes('\n')
				if err != nil {
					close(lines)
					return
				}
				var x resp
				_ = json.Unmarshal(line, &x)
				lines <- x
			}
		}()
	loop:
		for {
			select {
			case <-deadline:
				break loop
			case x, ok := <-lines:
				if !ok {
					fmt.Println("connection closed by host")
					break loop
				}
				if x.Frame == nil {
					x.Tail = nil
					b, _ := json.Marshal(x)
					fmt.Printf("reply: %s\n", b)
					continue
				}
				if x.Frame.Exit != nil {
					fmt.Printf("EXIT code=%d\n", *x.Frame.Exit)
					continue
				}
				if x.Frame.Out != next {
					fmt.Printf("GAP OR DUP: frame at %d, expected %d\n", x.Frame.Out, next)
				}
				next = x.Frame.Out + int64(len(x.Frame.Data))
				if f != nil {
					f.Write(x.Frame.Data)
					f.Sync()
				}
			}
		}
		fmt.Printf("done, next offset=%d\n", next)
	default:
		fatal("unknown verb %s", rest[0])
	}
}
