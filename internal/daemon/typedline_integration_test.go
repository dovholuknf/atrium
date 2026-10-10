//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// What xterm.js sends on its own account, by the exact bytes its InputHandler
// and CoreBrowserTerminal produce. None of it is the operator typing.
var terminalReports = map[string]string{
	"focus in":             "\x1b[I",
	"focus out":            "\x1b[O",
	"sgr mouse press":      "\x1b[<0;12;5M",
	"sgr mouse release":    "\x1b[<0;12;5m",
	"sgr wheel":            "\x1b[<64;30;9M",
	"x10 mouse":            "\x1b[M !!",
	"urxvt mouse":          "\x1b[32;12;5M",
	"primary DA":           "\x1b[?1;2c",
	"secondary DA":         "\x1b[>0;276;0c",
	"device status":        "\x1b[0n",
	"cursor position":      "\x1b[24;80R",
	"DEC cursor position":  "\x1b[?24;80R",
	"mode report":          "\x1b[?2004;1$y",
	"ansi mode report":     "\x1b[4;2$y",
	"window pixels":        "\x1b[4;600;800t",
	"cell pixels":          "\x1b[6;16;8t",
	"osc colour, ST":       "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\",
	"osc colour, BEL":      "\x1b]10;rgb:cccc/cccc/cccc\x07",
	"DECRQSS reply":        "\x1bP1$r0m\x1b\\",
	"DECRQSS invalid":      "\x1bP0$r\x1b\\",
	"focus in then out":    "\x1b[I\x1b[O",
	"a click, full report": "\x1b[I\x1b[<0;3;4M\x1b[<0;3;4m",
}

// THE BUG CLINT HIT. Clicking into a terminal sends a focus report, and each
// one added two characters nothing took away, so every say to that card waited
// behind an empty line. A report must count nothing, move no clock and wake no
// waiting message.
func TestTerminalReportsAreNotTyping(t *testing.T) {
	for name, seq := range terminalReports {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			woke := 0
			h := func() { woke++ }
			r.onKey.Store(&h)
			r.noteOperatorTyped([]byte(seq))
			if !r.line.empty() {
				t.Fatalf("%q put something on the line: %q, unsure %q", seq, string(r.line.text), r.line.unsure)
			}
			if !r.lastTyped.IsZero() {
				t.Fatalf("%q moved the idle clock", seq)
			}
			if woke != 0 {
				t.Fatalf("%q woke a waiting message as if the operator were back", seq)
			}
			if !r.peerGateOpen() {
				t.Fatalf("%q shut the gate", seq)
			}
		})
	}
}

// And a report in the middle of a line leaves the line as it was.
func TestAReportInsideALineLeavesItsText(t *testing.T) {
	for name, seq := range terminalReports {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			r.noteOperatorTyped([]byte("git st"))
			r.noteOperatorTyped([]byte(seq))
			r.noteOperatorTyped([]byte("atus" + seq))
			if got := string(r.line.text); got != "git status" || r.line.unsure != "" {
				t.Fatalf("line = %q, unsure %q", got, r.line.unsure)
			}
		})
	}
}

// A word delete is a word delete, by all three keys.
func TestWordDeleteTakesAWord(t *testing.T) {
	keys := map[string]string{
		"control-backspace": "\x08",
		"alt-backspace":     "\x1b\x7f",
		"control-w":         "\x17",
	}
	cases := []struct{ typed, want string }{
		{"git commit", "git "},
		{"git ", ""},
		{"git   ", ""},
		{"one", ""},
		// Stops at punctuation, which deletes less than readline's control-w.
		{"foo-bar", "foo-"},
		{"foo--", "foo"},
		{"a.b ", "a."},
		// Never across a newline.
		{"first\nsecond", "first\n"},
		{"first\n", "first"},
		{"", ""},
		{"héllo wörld", "héllo "},
	}
	for name, key := range keys {
		for _, c := range cases {
			r := &runner{}
			// A paste, so the newline is text.
			r.noteOperatorTyped([]byte("\x1b[200~" + c.typed + "\x1b[201~"))
			r.noteOperatorTyped([]byte(key))
			if got := string(r.line.text); got != c.want {
				t.Errorf("%s on %q left %q, want %q", name, c.typed, got, c.want)
			}
		}
	}
}

// A line deleted by words all the way back is empty, which is the case the old
// count got wrong: control-backspace took one character where the runner took
// a word, so the line read as part written.
func TestAWordDeletedLineOpensTheGate(t *testing.T) {
	r := &runner{}
	r.noteOperatorTyped([]byte("git commit"))
	r.noteOperatorTyped([]byte{0x08})
	r.noteOperatorTyped([]byte{0x08})
	if !r.line.empty() {
		t.Fatalf("two word deletes left %q", string(r.line.text))
	}
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	if !r.peerGateOpen() {
		t.Fatal("an emptied, quiet line kept the gate shut")
	}
}

// Characters, not bytes. A backspace after a two-byte glyph takes the glyph.
func TestTheLineCountsCharacters(t *testing.T) {
	r := &runner{}
	r.noteOperatorTyped([]byte("héllo"))
	if r.line.count() != 5 {
		t.Fatalf("count = %d, want 5", r.line.count())
	}
	r.noteOperatorTyped([]byte("✓"))
	r.noteOperatorTyped([]byte{0x7f})
	if got := string(r.line.text); got != "héllo" {
		t.Fatalf("backspace after a glyph left %q", got)
	}
}

// ERR TOWARD HOLDING. A key whose effect atrium cannot follow keeps the gate
// shut until something certainly empties the line, even if the text atrium
// kept has gone.
func TestKeysAtriumCannotFollowHoldTheGate(t *testing.T) {
	lost := map[string]string{
		"up arrow":       "\x1b[A",
		"down arrow":     "\x1b[B",
		"app up arrow":   "\x1bOA",
		"ctrl-up":        "\x1b[1;5A",
		"control-p":      "\x10",
		"control-r":      "\x12",
		"control-y":      "\x19",
		"alt-.":          "\x1b.",
		"kitty key":      "\x1b[97;5u",
		"an unknown CSI": "\x1b[5X",
		"an unknown SS3": "\x1bOp",
		"undo":           "\x1f",
		"control-n":      "\x0e",
		"alt-y":          "\x1by",
	}
	for name, key := range lost {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			r.noteOperatorTyped([]byte(key))
			if r.line.empty() {
				t.Fatalf("%q on an empty line read as still empty", key)
			}
			r.typeMu.Lock()
			r.lastTyped = time.Now().Add(-time.Hour)
			r.typeMu.Unlock()
			if r.peerGateOpen() {
				t.Fatalf("%q left the gate open", key)
			}
			st := r.typing()
			if st.Open || st.Unsure == "" {
				t.Fatalf("the readout does not say why: %+v", st)
			}
			for _, end := range []string{"\r", "\x03", "\x15"} {
				r2 := &runner{}
				r2.noteOperatorTyped([]byte(key))
				r2.noteOperatorTyped([]byte(end))
				if !r2.line.empty() {
					t.Fatalf("%q did not clear after %q", end, key)
				}
			}
		})
	}
}

// A cursor move on an empty line has nowhere to go and changes nothing. On a
// written line the next edit may not be at the end, so the line is unsure and
// backspacing it to nothing does not open the gate.
func TestACursorMoveOnlyMattersOnAWrittenLine(t *testing.T) {
	moves := map[string]string{
		"left":      "\x1b[D",
		"right":     "\x1b[C",
		"home":      "\x1b[H",
		"end":       "\x1b[F",
		"app left":  "\x1bOD",
		"ctrl-left": "\x1b[1;5D",
		"control-a": "\x01",
		"control-e": "\x05",
		"delete":    "\x1b[3~",
		"alt-b":     "\x1bb",
		"tab":       "\t",
	}
	for name, key := range moves {
		t.Run(name, func(t *testing.T) {
			r := &runner{}
			r.noteOperatorTyped([]byte(key))
			if !r.line.empty() {
				t.Fatalf("%q on an empty line made it unsure: %q", key, r.line.unsure)
			}
			r.noteOperatorTyped([]byte("ab" + key))
			r.noteOperatorTyped([]byte{0x7f, 0x7f, 0x7f})
			if r.line.empty() {
				t.Fatalf("after %q a backspaced line read as empty", key)
			}
		})
	}
}

// Keys that put nothing on the line leave it alone.
func TestKeysThatWriteNothingLeaveTheLine(t *testing.T) {
	for name, key := range map[string]string{
		"escape":    "\x1b",
		"page up":   "\x1b[5~",
		"insert":    "\x1b[2~",
		"f1":        "\x1bOP",
		"f5":        "\x1b[15~",
		"shift-tab": "\x1b[Z",
		"control-l": "\x0c",
		"control-d": "\x04",
	} {
		r := &runner{}
		r.noteOperatorTyped([]byte("ls"))
		r.noteOperatorTyped([]byte(key))
		if got := string(r.line.text); got != "ls" || r.line.unsure != "" {
			t.Errorf("%s changed the line to %q, unsure %q", name, got, r.line.unsure)
		}
		// But it is a keystroke: the operator is at the keyboard.
		if r.lastTyped.IsZero() {
			t.Errorf("%s did not move the idle clock", name)
		}
	}
}

// A long paste keeps its tail and its full count, and is not empty until every
// character is gone.
func TestALongPasteIsCountedInFull(t *testing.T) {
	r := &runner{}
	long := make([]byte, 3*typedLineCap)
	for i := range long {
		long[i] = 'x'
	}
	r.noteOperatorTyped(append(append([]byte("\x1b[200~"), long...), []byte("\x1b[201~")...))
	if r.line.count() != len(long) {
		t.Fatalf("count = %d, want %d", r.line.count(), len(long))
	}
	if len(r.line.text) > 2*typedLineCap {
		t.Fatalf("kept %d characters, over the cap", len(r.line.text))
	}
	r.noteOperatorTyped([]byte{0x17})
	if r.line.empty() {
		t.Fatal("a word delete into text too long to keep read the line as empty")
	}
}

// The readout's endpoint says what the gate says.
func TestTheTypingEndpointShowsTheLineAndTheGate(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)

	get := func() typingState {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/tasks/"+target.ID+"/typing", nil)
		req.SetPathValue("id", target.ID)
		rec := httptest.NewRecorder()
		d.handleTypingState(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("answered %d: %s", rec.Code, rec.Body)
		}
		var s typingState
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if s := get(); !s.Open || s.SinceMS != -1 || s.Count != 0 {
		t.Fatalf("a fresh terminal: %+v", s)
	}
	r.noteOperatorTyped([]byte("\x1b[Igit st"))
	s := get()
	if s.Open || s.Line != "git st" || s.Count != 6 || s.SinceMS < 0 || s.Reason == "" {
		t.Fatalf("a part written line: %+v", s)
	}
	r.noteOperatorTyped([]byte("\r"))
	if s := get(); s.Open || s.Count != 0 {
		t.Fatalf("an empty line just touched should be shut on the idle rule: %+v", s)
	}

	req := httptest.NewRequest("GET", "/v1/tasks/nope/typing", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	d.handleTypingState(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a card with no terminal answered %d", rec.Code)
	}
}
