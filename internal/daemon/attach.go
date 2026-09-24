package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
	// On carries the wanted state of shared multi-pane input for an
	// {"t":"echo"} frame. See the case in the reader and `runner.setEchoPeers`.
	On bool `json:"on"`
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
	// BracketedPaste comes from harness configuration because the startup enable
	// sequence can fall out of scrollback before attach. This describes runner
	// support, not current terminal mode; the board also checks the stream.
	BracketedPaste bool `json:"bracketed_paste"`
}

// attachSize is the size the pty is running at, sent before the backlog, so
// the board sizes its grid before the replay lands, and again every time it
// changes. See `tellSize` in `attach`.
type attachSize struct {
	T    string `json:"t"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// bracketedPasteFor returns the harness capability for a card.
// Return false for unknown runners and store errors to avoid unsupported markers.
// Shell panes use stream detection because shells toggle the mode around prompts.
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
// A CEILING, NOT A PAUSE. The wait ends the moment the size frame lands, and
// the board sends that from `onopen` with no round trip in between, so the
// common case does not wait at all. This only bounds a client that never sends
// one.
//
// It was 500ms, chosen as "long enough for a first frame" without asking how
// long that is. On loopback it is under a millisecond and over an overlay it is
// a round trip, so half a second bought nothing and paid for itself every time
// a frame was dropped or a client did not send one. It was visible: switching
// between terminals had a pause somebody could photograph.
//
// 60ms is comfortably more than a loopback frame and more than a round trip on
// any link a person would use this over. A client slower than that gets the
// backlog at whatever width the terminal is already at, which is the same
// answer it got before, sooner.
const sizeWait = 60 * time.Millisecond

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

	// Forget this viewer when it goes. A window that attached once and was
	// closed must stop constraining the size, or a phone that popped in and out
	// would hold the session at its width forever. The pty only follows this
	// back up if the viewer that left was the binding (smallest) one: see
	// `dropViewport`, which resizes only when the agreed size actually changes.
	defer run.dropViewport(c)

	// Closed once this viewer has said how big it is. See the wait below.
	sized := make(chan struct{})
	var sizedOnce sync.Once

	// This attach's own output channel, so the keystroke fan-out can skip it:
	// a pane already shows its own typing. Set once `subscribeSized` has run
	// below, and read under a lock because the reader goroutine started here
	// races that assignment. Nil until then, which `echoToPeers` treats as
	// "exclude nobody" because this attach is not yet a watcher.
	var (
		selfMu sync.Mutex
		self   chan []byte
	)

	// The oldest keystroke this attach has not yet seen output for, in unix
	// nanoseconds. Only touched when input-lag logging is on. See inputlag.go.
	var lagIn atomic.Int64
	lagLabel := taskID
	if shell {
		lagLabel += " (shell)"
	}

	// Reader: control frames from the browser.
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			got := lagStart()
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
				// Typing into it is looking at it. One map lookup when the
				// turn is already seen. A shell is another screen, and the
				// turn's text is not on it. See seen.go.
				if !shell {
					d.seenTyped(taskID)
				}
				// Through the input lock, so these bytes wait behind a peer
				// paste in flight rather than interleaving with it. The
				// bookkeeping above is not locked and stays instant. See
				// runner.writeOperatorInput and injectPeer.
				if got.IsZero() {
					if err := run.writeOperatorInput([]byte(in.D)); err != nil {
						return
					}
				} else {
					noteLagIn(&lagIn, got)
					if err := run.writeOperatorInputTimed([]byte(in.D), got, lagLabel); err != nil {
						return
					}
				}
				// SHARED MULTI-PANE INPUT, off unless this runner was opted in.
				// A DISPLAY echo to the OTHER panes, after the one Write above,
				// so stdin is written exactly once. See `runner.echoToPeers`.
				selfMu.Lock()
				me := self
				selfMu.Unlock()
				run.echoToPeers([]byte(in.D), me)
			case "echo":
				// Turn shared multi-pane input on or off for the whole runner.
				// The HUB board sends this from a per-terminal toggle. Ignored
				// by a daemon that does not know the frame, which is what makes
				// the board piece safe to ship ahead of this one.
				run.setEchoPeers(in.On)
			case "resize":
				// THIS VIEWER'S SIZE, not the terminal's. Several browsers can
				// be on one session, a pty has one size, and passing each
				// resize straight through meant the last window dragged set
				// the width for everybody. See `runner.setViewport`.
				//
				// NEVER UNDER THE FLOOR, for a runner. Whatever the viewer
				// reports, so a script, an old tab or a phone cannot shrink
				// it. A shell does not reprint, so it takes what it is given.
				// See `api.SettingTerminalMinCols`.
				cols := in.Cols
				if !shell && cols > 0 {
					cols = max(cols, api.TerminalMinCols(d.st))
				}
				if err := run.setViewport(c, cols, in.Rows); err != nil {
					log.Printf("[atrium] resize %s: %v", taskID, err)
				}
				sizedOnce.Do(func() { close(sized) })
			case "signal":
				// A browser cannot press ctrl-c the way a terminal does, so
				// the control character is sent explicitly on request.
				if strings.EqualFold(in.S, "int") {
					run.noteOperatorTyped([]byte{0x03})
					_ = run.writeOperatorInput([]byte{0x03})
				}
			}
		}
	}()

	// Send capabilities before waiting for size so an immediate paste can use
	// them. The board retains this message for the socket's lifetime.
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

	backlog, cuts, bufRows, wantCols, wrapped, updates := run.subscribeSized()
	defer run.unsubscribe(updates)
	// WITH WHAT THE CARD HELD BEFORE THE RESTART in front, cut where the
	// resumed runner's reprint picks it up. See `withCarried`.
	if !shell {
		var trimmed bool
		var joined bool
		before := len(backlog)
		backlog, cuts, trimmed = run.withCarried(backlog, cuts, wantCols, api.ScrollbackBytes(d.st))
		joined = len(backlog) != before
		if joined {
			// The ring's own "overwritten" answer was about the ring, and the
			// ring is no longer the oldest thing replayed. The saved file says
			// so inside itself when it was cut, and a trim here is the new cut.
			wrapped = trimmed
		}
	}
	// Now that this attach is a watcher, the fan-out can recognise its channel
	// and skip it, so this pane is not echoed its own keystrokes.
	selfMu.Lock()
	self = updates
	selfMu.Unlock()
	// THE PTY'S SIZE, told to this viewer whenever it moves.
	//
	// The pty follows the widest viewer (see `runner.setViewport`), so a
	// narrower one has to draw a width it did not ask for, and only the daemon
	// knows what that is. Checked before every chunk as well as on the wake-up,
	// because the repaint that follows a resize can reach `updates` before the
	// wake-up is picked, and a repaint drawn into the old width garbles.
	//
	// SENT ONLY TO A BOARD THAT KNOWS THE FRAME. An older board writes any text
	// frame that is not `caps` straight into the terminal, so the hub has to
	// carry `takeTermSize` before a room sends this.
	sizeWake := run.sizeChanged()
	var toldCols, toldRows int
	tellSize := func() error {
		cols, rows := run.buf.CurrentSize()
		if cols == toldCols && rows == toldRows {
			return nil
		}
		toldCols, toldRows = cols, rows
		msg, err := json.Marshal(attachSize{T: "size", Cols: cols, Rows: rows})
		if err != nil {
			return nil
		}
		return c.Write(ctx, websocket.MessageText, msg)
	}
	if err := tellSize(); err != nil {
		return
	}

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
		// REPLAYED THROUGH A SCREEN, not stripped of everything that moves.
		//
		// `flatten` deletes every sequence that could overwrite something, which
		// is the only way to make an append-only replay safe and is why it has
		// to invent spaces where a cursor move used to be. On a real session
		// that produced 362 padded lines out of 864 and printed every
		// intermediate repaint in sequence, so one tool call appeared three
		// times, twice half-drawn.
		//
		// The screen model runs the bytes through a grid instead and reports
		// what the terminal would have held, plus everything that scrolled off
		// the top. `bufRows` is the height the ring recorded, and it is the
		// whole reason this can replace `flatten` now: without it the grid grew
		// to whatever row got addressed, nothing ever scrolled into history,
		// and repaints overwrote content a real terminal had already filed away.
		//
		// The setting is the way back. This path was wired once before on
		// tests alone and had to be reverted, so there is a switch rather than
		// a rebuild between the operator and the behaviour they had.
		var body []byte
		mode := replayMode(d.st)
		switch mode {
		case "raw":
			// UNTOUCHED, and xterm.js is the terminal. The bytes the runner
			// wrote go down the socket exactly as it wrote them, so history and
			// live output are rendered by one emulator instead of two that can
			// disagree, and nothing here interprets a single sequence. That
			// last part is what makes it identical for claude, codex, ollama
			// and a bare shell rather than tuned for whichever one was in front
			// of somebody when the rendering was last changed.
			body = backlog
		case "flat":
			// Collapsed first, which is the flattener's own pre-filter and
			// belongs to nothing else. See `collapseRedraws`.
			body = flatten(collapseRedraws(backlog))
		default:
			// EACH RUN AT THE WIDTH IT WAS DRAWN AT, with the grid resized at
			// every mark. See `screen.applyCuts` for the doubled lines one width
			// for everything produced after a room restart.
			//
			// WITH THE CURSOR RESTORED. The grid knows where the session parked
			// its cursor; the attaching terminal would otherwise leave it at the
			// end of the last line, so the operator's first keystroke echoes in
			// the wrong column. See `screen.textWithCursor`.
			if len(cuts) == 0 {
				cuts = []sizeCut{{0, wantCols, 0}}
			}
			body = replayCut(backlog, "screen", cuts, bufRows)
		}
		if err := c.Write(ctx, websocket.MessageBinary, body); err != nil {
			return
		}
		// NO DIVIDER UNDER THE HISTORY, deliberately. A line reading "everything
		// above is history, live from here" used to sit here to stop the first
		// live redraw reading as corrupted scrollback. In practice it read as
		// noise on every reattach, and clint asked to ditch the whole preamble.
		// Silence is the better default: the live output picks up where the
		// history stops, and the honest "the buffer overwrote older output" note
		// above still fires when there is a real reason to say something.
	}

	// Writer: output from the runner.
	for {
		select {
		case <-ctx.Done():
			return
		case <-sizeWake:
			sizeWake = run.sizeChanged()
			if err := tellSize(); err != nil {
				return
			}
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
			if err := tellSize(); err != nil {
				return
			}
			sent := lagStart()
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
			if !sent.IsZero() {
				noteLagOut(&lagIn, lagLabel, sent, time.Now(), len(updates), len(chunk))
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
