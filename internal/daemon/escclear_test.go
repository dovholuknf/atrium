package daemon

import (
	"strings"
	"testing"
	"time"
)

// quiet puts the last keystroke past the gate's idle window, so only the line
// decides.
func quiet(r *runner) {
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-2 * peerGateIdle)
	r.typeMu.Unlock()
}

// Esc Esc clears a Claude prompt, so it empties the line and a held message
// goes in on the next retry. It used to stay held until a control-c.
func TestEscEscClearsTheLineAndReleasesAHeldMessage(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.line.escClears = true
	r.noteOperatorTyped([]byte("half a command"))

	m, err := d.st.QueueFromPeer(target.ID, "the build is green", "ci")
	if err != nil {
		t.Fatal(err)
	}
	d.deferPeerInjection(target.ID, m.ID, "ci", "the build is green", false)

	r.noteOperatorTyped([]byte("\x1b"))
	r.noteOperatorTyped([]byte("\x1b"))
	quiet(r)
	if !r.peerGateOpen() {
		t.Fatalf("Esc Esc left the line counted as %d characters", r.line.count())
	}
	d.pending.attempt(target.ID)
	if !strings.Contains(f.written(), "the build is green") {
		t.Fatalf("the held message was not typed once the line was clear: %q", f.written())
	}
	if n := heldCount(d.pending, target.ID); n != 0 {
		t.Fatalf("%d still held after the line cleared", n)
	}
}

// Both in one write is the same two keys.
func TestEscEscInOneWriteClearsTheLine(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)
	r.line.escClears = true
	r.noteOperatorTyped([]byte("half a command"))
	r.noteOperatorTyped([]byte("\x1b\x1b"))
	if !r.line.empty() {
		t.Fatalf("Esc Esc in one write left %d characters", r.line.count())
	}
}

// Not everything that starts with Esc is Esc, and not every runner clears on
// it. One Esc, an arrow key, an Esc with a key between, and Esc Esc in a shell
// all leave the line as it was.
func TestOnlyEscEscOnAClaudePromptClearsTheLine(t *testing.T) {
	d := testDaemon(t)
	_, r, _ := peerPair(t, d)
	for _, c := range []struct {
		name   string
		clears bool
		keys   []string
	}{
		{"one Esc", true, []string{"\x1b"}},
		{"an arrow and an Esc", true, []string{"\x1b[A", "\x1b"}},
		{"a key between", true, []string{"\x1b", "x", "\x1b"}},
		{"a shell", false, []string{"\x1b", "\x1b"}},
	} {
		r.typeMu.Lock()
		r.line.clear()
		r.line.escAt = time.Time{}
		r.typeMu.Unlock()
		r.line.escClears = c.clears
		r.noteOperatorTyped([]byte("abc"))
		for _, k := range c.keys {
			r.noteOperatorTyped([]byte(k))
		}
		if r.line.empty() {
			t.Errorf("%s: the line was read as cleared", c.name)
		}
	}
}
