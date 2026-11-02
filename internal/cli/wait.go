package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `atrium wait` blocks until the board, the room link and the named rooms answer, so a restart or a deploy is one
// command to wait on instead of a loop of polls. It exits non-zero naming whatever never did.

func newWait() *cobra.Command {
	var boardURL, rooms string
	var timeout, every time.Duration
	c := &cobra.Command{
		Use:   "wait",
		Short: "Block until the board, the room link and the named rooms answer.",
		Long: "Polls the hub's board until it answers, then until at least one room is linked, then until each " +
			"room named by --rooms is linked and answering. Prints each as it comes up. Exits non-zero, naming " +
			"what never did, when --timeout runs out.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var want []string
			for _, r := range strings.Split(rooms, ",") {
				if r = strings.TrimSpace(r); r != "" {
					want = append(want, r)
				}
			}
			return waitReady(cmd.OutOrStdout(), boardAddress(boardURL), want, timeout, every)
		},
	}
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	c.Flags().StringVar(&rooms, "rooms", "", "comma separated rooms that must be linked and answering")
	c.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "give up after this long")
	c.Flags().DurationVar(&every, "every", time.Second, "how often to ask")
	return c
}

type waitHealth struct {
	Rooms      int      `json:"rooms"`
	RoomNames  []string `json:"room_names"`
	RoomsQuiet []string `json:"rooms_quiet"`
}

func readHealth(client *http.Client, board string) (waitHealth, bool) {
	var h waitHealth
	res, err := client.Get(board + "/v1/health")
	if err != nil {
		return h, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return h, false
	}
	return h, json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&h) == nil
}

func waitReady(out io.Writer, board string, want []string, timeout, every time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	up := map[string]bool{}
	say := func(what string) {
		if !up[what] {
			up[what] = true
			fmt.Fprintf(out, "up: %s\n", what)
		}
	}
	for {
		var missing []string
		h, ok := readHealth(client, board)
		if ok {
			say("board")
		} else {
			missing = append(missing, "board")
		}
		if ok && h.Rooms > 0 {
			say("room link")
		} else {
			missing = append(missing, "room link")
		}
		quiet := map[string]bool{}
		for _, q := range h.RoomsQuiet {
			quiet[q] = true
		}
		linked := map[string]bool{}
		for _, n := range h.RoomNames {
			linked[n] = !quiet[n]
		}
		for _, r := range want {
			if ok && linked[r] {
				say("room " + r)
			} else {
				missing = append(missing, "room "+r)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if !time.Now().Add(every).Before(deadline) {
			sort.Strings(missing)
			fmt.Fprintf(out, "never came up: %s\n", strings.Join(missing, ", "))
			return errAlreadySaid
		}
		time.Sleep(every)
	}
}
