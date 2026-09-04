package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	var name, hubURL string
	c := &cobra.Command{
		Use:   "peers",
		Short: "List the other sessions this one can talk to.",
		Long: "Every session atrium knows about that is still going, with its handle, what it " +
			"is working on and how much is already queued for it.\n\n" +
			"Use the handle with `atrium tell`. A peer with things already waiting is one " +
			"to leave alone.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listPeers(cmd.OutOrStdout(), hubURL, name)
		},
	}
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself, so it is left out of its own list")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address")
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
}

func listPeers(out io.Writer, hubURL, name string) error {
	url := hubAddress(hubURL) + "/peers?me=" + whoAmI(name)
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

func printPeers(out io.Writer, peers []peerRow) {
	width := 0
	for _, p := range peers {
		if len(p.Handle) > width {
			width = len(p.Handle)
		}
	}
	for _, p := range peers {
		what := p.Why
		if what == "" {
			what = p.Worktree
		}
		queued := ""
		if p.Waiting > 0 {
			queued = fmt.Sprintf("  [%d waiting]", p.Waiting)
		}
		fmt.Fprintf(out, "%-*s  %-16s %s%s\n", width, p.Handle, p.Status, what, queued)
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
