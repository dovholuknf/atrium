package ptyhost

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The tests run a real pty with a runner they control: this test binary, re-executed as `__runner <mode> ...`.
//
//	echo                  prints READY, then GOT:<line> for each line typed. `quit` exits 0, `exit7` exits 7
//	tick                  prints T000001, T000002 ... every 10ms. `quit` exits 0
//	burst <kb>            writes <kb> KB at once, then behaves like echo
//	burstexit <kb> <code> writes <kb> KB and exits at once with <code>
//
// And as `__host <stateDir> <idle>`, which is what the Start test launches detached.
func TestMain(m *testing.M) {
	// Cleared, these fall back to the live room's location file, which is how the f-011 spike found it. Pointed at a
	// file that does not exist, a runner started here can never find the room.
	missing := os.TempDir() + string(os.PathSeparator) + "atrium-ptyhost-test-no-such-location"
	for _, k := range []string{"ATRIUM_LOCATION", "ATRIUM_SHARED_LOCATION"} {
		os.Setenv(k, missing)
	}
	os.Unsetenv("ATRIUM_DEBUG_INPUTLAG")
	os.Unsetenv("ATRIUM_REAL_SCROLLBACK")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__runner":
			runnerMain(os.Args[2:])
			return
		case "__host":
			idle, _ := time.ParseDuration(os.Args[3])
			if err := Run(RunOptions{StateDir: os.Args[2], Build: "test-host", IdleExit: idle}); err != nil {
				fmt.Fprintln(os.Stderr, "host:", err)
				os.Exit(1)
			}
			return
		}
	}
	os.Exit(m.Run())
}

func runnerMain(args []string) {
	out := func(s string) { _, _ = os.Stdout.WriteString(s) }
	mode := args[0]
	atoi := func(i int) int { n, _ := strconv.Atoi(args[i]); return n }
	switch mode {
	case "burstexit":
		out(kb(atoi(1)))
		os.Exit(atoi(2))
	case "burst":
		out(kb(atoi(1)))
		echoLoop(out)
	case "tick":
		go echoLoop(out)
		for i := 1; ; i++ {
			out(fmt.Sprintf("T%06d\r\n", i))
			time.Sleep(10 * time.Millisecond)
		}
	default:
		echoLoop(out)
	}
}

// kb is n KB of numbered lines, so a gap or a repeat is visible.
func kb(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n*1024; i++ {
		fmt.Fprintf(&b, "B%07d ................................................\r\n", i)
	}
	return b.String()
}

func echoLoop(out func(string)) {
	out("READY\r\n")
	var line []byte
	one := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(one)
		if n == 0 || err != nil {
			os.Exit(3)
		}
		if one[0] != '\r' && one[0] != '\n' {
			line = append(line, one[0])
			continue
		}
		s := string(line)
		line = line[:0]
		switch s {
		case "":
		case "quit":
			out("bye\r\n")
			os.Exit(0)
		case "exit7":
			out("bye7\r\n")
			os.Exit(7)
		default:
			out("GOT:" + s + "\r\n")
		}
	}
}

// ---- harness ----

func testHost(t *testing.T, o Options) (*Host, string) {
	t.Helper()
	dir := shortDir(t)
	addr := Address(dir)
	ln, err := listenChannel(addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if o.Logf == nil {
		o.Logf = func(f string, a ...any) { t.Logf("host: "+f, a...) }
	}
	h := NewHost(o)
	go h.Serve(ln)
	t.Cleanup(h.Close)
	return h, addr
}

// shortDir is a state directory whose name does not carry the test's. On Linux the channel is a unix socket inside
// it, and a socket path over 108 bytes does not bind: t.TempDir() under a long subtest name is past that.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ph")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func dialT(t *testing.T, addr string) *Client {
	t.Helper()
	c, err := Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// daemonT dials and says hello, retrying while a previous connection is still being noticed as gone.
func daemonT(t *testing.T, addr string) *Client {
	t.Helper()
	c := dialT(t, addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := c.Hello("test", false)
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("hello: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func runnerArgv(mode string, args ...string) []string {
	return append([]string{os.Args[0], "__runner", mode}, args...)
}

func spawnT(t *testing.T, c *Client, id, kind string, ring int, mode string, args ...string) Spawned {
	t.Helper()
	sp, err := c.Spawn(SpawnArgs{ID: id, Kind: kind, Argv: runnerArgv(mode, args...), Cols: 100, Rows: 30, Ring: ring})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = c.Signal(sp.RunID, SigKill) })
	return sp
}

// stream follows an Attachment: the replay and then every live event, checking each is contiguous with the last.
type stream struct {
	a    *Attachment
	all  []byte // the replay, then everything live
	from int64  // the offset all[0] is at
	next int64  // the offset after the last byte of all
	exit *Event
}

func newStream(a *Attachment) *stream {
	return &stream{a: a, all: append([]byte(nil), a.Replay...), from: a.From, next: a.From + int64(len(a.Replay))}
}

// until reads until pred(all) is true, or the exit arrives, or the events close, and fails on a timeout. A nil
// pred reads to the exit.
func (s *stream) until(t *testing.T, d time.Duration, pred func(all []byte) bool) {
	t.Helper()
	timeout := time.After(d)
	for {
		if pred != nil && pred(s.all) {
			return
		}
		if s.exit != nil {
			return
		}
		select {
		case ev, ok := <-s.a.Events:
			if !ok {
				return
			}
			if ev.Ev == EvExit {
				e := ev
				s.exit = &e
				continue
			}
			if ev.Off != s.next {
				t.Fatalf("gap or repeat: event at %d, expected %d", ev.Off, s.next)
			}
			s.next = ev.Off + int64(len(ev.Data))
			s.all = append(s.all, ev.Data...)
		case <-timeout:
			t.Fatalf("timed out after %s, %d bytes: %q", d, len(s.all), tailOf(s.all, 200))
		}
	}
}

func has(s string) func([]byte) bool { return func(b []byte) bool { return contains(b, s) } }

func tailOf(b []byte, n int) []byte {
	if len(b) > n {
		return b[len(b)-n:]
	}
	return b
}

func contains(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }

// waitFor polls f for up to d.
func waitFor(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func infoOf(t *testing.T, c *Client, runID string) PtyInfo {
	t.Helper()
	l, err := c.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range l {
		if p.RunID == runID {
			return p
		}
	}
	t.Fatalf("run %s not listed", runID)
	return PtyInfo{}
}
