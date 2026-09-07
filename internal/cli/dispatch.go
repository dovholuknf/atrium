package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// Handing an item to another machine.
//
// The counterpart of `atrium launch`, and the difference is the whole point:
// `launch` starts a runner HERE, in a directory this machine has. This queues
// one for a room, and the room starts it when it next checks in, which is
// within twenty seconds.
//
// NOTHING IS VALIDATED AGAINST THIS MACHINE, and that is deliberate rather than
// lazy. The runner id belongs to that room's harness table and the directory,
// if one is given at all, belongs to that room's filesystem. Checking either
// here would refuse a correct dispatch because a cloud box has a runner this
// desktop does not, which is the normal case. The room refuses, in its own
// words, and the refusal comes back onto the queue row.
//
// THE ORDINARY DISPATCH NAMES NO DIRECTORY. A room with one checkout points its
// runner at it once, and every item queued for that room lands there. `--dir`
// is for a room started with `--workspace`, and is refused by any room that was
// not.

func newDispatch() *cobra.Command {
	c := &cobra.Command{
		Use:   "dispatch",
		Short: "Queue work for another machine, and see what is waiting.",
		Long: "Hands an item to a room: it sits on this hub until that machine checks in, and " +
			"rides back down the reply to the check-in it was already making. Nothing dials " +
			"the room, so a machine behind NAT needs no configuring.\n\n" +
			"A room that is switched off is still a room you can queue for. It collects the " +
			"queue when it comes back.\n\n" +
			"ATRIUM DOES NOT MAKE THE DIRECTORY. Send no directory and the room's own runner " +
			"answers, which is the ordinary case. Send --dir and only a room started with " +
			"--workspace will take it. Either way, a directory that is not there on that " +
			"machine is refused before anything starts, and the reason lands on the row.",
	}
	c.AddCommand(newDispatchTo(), newDispatchList(), newDispatchCancel())
	return c
}

type dispatchOpts struct {
	room, harness, dir, title, prompt, why, window, boardURL string
	tags                                                     []string
	quiet                                                    bool
}

func newDispatchTo() *cobra.Command {
	var o dispatchOpts
	c := &cobra.Command{
		Use:   "to <room>",
		Short: "Queue a launch for one room.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.room = args[0]
			return dispatchTo(o)
		},
	}
	c.Flags().StringVar(&o.harness, "runner", "claude",
		"which runner to start THERE, named as that machine names it")
	c.Flags().StringVar(&o.dir, "dir", "",
		"a directory on that machine. only a room started with --workspace will take one")
	c.Flags().StringVar(&o.title, "title", "", "what to show on the card that machine makes")
	c.Flags().StringVar(&o.prompt, "prompt", "", "the first instruction that session gets")
	c.Flags().StringVar(&o.why, "why", "", "what this is for, read back later")
	c.Flags().StringSliceVar(&o.tags, "tags", nil, "what you call this work, comma separated")
	c.Flags().StringVar(&o.window, "window", "", "which pile the card belongs to on that board")
	c.Flags().BoolVar(&o.quiet, "quiet", false, "print only the queued item's id")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func dispatchTo(o dispatchOpts) error {
	if strings.TrimSpace(o.room) == "" {
		return fmt.Errorf("say which room this is for. `atrium dispatch list` shows what has " +
			"checked in")
	}
	body, err := json.Marshal(map[string]any{
		"room": o.room, "harness": o.harness, "cwd": o.dir, "title": o.title,
		"prompt": o.prompt, "why": o.why, "tags": o.tags, "window": o.window,
	})
	if err != nil {
		return err
	}
	raw, err := dispatchPost(o.boardURL, http.MethodPost, "/v1/dispatch", body)
	if err != nil {
		return err
	}
	var item struct {
		ID   string `json:"id"`
		Room string `json:"room"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}
	if o.quiet {
		fmt.Println(item.ID)
		return nil
	}
	fmt.Printf("queued for %s\n", item.Room)
	fmt.Printf("  item  %s\n", item.ID)
	// Said because the alternative is watching a queue row and wondering. A
	// room checks in every twenty seconds, and one that is switched off holds
	// the item until it is not.
	fmt.Printf("  when  next time that room checks in, which is within %s if it is up\n",
		roomBeat)
	return nil
}

func newDispatchList() *cobra.Command {
	var boardURL string
	var all bool
	c := &cobra.Command{
		Use:   "list",
		Short: "What is queued for other machines, and what came of it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := dispatchPost(boardURL, http.MethodGet, "/v1/dispatch", nil)
			if err != nil {
				return err
			}
			var body struct {
				Dispatches []struct {
					ID      string `json:"id"`
					Room    string `json:"room"`
					Harness string `json:"harness"`
					Title   string `json:"title"`
					State   string `json:"state"`
					CardID  string `json:"card_id"`
					Error   string `json:"error"`
				} `json:"dispatches"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ITEM\tROOM\tRUNNER\tSTATE\tWHAT")
			shown := 0
			for _, d := range body.Dispatches {
				open := d.State == "queued" || d.State == "claimed"
				if !all && !open {
					continue
				}
				shown++
				what := d.Title
				if d.Error != "" {
					what = d.Error
				} else if d.CardID != "" {
					what = "card " + d.CardID
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					d.ID, d.Room, d.Harness, d.State, what)
			}
			w.Flush()
			if shown == 0 {
				if all {
					fmt.Println("nothing has ever been queued for another machine here")
				} else {
					fmt.Println("nothing is waiting. --all shows what has already been settled")
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include items that have already been settled")
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func newDispatchCancel() *cobra.Command {
	var boardURL string
	c := &cobra.Command{
		Use:   "cancel <item>",
		Short: "Withdraw an item no room has taken yet.",
		Long: "Only works while the item is still waiting. Once a room has claimed one there " +
			"is no way to reach in and stop it from here: the hub never dials a room. Stop " +
			"it on that machine instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := dispatchPost(boardURL, http.MethodDelete,
				"/v1/dispatch/"+args[0], nil); err != nil {
				return err
			}
			fmt.Printf("withdrew %s\n", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

// dispatchPost is the one HTTP shape all three of these share, including how
// the daemon's own refusal is surfaced instead of a status code.
func dispatchPost(boardURL, method, path string, body []byte) ([]byte, error) {
	url := boardAddress(boardURL) + path
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("atrium refused: %s", e.Error)
		}
		return nil, fmt.Errorf("atrium refused: %s", resp.Status)
	}
	return raw, nil
}
