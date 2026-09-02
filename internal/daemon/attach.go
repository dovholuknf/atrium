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

func (d *Daemon) handleAttach(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	run := d.sup.get(taskID)
	if run == nil {
		// Being explicit beats an empty terminal. A window mode runner owns
		// its own terminal and there is nothing here to show.
		http.Error(w, "nothing to attach to: this task has no runner atrium owns. "+
			"only a harness in pty mode can be attached to.", http.StatusNotFound)
		return
	}

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
				// The runner exited. Say so in the terminal rather than just
				// going quiet, then close.
				_ = c.Write(ctx, websocket.MessageBinary,
					[]byte("\r\n\x1b[38;5;244m[atrium] this runner has exited\x1b[0m\r\n"))
				c.Close(websocket.StatusNormalClosure, "runner exited")
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
