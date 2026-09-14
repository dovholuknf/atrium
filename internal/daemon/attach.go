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

// attachCaps is what the board cannot work out from the bytes, sent once as
// the first message of an attach.
//
// The only direction that had a control channel was browser to daemon, because
// output needs no framing. This is the first thing the OTHER way, and it is
// text where output is binary, which is how the board tells them apart without
// a framing layer either.
type attachCaps struct {
	T string `json:"t"`
	// BracketedPaste is whether this card's runner asked for bracketed paste,
	// read off its harness row rather than out of the output stream.
	//
	// WHY IT CANNOT BE INFERRED, and this is the whole of `B2-18`. The board's
	// evidence was the `\x1b[?2004h` the runner emits once at startup, parsed
	// out of the replayed scrollback. Once the ring has wrapped past it a
	// freshly opened pane has no evidence at all and pastes raw, so a long
	// paste is delivered to the runner a line at a time and the first line is
	// submitted while the rest lands in a prompt that is now busy. The ring is
	// entitled to discard that byte, so the answer has to come from somewhere
	// that is not the stream.
	//
	// NOT THE DAEMON TRACKING THE MODE. This says what the runner is, which is
	// fixed for the life of the process and is already configuration; it does
	// not say what the terminal is doing right now. A pane that sees a real
	// enable go past still believes that too, so a runner nobody has declared
	// behaves exactly as it did before.
	BracketedPaste bool `json:"bracketed_paste"`
}

// bracketedPasteFor answers `attachCaps.BracketedPaste` for one card.
//
// FALSE WHENEVER THE ANSWER IS NOT KNOWN, which covers a card with no harness
// row, a runner recorded by a hook that atrium never launched, and a database
// that cannot be read. Sending the markers to something that never asked for
// them puts `200~` on screen, so an unknown runner keeps the old behaviour of
// believing the stream and nothing else.
//
// A SHELL IS ALWAYS FALSE. `kind=shell` is not the harness's runner, it is a
// command line, and a shell turns the mode on and off around each prompt
// rather than for its whole run. It also re-emits the enable constantly, so the
// stream is a good answer there and this one would be a guess.
func (d *Daemon) bracketedPasteFor(taskID string, shell bool) bool {
	if shell {
		return false
	}
	t, err := d.st.Get(taskID)
	if err != nil || t == nil || t.Runner == "" {
		return false
	}
	h, err := d.st.Harness(t.Runner)
	if err != nil || h == nil {
		return false
	}
	return h.BracketedPaste
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
				// THE ONE PLACE THE OPERATOR'S OWN KEYSTROKES ARRIVE, which is
				// what makes the record trustworthy. `Say` and the peer bus
				// also reach `Write`, so counting bytes there would have
				// atrium reading its own typing as the person being busy.
				// See `runner.noteOperatorTyped`.
				run.noteOperatorTyped([]byte(in.D))
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

	// WHAT THIS RUNNER IS, before anything is drawn.
	//
	// Ahead of the size wait rather than beside the backlog, because a paste
	// can happen the moment the pane is open and this must not be behind half
	// a second of waiting for a client that may never say how big it is. It is
	// one small text message and the board holds it for the life of the
	// socket.
	if caps, err := json.Marshal(attachCaps{
		T:              "caps",
		BracketedPaste: d.bracketedPasteFor(taskID, shell),
	}); err == nil {
		_ = c.Write(ctx, websocket.MessageText, caps)
	}

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
		// REPLAYED THROUGH A SCREEN, because the history was COMPOSED rather
		// than appended and the meaning of a repaint is where it landed.
		//
		// This used to call `flatten`, which dropped every sequence that could
		// move the cursor onto a line already written. That kept the text and
		// lost the layout, and three separate failures came out of it: a patch
		// to columns 7 onward of a row arrived as the orphan `do…)`, a spinner
		// redrawn beside a prompt arrived as one line per frame, and a prompt
		// redrawn while somebody typed arrived as `f`, `ou`, `nd`, `it`.
		//
		// None of those is fixable by another rule, because the missing
		// information is not in the byte stream. It is on the screen the runner
		// could see. `screen.go` keeps that screen, and what scrolls off it is
		// the history. Measured on twelve real sessions: 4,921 `Forging…`
		// frames become 3.
		//
		// AT THE WIDTH THE HISTORY WAS COMPOSED AT, not the width of the
		// browser asking. A repaint aimed at column 100 means nothing against a
		// grid 80 wide, and `widths` is the record of what it was written for.
		//
		// The LIVE stream below is untouched, so a terminal user interface
		// works normally from here on.
		if note := widthNote(widths, wantCols); note != "" {
			_ = c.Write(ctx, websocket.MessageBinary, []byte(note))
		}
		// NOT THROUGH THE SCREEN MODEL, and that is a decision rather than an
		// oversight. `screen.go` exists, it is tested, and wiring it here made
		// the pane worse in the operator's hands on the first attempt. It is
		// left unwired until somebody has read a real pane through it and said
		// otherwise. See the head of `screen.go`.
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
