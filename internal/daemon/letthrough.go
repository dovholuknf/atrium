package daemon

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
)

// "Let it through": the operator hovers the held messages chip and chooses to have what is waiting delivered NOW,
// without losing the line they were typing.
//
// The gate (injectPeer) will not type into a part written line, and that stays true. This is the operator, by hand,
// asking for the one thing the gate exists to prevent, so it is done as one indivisible act:
//
//  1. the board has already copied the line to the clipboard (a browser needs the click for that), and sends it here;
//  2. the line is cleared with control-c, which is what a Claude Code prompt takes as "throw this draft away";
//  3. every waiting message is typed and submitted, oldest first, the way the retry would have;
//  4. the draft is typed back, WITHOUT Enter, a newline in it as the board's shift-enter (ESC CR).
//
// THE INPUT LOCK IS HELD THROUGHOUT. The operator's keystrokes are recorded in the typing model the moment they
// land and their BYTES wait on `pasteMu`, so a key pressed during the sequence is held and goes to the terminal after
// the draft is back, which is where it belongs. The model is then the draft followed by those keys.
//
// NEVER RETYPE A GUESS. The line is typed back only when the model says it is exact AND it is the line the board
// copied. A line atrium cannot follow (an arrow that recalls history, a cursor move) is left alone and the board has
// only copied it, because a wrong retype is worse than a held message.
//
// Mid-turn runners take it too: a message submitted mid-turn is queued by Claude Code, and the draft restored after.
// A dialog on screen refuses, because the Enter would answer it.

// letThroughQuiet is how long the keyboard must have been still, so a key that has been recorded but whose bytes are
// still queued behind the input lock cannot be in the model and not yet on the prompt.
const letThroughQuiet = 500 * time.Millisecond

// letThroughPause is the beat after a clear, so the runner has redrawn an empty prompt before text goes into it.
const letThroughPause = sayThenEnter

type letThroughBody struct {
	Line string `json:"line"`
}

type letThroughAnswer struct {
	Delivered int  `json:"delivered"`
	Restored  bool `json:"restored"`
}

// errLetThrough is a refusal with the status it answers.
type errLetThrough struct {
	code int
	why  string
}

func (e *errLetThrough) Error() string { return e.why }

func (d *Daemon) handleLetThrough(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	var body letThroughBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		small.WriteJSONErr(w, http.StatusBadRequest, err)
		return
	}
	n, err := d.letThrough(taskID, body.Line)
	if err != nil {
		var le *errLetThrough
		if errors.As(err, &le) {
			small.WriteJSONErr(w, le.code, err)
			return
		}
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(letThroughAnswer{Delivered: n, Restored: true})
}

// retypeDraft is the bytes that put a draft back: its newlines as ESC CR, the board's shift-enter.
func retypeDraft(line string) []byte {
	return []byte(strings.ReplaceAll(strings.ReplaceAll(line, "\r\n", "\n"), "\n", "\x1b\r"))
}

func (d *Daemon) letThrough(taskID, line string) (int, error) {
	pi := d.pending
	run := d.sup.get(taskID)
	if pi == nil || run == nil {
		return 0, &errLetThrough{http.StatusNotFound, "no supervised terminal on this card"}
	}
	if line == "" {
		return 0, &errLetThrough{http.StatusBadRequest, "no line to put back"}
	}
	pi.mu.Lock()
	var entries []pendingMsg
	if ht := pi.by[taskID]; ht != nil {
		entries = append(entries, ht.entries...)
	}
	pi.mu.Unlock()
	if len(entries) == 0 {
		return 0, &errLetThrough{http.StatusConflict, "nothing is waiting"}
	}
	// A dialog the runner put up would be answered by the Enter, and a new-context cycle or a deploy hold owns the
	// terminal. Only a line is the operator's to override.
	if why := pi.heldFor(taskID, entries[0]); why != HeldForLine && why != HeldForTurn {
		return 0, &errLetThrough{http.StatusConflict, "the messages are not held by your line"}
	}

	run.injectMu.Lock()
	defer run.injectMu.Unlock()
	run.pasteMu.Lock()
	defer run.pasteMu.Unlock()

	// Verified and emptied in one step under typeMu, so the model cannot move between the check and the clear. Keys
	// that land from here on are recorded after the clear, and their bytes wait on pasteMu.
	run.typeMu.Lock()
	switch {
	case !run.line.exactLocked():
		run.typeMu.Unlock()
		return 0, &errLetThrough{http.StatusConflict, "atrium cannot read your line exactly"}
	case string(run.line.text) != line:
		run.typeMu.Unlock()
		return 0, &errLetThrough{http.StatusConflict, "your line changed"}
	case !run.lastTyped.IsZero() && time.Since(run.lastTyped) < letThroughQuiet:
		run.typeMu.Unlock()
		return 0, &errLetThrough{http.StatusConflict, "you are still typing"}
	}
	run.line.clear()
	run.typeMu.Unlock()

	// Whatever happens from here the draft is put back, so a failed write cannot lose it.
	restore := func() error {
		if err := run.Write(retypeDraft(line)); err != nil {
			return err
		}
		run.typeMu.Lock()
		// The draft goes IN FRONT of whatever keys were recorded meanwhile. A draft is not a keystroke, but the prompt
		// hook of a delivered message would clear a model it thinks nothing was typed into, so it counts as one.
		run.line.text = append([]rune(line), run.line.text...)
		run.keySeq++
		run.typeMu.Unlock()
		return nil
	}

	if err := run.Write([]byte{0x03}); err != nil {
		return 0, err
	}
	time.Sleep(letThroughPause)

	delivered := 0
	var failed error
	for _, e := range entries {
		if err := run.Write([]byte(e.banner + e.body)); err != nil {
			failed = err
			break
		}
		time.Sleep(sayThenEnter)
		if err := run.Write([]byte("\r")); err != nil {
			failed = err
			break
		}
		if e.banner != "" {
			run.typeMu.Lock()
			run.peerSent = time.Now()
			run.peerKeySeq = run.keySeq
			run.peerCause = causeOfBanner(e.banner)
			run.peerBanner = e.banner
			run.typeMu.Unlock()
		}
		if err := d.markDelivered(taskID, "terminal", []string{e.msgID}); err != nil {
			log.Printf("[atrium] let a held message through to %s but could not mark it delivered: %v", taskID, err)
		}
		d.notePeerTyped(taskID, e.from, e.text, e.kind, "typed and sent when you let it through your line")
		delivered++
		time.Sleep(letThroughPause)
	}
	if err := restore(); err != nil {
		return delivered, err
	}
	if failed != nil {
		return delivered, failed
	}
	return delivered, nil
}
