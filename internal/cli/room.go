package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
//
// WHAT DOES TRAVEL IS A PERMISSION REQUEST, AND THE ANSWER BACK. An agent here
// that is blocked waiting for a human is invisible on the hub's board unless
// somebody opens this machine's board, which defeats the point of one board.
// So the report carries the pending requests, and the reply to a check-in
// carries whatever was decided about them.
//
// THE DECISION COMES BACK AND THE CHANNEL STAYS HERE, which is the one thing
// the design would not let go either way. `HandlePermission` blocks on a
// channel in THIS machine's daemon, so this posts the decision to that daemon
// and it unblocks its own request. The hub never answers on this machine's
// behalf, never learns whether the agent moved, and holds no record of the
// decision: the rule it may create and the history it lands in are both written
// here, by the daemon that was asked and will be asked again.

// roomBeat is how often to check in.
//
// Matched to the hub's own idea of it, which comes back in the answer to every
// check-in, so the two cannot drift and a room does not need configuring with
// something the hub already knows.
const roomBeat = 20 * time.Second

// roomBusyBeat is how often to check in while an agent on THIS machine is
// frozen waiting for somebody.
//
// The reply to a check-in is the only thing travelling from the hub to here, so
// it is what carries a decision, so how fast a decision arrives is how often
// this asks. At the ordinary beat, approving something on the hub's board would
// take up to twenty seconds to release the agent, which reads as a button that
// did nothing.
//
// Matched to the hub's own constant and reported in every reply, like the
// ordinary beat, so the two cannot drift.
const roomBusyBeat = 2 * time.Second

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
		res, err := checkIn(ctx, client, hubURL, local, o)
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
			// A machine with somebody frozen on it asks more often, because
			// asking is how a decision gets here. See `roomBusyBeat`.
			beat = roomBeat
			if res.perms > 0 {
				beat = roomBusyBeat
			}
			if o.Once {
				log.Printf("[atrium] checked in with %d card(s) and %d pending request(s), "+
					"and --once was asked for", res.cards, res.perms)
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

// checkInResult is what one check-in learned, which is only ever used to
// decide how soon to do the next one.
type checkInResult struct {
	cards int
	perms int
}

// checkIn reads this machine's cards and pending requests, tells the hub about
// them, and applies whatever the hub decided in the meantime.
//
// One round trip for both directions, on purpose. A separate endpoint for
// collecting decisions would be a second connection to keep working over an
// overlay, a second thing to back off on, and a second place for the two ends
// to disagree about whether a room is present.
func checkIn(ctx context.Context, client *http.Client, hubURL, local string, o roomOpts) (checkInResult, error) {
	cards, err := localCards(ctx, local)
	if err != nil {
		// REPORTED WITHOUT CARDS rather than skipped. A room whose own daemon
		// is down is exactly the thing the hub should be showing, and going
		// silent makes it look like the room is gone instead.
		log.Printf("[atrium] could not read this machine's cards: %v", err)
	}
	perms, err := localPerms(ctx, local)
	if err != nil {
		// Same posture, and it matters more here: the request this failed to
		// read is one an agent is frozen on. Reporting the cards without it is
		// still better than reporting nothing.
		log.Printf("[atrium] could not read what this machine is waiting to be allowed: %v", err)
	}
	out := checkInResult{cards: len(cards), perms: len(perms)}

	host, _ := os.Hostname()
	body, err := json.Marshal(map[string]any{
		"name": o.Name, "board": o.Board, "host": host,
		"version": VersionLine(), "cards": cards, "permissions": perms,
	})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/v1/rooms",
		bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return out, fmt.Errorf("the hub answered %s", res.Status)
	}

	var reply struct {
		Decisions []roomDecision `json:"decisions"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&reply); err != nil {
		// The report landed, so this is not a failed check-in, but a reply
		// that cannot be read is a hub no decision can arrive from. Backing
		// off on it is right and costs nothing: the cards are already there.
		return out, fmt.Errorf("the hub's answer could not be read: %w", err)
	}
	for _, d := range reply.Decisions {
		applyDecision(ctx, local, d)
	}
	return out, nil
}

// roomDecision is one answer the hub is handing over. It mirrors
// `daemon.RoomDecision` and is the body of this machine's own decide endpoint,
// because that is what happens to it.
type roomDecision struct {
	Perm     string `json:"permission_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Command  string `json:"command,omitempty"`
	Forever  bool   `json:"forever,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// applyDecision posts a decision made on the hub's board to THIS machine's own
// daemon.
//
// This is the hop that releases the agent, and there is no other one. The
// request is blocked on a channel in that daemon's process, so a decision has
// to be delivered to it and nothing the hub does can substitute.
//
// NOT RETRIED, and never fatal. The daemon may refuse it, and the interesting
// refusal is the correct one: somebody answered the same request on this
// machine's own board first, and it says so with a conflict. If the refusal was
// something else and the request is still pending, the next report still
// carries it and the hub offers it again once its own decision expires, so
// retrying here would only race that.
func applyDecision(ctx context.Context, local string, d roomDecision) {
	if strings.TrimSpace(d.Perm) == "" || strings.TrimSpace(d.Decision) == "" {
		log.Printf("[atrium] the hub sent a decision with nothing in it, ignoring")
		return
	}
	body, err := json.Marshal(map[string]any{
		"decision": d.Decision, "reason": d.Reason, "command": d.Command,
		"forever": d.Forever, "prefix": d.Prefix, "kind": d.Kind,
	})
	if err != nil {
		log.Printf("[atrium] could not pass on the hub's decision: %v", err)
		return
	}
	url := strings.TrimRight(local, "/") + "/v1/permissions/" + neturl.PathEscape(d.Perm) + "/decide"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[atrium] could not pass on the hub's decision: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Printf("[atrium] could not pass on the hub's decision: %v", err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		what, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		log.Printf("[atrium] this machine refused the hub's %s of %s: %s %s",
			d.Decision, d.Perm, res.Status, strings.TrimSpace(string(what)))
		return
	}
	// SAID EVERY TIME. One line per decision a human made is not noise, it is
	// the only local record that something on another board released an agent
	// here.
	extra := ""
	if d.Forever {
		extra = fmt.Sprintf(", and made a standing rule for %q", d.Prefix)
	}
	// "approve" plus "d" is "approved" and "block" plus "d" is "blockd". Past
	// tense is a lookup, not a suffix.
	past := map[string]string{"approve": "approved", "block": "blocked"}[d.Decision]
	if past == "" {
		past = d.Decision
	}
	log.Printf("[atrium] the hub %s %s%s", past, d.Perm, extra)
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

// roomPerm is one pending request as it is reported. It mirrors
// `daemon.RoomPerm` and is declared here for the same reason `roomCard` is: so
// this command does not pull the daemon package in.
type roomPerm struct {
	ID      string `json:"id"`
	TaskID  string `json:"task_id,omitempty"`
	Tool    string `json:"tool"`
	Command string `json:"command"`
	Agent   string `json:"agent,omitempty"`
	Details string `json:"details,omitempty"`
	Waiting int    `json:"waiting_seconds"`
}

// roomMaxPerms and roomMaxDetails bound what one report carries.
//
// The hub enforces the same two numbers, and this is the end that decides them:
// a report has to fit in one POST, the hub reads at most a megabyte of it, and
// a report refused for being too big takes this machine's CARDS off the board
// with it. So a large diff is cut short here rather than being allowed to make
// the whole machine look gone.
//
// The count matters for a different reason than the size. Fifty text boxes is
// already more than anybody is going to read.
const (
	roomMaxPerms   = 50
	roomMaxDetails = 4000
)

// localPerms asks this machine's own daemon what is frozen waiting for a human.
//
// Over loopback and through the same JSON API the board uses, exactly like
// `localCards` and for the same reasons.
//
// THE WAIT IS SENT AS SECONDS, not as the timestamp this reads. The hub draws
// it against ITS clock, and two machines that disagree by a minute would put a
// request on the board as frozen a minute before it was made. Seconds computed
// here, against the clock that recorded the request, cannot be wrong that way.
// It is the same rule `wait_seconds` on a card already follows.
func localPerms(ctx context.Context, local string) ([]roomPerm, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(local, "/")+"/v1/permissions", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var body struct {
		Permissions []struct {
			ID          string `json:"id"`
			TaskID      string `json:"task_id"`
			Tool        string `json:"tool"`
			Command     string `json:"command"`
			RequestedAt string `json:"requested_at"`
			Details     string `json:"details"`
			Agent       string `json:"agent"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]roomPerm, 0, len(body.Permissions))
	for _, p := range body.Permissions {
		if len(out) >= roomMaxPerms {
			break
		}
		waited := 0
		if at, err := time.Parse(time.RFC3339, p.RequestedAt); err == nil {
			// Never negative. A request cannot have been made in the future,
			// and sending a negative would only put the impossible number on
			// the wire for the hub to clamp.
			if d := now.Sub(at); d > 0 {
				waited = int(d / time.Second)
			}
		}
		out = append(out, roomPerm{
			ID: p.ID, TaskID: p.TaskID, Tool: p.Tool, Command: p.Command,
			Agent: p.Agent, Details: clip(p.Details, roomMaxDetails), Waiting: waited,
		})
	}
	return out, nil
}

// clip cuts a string to at most n bytes without cutting a character in half.
//
// A diff is arbitrary text and a byte slice through a multi-byte character
// produces invalid UTF-8, which `encoding/json` writes as a replacement
// character. One of those at the end of a truncated diff is harmless, but it is
// avoidable in two lines.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
