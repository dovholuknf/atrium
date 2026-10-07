package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

// A card's terminal, in this terminal: `atrium open <url> --attach`.
//
// THE BOARD'S OWN SOCKET, spoken from a console. Output arrives as binary frames and is written to stdout as it is,
// so this terminal is the emulator. What is typed goes up as {"t":"in"} frames, the size as {"t":"resize"} when it
// changes. Ctrl+] leaves, as telnet's escape does, and the card keeps running: this is a viewer, like a board tab.

// detachKey is Ctrl+], which nothing a runner draws asks for.
const detachKey = 0x1d

// attachTerminal attaches stdin and stdout to a card's terminal until Ctrl+] or the socket closes.
func attachTerminal(board, card string) error {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return errors.New("--attach needs a terminal on both stdin and stdout")
	}
	ws := "ws" + strings.TrimPrefix(strings.TrimRight(board, "/"), "http") + "/v1/tasks/" + url.PathEscape(card) + "/attach"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	c, _, err := websocket.Dial(dctx, ws, nil)
	dcancel()
	if err != nil {
		return fmt.Errorf("could not attach to %s: %w", card, err)
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)

	restoreVT := enableVT()
	defer restoreVT()
	was, err := term.MakeRaw(in)
	if err != nil {
		return fmt.Errorf("could not put this terminal in raw mode: %w", err)
	}
	defer term.Restore(in, was)
	fmt.Fprint(os.Stderr, "attached to "+card+". ctrl+] leaves, and the card keeps running.\r\n")

	send := func(v any) {
		raw, _ := json.Marshal(v)
		_ = c.Write(ctx, websocket.MessageText, raw)
	}
	// The size, now and whenever it changes. Polled, because a resize signal is not a thing every console has.
	go func() {
		cols, rows := 0, 0
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			if w, h, err := term.GetSize(out); err == nil && (w != cols || h != rows) {
				cols, rows = w, h
				send(map[string]any{"t": "resize", "cols": cols, "rows": rows})
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	// Keys up. Ctrl+] anywhere in a read ends the attach, and what came before it in that read is sent.
	go func() {
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if i := strings.IndexByte(string(chunk), detachKey); i >= 0 {
					if i > 0 {
						send(map[string]any{"t": "in", "d": string(chunk[:i])})
					}
					return
				}
				send(map[string]any{"t": "in", "d": string(chunk)})
			}
			if err != nil {
				return
			}
		}
	}()
	// Output down, until the socket or the keys end it.
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		if typ == websocket.MessageBinary {
			_, _ = os.Stdout.Write(data)
		}
	}
	term.Restore(in, was)
	fmt.Fprint(os.Stderr, "\r\ndetached from "+card+"\r\n")
	return nil
}
