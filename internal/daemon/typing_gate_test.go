package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The count of what the operator has typed and not sent is the whole gate, so
// the keys that put a newline INTO a prompt must not read as sending it.
func TestANewlineInsideAPromptKeepsTheLineUnsent(t *testing.T) {
	cases := map[string][]byte{
		"shift-enter":        []byte("first line\x1b\r"),
		"ctrl-enter":         []byte("first line\n"),
		"cr inside a paste":  []byte("\x1b[200~one\rtwo\x1b[201~"),
		"shift-enter at end": []byte("x\x1b\r"),
	}
	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			r.noteOperatorTyped(keys)
			if r.unsent == 0 || !r.midLine {
				t.Fatalf("%q left the line counted as empty: unsent=%d", keys, r.unsent)
			}
		})
	}
}

func TestEnterControlCAndControlUStillEmptyTheLine(t *testing.T) {
	for name, end := range map[string]string{"enter": "\r", "ctrl-c": "\x03", "ctrl-u": "\x15"} {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			r.noteOperatorTyped([]byte("a line\x1b\rmore"))
			r.noteOperatorTyped([]byte(end))
			if r.unsent != 0 || r.midLine {
				t.Fatalf("%s did not empty the line: unsent=%d", name, r.unsent)
			}
		})
	}
}

// A paste followed by Enter in a later frame sends it.
func TestEnterAfterAPasteSendsIt(t *testing.T) {
	r := &runner{}
	r.noteOperatorTyped([]byte("\x1b[200~pasted\rtext\x1b[201~"))
	r.noteOperatorTyped([]byte("\r"))
	if r.unsent != 0 {
		t.Fatalf("enter after a paste did not send it: unsent=%d", r.unsent)
	}
}

// THE BUG CLINT HIT. A message with no sender, which is how a script or another
// session using the API arrives, was typed straight in over a line he was
// writing. It must be queued and the terminal left alone.
func TestAnOperatorMessageNeverTypesIntoAPartWrittenLine(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a thought\x1b\rand a second line"))

	if code := message(t, d, target.ID, map[string]string{"text": "/rename something"}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	if f.written() != "" {
		t.Fatalf("typed into a part written line: %q", f.written())
	}
	pending, err := d.st.PendingMessages(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !pending[0].FromHuman() {
		t.Fatalf("the message was not queued as the operator's: %+v", pending)
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the queued message is not held for an on-screen retry")
	}
}

// A free terminal still takes the operator's message at once, with no banner,
// so a slash command reaches the runner as one.
func TestAnOperatorMessageToAFreeTerminalIsTypedBare(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("ls\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	if code := message(t, d, target.ID, map[string]string{"text": "/rename something"}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	got := f.written()
	if !strings.Contains(got, "/rename something") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the message was not typed and sent: %q", got)
	}
	if strings.Contains(got, "says:") {
		t.Fatalf("the operator's message carries a peer banner: %q", got)
	}
}
