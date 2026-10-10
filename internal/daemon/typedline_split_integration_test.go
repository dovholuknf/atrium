//go:build integration

package daemon

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

// state is everything of the model a split could change.
func lineState(l *typedLine) string {
	return string(l.text) + "|" + strconv.Itoa(l.count()) + "|" + l.unsure + "|" + boolS(l.inPaste) + "|" + l.submitted
}

func boolS(b bool) string {
	if b {
		return "T"
	}
	return "F"
}

// frozen is a line whose clock never moves, so the watchdog and the Esc timing
// cannot differ between a whole run and a split one.
func frozenLine() *typedLine {
	t0 := time.Unix(1000, 0)
	return &typedLine{clock: func() time.Time { return t0 }, escClears: true}
}

var splitStreams = map[string]string{
	"typing":                "hello world",
	"typing and enter":      "hello\r",
	"edits":                 "helo\x7f\x7flo wor\x17ld\x7f",
	"unicode":               "héllo wörld ✓ 日本語",
	"arrows":                "ab\x1b[D\x1b[Cc\x1b[A",
	"ss3 and delete":        "ab\x1bOD\x1b[3~c",
	"alt keys":              "ab\x1bb\x1bf\x1b.c",
	"shift enter":           "ab\x1b\rcd",
	"focus reports":         "ab\x1b[I\x1b[Ocd\x1b[I",
	"click":                 "\x1b[I\x1b[<0;3;4M\x1b[<0;3;4mab",
	"x10 mouse":             "ab\x1b[M !!cd",
	"replies":               "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\x\x1b]10;rgb:cc/cc/cc\x07y\x1bP1$r0m\x1b\\z\x1b[24;80R",
	"control keys":          "abc\x03def\x15gh\x01\x05i\x12",
	"ctrl-c after typing":   "abc\x03",
	"ctrl-u after typing":   "abc\x15",
	"paste":                 "\x1b[200~line one\rline two\x1b[201~",
	"paste then enter":      "\x1b[200~line one\rline two\x1b[201~\r",
	"paste in typing":       "ab\x1b[200~cd\nef\x1b[201~gh",
	"paste unicode":         "\x1b[200~héllo ✓\x1b[201~x",
	"2228 paste then enter": "\x1b[200~" + strings.Repeat("x", 2223) + "\x1b[201~\r",
	"2228 paste":            "\x1b[200~" + strings.Repeat("0123456789", 222) + "abcdef\x1b[201~",
	"focus mid paste":       "\x1b[200~ab\x1b[201~\x1b[I\x1b[Ocd",
	"two pastes":            "\x1b[200~a\x1b[201~\x1b[200~b\x1b[201~\r",
	"lone esc then text":    "ab\x1b",
	"esc esc":               "ab\x1b\x1b",
	"function keys":         "a\x1b[15~\x1b[1;5Pb",
}

func runFrames(frames []string) string {
	l := frozenLine()
	for _, f := range frames {
		l.feed([]byte(f))
	}
	l.settle()
	return lineState(l)
}

// THE PARSER NEVER ASSUMES A FRAME BOUNDARY. A stream cut at any byte, or into
// frames of 1 to 7 bytes, must leave the model exactly where the whole stream
// does. This is the test that would have caught the 2228 phantom.
func TestTypedLineSameStateWhereverTheFramesSplit(t *testing.T) {
	for name, s := range splitStreams {
		want := runFrames([]string{s})
		for i := 1; i < len(s); i++ {
			if got := runFrames([]string{s[:i], s[i:]}); got != want {
				t.Errorf("%s split at %d: got %q, want %q", name, i, got, want)
			}
		}
		rng := rand.New(rand.NewSource(int64(len(s))))
		for round := 0; round < 50; round++ {
			var frames []string
			for rest := s; rest != ""; {
				n := min(1+rng.Intn(7), len(rest))
				frames = append(frames, rest[:n])
				rest = rest[n:]
			}
			if got := runFrames(frames); got != want {
				t.Errorf("%s in frames %q: got %q, want %q", name, frames, got, want)
				break
			}
		}
	}
}

// The five lines of the plan's probe.
func TestTypedLinePasteProbe(t *testing.T) {
	body := strings.Repeat("x", 2228)
	cases := []struct {
		name      string
		frames    []string
		count     int
		inPaste   bool
		wantEmpty bool
	}{
		{"whole paste", []string{"\x1b[200~" + body + "\x1b[201~"}, 2228, false, false},
		{"whole paste then enter", []string{"\x1b[200~" + body + "\x1b[201~", "\r"}, 0, false, true},
		{"split end marker", []string{"\x1b[200~" + body + "\x1b[20", "1~"}, 2228, false, false},
		{"split end marker, enter after", []string{"\x1b[200~" + body + "\x1b[", "201~", "\r"}, 0, false, true},
		{"lost end marker", []string{"\x1b[200~" + body}, 2228, true, false},
		{"lost end marker, enter after", []string{"\x1b[200~" + body, "\r"}, 2229, true, false},
		{"lost end marker, ctrl-c after", []string{"\x1b[200~" + body, "\x03"}, 0, false, true},
		{"lost end marker, ctrl-u after", []string{"\x1b[200~" + body, "\x15"}, 0, false, true},
	}
	for _, c := range cases {
		l := frozenLine()
		for _, f := range c.frames {
			l.feed([]byte(f))
		}
		if l.count() != c.count || l.inPaste != c.inPaste || l.empty() != c.wantEmpty {
			t.Errorf("%s: count=%d inPaste=%v empty=%v, want %d %v %v", c.name, l.count(), l.inPaste, l.empty(),
				c.count, c.inPaste, c.wantEmpty)
		}
	}
}

// A paste with no byte for two seconds is over, so the Enter after it submits.
func TestTypedLinePasteWatchdog(t *testing.T) {
	now := time.Unix(1000, 0)
	l := &typedLine{clock: func() time.Time { return now }}
	l.feed([]byte("\x1b[200~pasted text"))
	if !l.inPaste {
		t.Fatal("the paste should be open")
	}
	now = now.Add(pasteQuiet - time.Millisecond)
	l.feed([]byte("\r"))
	if !l.inPaste || l.count() != 12 {
		t.Fatalf("inside the window Enter is text: inPaste=%v count=%d", l.inPaste, l.count())
	}
	now = now.Add(pasteQuiet)
	l.feed([]byte("\r"))
	if l.inPaste || !l.empty() {
		t.Fatalf("after the watchdog Enter submits: inPaste=%v count=%d", l.inPaste, l.count())
	}
	if l.submitted == "" {
		t.Error("the submit was not seen")
	}
}

// A real alt-[ is not held for ever.
func TestTypedLineSettlesAHeldSequence(t *testing.T) {
	now := time.Unix(1000, 0)
	l := &typedLine{clock: func() time.Time { return now }}
	l.feed([]byte("ab\x1b["))
	if l.unsure != "" {
		t.Fatal("held, not yet read")
	}
	now = now.Add(pendingStale)
	l.settle()
	if l.unsure == "" {
		t.Error("alt-[ should have been read once the tail did not come")
	}
}

func TestTypingReadoutSaysAStuckPaste(t *testing.T) {
	r := &runner{taskID: "t"}
	r.noteOperatorTyped([]byte("\x1b[200~" + strings.Repeat("x", 50)))
	s := r.typing()
	if !s.InPaste || s.Open || strings.Contains(s.Reason, "paste has been open") {
		t.Fatalf("fresh paste: %+v", s)
	}
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-2 * pasteWedged)
	r.typeMu.Unlock()
	s = r.typing()
	if !s.InPaste || !strings.Contains(s.Reason, "paste has been open") {
		t.Fatalf("stuck paste: %+v", s)
	}
	if !r.line.wedgeLogged {
		t.Error("the occurrence was not logged")
	}
}

func resetRig(t *testing.T) (*Daemon, *runner, string) {
	d := testDaemon(t)
	task := peerCard(t, d, "typist")
	r := &runner{taskID: task.ID, started: time.Now()}
	d.sup.mu.Lock()
	d.sup.runners[task.ID] = r
	d.sup.mu.Unlock()
	return d, r, task.ID
}

// A submit emptied the prompt, so a model that still holds text (an Enter read as
// paste text) is cleared by the prompt hook.
func TestPromptHookClearsTheTypingModel(t *testing.T) {
	d, r, id := resetRig(t)
	r.noteOperatorTyped([]byte("\x1b[200~" + strings.Repeat("x", 2228)))
	r.noteOperatorTyped([]byte("\r"))
	if r.typing().Count == 0 {
		t.Fatal("rig: the model should be wedged")
	}
	d.onActivity(ActivityEvent{TaskID: id, Event: "prompt"})
	if s := r.typing(); s.Count != 0 || s.InPaste {
		t.Fatalf("model not reset: %+v", s)
	}
}

// A key after the prompt began is a draft, and stays.
func TestPromptHookKeepsADraftTypedAfterIt(t *testing.T) {
	d, r, id := resetRig(t)
	r.noteOperatorTyped([]byte("old\r"))
	r.noteOperatorTyped([]byte("new draft"))
	d.onActivity(ActivityEvent{TaskID: id, Event: "prompt"})
	if s := r.typing(); s.Count != 9 {
		t.Fatalf("draft lost: %+v", s)
	}
	// And a Stop is no reset.
	d.onActivity(ActivityEvent{TaskID: id, Event: "idle"})
	if s := r.typing(); s.Count != 9 {
		t.Fatalf("a Stop cleared the draft: %+v", s)
	}
}
