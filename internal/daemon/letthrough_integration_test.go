//go:build integration

package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// quietSince backdates the last keystroke, so the test does not sleep out letThroughQuiet.
func quietSince(r *runner) {
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-time.Minute)
	r.typeMu.Unlock()
}

func holdTwo(t *testing.T, d *Daemon, id string) {
	t.Helper()
	for _, text := range []string{"first words", "second words"} {
		m, err := d.st.QueueFromPeer(id, text, "sg4/doer")
		if err != nil {
			t.Fatal(err)
		}
		d.deferPeerInjection(id, m.ID, "sg4/doer", text, "", false)
	}
	t.Cleanup(func() { d.pending.stopAll() })
}

// The four steps, in order: clear the line, deliver every waiting message, type the draft back with the board's
// newline key and no Enter.
func TestLetItThroughClearsDeliversAndRestores(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("fix the\x1b\rthing"))
	quietSince(r)
	holdTwo(t, d, target.ID)

	n, err := d.letThrough(target.ID, "fix the\nthing")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("delivered %d, want both", n)
	}
	got := f.written()
	clear := strings.Index(got, "\x03")
	one, two := strings.Index(got, "first words"), strings.Index(got, "second words")
	draft := strings.Index(got, "fix the\x1b\rthing")
	if clear != 0 || one < clear || two < one || draft < two {
		t.Fatalf("steps out of order: %q", got)
	}
	if strings.Count(got, "\r") != 2+1 { // two submits and the one inside the draft's ESC CR
		t.Fatalf("an Enter was pressed after the draft, or a message was not submitted: %q", got)
	}
	if strings.HasSuffix(got, "\r") {
		t.Fatalf("the draft was submitted: %q", got)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the messages are still held")
	}
	r.typeMu.Lock()
	line := string(r.line.text)
	r.typeMu.Unlock()
	if line != "fix the\nthing" {
		t.Fatalf("the typing model lost the draft: %q", line)
	}
}

// A line atrium cannot follow is never retyped.
func TestLetItThroughRefusesAGuess(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("abc"))
	r.noteOperatorTyped([]byte("\x1b[D")) // a cursor move: unsure
	quietSince(r)
	holdTwo(t, d, target.ID)

	_, err := d.letThrough(target.ID, "abc")
	var le *errLetThrough
	if !errors.As(err, &le) {
		t.Fatalf("want a refusal, got %v", err)
	}
	if f.written() != "" {
		t.Fatalf("wrote to a line atrium cannot read: %q", f.written())
	}
	if heldCount(d.pending, target.ID) != 2 {
		t.Fatal("the messages were let go anyway")
	}
}

// A line that is not the one the board copied is refused.
func TestLetItThroughRefusesAChangedLine(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("abcd"))
	quietSince(r)
	holdTwo(t, d, target.ID)

	if _, err := d.letThrough(target.ID, "abc"); err == nil {
		t.Fatal("a stale line was accepted")
	}
	if f.written() != "" {
		t.Fatalf("wrote anyway: %q", f.written())
	}
}

// A key pressed during the sequence waits on the input lock, so its bytes land after the draft, and the model is the
// draft followed by the key.
func TestLetItThroughHoldsKeysPressedMeanwhile(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("draft"))
	quietSince(r)
	holdTwo(t, d, target.ID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(50 * time.Millisecond) // wait: a key inside the sequence's pauses, no event marks it
		r.noteOperatorTyped([]byte("Z"))
		_ = r.writeOperatorInput([]byte("Z"))
	}()
	if _, err := d.letThrough(target.ID, "draft"); err != nil {
		t.Fatal(err)
	}
	<-done
	got := f.written()
	if !strings.HasSuffix(got, "draftZ") {
		t.Fatalf("the key was not held until after the draft: %q", got)
	}
	r.typeMu.Lock()
	line := string(r.line.text)
	r.typeMu.Unlock()
	if line != "draftZ" {
		t.Fatalf("model %q", line)
	}
}
