package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Sessions that can address each other.
//
// Two commands rather than a tool, for the reason `atrium finish` is a command:
// it is the one channel every runner already has. An agent that can run `ls`
// can run these, with no MCP server and no cooperation from the harness, which
// matters because the sessions worth introducing to each other are not all
// claude.
//
// The cost of that choice, stated plainly because `docs/charon.md` names it:
// their version makes listing MANDATORY before sending, enforced by the tool
// description. A bare subcommand enforces nothing. So `tell` re-derives it: a
// handle that does not resolve comes back with the list of ones that would
// have worked, which turns the failure into the discovery.

const peerTimeout = 5 * time.Second

func newPeers() *cobra.Command {
	var name, hubURL, since string
	var fleet bool
	c := &cobra.Command{
		Use:   "peers",
		Short: "List the other sessions, most wanting a human first.",
		Long: "Every session atrium knows about that is still going, grouped by what it wants " +
			"from you: stopped and asking, held at the permission gate, asking while it " +
			"carries on, or gone quiet with nothing recorded.\n\n" +
			"Use the handle with `atrium tell`. A peer with things already waiting is one " +
			"to leave alone.\n\n" +
			"`--fleet` answers the dispatcher's question instead of the addressing one. It " +
			"adds the sessions that have FINISHED, which are not addressable and are the " +
			"ones you would otherwise find out about by asking them one at a time.\n\n" +
			"A session that FINISHED counts for twelve hours, which is about a shift. " +
			"`--since 3h` narrows it to what has ended since you last looked, and everything " +
			"that ever ran here is a different question the board's history answers.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listPeers(cmd.OutOrStdout(), hubURL, name, fleet, since)
		},
	}
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself, so it is left out of its own list")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address")
	c.Flags().BoolVar(&fleet, "fleet", false,
		"which of these want me: include the sessions that have finished")
	c.Flags().StringVar(&since, "since", "",
		"how far back a finished session still counts, as a duration. default 12h")
	return c
}

func newTell() *cobra.Command {
	var name, hubURL string
	c := &cobra.Command{
		Use:   "tell <handle> <message>",
		Short: "Say something to another session.",
		Long: "Queues a message for another session. It arrives on that session's next tool " +
			"call or at the end of its turn, so this is not a conversation and there is no " +
			"reply to wait for.\n\n" +
			"The message is labeled with who sent it, and the receiving session is told it " +
			"came from a peer rather than from the human. Run `atrium peers` first if you do " +
			"not know the handle.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			to := args[0]
			text := strings.Join(args[1:], " ")
			if strings.TrimSpace(text) == "" {
				text = pipedRecap()
			}
			return tellPeer(cmd.OutOrStdout(), hubURL, name, to, text)
		},
	}
	c.Flags().StringVar(&name, "name", "", "what this session calls itself")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address")
	return c
}

// whoAmI works out this session's handle the same way every hook does, so a
// session is one name everywhere.
func whoAmI(name string) string {
	if name != "" {
		return name
	}
	if n := os.Getenv("ATRIUM_AGENT_NAME"); n != "" {
		return n
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Base(cwd)
	}
	return ""
}

type peerRow struct {
	Handle   string `json:"handle"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
	Why      string `json:"why"`
	Waiting  int    `json:"waiting"`
	// What this card wants from a human, and the same thing in a sentence.
	Want string `json:"want"`
	Note string `json:"note"`
	// Recap is what a finished session said it did.
	Recap string `json:"recap"`
	// Seconds is how long it has wanted that.
	Seconds int64 `json:"seconds"`
}

// The buckets, in the order the daemon ranks them, with a heading each.
//
// Held here as well as in the daemon rather than printed from the wire, because
// the heading is what makes the four confusable states read differently and it
// is a sentence, not a field. An unknown bucket from a newer daemon still
// prints, under its own name, at the end.
var peerBuckets = []struct{ want, heading string }{
	{"blocked", "STOPPED AND ASKING. These cannot go on until you answer."},
	{"permission", "HELD AT THE GATE. A decision each, and they are stopped too."},
	{"question", "ASKED WHILE STILL WORKING. Worth an answer, not an interruption."},
	{"finished", "FINISHED. These want reading, not answering."},
	{"quiet", "NOTHING RECORDED. No recap, no question, no word. Go and look."},
	{"working", "WORKING. Nothing to do about these."},
}

func listPeers(out io.Writer, hubURL, name string, fleet bool, since string) error {
	url := hubAddress(hubURL) + "/peers?me=" + neturl.QueryEscape(whoAmI(name))
	if fleet {
		url += "&fleet=1"
	}
	if strings.TrimSpace(since) != "" {
		// Checked here so a typo is a refusal rather than a list that quietly
		// used the default window and looks like the answer.
		if _, err := time.ParseDuration(since); err != nil {
			return fmt.Errorf("--since %q is not a duration. try 3h, 90m, 45s", since)
		}
		url += "&since=" + neturl.QueryEscape(since)
	}
	client := &http.Client{Timeout: peerTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	var body struct {
		Peers []peerRow `json:"peers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return err
	}
	if len(body.Peers) == 0 {
		fmt.Fprintln(out, "no other sessions are running.")
		return nil
	}
	printPeers(out, body.Peers)
	return nil
}

// printPeers draws the list grouped by what each card wants.
//
// GROUPED RATHER THAN SORTED, because a sorted list of sixteen still has to be
// read from the top to find out where the part that wants you stops. A heading
// says how many there are and what answering one of them means, and the
// dispatcher reads the first group and stops.
func printPeers(out io.Writer, peers []peerRow) {
	width := 0
	for _, p := range peers {
		if len(p.Handle) > width {
			width = len(p.Handle)
		}
	}

	seen := map[string]bool{}
	first := true
	group := func(want, heading string) {
		var rows []peerRow
		for _, p := range peers {
			if p.Want == want {
				rows = append(rows, p)
			}
		}
		if len(rows) == 0 {
			return
		}
		if !first {
			fmt.Fprintln(out)
		}
		first = false
		fmt.Fprintf(out, "%s  (%d)\n", heading, len(rows))
		for _, p := range rows {
			printPeerRow(out, width, p)
		}
	}
	for _, b := range peerBuckets {
		seen[b.want] = true
		group(b.want, b.heading)
	}
	// A bucket this binary has never heard of still prints. An older CLI
	// against a newer daemon drops a whole group otherwise, silently, which is
	// the worst way for this to be wrong.
	for _, p := range peers {
		if !seen[p.Want] {
			seen[p.Want] = true
			group(p.Want, strings.ToUpper(p.Want))
		}
	}
}

func printPeerRow(out io.Writer, width int, p peerRow) {
	// The note is what the daemon decided this card wants, in a sentence. It
	// falls back to the operator's `why` and then to the path, which is what
	// this printed before there was anything better.
	what := p.Note
	if what == "" {
		what = p.Why
	}
	if what == "" {
		what = p.Worktree
	}
	if p.Recap != "" {
		what += ": " + oneLine(p.Recap)
	}
	age := ""
	if p.Seconds > 0 {
		age = "  " + shortAge(p.Seconds)
	}
	queued := ""
	if p.Waiting > 0 {
		queued = fmt.Sprintf("  [%d waiting]", p.Waiting)
	}
	fmt.Fprintf(out, "  %-*s  %s%s%s\n", width, p.Handle, oneLine(what), age, queued)
}

// oneLine flattens and bounds a field that may hold a paragraph.
//
// A recap is up to two thousand characters and an ask up to five hundred, and
// both are drawn here in a list somebody is scanning. Cut to a width a terminal
// holds rather than wrapped: the rest is on the card.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 90
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

// shortAge writes seconds the way a person says it. Matches what the daemon
// puts in its own notes, so one line does not carry two spellings of an age.
func shortAge(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		if m := sec % 3600 / 60; m > 0 {
			return fmt.Sprintf("%dh%dm", sec/3600, m)
		}
		return fmt.Sprintf("%dh", sec/3600)
	default:
		return fmt.Sprintf("%dd", sec/86400)
	}
}

func tellPeer(out io.Writer, hubURL, name, to, text string) error {
	from := whoAmI(name)
	if from == "" {
		return fmt.Errorf("could not work out which session this is. pass --name")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("there is nothing to say. put the message after the handle")
	}

	body, err := json.Marshal(map[string]string{"from": from, "to": to, "text": text})
	if err != nil {
		return err
	}
	url := hubAddress(hubURL) + "/tell"
	client := &http.Client{Timeout: peerTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var answer struct {
		Queued bool      `json:"queued"`
		Note   string    `json:"note"`
		Error  string    `json:"error"`
		Peers  []peerRow `json:"peers"`
	}
	_ = json.Unmarshal(raw, &answer)

	if resp.StatusCode == http.StatusNotFound {
		// The failure IS the discovery. A model that guessed a handle now has
		// the set that would have worked, in the same breath.
		fmt.Fprintf(out, "%s\n", answer.Error)
		if len(answer.Peers) > 0 {
			fmt.Fprintln(out, "\nthese are the sessions you can tell:")
			printPeers(out, answer.Peers)
		}
		return fmt.Errorf("nothing was sent")
	}
	if resp.StatusCode >= 300 {
		if answer.Error != "" {
			return fmt.Errorf("atrium refused: %s", answer.Error)
		}
		return fmt.Errorf("atrium refused: %s", strings.TrimSpace(string(raw)))
	}

	fmt.Fprintf(out, "told %s.\n", to)
	if answer.Note != "" {
		fmt.Fprintf(out, "  %s\n", answer.Note)
	}
	return nil
}
