//go:build integration

package link

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// throttled carries rate bytes a second each way, as a slow uplink does.
type throttled struct {
	net.Conn
	rate int
}

func (c *throttled) pace(n int) {
	time.Sleep(time.Duration(n) * time.Second / time.Duration(c.rate))
}

func (c *throttled) Read(b []byte) (int, error) {
	if len(b) > 16<<10 {
		b = b[:16<<10]
	}
	n, err := c.Conn.Read(b)
	c.pace(n)
	return n, err
}

func (c *throttled) Write(b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		chunk := b
		if len(chunk) > 16<<10 {
			chunk = chunk[:16<<10]
		}
		n, err := c.Conn.Write(chunk)
		total += n
		c.pace(n)
		if err != nil {
			return total, err
		}
		b = b[n:]
	}
	return total, nil
}

// laneCount times every echoed beat. shared puts control on the data lane, as a room with one session would.
type laneCount struct {
	Direct
	shared  bool
	mu      sync.Mutex
	control int
	worst   time.Duration
}

func (d *laneCount) Dial(ctx context.Context) (net.Conn, error) {
	control := onControlLane(ctx)
	if d.shared {
		ctx = context.WithValue(ctx, laneKey{}, false)
	}
	c, err := d.Direct.Dial(ctx)
	if err != nil || !control {
		return c, err
	}
	d.mu.Lock()
	d.control++
	d.mu.Unlock()
	return &beatClock{Conn: c, d: d}, nil
}

var echoed = regexp.MustCompile(`"beat":(\d+)`)

// beatClock takes each echoed beat's round trip from the send time it carries.
type beatClock struct {
	net.Conn
	d    *laneCount
	tail []byte
}

func (c *beatClock) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.tail = append(c.tail, b[:n]...)
	i := bytes.LastIndexByte(c.tail, '\n')
	if i < 0 {
		return n, err
	}
	now := time.Now().UnixMilli()
	for _, m := range echoed.FindAllSubmatch(c.tail[:i], -1) {
		sent, _ := strconv.ParseInt(string(m[1]), 10, 64)
		rt := time.Duration(now-sent) * time.Millisecond
		c.d.mu.Lock()
		if rt > c.d.worst {
			c.d.worst = rt
		}
		c.d.mu.Unlock()
	}
	c.tail = append(c.tail[:0], c.tail[i+1:]...)
	return n, err
}

// bulkRoundTrip runs bulk downloads through a room whose link carries rate bytes a second. It says how long the
// slowest beat took to come back while they ran, and whether the hub dropped the room meanwhile.
func bulkRoundTrip(t *testing.T, shared bool) (time.Duration, bool) {
	const (
		rate    = 256 << 10
		streams = 4
		size    = 256 << 10
	)
	body := strings.Repeat("x", size)
	var dialer *laneCount
	r := newMuxRigAt(t, false, func(d Direct) Dialer { dialer = &laneCount{Direct: d, shared: shared}; return dialer },
		rate, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
	silence := r.hub.T.Silence
	since := r.hub.Rooms()[0].Since

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(r.front.URL + "/v1/big")
			if err != nil {
				if !shared {
					t.Error(err)
				}
				return
			}
			n, _ := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if n != size && !shared {
				t.Errorf("a bulk transfer carried %d bytes, want %d", n, size)
			}
		}()
	}
	wg.Wait()
	if took := time.Since(start); took < 2*silence {
		t.Fatalf("the bulk transfer took %s, too short to saturate the link for two Silences", took)
	}
	rooms := r.hub.Rooms()
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return dialer.worst, len(rooms) != 1 || !rooms[0].Since.Equal(since) || dialer.control != 1
}

func TestABeatArrivesWithinSilenceWhileBulkSaturatesTheLink(t *testing.T) {
	lanes, dropped := bulkRoundTrip(t, false)
	if dropped {
		t.Fatalf("the hub dropped the room while bulk ran on the data lane")
	}
	if lanes >= time.Second/2 {
		t.Fatalf("a beat took %s to come back while bulk ran on the data lane", lanes)
	}
	// The same load with control on the data lane has to hurt, or this test proves nothing.
	shared, dropped := bulkRoundTrip(t, true)
	t.Logf("slowest beat round trip: %s on two lanes, %s on one (dropped %v)", lanes, shared, dropped)
	if !dropped && shared < time.Second {
		t.Fatalf("with one lane a beat still came back in %s and the room stayed, so the link was never saturated", shared)
	}
}
