package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
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

	// The retained output first, so attaching shows what is already on screen
	// rather than an empty box waiting for the next keystroke.
	backlog, updates := run.subscribe()
	defer run.unsubscribe(updates)
	if len(backlog) > 0 {
		if err := c.Write(ctx, websocket.MessageBinary, backlog); err != nil {
			return
		}
	}

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
				if in.Cols > 0 && in.Rows > 0 {
					if err := run.Resize(in.Cols, in.Rows); err != nil {
						log.Printf("[atrium] resize %s: %v", taskID, err)
					}
				}
			case "signal":
				// A browser cannot press ctrl-c the way a terminal does, so
				// the control character is sent explicitly on request.
				if strings.EqualFold(in.S, "int") {
					_ = run.Write([]byte{0x03})
				}
			}
		}
	}()

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
