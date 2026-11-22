//go:build integration

package daemon

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// noticeWatch reads the room's event stream from before the test acts, and counts the notices that arrive by a marker.
type noticeWatch struct {
	t     *testing.T
	d     *Daemon
	kinds chan string
	datas chan string
}

func watchNotices(t *testing.T, d *Daemon, cardID string) *noticeWatch {
	t.Helper()
	ts := httptest.NewServer(d.ap.Handler())
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close(); ts.Close() })
	w := &noticeWatch{t: t, d: d, kinds: make(chan string, 256), datas: make(chan string, 256)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		kind := ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "event: ") {
				kind = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				w.kinds <- kind
				w.datas <- strings.TrimPrefix(line, "data: ")
			}
		}
		close(w.kinds)
	}()
	until(t, "the event stream to subscribe", func() bool {
		d.Publish(cardID)
		select {
		case k := <-w.kinds:
			<-w.datas
			return k == "task"
		case <-time.After(20 * time.Millisecond):
			return false
		}
	})
	return w
}

// notices is every notice up to now, read by broadcasting a marker and stopping at it.
func (w *noticeWatch) notices() []string {
	w.t.Helper()
	w.d.ap.Broadcast("test-marker", map[string]any{})
	var out []string
	for {
		select {
		case k, ok := <-w.kinds:
			if !ok {
				w.t.Fatal("the event stream ended")
			}
			data := <-w.datas
			switch k {
			case "test-marker":
				return out
			case "notice":
				out = append(out, data)
			}
		case <-time.After(5 * time.Second):
			w.t.Fatal("no marker on the event stream")
		}
	}
}

// clearedLineEvents is the card's cleared-line events.
func clearedLineEvents(t *testing.T, d *Daemon, id string) []map[string]any {
	t.Helper()
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		p := map[string]any{}
		if err := json.Unmarshal(e.Payload, &p); err == nil && p["by"] == lineClearedBy {
			out = append(out, p)
		}
	}
	return out
}

// The fake clock is the last key: put back past staleLineAfter, the line is as old as the cycle needs.
func typedAgo(r *runner, text string, ago time.Duration) {
	r.noteOperatorTyped([]byte(text))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-ago)
	r.typeMu.Unlock()
}

func TestANewContextClearsALineLeftForFiveMinutesKeepsItAndSaysSo(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	run := d.sup.get(id)
	typedAgo(run, "x", staleLineAfter+time.Second)
	w := watchNotices(t, d, id)

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the hold once the line is cleared", func() bool { return d.Nctx.TypingHeld(id) })
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })

	got := f.written()
	if c := strings.Index(got, "\x03"); c < 0 || c > strings.Index(got, limitPrompt) {
		t.Fatalf("the line was not cleared before the limit prompt: %q", got)
	}
	if s := run.typing(); s.Count != 0 {
		t.Fatalf("the model still holds %q", s.Line)
	}
	evs := clearedLineEvents(t, d, id)
	if len(evs) != 1 || evs[0]["line"] != "x" {
		t.Fatalf("cleared-line events: %v", evs)
	}
	file, _ := evs[0]["file"].(string)
	if b, err := os.ReadFile(file); err != nil || string(b) != "x" {
		t.Fatalf("the kept file %q holds %q: %v", file, b, err)
	}
	ns := w.notices()
	want := "You had 'x' on the line and did not send it within 5 minutes, so I cleared it."
	if len(ns) != 1 || !strings.Contains(ns[0], want) {
		t.Fatalf("notices %v, want one saying %q", ns, want)
	}
}

func TestANewContextNeverClearsALineTypedAMinuteAgo(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	run := d.sup.get(id)
	typedAgo(run, "still writing this", time.Minute)

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the cycle's claim", func() bool { return d.Nctx.Get(id) != nil })
	time.Sleep(100 * time.Millisecond) // wait: negative check, a clear would have happened by now
	if strings.Contains(f.written(), "\x03") || d.Nctx.TypingHeld(id) || len(clearedLineEvents(t, d, id)) != 0 {
		t.Fatalf("a line typed a minute ago was cleared: %q", f.written())
	}
	if s := run.typing(); s.Line != "still writing this" {
		t.Fatalf("the model lost the line: %q", s.Line)
	}
}

func TestAHeldSayClearsALineLeftForFiveMinutesAndGoesIn(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	typedAgo(r, "stray", staleLineAfter+time.Second)

	m, err := d.st.QueueFromPeer(target.ID, "the migration is ready", "room-e/doer")
	if err != nil {
		t.Fatal(err)
	}
	d.deferPeerInjection(target.ID, m.ID, "room-e/doer", "the migration is ready", "", false)
	d.pending.attempt(target.ID)

	got := f.written()
	if c := strings.Index(got, "\x03"); c < 0 || c > strings.Index(got, "the migration is ready") {
		t.Fatalf("the say did not go in after the line was cleared: %q", got)
	}
	if n := heldCount(d.pending, target.ID); n != 0 {
		t.Fatalf("%d still held", n)
	}
	if evs := clearedLineEvents(t, d, target.ID); len(evs) != 1 || evs[0]["line"] != "stray" {
		t.Fatalf("cleared-line events: %v", evs)
	}
}
