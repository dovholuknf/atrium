package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/spf13/cobra"
)

// A card by its handle, from a shell. See docs/rnd/handle-addressed-http-design.md
// section 8.
//
// `atrium task`, `atrium exit` and `atrium new-context` name a card the way an
// agent does, `rnd`, `@rnd`, `rnd@claude-sg4`, `room~id` or an id, and the hub
// (or the room, on its own port) resolves it. Each prints what it reached, from
// the answer's X-Atrium-Handle, so a script can check before it acts again.
//
// ON THE BOARD'S PORT, not the agent listener `atrium tell` uses, because these
// are the human API's routes. The address is `boardAddress`: the flag, then
// $ATRIUM_BOARD_URL, then what the running daemon wrote down.
//
// A BARE ALIAS REACHES WHOEVER HOLDS IT NOW. An alias moves to a new card once
// the old one is done, so `atrium exit sa12` meant for a done sa12 reaches a
// live one if there is one. That is atrium_exit's rule too, and what was reached
// is printed.

// cardCall is one request naming a card, answered as JSON.
type cardCall struct {
	status  int
	handle  string
	card    string
	body    []byte
	address string
}

func callCard(boardURL, method, who, verb string, body any) (cardCall, error) {
	who = strings.TrimSpace(who)
	if who == "" {
		return cardCall{}, fmt.Errorf("say which card: a handle, an alias, name@room or an id")
	}
	path := "/v1/tasks/" + url.PathEscape(who)
	if verb != "" {
		path += "/" + verb
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return cardCall{}, err
		}
		rd = bytes.NewReader(raw)
	}
	addr := boardAddress(boardURL)
	req, err := http.NewRequest(method, addr+path, rd)
	if err != nil {
		return cardCall{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return cardCall{}, fmt.Errorf("no board answered at %s: %w", addr, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	out := cardCall{status: res.StatusCode, handle: res.Header.Get("X-Atrium-Handle"),
		card: res.Header.Get("X-Atrium-Card"), body: raw, address: addr}
	if res.StatusCode >= 300 {
		return out, refusedCard(res.StatusCode, raw)
	}
	return out, nil
}

// refusedCard is the sentence for a refusal, with the list that says what would
// have worked or which cards were meant.
func refusedCard(code int, raw []byte) error {
	var e struct {
		Error      string   `json:"error"`
		WouldWork  []string `json:"would_work"`
		Candidates []string `json:"candidates"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Error == "" {
		return fmt.Errorf("atrium answered %d: %s", code, strings.TrimSpace(string(raw)))
	}
	msg := e.Error
	if len(e.Candidates) > 0 {
		msg += "\n  " + strings.Join(e.Candidates, "\n  ")
	}
	return fmt.Errorf("%s", msg)
}

// reached says which card an answer was about.
func (c cardCall) reached() string {
	switch {
	case c.handle != "" && c.card != "":
		return c.handle + " (" + c.card + ")"
	case c.handle != "":
		return c.handle
	case c.card != "":
		return c.card
	}
	return "the card"
}

func newTaskCmd() *cobra.Command {
	var boardURL string
	var asJSON bool
	c := &cobra.Command{
		Use:   "task <who>",
		Short: "Show one card, named by its handle, alias, name@room or id.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := callCard(boardURL, http.MethodGet, args[0], "", nil)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				_, err := out.Write(append(bytes.TrimSpace(got.body), '\n'))
				return err
			}
			var t struct {
				ID       string `json:"id"`
				Title    string `json:"display_title"`
				Wire     string `json:"wire_name"`
				Alias    string `json:"alias"`
				Status   string `json:"status"`
				Room     string `json:"room"`
				Wait     int    `json:"wait_seconds"`
				Activity struct {
					What string `json:"what"`
				} `json:"activity"`
			}
			if err := json.Unmarshal(got.body, &t); err != nil {
				return fmt.Errorf("could not read the card: %w", err)
			}
			fmt.Fprintf(out, "%s\n", orDefaultWord(t.Title, t.ID))
			fmt.Fprintf(out, "  handle  %s\n", orDefaultWord(got.handle, t.Wire))
			if t.Alias != "" {
				fmt.Fprintf(out, "  alias   @%s\n", t.Alias)
			}
			fmt.Fprintf(out, "  card    %s\n", orDefaultWord(got.card, t.ID))
			fmt.Fprintf(out, "  status  %s\n", t.Status)
			if t.Activity.What != "" {
				fmt.Fprintf(out, "  doing   %s\n", t.Activity.What)
			}
			if t.Wait > 0 {
				fmt.Fprintf(out, "  waiting %s\n", (time.Duration(t.Wait) * time.Second).String())
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the card as the board API answers it")
	c.Flags().StringVar(&boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or the running daemon's)")
	return c
}

func newExitCmd() *cobra.Command {
	var boardURL string
	var force bool
	c := &cobra.Command{
		Use:   "exit <who>",
		Short: "Ask a card's runner to leave, the way atrium_exit does. The card stays down across a restart.",
		Long: "Run by an agent (ATRIUM_AGENT_NAME set), it may exit itself or a card it launched. Any other card " +
			"needs --force, which is recorded on that card, and a director cannot be exited by an agent at all. " +
			"Run by the operator, with no ATRIUM_AGENT_NAME, there is no limit.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")); me != "" {
				body["from"], body["force"] = me, force
				// A card named on another room is asked about by a name that is
				// foreign there.
				if isAcross(args[0]) {
					if _, room, err := daemon.SplitAddress(args[0]); err == nil &&
						!strings.EqualFold(room, strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))) {
						body["from"], body["foreign"] = me+"@"+os.Getenv("ATRIUM_ROOM"), true
					}
				}
			}
			got, err := callCard(boardURL, http.MethodPost, args[0], "exit", body)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "asked %s to exit\n", got.reached())
			return nil
		},
	}
	c.Flags().StringVar(&boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or the running daemon's)")
	c.Flags().BoolVar(&force, "force", false, "exit a card that is neither you nor one you launched (recorded on it)")
	return c
}

func newNewContextCmd() *cobra.Command {
	var boardURL string
	c := &cobra.Command{
		Use:   "new-context <who>",
		Short: "Cycle a card onto a fresh context, the way the board's new-context does.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			got, err := callCard(boardURL, http.MethodPost, args[0], "new-context", map[string]any{})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "asked %s for a new context\n", got.reached())
			return nil
		},
	}
	c.Flags().StringVar(&boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or the running daemon's)")
	return c
}
