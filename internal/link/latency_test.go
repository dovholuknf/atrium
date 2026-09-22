package link

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// KEYSTROKE ECHO THROUGH THE HUB, timed.
//
// The always-on test here pins the link itself: a small websocket frame through
// the hub proxy and a real mutual-TLS link must come back in about the time it
// takes going straight to the room. A buffered writer, a flush on a timer, or a
// socket that lost TCP_NODELAY would turn that into tens of milliseconds, and
// the median catches it without being flaky on a busy machine.
//
// The two probes below it are skipped unless asked for. They are how the
// input-lag investigation was measured, kept so the next one starts from
// numbers. See docs/input-lag-logging.md.

// echoWS answers every websocket message with itself.
func echoWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	for {
		typ, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		if err := c.Write(r.Context(), typ, b); err != nil {
			return
		}
	}
}

// tlsPair is `pair` over the direct transport, certificates and all, so the
// bytes cross the same tls.Conn the live link uses.
func tlsPair(t *testing.T, handler http.Handler) (*httptest.Server, func()) {
	t.Helper()
	hk := hubKeys(t)
	ln, err := Direct{Addr: "127.0.0.1:0", Keys: hk}.Listen()
	if err != nil {
		t.Fatal(err)
	}
	rk := Keys{Dir: t.TempDir()}
	key, csr, err := NewCSR("testroom")
	if err != nil {
		t.Fatal(err)
	}
	der, err := hk.sign(csr, "testroom")
	if err != nil {
		t.Fatal(err)
	}
	ca, _, err := hk.CA()
	if err != nil {
		t.Fatal(err)
	}
	if err := rk.SaveRoom(key, der, ca.Raw, ln.Addr().String(), "testroom"); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 2})
	hub.Authenticated = DirectAuthenticated
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	room := &Room{
		Name:    "testroom",
		Dial:    Direct{Addr: ln.Addr().String(), Keys: rk},
		Handler: handler,
		T:       Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, 5*time.Second, func() bool { return hub.Has("testroom") })
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, func() {
		front.Close()
		stop()
		ln.Close()
	}
}

// echoTimes sends n one-byte frames, each after the last one's echo and a gap,
// and returns every round trip.
func echoTimes(t *testing.T, base string, n int, gap time.Duration) []time.Duration {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/v1/tasks/x/attach", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var out []time.Duration
	for i := 0; i < n; i++ {
		t0 := time.Now()
		if err := c.Write(ctx, websocket.MessageBinary, []byte("k")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Read(ctx); err != nil {
			t.Fatal(err)
		}
		out = append(out, time.Since(t0))
		time.Sleep(gap)
	}
	return out
}

func quantile(d []time.Duration, p float64) time.Duration {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(p*float64(len(s)-1))]
}

func summarize(d []time.Duration) string {
	return fmt.Sprintf("p50 %v p95 %v max %v", quantile(d, .5), quantile(d, .95), quantile(d, 1))
}

// A keystroke through the hub and a TLS link comes straight back. Measured at
// about half a millisecond on loopback, so 5ms at the median is a wide margin
// that still fails on any buffering or Nagle delay, which cost 40ms or more.
func TestAKeystrokeCrossesTheLinkWithoutDelay(t *testing.T) {
	front, done := tlsPair(t, http.HandlerFunc(echoWS))
	defer done()
	for _, gap := range []time.Duration{0, 5 * time.Millisecond, 50 * time.Millisecond} {
		d := echoTimes(t, front.URL, 60, gap)
		if p50 := quantile(d, .5); p50 > 5*time.Millisecond {
			t.Errorf("gap %v: echo through the hub took %s, want the median under 5ms", gap, summarize(d))
		}
	}
}

// ── probes, skipped unless asked for ─────────────────────

// typist holds one real terminal attach and times a keystroke to its first
// output frame.
type typist struct {
	c      *websocket.Conn
	frames chan time.Time
	n      int
}

func newTypist(t *testing.T, ctx context.Context, base, task string) *typist {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/v1/tasks/"+task+"/attach", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(64 << 20)
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"resize","cols":120,"rows":30}`))
	ty := &typist{c: c, frames: make(chan time.Time, 1024)}
	go func() {
		for {
			typ, _, err := c.Read(ctx)
			if err != nil {
				close(ty.frames)
				return
			}
			if typ == websocket.MessageBinary {
				ty.frames <- time.Now()
			}
		}
	}()
	ty.drain(500 * time.Millisecond)
	return ty
}

func (ty *typist) drain(quiet time.Duration) {
	for {
		select {
		case <-ty.frames:
		case <-time.After(quiet):
			return
		}
	}
}

// key types a character and then rubs it out on the next call, so the line
// never grows.
func (ty *typist) key(t *testing.T, ctx context.Context) time.Duration {
	t.Helper()
	keys := []string{"x", `\b`}
	t0 := time.Now()
	if err := ty.c.Write(ctx, websocket.MessageText, []byte(`{"t":"in","d":"`+keys[ty.n%2]+`"}`)); err != nil {
		t.Fatal(err)
	}
	ty.n++
	select {
	case at := <-ty.frames:
		return at.Sub(t0)
	case <-time.After(5 * time.Second):
		t.Fatal("a keystroke got no echo")
	}
	return 0
}

func over(d []time.Duration, limit time.Duration) int {
	n := 0
	for _, x := range d {
		if x >= limit {
			n++
		}
	}
	return n
}

// TWO RUNNING PAIRS, KEYS ALTERNATED BETWEEN THEM, so both see the same load
// at the same moment. One pair after the other measures the machine changing
// between runs more than the change being tested.
//
//	PROBE_A=http://hub-a|task-id  PROBE_B=http://hub-b|task-id  PROBE_KEYS=1500
func TestProbeTwoPairsSideBySide(t *testing.T) {
	a, b := os.Getenv("PROBE_A"), os.Getenv("PROBE_B")
	if a == "" || b == "" {
		t.Skip("set PROBE_A and PROBE_B to compare two running hub and room pairs")
	}
	n := 1500
	fmt.Sscan(os.Getenv("PROBE_KEYS"), &n)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pa, pb := strings.SplitN(a, "|", 2), strings.SplitN(b, "|", 2)
	ta, tb := newTypist(t, ctx, pa[0], pa[1]), newTypist(t, ctx, pb[0], pb[1])
	var da, db []time.Duration
	for i := 0; i < n; i++ {
		first, second, d1, d2 := ta, tb, &da, &db
		if i%2 == 1 {
			first, second, d1, d2 = tb, ta, &db, &da
		}
		*d1 = append(*d1, first.key(t, ctx))
		first.drain(20 * time.Millisecond)
		*d2 = append(*d2, second.key(t, ctx))
		second.drain(20 * time.Millisecond)
	}
	for _, r := range []struct {
		name string
		d    []time.Duration
	}{{"A", da}, {"B", db}} {
		t.Logf("%s: %s, keys>=20ms %d, >=50ms %d of %d", r.name, summarize(r.d),
			over(r.d, 20*time.Millisecond), over(r.d, 50*time.Millisecond), len(r.d))
	}
}

// A PROCESS THAT ONLY SLEEPS, to tell a slow program from a machine that is
// not scheduling it. Run it at two priorities at once and compare.
//
//	HICCUP=120   seconds to run, logging every 1ms sleep that overslept 15ms+
func TestProbeSchedulingHiccups(t *testing.T) {
	secs := os.Getenv("HICCUP")
	if secs == "" {
		t.Skip("set HICCUP to a number of seconds")
	}
	d, err := time.ParseDuration(secs + "s")
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(d); time.Now().Before(end); {
		t0 := time.Now()
		time.Sleep(time.Millisecond)
		if late := time.Since(t0) - time.Millisecond; late >= 15*time.Millisecond {
			t.Logf("hiccup %s %v", time.Now().Format("15:04:05.000"), late)
		}
	}
}
