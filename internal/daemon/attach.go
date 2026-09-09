package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/dovholuknf/atrium/internal/api"
)

// Attach is the one place the client contract widens past JSON and SSE.
//
// A terminal needs traffic in both directions, and SSE only flows one way. One
// HTTP request per keystroke is not a serious option, so attach is a WebSocket
// and nothing else is.
//
// Bytes from the runner arrive as binary messages and go straight to the
// terminal. Text messages from the browser are JSON control frames, which keeps
// keystrokes and resizes distinguishable without a framing layer:
//
//	{"t":"in","d":"ls\r"}        keystrokes
//	{"t":"resize","cols":120,"rows":40}
//	{"t":"signal","s":"int"}     interrupt, which a browser cannot type

type attachIn struct {
	T    string `json:"t"`
	D    string `json:"d"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	S    string `json:"s"`
}

// WHICH OF A CARD'S TWO TERMINALS.
//
// A card may hold the runner's terminal and one shell (`shell.go`). They are
// told apart by a query parameter rather than by a second route, because a
// second route would duplicate the upgrade, the read limit, the backlog replay
// and the write loop, which is the copy that drifts.
//
// ABSENT MEANS THE RUNNER. Every caller written before shells existed sends
// nothing, and their meaning must not move. Making attach prefer a shell when
// one exists would silently repoint all of them.
const shellKind = "shell"

// How long an attach waits for the viewer to say how big it is before
// replaying anything.
//
// Long enough for the first frame of a socket that has just opened, which the
// board sends from `onopen` with no round trip in between, and short enough
// that a client which never sends one is not left looking at an empty
// terminal.
const sizeWait = 500 * time.Millisecond

func (d *Daemon) handleAttach(w http.ResponseWriter, r *http.Request) {
	d.attach(w, r, r.PathValue("id"), r.URL.Query().Get("kind") == shellKind)
}

// handleShellOpen starts a card's shell, or answers that it already has one.
//
// A POST rather than spawning inside the websocket upgrade, and the reason is
// what happens when it fails. A shell can fail to start in ways worth reading:
// the directory is gone, the configured shell is not installed. A websocket
// close code cannot say either, so the board would show a terminal that
// appeared and vanished. This answers with the sentence.
func (d *Daemon) handleShellOpen(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if err := d.EnsureShell(taskID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleShellClose ends it, for an operator who is done rather than for one who
// navigated away. The idle sweep covers the second case.
func (d *Daemon) handleShellClose(w http.ResponseWriter, r *http.Request) {
	d.CloseShell(r.PathValue("id"))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// whyClosed says what to put on screen and what to put in the close reason.
//
// THE REASON IS LOAD-BEARING, not decoration. Three things end an attach and
// they want three different answers from the board:
//
//	a session ended     stop, say so, do not retry
//	a shell closed      go back to the agent
//	a restart           wait, it is coming back
//
// From the browser all three are a socket closing, and it cannot tell them
// apart. Asking `/v1/health` afterwards gives the WRONG answer for the third,
// because a wind-down stops supervised runners while the listener is still up,
// so the daemon answers yes while every runner is being taken down. A board
// that trusted that tore its pane down a second before the session came back,
// and in a popped-out window it took the window's whole reason for existing.
//
// Without the first answer the board waits out a session that somebody ended
// on purpose: five minutes of failed connections after typing `exit`.
//
// The stop channel closes before any runner is touched, so the wind-down test
// is never a race.
func (d *Daemon) whyClosed(shell bool) (say, reason string) {
	select {
	case <-d.stop.ch:
		return "[atrium] atrium is restarting", "restarting"
	default:
	}
	if shell {
		return "[atrium] this shell has closed", "shell closed"
	}
	return "[atrium] this runner has exited", "runner exited"
}

// humanBytes is a size in the units somebody would say out loud.
//
// Only ever used in a sentence a person reads, so it rounds and does not
// apologise for it. The scrollback setting is in whole megabytes, which is
// where every number this is handed comes from.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.0fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// widthNote is the line drawn above replayed output that was composed for a
// terminal other than this one, and the empty string when there is nothing to
// say.
//
// SAID RATHER THAN WITHHELD. The scrollback really may sit in the wrong
// columns, and the reader is the one who gets to decide whether that is worth
// having. Everything below the note is theirs to judge, and everything the
// runner draws after it is composed for this terminal.
//
// Three cases, because "it might look wrong" is not the same sentence as
// "here is which part":
//
//	nothing to say   one width, and it is this one
//	one width        the whole backlog was drawn elsewhere
//	several          the session was resized while it ran
func widthNote(widths []int, wantCols int) string {
	if len(widths) == 0 {
		return ""
	}
	if len(widths) == 1 {
		if widths[0] == wantCols || widths[0] <= 0 {
			return ""
		}
		return fmt.Sprintf("\x1b[38;5;244m[atrium] the scrollback that follows was drawn "+
			"for a terminal %d columns wide and this one is %d, so it may sit in the "+
			"wrong places. anything the session draws from now on is drawn for this "+
			"one.\x1b[0m\r\n", widths[0], wantCols)
	}
	seen := make([]string, 0, len(widths))
	for _, w := range widths {
		seen = append(seen, strconv.Itoa(w))
	}
	return fmt.Sprintf("\x1b[38;5;244m[atrium] this session was resized while it ran, so the "+
		"scrollback that follows was drawn at %s columns and this terminal is %d. some of "+
		"it may sit in the wrong places. anything the session draws from now on is drawn "+
		"for this one.\x1b[0m\r\n", strings.Join(seen, ", "), wantCols)
}

func (d *Daemon) attach(w http.ResponseWriter, r *http.Request, taskID string, shell bool) {
	var run *runner
	if shell {
		run = d.sup.getShell(taskID)
		if run == nil {
			// Distinct from the runner's message below, because the fix is
			// different: this one is "ask for it first", not "this card cannot
			// be attached to at all".
			http.Error(w, "this card has no shell open. ask for one first.",
				http.StatusNotFound)
			return
		}
	} else {
		run = d.sup.get(taskID)
		if run == nil {
			// Being explicit beats an empty terminal. A window mode runner owns
			// its own terminal and there is nothing here to show.
			http.Error(w, "nothing to attach to: this task has no runner atrium owns. "+
				"only a harness in pty mode can be attached to.", http.StatusNotFound)
			return
		}
	}
	// The idle sweep closes a shell nobody is looking at, and this is both
	// ends of "looking at it": the clock is reset on arrival, and again on the
	// way out so the window runs from when the last person left.
	run.touch()
	defer run.touch()

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Loopback only, and the board is served from the same origin. A
		// stricter check here would break reaching it over an overlay.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[atrium] attach %s: %v", taskID, err)
		return
	}
	defer c.CloseNow()

	// PASTING SOMETHING BIG MUST NOT KILL THE SESSION.
	//
	// `coder/websocket` reads at most 32KB per message by default and closes
	// the connection when one is bigger, which is the right default for a
	// server taking messages from strangers and the wrong one here: this
	// carries keystrokes from a person who already owns the machine, and a
	// paste of a stack trace or a diff goes past 32KB without trying. The
	// symptom was a terminal that detached the moment you pasted anything
	// substantial, with nothing on screen to say why.
	//
	// Four megabytes, which is far more than anybody types and still bounded.
	// The other direction, output, is written rather than read and was never
	// affected.
	c.SetReadLimit(4 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// And give the size back when this viewer goes. A window that attached
	// once and was closed would otherwise hold the session at its width
	// forever, which is worse than the bug this pairs with: at least a
	// last-writer-wins resize could be undone by dragging something.
	defer run.dropViewport(c)

	// Closed once this viewer has said how big it is. See the wait below.
	sized := make(chan struct{})
	var sizedOnce sync.Once

	// Reader: control frames from the browser.
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var in attachIn
			if err := json.Unmarshal(data, &in); err != nil {
				continue
			}
			switch in.T {
			case "in":
				if err := run.Write([]byte(in.D)); err != nil {
					return
				}
			case "resize":
				// THIS VIEWER'S SIZE, not the terminal's. Several browsers can
				// be on one session, a pty has one size, and passing each
				// resize straight through meant the last window dragged set
				// the width for everybody. See `runner.setViewport`.
				if err := run.setViewport(c, in.Cols, in.Rows); err != nil {
					log.Printf("[atrium] resize %s: %v", taskID, err)
				}
				sizedOnce.Do(func() { close(sized) })
			case "signal":
				// A browser cannot press ctrl-c the way a terminal does, so
				// the control character is sent explicitly on request.
				if strings.EqualFold(in.S, "int") {
					_ = run.Write([]byte{0x03})
				}
			}
		}
	}()

	// THE SIZE BEFORE THE BACKLOG, and this order is the fix.
	//
	// The browser sends its size as its first frame, but a frame arrives after
	// the upgrade, and the backlog used to be written before anything was
	// read. So the daemon decided what to replay while the terminal was still
	// whatever size the last viewer left it, then resized underneath what it
	// had just sent. Every attach that changed the width replayed output
	// composed for the old one, which is exactly what cannot be rendered.
	//
	// Waiting for one frame costs nothing when it comes, and the timeout is
	// for a client that never sends one at all: it gets the output held at
	// whatever width the terminal is already at, which is the best answer
	// available for a viewer that will not say how big it is.
	select {
	case <-sized:
	case <-ctx.Done():
		return
	case <-time.After(sizeWait):
	}

	// The retained output, so attaching shows what is already on screen rather
	// than an empty box waiting for the next keystroke. All of it, whatever
	// width each part was drawn for: see `ringBuffer.Replay`.
	// The size setting, applied before anything is read out of the buffer.
	//
	// It was only ever read at spawn, so raising it did nothing until every
	// runner had been restarted, and a restart is what somebody raising it is
	// trying to survive. Growing keeps every byte, so the worst case here is
	// that it does nothing.
	run.buf.Grow(api.ScrollbackBytes(d.st))

	backlog, widths, wantCols, wrapped, updates := run.subscribe()
	defer run.unsubscribe(updates)
	if len(backlog) > 0 {
		// WHY THE SCROLLBACK STOPS WHERE IT STOPS, said at the top where
		// somebody who has scrolled all the way up is looking.
		//
		// A history that ends is either everything there was or the buffer's
		// own limit, and the two call for opposite reactions: nothing, or a
		// number in the settings. Without this line every short scrollback
		// reads as the limit, which is how an hour lost to a kill got reported
		// as the ring being too small.
		if wrapped {
			_ = c.Write(ctx, websocket.MessageBinary, []byte(fmt.Sprintf(
				"\x1b[38;5;244m[atrium] ---- THIS IS NOT THE START OF THE SESSION. the buffer "+
					"holds %s and this session has produced more than that, so older output "+
					"has been overwritten. raise it in settings, scrollback ----\x1b[0m\r\n",
				humanBytes(int64(api.ScrollbackBytes(d.st))))))
		}
		// FLATTENED, because sending it whole was not enough on its own.
		//
		// The history is full of absolute cursor moves and erases, and on
		// replay each one lands on the history already on screen rather than
		// on the line it was drawn to replace. A megabyte arrived and erased
		// itself down to two screens. `flatten` takes the ability to overwrite
		// away and leaves the text.
		//
		// The LIVE stream below is untouched, so a terminal user interface
		// works normally from here on.
		if note := widthNote(widths, wantCols); note != "" {
			_ = c.Write(ctx, websocket.MessageBinary, []byte(note))
		}
		if err := c.Write(ctx, websocket.MessageBinary, flatten(backlog)); err != nil {
			return
		}
		// AND A LINE UNDER IT, so the boundary between what was flattened and
		// what is live is visible. Without it the first redraw after attaching
		// reads as the history having been corrupted.
		_ = c.Write(ctx, websocket.MessageBinary, []byte("\x1b[38;5;244m"+
			"[atrium] ---- everything above is history, laid out flat so it could not "+
			"erase itself. live from here ----\x1b[0m\r\n"))
	}

	// Writer: output from the runner.
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-updates:
			if !ok {
				// Exited. Say so in the terminal rather than just going quiet,
				// then close. Worded for whichever of the two this is: "the
				// runner has exited" over a shell somebody typed `exit` into
				// would read as the agent having died.
				gone, why := d.whyClosed(shell)
				_ = c.Write(ctx, websocket.MessageBinary,
					[]byte("\r\n\x1b[38;5;244m"+gone+"\x1b[0m\r\n"))
				c.Close(websocket.StatusNormalClosure, why)
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case <-time.After(45 * time.Second):
			// Keeps an idle attach alive through anything in the middle that
			// times out quiet connections.
			if err := c.Ping(ctx); err != nil {
				return
			}
		}
	}
}
