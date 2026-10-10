//go:build integration

package ptyhost

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProbeNeverEvicts_SecondHelloRefused_TakeoverReplaces(t *testing.T) {
	_, addr := testHost(t, Options{Build: "b1"})
	d1 := dialT(t, addr)
	if p, err := d1.Hello("first", false); err != nil || !p.Daemon || p.Proto != Proto || p.Build != "b1" {
		t.Fatalf("hello: %+v %v", p, err)
	}
	// a probe, from a third party and from the daemon's own connection, changes nothing
	pc := dialT(t, addr)
	p, err := pc.Probe()
	// in_job is whatever this process is: the test runner may itself be in a job
	wantJob, _ := inJob()
	if err != nil || !p.Daemon || p.Pid == 0 || p.Proto != Proto || p.InJob != wantJob {
		t.Fatalf("probe: %+v %v", p, err)
	}
	if _, err := d1.Probe(); err != nil {
		t.Fatalf("probe from the daemon: %v", err)
	}
	if _, err := d1.List(); err != nil {
		t.Fatalf("the daemon was evicted by a probe: %v", err)
	}
	// a second hello without takeover is refused, and the first daemon stays
	d2 := dialT(t, addr)
	_, err = d2.Hello("second", false)
	if err == nil || !strings.Contains(err.Error(), "a daemon is already connected") {
		t.Fatalf("second hello: %v", err)
	}
	if _, err := d2.List(); err == nil {
		t.Fatal("a refused hello still let its connection list")
	}
	if _, err := d1.List(); err != nil {
		t.Fatalf("first daemon lost to a refused hello: %v", err)
	}
	// takeover replaces it
	p, err = d2.Hello("second", true)
	if err != nil || !p.TookOver {
		t.Fatalf("takeover: %+v %v", p, err)
	}
	if _, err := d2.List(); err != nil {
		t.Fatalf("the new daemon cannot list: %v", err)
	}
	waitFor(t, 3*time.Second, "the old daemon to be closed", func() bool { return d1.Err() != nil })
	if _, err := d1.List(); err == nil {
		t.Fatal("the old daemon still works after a takeover")
	}
}

func TestSpawnWriteAndOutput(t *testing.T) {
	_, addr := testHost(t, Options{})
	c := daemonT(t, addr)
	sp := spawnT(t, c, "card1", KindRunner, 0, "echo")
	if sp.RunID == "" || len(sp.RunID) != 26 || sp.Pid == 0 {
		t.Fatalf("spawn: %+v", sp)
	}
	a, err := c.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Info.ID != "card1" || a.Info.Kind != KindRunner || a.Info.Cols != 100 || a.Info.Rows != 30 {
		t.Fatalf("info: %+v", a.Info)
	}
	s := newStream(a)
	s.until(t, 20*time.Second, has("READY"))
	if err := c.Write(sp.RunID, []byte("hello there\r")); err != nil {
		t.Fatal(err)
	}
	s.until(t, 20*time.Second, has("GOT:hello there"))
}

func TestReattachLosesAndRepeatsNothing(t *testing.T) {
	_, addr := testHost(t, Options{})
	c1 := daemonT(t, addr)
	sp := spawnT(t, c1, "c", KindRunner, 8<<20, "tick")
	a1, err := c1.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	s1 := newStream(a1)
	s1.until(t, 20*time.Second, has("T000005"))
	seen := append([]byte(nil), s1.all...)
	next := s1.next
	// the client dies mid-output
	c1.Close()
	waitFor(t, 3*time.Second, "the host to notice", func() bool {
		pc := dialT(t, addr)
		p, err := pc.Probe()
		return err == nil && !p.Daemon
	})
	before := next
	time.Sleep(300 * time.Millisecond) // the ring keeps growing with nobody there

	c2 := daemonT(t, addr)
	if got := infoOf(t, c2, sp.RunID).OutOffset; got <= before {
		t.Fatalf("the ring did not grow while no daemon was connected: %d then %d", before, got)
	}
	a2, err := c2.Attach(sp.RunID, next)
	if err != nil {
		t.Fatal(err)
	}
	if a2.From != next || a2.Truncated {
		t.Fatalf("reattach from %d: from=%d truncated=%v", next, a2.From, a2.Truncated)
	}
	if len(a2.Replay) == 0 {
		t.Fatal("nothing replayed for the gap")
	}
	s2 := newStream(a2)
	s2.until(t, 20*time.Second, func(b []byte) bool { return len(b) > len(a2.Replay)+200 })
	seen = append(seen, s2.all...)

	// from 0, on a third connection, is the same bytes
	c3 := daemonT2(t, addr, c2)
	a3, err := c3.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a3.From != 0 || a3.Truncated {
		t.Fatalf("from 0: from=%d truncated=%v", a3.From, a3.Truncated)
	}
	n := min(len(seen), len(a3.Replay))
	if !bytes.Equal(seen[:n], a3.Replay[:n]) {
		t.Fatalf("a reattach from the last offset and a replay from 0 disagree")
	}
	if len(a3.Replay) < len(seen) {
		t.Fatalf("replay from 0 is shorter (%d) than what was seen (%d)", len(a3.Replay), len(seen))
	}
}

// daemonT2 takes over from a daemon the test still holds open.
func daemonT2(t *testing.T, addr string, _ *Client) *Client {
	t.Helper()
	c := dialT(t, addr)
	if _, err := c.Hello("test", true); err != nil {
		t.Fatalf("takeover hello: %v", err)
	}
	return c
}

func TestWrappedRingIsTruncatedWithRebasedCuts(t *testing.T) {
	_, addr := testHost(t, Options{})
	c := daemonT(t, addr)
	sp := spawnT(t, c, "c", KindRunner, 4096, "burst", "64")
	waitFor(t, 20*time.Second, "the burst", func() bool { return infoOf(t, c, sp.RunID).OutOffset > 20000 })
	if _, err := c.Resize(sp.RunID, 80, 24); err != nil {
		t.Fatal(err)
	}
	a, err := c.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Truncated || a.From == 0 || a.Info.RingStart != a.From {
		t.Fatalf("a wrapped ring: truncated=%v from=%d ring_start=%d", a.Truncated, a.From, a.Info.RingStart)
	}
	if len(a.Replay) > 4096 {
		t.Fatalf("the ring kept %d bytes, cap is 4096", len(a.Replay))
	}
	if a.From+int64(len(a.Replay)) != a.Info.OutOffset {
		t.Fatalf("replay does not end at the offset: %d + %d != %d", a.From, len(a.Replay), a.Info.OutOffset)
	}
	if len(a.Cuts) < 2 || a.Cuts[0].Off != a.From || a.Cuts[0].Cols != 100 || a.Cuts[0].Rows != 30 {
		t.Fatalf("the first cut is not rebased to the ring start: %+v (from %d)", a.Cuts, a.From)
	}
	last := a.Cuts[len(a.Cuts)-1]
	if last.Cols != 80 || last.Rows != 24 {
		t.Fatalf("last cut %+v, want 80x24", last)
	}
}

func TestResizeAfterReattachCutsBeforeNewSizeBytes(t *testing.T) {
	_, addr := testHost(t, Options{})
	c1 := daemonT(t, addr)
	sp := spawnT(t, c1, "c", KindRunner, 8<<20, "tick")
	a1, _ := c1.Attach(sp.RunID, 0)
	s1 := newStream(a1)
	s1.until(t, 20*time.Second, has("T000003"))
	next := s1.next
	c1.Close()
	c2 := daemonT(t, addr)
	a2, err := c2.Attach(sp.RunID, next)
	if err != nil {
		t.Fatal(err)
	}
	cut, err := c2.Resize(sp.RunID, 70, 20)
	if err != nil {
		t.Fatal(err)
	}
	if cut.Cols != 70 || cut.Rows != 20 || cut.Off < next {
		t.Fatalf("cut %+v", cut)
	}
	// every byte that reaches us at or after the resize was produced after the cut was recorded, so none sits
	// ahead of it: the cut is at or before the first byte at the new size.
	s2 := newStream(a2)
	s2.until(t, 20*time.Second, func(b []byte) bool { return len(b) > len(a2.Replay)+300 })
	c3 := daemonT2(t, addr, c2)
	a3, _ := c3.Attach(sp.RunID, 0)
	found := false
	for _, k := range a3.Cuts {
		if k == cut {
			found = true
		}
	}
	if !found {
		t.Fatalf("the cut %+v is not in the cut list %+v", cut, a3.Cuts)
	}
	if info := infoOf(t, c3, sp.RunID); info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("size %dx%d", info.Cols, info.Rows)
	}
	for i := 1; i < len(a3.Cuts); i++ {
		if a3.Cuts[i].Off < a3.Cuts[i-1].Off {
			t.Fatalf("cuts out of order: %+v", a3.Cuts)
		}
	}
}

func TestRunnerAndShellOnOneCardAreAddressedByRunID(t *testing.T) {
	_, addr := testHost(t, Options{})
	c := daemonT(t, addr)
	run := spawnT(t, c, "card", KindRunner, 0, "echo")
	sh := spawnT(t, c, "card", KindShell, 0, "echo")
	if run.RunID == sh.RunID {
		t.Fatal("one run_id for two ptys")
	}
	l, _ := c.List()
	kinds := map[string]string{}
	for _, p := range l {
		if p.ID != "card" {
			t.Fatalf("id %q", p.ID)
		}
		kinds[p.RunID] = p.Kind
	}
	if kinds[run.RunID] != KindRunner || kinds[sh.RunID] != KindShell || len(kinds) != 2 {
		t.Fatalf("list %+v", l)
	}
	ar, _ := c.Attach(run.RunID, 0)
	as, _ := c.Attach(sh.RunID, 0)
	sr, ss := newStream(ar), newStream(as)
	sr.until(t, 20*time.Second, has("READY"))
	ss.until(t, 20*time.Second, has("READY"))
	if err := c.Write(sh.RunID, []byte("only-the-shell\r")); err != nil {
		t.Fatal(err)
	}
	ss.until(t, 20*time.Second, has("GOT:only-the-shell"))
	time.Sleep(300 * time.Millisecond)
	// the runner never saw it
	ar2, err := c.Attach(run.RunID, 0)
	if err == nil && contains(ar2.Replay, "only-the-shell") {
		t.Fatal("a write to the shell reached the runner")
	}
	// resize and signal reach only theirs
	if _, err := c.Resize(sh.RunID, 60, 20); err != nil {
		t.Fatal(err)
	}
	if p := infoOf(t, c, run.RunID); p.Cols != 100 || p.Rows != 30 {
		t.Fatalf("resizing the shell resized the runner: %dx%d", p.Cols, p.Rows)
	}
	if err := c.Signal(sh.RunID, SigKill); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "the shell to exit", func() bool { return infoOf(t, c, sh.RunID).Exited })
	if infoOf(t, c, run.RunID).Exited {
		t.Fatal("killing the shell killed the runner")
	}
	// an unknown run_id is refused by every verb, and so is an empty one
	for _, id := range []string{"01ZZZZZZZZZZZZZZZZZZZZZZZZ", "", "card"} {
		if _, err := c.Attach2(id); err == nil {
			t.Fatalf("attach %q accepted", id)
		}
		if err := c.Write(id, []byte("x")); err == nil {
			t.Fatalf("write %q accepted", id)
		}
		if _, err := c.Resize(id, 10, 10); err == nil {
			t.Fatalf("resize %q accepted", id)
		}
		if err := c.Signal(id, SigKill); err == nil {
			t.Fatalf("signal %q accepted", id)
		}
		if err := c.Collect(id); err == nil {
			t.Fatalf("collect %q accepted", id)
		}
	}
}

// Attach2 is a test alias for a from-zero attach, so a loop can name it.
func (c *Client) Attach2(runID string) (*Attachment, error) { return c.Attach(runID, 0) }

func TestExitWithNoClientIsKeptAndCollected(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		code int
		tail string
	}{{"exit7", 7, "bye7"}, {"quit", 0, "bye"}} {
		t.Run(tc.cmd, func(t *testing.T) {
			_, addr := testHost(t, Options{})
			c1 := daemonT(t, addr)
			sp := spawnT(t, c1, "c", KindRunner, 0, "echo")
			a, _ := c1.Attach(sp.RunID, 0)
			newStream(a).until(t, 20*time.Second, has("READY"))
			if err := c1.Collect(sp.RunID); err == nil {
				t.Fatal("collect of a live runner was accepted")
			}
			if err := c1.Write(sp.RunID, []byte(tc.cmd+"\r")); err != nil {
				t.Fatal(err)
			}
			c1.Close() // and nobody is there when it exits
			time.Sleep(700 * time.Millisecond)
			c2 := daemonT(t, addr)
			info := infoOf(t, c2, sp.RunID)
			if !info.Exited || info.ExitCode != tc.code {
				t.Fatalf("info %+v, want exited with %d", info, tc.code)
			}
			raw, _ := json.Marshal(info)
			if !strings.Contains(string(raw), `"exited":true`) || !strings.Contains(string(raw), `"exit_code":`) {
				t.Fatalf("exited and exit_code must always be on the wire: %s", raw)
			}
			a2, err := c2.Attach(sp.RunID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !contains(a2.Replay, tc.tail) {
				t.Fatalf("the tail %q is not in the replay: %q", tc.tail, tailOf(a2.Replay, 200))
			}
			s2 := newStream(a2)
			s2.until(t, 10*time.Second, nil)
			exit := s2.exit
			if exit == nil || !exit.Exited || exit.ExitCode != tc.code {
				t.Fatalf("exit event %+v", exit)
			}
			if err := c2.Collect(sp.RunID); err != nil {
				t.Fatal(err)
			}
			if l, _ := c2.List(); len(l) != 0 {
				t.Fatalf("collect did not forget it: %+v", l)
			}
			if err := c2.Collect(sp.RunID); err == nil {
				t.Fatal("a second collect was accepted")
			}
		})
	}
}

func TestExitComesAfterTheLastBytes(t *testing.T) {
	_, addr := testHost(t, Options{})
	c := daemonT(t, addr)
	sp, err := c.Spawn(SpawnArgs{ID: "c", Kind: KindRunner, Argv: runnerArgv("burstexit", "512", "5"), Cols: 100, Rows: 30, Ring: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	a, err := c.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	se := newStream(a)
	se.until(t, 30*time.Second, nil)
	end, exit := se.next, se.exit
	if exit == nil || exit.ExitCode != 5 {
		t.Fatalf("exit %+v", exit)
	}
	info := infoOf(t, c, sp.RunID)
	if end != info.OutOffset {
		t.Fatalf("the exit was delivered at offset %d but the ring holds %d: bytes came after it", end, info.OutOffset)
	}
	if info.OutOffset < 512*1024 {
		t.Fatalf("only %d bytes of output", info.OutOffset)
	}
	// and nothing else follows it
	select {
	case ev, ok := <-a.Events:
		if ok {
			t.Fatalf("event after the exit: %+v", ev.Ev)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSlowReaderIsClosedAndTheDrainCarriesOn(t *testing.T) {
	_, addr := testHost(t, Options{ClientQueue: 64 << 10})
	c := daemonT(t, addr)
	sp := spawnT(t, c, "c", KindRunner, 16<<20, "burst", "3072")
	// Attach and never read: the raw connection is used by hand, since Client always reads.
	raw, err := dialChannel(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	// c is the daemon, so this raw one must take over to attach, then it never reads again.
	pc := dialT(t, addr)
	_ = pc
	enc := json.NewEncoder(raw)
	_ = enc.Encode(Request{V: VHello, Seq: 1, Proto: Proto, Takeover: true})
	buf := make([]byte, 4096)
	_, _ = raw.Read(buf) // the hello reply
	_ = enc.Encode(Request{V: VAttach, Seq: 2, RunID: sp.RunID})
	// The probe connection stays answered while the runner floods and the slow one backs up.
	waitFor(t, 30*time.Second, "the slow client to be closed", func() bool {
		p, err := pc.Probe()
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		return !p.Daemon
	})
	// The drain never slowed: the whole burst lands in the ring.
	d := daemonT(t, addr)
	waitFor(t, 60*time.Second, "the whole burst to be drained", func() bool {
		return infoOf(t, d, sp.RunID).OutOffset >= 3072*1024
	})
	a, err := d.Attach(sp.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Replay) < 3072*1024 || a.Truncated {
		t.Fatalf("replay %d bytes truncated=%v", len(a.Replay), a.Truncated)
	}
}

func TestIdleExit(t *testing.T) {
	t.Run("empty host with no daemon exits", func(t *testing.T) {
		h, _ := testHost(t, Options{IdleExit: 300 * time.Millisecond})
		select {
		case <-h.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("an idle host did not exit")
		}
	})
	t.Run("a connected daemon holds it", func(t *testing.T) {
		h, addr := testHost(t, Options{IdleExit: 300 * time.Millisecond})
		daemonT(t, addr)
		select {
		case <-h.Done():
			t.Fatal("exited with a daemon connected")
		case <-time.After(900 * time.Millisecond):
		}
	})
	t.Run("never with a runner alive, then once it is collected", func(t *testing.T) {
		h, addr := testHost(t, Options{IdleExit: 300 * time.Millisecond})
		c := daemonT(t, addr)
		sp := spawnT(t, c, "c", KindRunner, 0, "echo")
		c.Close()
		select {
		case <-h.Done():
			t.Fatal("exited with a runner alive")
		case <-time.After(1200 * time.Millisecond):
		}
		d := daemonT(t, addr)
		_ = d.Write(sp.RunID, []byte("quit\r"))
		waitFor(t, 10*time.Second, "exit", func() bool { return infoOf(t, d, sp.RunID).Exited })
		d.Close()
		select {
		case <-h.Done():
			t.Fatal("exited holding an exit nobody collected")
		case <-time.After(900 * time.Millisecond):
		}
		e := daemonT(t, addr)
		if err := e.Collect(sp.RunID); err != nil {
			t.Fatal(err)
		}
		e.Close()
		select {
		case <-h.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("did not exit after the last pty was collected and the daemon left")
		}
	})
}

// G7: closing a connection and the host from two goroutines at once. Meaningful without -race (no crash, no
// double close, no hang) and it still wants a -race run somewhere with a C compiler.
func TestConcurrentCloseOfConnectionAndHost(t *testing.T) {
	for i := 0; i < 15; i++ {
		h, addr := testHost(t, Options{})
		c := daemonT(t, addr)
		sp := spawnT(t, c, "c", KindRunner, 0, "tick")
		if _, err := c.Attach(sp.RunID, 0); err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		var cs []*client
		for k := range h.clients {
			cs = append(cs, k)
		}
		h.mu.Unlock()
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, k := range cs {
			k := k
			for n := 0; n < 2; n++ {
				wg.Add(1)
				go func() { defer wg.Done(); <-start; k.close("test") }()
			}
		}
		for n := 0; n < 2; n++ {
			wg.Add(2)
			go func() { defer wg.Done(); <-start; h.Close() }()
			go func() { defer wg.Done(); <-start; c.Close() }()
		}
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("concurrent close hung")
		}
	}
}

func TestCloseOnceRunsOnce(t *testing.T) {
	n := 0
	c := newCloser(func() error { n++; return nil })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Close() }()
	}
	wg.Wait()
	if n != 1 {
		t.Fatalf("ran %d times", n)
	}
}

func TestRunIDsAreUniqueULIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id := newRunID()
		if len(id) != 26 || seen[id] || strings.Trim(id, crockford) != "" {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
	}
}

func TestSpawnRefusals(t *testing.T) {
	_, addr := testHost(t, Options{})
	c := daemonT(t, addr)
	if _, err := c.Spawn(SpawnArgs{ID: "c", Kind: "wizard", Argv: []string{"x"}}); err == nil {
		t.Fatal("a bad kind was accepted")
	}
	if _, err := c.Spawn(SpawnArgs{ID: "c", Kind: KindRunner}); err == nil {
		t.Fatal("no argv was accepted")
	}
	if _, err := c.Spawn(SpawnArgs{ID: "c", Kind: KindRunner, Argv: []string{"definitely-not-a-program-atrium"}}); err == nil {
		t.Fatal("a missing program was accepted")
	}
	// not a daemon, not allowed
	pc := dialT(t, addr)
	if _, err := pc.List(); err == nil {
		t.Fatal("a connection that never said hello could list")
	}
}
