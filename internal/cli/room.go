package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/spf13/cobra"
)

// A machine reporting itself into somebody else's board.
//
// `docs/federation-design-v2.md` settled the shape: LEAVES DIAL OUT AND THE
// FORUM HOLDS NOTHING DURABLE. This is the leaf. It reads its own daemon's
// cards over loopback and posts a summary to the hub on a timer, and that is
// the whole of it.
//
// IT DIALS OUT BECAUSE OF REACHABILITY, and which end is behind NAT decides
// nothing about the design. The design assumes the leaf is the awkward one, a
// laptop on a coffee shop network. On the operator's own machines it is
// inverted: the rooms are public cloud instances and the HUB is the desktop
// behind NAT. The answer is the same either way, because `--service` dials the
// hub over an OpenZiti service and neither end has to be reachable from the
// other. Both dial the network.
//
// WHAT THIS DOES NOT DO IS FEDERATE ATTACH. A pseudo terminal cannot leave the
// machine that made it. What travels is cards, their status, and what each is
// waiting for. Typing into a runner on this machine means opening this
// machine's own board, which is why the report carries `--board`.

// roomBeat is how often to check in.
//
// Matched to the hub's own idea of it, which comes back in the answer to every
// check-in, so the two cannot drift and a room does not need configuring with
// something the hub already knows.
const roomBeat = 20 * time.Second

// roomBackoff is the wait after a failed check-in.
//
// Longer than the heartbeat, and this is the same posture every other outbound
// thing in atrium takes: a hub that is down is not this room's problem, and a
// room that retries hard turns one unreachable machine into a busy one. It
// keeps trying forever, quietly, because the hub coming back should need no
// action here.
const roomBackoff = time.Minute

// roomQuiet is how long to go between repeating the same complaint.
//
// One line when a hub goes away, then silence. A log that says the same thing
// every twenty seconds for an hour is a log nobody reads the rest of.
const roomQuiet = 10 * time.Minute

func newRoom() *cobra.Command {
	var hub, name, board, service, identity, local string
	var once bool

	c := &cobra.Command{
		Use:   "room",
		Short: "Report this machine's cards into another atrium's board.",
		Long: "Turns this machine into a ROOM: it keeps its own daemon, its own database and " +
			"its own terminals, and tells a hub what is on it.\n\n" +
			"The hub holds nothing durable. What it gets is a summary, replaced on every " +
			"check-in, that goes stale and then disappears when this room stops talking. " +
			"This machine remains the truth about itself.\n\n" +
			"IT DOES NOT FEDERATE TERMINALS. A pseudo terminal cannot leave the machine that " +
			"made it, so attaching to a runner here means opening this machine's own board. " +
			"Pass --board so the hub can offer that link.\n\n" +
			"Reaching the hub: --hub for an ordinary address, or --service and --identity to " +
			"dial it over an OpenZiti service, which is what you want when neither end can " +
			"reach the other directly.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoom(cmd.Context(), roomOpts{
				Hub: hub, Name: name, Board: board,
				Service: service, Identity: identity, Local: local, Once: once,
			})
		},
	}
	c.Flags().StringVar(&hub, "hub", "", "the hub's board address, for an ordinary connection")
	c.Flags().StringVar(&service, "service", "",
		"an OpenZiti service to reach the hub on, instead of --hub")
	c.Flags().StringVar(&identity, "identity", "",
		"the ziti identity file to dial that service with")
	c.Flags().StringVar(&name, "name", "", "what to call this room (default: this machine's hostname)")
	c.Flags().StringVar(&board, "board", "",
		"where a browser should go to reach THIS machine directly, since terminals do not federate")
	c.Flags().StringVar(&local, "local", "", "this machine's own daemon (default: $ATRIUM_BOARD_URL or localhost:7778)")
	c.Flags().BoolVar(&once, "once", false, "check in a single time and exit, for testing the path")
	return c
}

type roomOpts struct {
	Hub, Name, Board, Service, Identity, Local string
	Once                                       bool
}

func runRoom(ctx context.Context, o roomOpts) error {
	if strings.TrimSpace(o.Hub) == "" && strings.TrimSpace(o.Service) == "" {
		return fmt.Errorf("say where the hub is: --hub for an address, or --service with " +
			"--identity to dial it over ziti")
	}
	if strings.TrimSpace(o.Service) != "" && strings.TrimSpace(o.Identity) == "" {
		return fmt.Errorf("--service needs --identity: dialling a ziti service means being " +
			"somebody on that network")
	}
	if strings.TrimSpace(o.Name) == "" {
		host, _ := os.Hostname()
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("this machine has no hostname, so --name is not optional here")
		}
		o.Name = host
	}
	local := strings.TrimSpace(o.Local)
	if local == "" {
		local = boardAddress("")
	}

	client, hubURL, err := hubClient(o)
	if err != nil {
		return err
	}
	log.Printf("[atrium] room %q reporting to %s every %s", o.Name, hubURL, roomBeat)

	beat := roomBeat
	var lastMoan time.Time
	for {
		n, err := checkIn(ctx, client, hubURL, local, o)
		switch {
		case err != nil:
			// Rate limited, and never fatal. See `roomQuiet`.
			if time.Since(lastMoan) > roomQuiet {
				log.Printf("[atrium] could not reach the hub, still trying: %v", err)
				lastMoan = time.Now()
			}
			beat = roomBackoff
		default:
			if !lastMoan.IsZero() {
				log.Printf("[atrium] the hub is back")
				lastMoan = time.Time{}
			}
			beat = roomBeat
			if o.Once {
				log.Printf("[atrium] checked in with %d card(s), and --once was asked for", n)
				return nil
			}
		}
		if o.Once {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(beat):
		}
	}
}

// hubClient builds the thing that talks to the hub, over whichever transport
// was asked for.
//
// The ziti case replaces the transport's DIAL rather than the client, so
// everything above it is ordinary `net/http` against an ordinary URL. The
// service name becomes the host, which is what makes `http://atrium-board/v1`
// work without anything else in this file knowing an overlay is involved.
func hubClient(o roomOpts) (*http.Client, string, error) {
	if strings.TrimSpace(o.Service) == "" {
		return &http.Client{Timeout: 20 * time.Second}, strings.TrimRight(o.Hub, "/"), nil
	}

	cfg, err := ziti.NewConfigFromFile(o.Identity)
	if err != nil {
		return nil, "", fmt.Errorf("could not load %s: %w", o.Identity, err)
	}
	zctx, err := ziti.NewContext(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("could not use that identity: %w", err)
	}
	svc := strings.TrimSpace(o.Service)
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// The address is ignored on purpose. A ziti dial is to a SERVICE,
			// not to a host and port, and honouring the URL's host here would
			// let a redirect or a typo send the connection somewhere else.
			return zctx.Dial(svc)
		},
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}, "http://" + svc, nil
}

// checkIn reads this machine's cards and tells the hub about them.
func checkIn(ctx context.Context, client *http.Client, hubURL, local string, o roomOpts) (int, error) {
	cards, err := localCards(ctx, local)
	if err != nil {
		// REPORTED WITHOUT CARDS rather than skipped. A room whose own daemon
		// is down is exactly the thing the hub should be showing, and going
		// silent makes it look like the room is gone instead.
		log.Printf("[atrium] could not read this machine's cards: %v", err)
	}
	host, _ := os.Hostname()
	body, err := json.Marshal(map[string]any{
		"name": o.Name, "board": o.Board, "host": host,
		"version": VersionLine(), "cards": cards,
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/v1/rooms",
		bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return 0, fmt.Errorf("the hub answered %s", res.Status)
	}
	return len(cards), nil
}

// roomCard is the summary sent for one card. It mirrors `daemon.RoomCard` and
// is declared here rather than imported so this command does not pull the
// daemon package in.
type roomCard struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Waiting int    `json:"waiting_seconds,omitempty"`
	Runner  string `json:"runner,omitempty"`
	Doing   string `json:"doing,omitempty"`
}

// localCards asks this machine's own daemon what is on it.
//
// Over loopback and through the same JSON API a browser uses, rather than by
// opening the database. Two processes on one sqlite file is a way to find out
// what sqlite does about locking, and the daemon is already answering this
// question for the board.
func localCards(ctx context.Context, local string) ([]roomCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(local, "/")+"/v1/tasks", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var body struct {
		Tasks []struct {
			ID       string `json:"id"`
			Title    string `json:"display_title"`
			Status   string `json:"status"`
			Wait     int    `json:"wait_seconds"`
			Runner   string `json:"runner"`
			Activity struct {
				What string `json:"what"`
			} `json:"activity"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]roomCard, 0, len(body.Tasks))
	for _, t := range body.Tasks {
		out = append(out, roomCard{
			ID: t.ID, Title: t.Title, Status: t.Status,
			Waiting: t.Wait, Runner: t.Runner, Doing: t.Activity.What,
		})
	}
	return out, nil
}
