package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// typedLine is atrium's model of the operator's current input line: the TEXT,
// not a count of bytes. It is what the peer gate reads, and the gate lets a
// message be typed only when this says the line is empty.
//
// ONLY KEYSTROKES MOVE IT. A terminal user interface asks the terminal to tell
// it things, and xterm.js answers ON THE SAME CHANNEL AS THE KEYBOARD: `ESC [ I`
// and `ESC [ O` on focus and blur, a coordinate report on every click, and
// replies to the device, cursor, mode and colour queries a TUI sends at start.
// Claude Code turns on focus reporting, so clicking into a terminal used to add
// two characters that nothing ever took away, and every say to that card sat
// held behind an empty line. `feed` reads each of those as a report and counts
// nothing.
//
// ERR TOWARD HOLDING. A key whose effect atrium cannot follow (an up arrow that
// recalls a line, a cursor move before an edit, a yank, a history search)
// marks the line `unsure` until something certainly empties it: Enter,
// control-c or control-u. The gate then stays shut, and the readout says why.
// A message that waits is a nuisance. One typed into somebody's half written
// line is the thing the gate exists to prevent.
//
// Not safe for concurrent use. The runner holds it under typeMu.
type typedLine struct {
	text []rune
	// dropped is how many characters `typedLineCap` pushed off the front of
	// text. They are still on the operator's line, so the line is not empty
	// until they are deleted too.
	dropped int
	// unsure names the key that took the line out of what atrium can follow,
	// or is empty when the text is believed exact.
	unsure string
	// inPaste is inside a bracketed paste, where every byte is text and a
	// carriage return is a newline rather than Enter.
	inPaste bool
	// escClears is a runner whose prompt a second Esc clears, and escAt is when
	// the last lone Esc landed. See `loneEsc`.
	escClears bool
	escAt     time.Time
	// submitted is the line the last Enter sent, when atrium followed it exactly,
	// and empty when it did not. Read once by `takeSubmitted`, so a line is acted
	// on once. See modelswitch.go, which reads a hand typed `/model`.
	submitted string
	// pending is the front of an escape sequence, or of a character, that a frame
	// ended in the middle of. It is put back in front of the next frame. See `feed`.
	pending []byte
	// lastIn is when the last input byte landed, and is what the paste watchdog
	// measures. clock is the time source, nil for the real one, so a test can move
	// it without sleeping.
	lastIn time.Time
	clock  func() time.Time
	// loneAt, loneUndo and loneLive remember a lone Esc that ended a frame, so a
	// sequence that was only cut after its Esc can be put back together. See
	// `rejoinEsc`.
	loneAt   time.Time
	loneUndo time.Time
	loneLive bool
	// wedgeLogged is set once a paste that sat open and quiet has been logged, so
	// the log names each occurrence once. Cleared when the paste ends.
	wedgeLogged bool
}

const (
	// pasteQuiet is how long a bracketed paste may go without a byte before it is
	// taken as over. A real paste is one frame, or a few in a row.
	pasteQuiet = 2 * time.Second
	// pasteWedged is how long a non empty line may sit open in a paste with no key
	// before the readout calls it stuck.
	pasteWedged = 60 * time.Second
	// escRejoin is how soon after a lone Esc a frame that starts like the rest of
	// a sequence is taken as that sequence's tail. A person does not press Esc and
	// then another key inside this.
	escRejoin = 50 * time.Millisecond
	// pendingStale is how long a held partial sequence waits for its tail before it
	// is read as what it is so far (alt-[ is `ESC [` and nothing more).
	pendingStale = 250 * time.Millisecond
	// pendingMax bounds what is held, so a stream that never ends a sequence cannot
	// grow it.
	pendingMax = 4096
)

func (l *typedLine) now() time.Time {
	if l.clock != nil {
		return l.clock()
	}
	return time.Now()
}

// typedLineCap is how much of the line's text is kept. A long paste is still
// counted in full, through `dropped`, but only its tail is held.
const typedLineCap = 4096

func (l *typedLine) empty() bool {
	return len(l.text) == 0 && l.dropped == 0 && l.unsure == ""
}

func (l *typedLine) count() int { return l.dropped + len(l.text) }

func (l *typedLine) clear() {
	l.text = l.text[:0]
	l.dropped = 0
	l.unsure = ""
	l.inPaste = false
	l.pending = nil
	l.wedgeLogged = false
}

func (l *typedLine) add(r rune) {
	l.text = append(l.text, r)
	// Trimmed in a batch, so a long paste costs one copy per cap of text rather
	// than one per character.
	if len(l.text) > 2*typedLineCap {
		cut := len(l.text) - typedLineCap
		l.dropped += cut
		l.text = append(l.text[:0], l.text[cut:]...)
	}
}

func (l *typedLine) backspace() {
	switch {
	case len(l.text) > 0:
		l.text = l.text[:len(l.text)-1]
	case l.dropped > 0:
		l.dropped--
	}
}

// wordDelete is control-backspace, alt-backspace and control-w.
//
// ONE RULE FOR ALL THREE, and it is the one that deletes LEAST. Readline's
// control-w takes everything back to a space and its alt-backspace stops at
// punctuation, and a runner may do either. This skips trailing blanks and then
// takes one run of word characters, or one run of punctuation if that is what
// ends the line. So `foo-bar` becomes `foo-`, where either readline rule would
// take more. Deleting less than the runner did leaves the model reading a line
// as still written, which holds a message. Deleting more would read a written
// line as empty, which types over somebody.
//
// It never crosses a newline: a newline at the end is removed on its own, like
// a backspace.
func (l *typedLine) wordDelete() {
	t := l.text
	if len(t) == 0 {
		if l.dropped > 0 {
			l.unsure = "a word delete into text too long to keep"
		}
		return
	}
	if t[len(t)-1] == '\n' {
		l.text = t[:len(t)-1]
		return
	}
	i := len(t)
	for i > 0 && (t[i-1] == ' ' || t[i-1] == '\t') {
		i--
	}
	if i > 0 {
		word := isWordRune(t[i-1])
		for i > 0 && t[i-1] != '\n' && t[i-1] != ' ' && t[i-1] != '\t' && isWordRune(t[i-1]) == word {
			i--
		}
	}
	l.text = t[:i]
	if i == 0 && l.dropped > 0 {
		l.unsure = "a word delete into text too long to keep"
	}
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// moved is a key that moves the cursor or edits somewhere other than the end.
// On an empty line there is nowhere to move, so it changes nothing. On a
// written line the next edit may not land at the end, so the text is no longer
// known.
func (l *typedLine) moved(key string) {
	if !l.empty() {
		l.unsure = key
	}
}

// lost is a key that may put text on the line that atrium never saw, like an
// up arrow recalling history. Unsure whatever the line held.
func (l *typedLine) lost(key string) {
	l.unsure = key
}

// feed applies one attach frame of operator input and reports whether it held
// a keystroke. A frame of nothing but terminal reports returns false, so it
// neither moves the idle clock nor re-arms a waiting message.
//
// xterm.js hands input over a keystroke or a report at a time, but a websocket
// re-frame, a reattach or a paste box can cut a sequence anywhere. A sequence
// that the frame ends inside is HELD in `pending` and read with the next frame,
// so a paste end marker split in two still ends the paste. Held bytes count as
// no keystroke until they complete.
func (l *typedLine) feed(p []byte) (keyed bool) {
	if len(p) == 0 {
		return false
	}
	now := l.now()
	// A paste that has gone quiet is over, whatever became of its end marker. The
	// watchdog runs on the next byte, not on a timer, so the readout can still
	// show a stuck paste.
	if l.inPaste && !l.lastIn.IsZero() && now.Sub(l.lastIn) >= pasteQuiet {
		l.inPaste = false
		l.pending = nil
		l.wedgeLogged = false
	}
	l.lastIn = now
	if l.inPaste && len(p) == 1 && (p[0] == 0x03 || p[0] == 0x15) {
		// A bare control-c or control-u is never paste text. Whatever is stuck, this
		// leaves it. Inside a bigger frame it stays text, as it was.
		l.clear()
		l.escAt = time.Time{}
		return true
	}
	return l.parse(l.rejoinEsc(p, now), true)
}

// rejoinEsc puts a lone Esc that ended the last frame back in front of this one
// when this one reads as the rest of its sequence. The Esc had already been
// counted as the key, so that count is undone and the pair parsed as one.
func (l *typedLine) rejoinEsc(p []byte, now time.Time) []byte {
	live := l.loneLive
	l.loneLive = false
	if !live || p[0] == 0x1b || now.Sub(l.loneAt) >= escRejoin {
		return p
	}
	l.escAt = l.loneUndo
	return append([]byte{0x1b}, p...)
}

// settle reads a held partial sequence as what it is so far once its tail has
// not come for `pendingStale`, so a real alt-[ is not held for ever. The gate
// and the readout call it.
func (l *typedLine) settle() {
	if len(l.pending) == 0 || l.now().Sub(l.lastIn) < pendingStale {
		return
	}
	if l.inPaste {
		// Half an end marker. Not text, and the watchdog ends the paste.
		l.pending = nil
		return
	}
	l.parse(nil, false)
}

// parse reads the held bytes and then p. `more` says whether a later frame may
// follow, which decides whether a sequence cut off at the end is held or read
// as it stands.
func (l *typedLine) parse(p []byte, more bool) (keyed bool) {
	if len(l.pending) > 0 {
		p = append(l.pending, p...)
		l.pending = nil
	}
	for i := 0; i < len(p); {
		if len(p)-i > pendingMax {
			more = false
		}
		if l.inPaste {
			// Everything up to the end marker is text.
			if bytes.HasPrefix(p[i:], pasteEnd) {
				l.inPaste = false
				l.wedgeLogged = false
				i += len(pasteEnd)
				keyed = true
				continue
			}
			if more && (p[i] == 0x1b && bytes.HasPrefix(pasteEnd, p[i:]) || !utf8.FullRune(p[i:])) {
				l.pending = append([]byte(nil), p[i:]...)
				break
			}
			b := p[i]
			switch {
			case b == '\r' || b == '\n':
				l.add('\n')
				i++
			case b < 0x20 || b == 0x7f:
				i++
			default:
				r, n := utf8.DecodeRune(p[i:])
				l.add(r)
				i += n
			}
			keyed = true
			continue
		}
		b := p[i]
		if b == 0x1b {
			n, key := l.escape(p[i:], more)
			if n == 0 {
				l.pending = append([]byte(nil), p[i:]...)
				break
			}
			i += n
			keyed = keyed || key
			continue
		}
		if more && !utf8.FullRune(p[i:]) {
			l.pending = append([]byte(nil), p[i:]...)
			break
		}
		keyed = true
		l.escAt = time.Time{}
		if b < 0x20 || b == 0x7f {
			l.control(b)
			i++
			continue
		}
		r, n := utf8.DecodeRune(p[i:])
		l.add(r)
		i += n
	}
	return keyed
}

func (l *typedLine) control(b byte) {
	switch b {
	case '\r': // Enter
		l.submitted = ""
		if l.unsure == "" && l.dropped == 0 {
			l.submitted = string(l.text)
		}
		l.clear()
	case '\n': // the board's ctrl-enter, a newline in the prompt
		l.add('\n')
	case 0x03, 0x15: // control-c, control-u
		l.clear()
	case 0x7f: // backspace
		l.backspace()
	case 0x08, 0x17: // control-backspace, control-w
		l.wordDelete()
	case 0x01, 0x02, 0x05, 0x06: // control-a, -b, -e, -f
		l.moved("a cursor move (control-a/b/e/f)")
	case '\t':
		l.moved("tab, which can complete")
	case 0x0e, 0x10: // control-n, control-p
		l.lost("control-n/p, which can recall a line")
	case 0x12:
		l.lost("control-r, a history search")
	case 0x19:
		l.lost("control-y, which pastes what was cut")
	case 0x1f:
		l.lost("control-_, an undo")
	}
	// Everything else, control-d, -k and -l among them, leaves the text alone:
	// with the cursor at the end they delete or draw nothing on the line.
}

// escape reads one escape sequence at the start of p and reports how many
// bytes it took and whether it was a keystroke. False means the terminal sent
// it on its own account.
//
// It returns 0 bytes when `more` is set and the sequence is cut off, so the
// caller holds it for the next frame.
func (l *typedLine) escape(p []byte, more bool) (int, bool) {
	if len(p) == 1 || p[1] == 0x1b {
		// Escape pressed, then whatever follows. Remember what a rejoin would have
		// to restore, and whether this Esc cleared the line, which cannot be undone.
		l.loneUndo = l.escAt
		cleared := l.loneEsc()
		if cleared {
			l.loneUndo = time.Time{}
		}
		l.loneLive = !cleared && len(p) == 1
		l.loneAt = l.now()
		return 1, true
	}
	n, key := l.escapeSeq(p, more)
	if n == 0 {
		return 0, false
	}
	if key {
		l.escAt = time.Time{}
	}
	return n, key
}

// loneEsc is the Esc key on its own.
//
// ESC ESC CLEARS A CLAUDE PROMPT ("Esc again to clear"), and was read as editing
// nothing, so a line cleared that way stayed written here and held every say
// until a control-c. Two in a row within `escAgainWithin` empty the line on a
// runner that clears on them. Only that runner: in a shell Esc is a meta prefix
// and clears nothing, and reading it as a clear there would type into a half
// written command. Clearing the scrollback sends no byte and empties no line, so
// it rightly releases nothing.
func (l *typedLine) loneEsc() (cleared bool) {
	if l.escClears && !l.escAt.IsZero() && l.now().Sub(l.escAt) < escAgainWithin {
		l.clear()
		l.escAt = time.Time{}
		return true
	}
	l.escAt = l.now()
	return false
}

// escapeSeq is `escape` for a sequence longer than the Esc key alone.
func (l *typedLine) escapeSeq(p []byte, more bool) (int, bool) {
	switch p[1] {
	case '[':
		return l.csi(p, more)
	case 'O':
		if len(p) < 3 {
			if more {
				return 0, false
			}
			return l.meta(p)
		}
		l.ss3(p[2])
		return 3, true
	case ']':
		// An OSC reply, a colour query answered. Ends at BEL or ST.
		if end := oscEnd(p); end > 0 {
			return end, false
		}
		if more {
			return 0, false
		}
		return l.meta(p)
	case 'P':
		// A DCS reply, DECRQSS answered. Ends at ST.
		if end := bytes.Index(p, []byte("\x1b\\")); end > 0 {
			return end + 2, false
		}
		if more {
			return 0, false
		}
		return l.meta(p)
	case '\r': // the board's shift-enter, a newline in the prompt
		l.add('\n')
		return 2, true
	case 0x7f, 0x08: // alt-backspace, control-alt-backspace
		l.wordDelete()
		return 2, true
	}
	if more && !utf8.FullRune(p[1:]) {
		return 0, false // alt with half a character
	}
	return l.meta(p)
}

// meta is alt with a key: ESC and the key's own bytes.
func (l *typedLine) meta(p []byte) (int, bool) {
	r, n := utf8.DecodeRune(p[1:])
	switch r {
	case '.', '_', 'y', 'Y':
		l.lost("alt-" + string(r) + ", which can insert text")
	default:
		l.moved("alt-" + keyName(r))
	}
	return 1 + n, true
}

func keyName(r rune) string {
	if r < 0x20 {
		return "control-" + string(rune('@'+r))
	}
	return string(r)
}

func oscEnd(p []byte) int {
	for i := 2; i < len(p); i++ {
		if p[i] == 0x07 {
			return i + 1
		}
		if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
			return i + 2
		}
	}
	return 0
}

// csi reads ESC [ params intermediates final.
func (l *typedLine) csi(p []byte, more bool) (int, bool) {
	j := 2
	for j < len(p) && p[j] >= 0x30 && p[j] <= 0x3f {
		j++
	}
	params := string(p[2:j])
	k := j
	for k < len(p) && p[k] >= 0x20 && p[k] <= 0x2f {
		k++
	}
	inter := string(p[j:k])
	if k >= len(p) && more {
		return 0, false // cut off before its final byte
	}
	if k >= len(p) || p[k] < 0x40 || p[k] > 0x7e {
		// No final byte, so not a sequence at all: alt-[.
		return l.meta(p)
	}
	final := p[k]
	n := k + 1

	// What the terminal sends on its own. See xterm.js `InputHandler`.
	switch {
	case (final == 'I' || final == 'O') && params == "" && inter == "":
		return n, false // focus in, focus out
	case final == 'M' && params == "" && inter == "":
		// X10 mouse: three raw bytes follow.
		if more && n+3 > len(p) {
			return 0, false
		}
		return min(n+3, len(p)), false
	case (final == 'M' || final == 'm') && strings.HasPrefix(params, "<"):
		return n, false // SGR mouse
	case final == 'M' && strings.Count(params, ";") == 2:
		return n, false // urxvt mouse
	case final == 'c':
		return n, false // device attributes, primary or secondary
	case final == 'n':
		return n, false // device status
	case final == 'R':
		// Cursor position report. Modified F3 has the same shape and edits no
		// line, so reading it as a report costs nothing.
		return n, false
	case final == 'y' && inter == "$":
		return n, false // DECRPM, a mode report
	case final == 't':
		return n, false // window size in pixels or cells
	}

	switch {
	case final == '~' && params == "200":
		l.inPaste = true
	case final == '~' && params == "201":
		// An end marker with no start. Nothing to close.
	case final == 'A' || final == 'B':
		l.lost("an up or down arrow, which can recall a line")
	case final == 'C' || final == 'D' || final == 'H' || final == 'F':
		l.moved("a cursor move (arrow, home or end)")
	case final == '~' && (params == "3" || strings.HasPrefix(params, "3;")):
		l.moved("delete, which edits under the cursor")
	case final == '~', final == 'Z', final == 'P', final == 'Q', final == 'S':
		// Insert, page up and down, function keys, shift-tab. None of them
		// puts anything on the line.
	default:
		l.lost("a key atrium does not know (ESC [ " + params + inter + string(rune(final)) + ")")
	}
	return n, true
}

// ss3 is ESC O and one byte: the arrows and home and end in application cursor
// mode, and F1 to F4.
func (l *typedLine) ss3(b byte) {
	switch b {
	case 'A', 'B':
		l.lost("an up or down arrow, which can recall a line")
	case 'C', 'D', 'H', 'F':
		l.moved("a cursor move (arrow, home or end)")
	case 'P', 'Q', 'R', 'S':
		// F1 to F4.
	default:
		l.lost("a keypad key atrium does not know (ESC O " + string(rune(b)) + ")")
	}
}

// handleTypingState answers the board's typing readout: what atrium thinks is
// on the operator's line, how long since the last keystroke, and whether a say
// may be typed in now and why not. `?kind=shell` asks about the card's shell,
// the same parameter `attach` takes.
//
// Read on demand rather than pushed, so the attach path, which is hot, does no
// more work for it than it already did. The board polls this only while the
// readout is switched on. Not on the guest surface, which is an allowlist.
func (d *Daemon) handleTypingState(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	run := d.sup.get(taskID)
	if r.URL.Query().Get("kind") == "shell" {
		run = d.sup.getShell(taskID)
	}
	if run == nil {
		writeJSONErr(w, http.StatusNotFound, errors.New("no supervised terminal on this card"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(run.typing())
}
