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

	"github.com/spf13/cobra"
)

// A session saying it is stuck, and what it needs.
//
// The other half of `atrium finish`. That one lets a session say its work is
// over. This lets it say the opposite, and say WHY, which is the part nothing
// else could carry.
//
// A stuck session already reaches `needs-input`, but by inference: a hook fires
// at the end of a turn and atrium concludes nobody is typing. So the board can
// say a card is waiting and cannot say what for, and the operator opens the
// terminal and reads back through the scrollback to find out. That is the thing
// the board exists to save them from.
//
// Named `ask` rather than `help` on the command line, because `atrium help` is
// cobra's own and always will be.
//
// `--peer` routes the question to another session instead of onto the card.
// The answer comes back through `atrium answer`, which is in `peer.go` with
// the rest of the bus it rides.

func newAsk() *cobra.Command {
	var name, hubURL, peer string
	var carryOn, working bool

	c := &cobra.Command{
		Use:   "ask [what you need]",
		Short: "Say this session is stuck, and what would unstick it.",
		Long: "Puts what you need on your card, where somebody is already looking.\n\n" +
			"A session that stops already shows as waiting, because a hook noticed nobody is " +
			"typing. That says THAT you stopped and never WHY, so the operator has to open " +
			"your terminal and read back through it to find out. This is how you tell them " +
			"instead.\n\n" +
			"Say what would unstick you, not what went wrong. \"which of these two schemas is " +
			"authoritative\" is useful. \"the build failed\" is what the terminal already says.\n\n" +
			"By default this means you have STOPPED, and the card moves to waiting. Pass " +
			"--continue if you are carrying on and would like an answer when somebody has one: " +
			"a session still working does not belong in a bucket of things needing attention.\n\n" +
			"Pass --peer <handle> to ask ANOTHER SESSION rather than a human. The question is " +
			"queued for it and arrives on its next tool call or at the end of its turn, and it " +
			"answers with `atrium answer`. Run `atrium peers` for the handles. Your card still " +
			"shows the question and says who you asked, so a peer that never answers is " +
			"visible rather than a session quietly stuck forever.\n\n" +
			"Which session this is comes from $ATRIUM_AGENT_NAME, or the current directory's " +
			"name, exactly like the hooks.",
		Args: cobra.ArbitraryArgs,
		RunE: speaksForItself(func(cmd *cobra.Command, args []string) error {
			ask := strings.TrimSpace(strings.Join(args, " "))
			if ask == "" {
				ask = pipedRecap()
			}
			if ask == "" {
				return fmt.Errorf("say what you need. an empty ask is what waiting already means")
			}
			return reportStuck(cmd.OutOrStdout(), hubURL, name, ask, peer, !(carryOn || working))
		}),
	}
	// --continue NAMES THE DECISION, NOT THE STATE.
	//
	// This was `--working`, which named the state the session is already in and
	// which the reader already knows, and which reads as a claim about being
	// busy rather than as a choice about what happens next. The only thing the
	// flag controls is whether the card is filed as waiting.
	//
	// It also fixes an asymmetry: `atrium ask "..."` and `atrium ask --working
	// "..."` were the pair, and nothing about the first said it stops. The pair
	// now reads as stop by default, carry on by request, which is what it is.
	c.Flags().BoolVar(&carryOn, "continue", false,
		"you are carrying on rather than stopping, so do not file the card as waiting")
	// The old name still works and is hidden, because it is written down in
	// prompts and scripts this repo cannot reach.
	c.Flags().BoolVar(&working, "working", false, "deprecated name for --continue")
	_ = c.Flags().MarkHidden("working")
	// The backquoted word is the ARGUMENT NAME, not emphasis. Cobra takes the
	// first one in a flag's usage and prints it beside the flag, so
	// "`atrium peers` lists the handles" rendered as `--peer atrium peers`.
	c.Flags().StringVar(&peer, "peer", "",
		"the `handle` of a session to ask instead of a human. atrium peers lists them")
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself (default: $ATRIUM_AGENT_NAME, or the directory name)")
	c.Flags().StringVar(&hubURL, "url", "",
		"atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

func reportStuck(out io.Writer, hubURL, name, ask, peer string, blocked bool) error {
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" {
		if cwd, err := os.Getwd(); err == nil {
			agent = filepath.Base(cwd)
		}
	}
	if agent == "" {
		return fmt.Errorf("could not work out which session this is. pass --name")
	}

	// THE TASK ID IS THIS SESSION'S, SO A NAMED SESSION MUST NOT SEND IT.
	//
	// The daemon resolves a card by id first, because an id is more reliable
	// than a name. `ATRIUM_TASK_ID` is in the environment of the session
	// running this, so with `--name` it names one session and the environment
	// names another, and the ask lands on the wrong card with nothing said.
	// Passing a name is saying which session this is about, so it wins.
	taskID := ""
	if name == "" {
		taskID = os.Getenv("ATRIUM_TASK_ID")
	}

	body, err := json.Marshal(map[string]any{
		"agent":   agent,
		"task_id": taskID,
		"ask":     ask,
		"blocked": blocked,
		"peer":    peer,
	})
	if err != nil {
		return err
	}

	url := hubAddress(hubURL) + "/help"
	resp, err := (&http.Client{Timeout: finishTimeout}).
		Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	var answer struct {
		Recorded bool      `json:"recorded"`
		Waiting  bool      `json:"waiting"`
		Peer     string    `json:"peer"`
		Error    string    `json:"error"`
		Peers    []peerRow `json:"peers"`
	}
	_ = json.Unmarshal(raw, &answer)

	// A handle nobody has answers with the ones that would have worked, and
	// nothing was recorded. Same as `tell`, and for the same reason: a bare
	// subcommand cannot make listing mandatory, so the failure has to teach.
	if resp.StatusCode == http.StatusNotFound && len(answer.Peers) > 0 {
		fmt.Fprintf(out, "%s\n", answer.Error)
		fmt.Fprintln(out, "\nthese are the sessions you can ask:")
		printPeers(out, answer.Peers)
		fmt.Fprintln(out, "\nnothing was asked. run it again with one of those, "+
			"or without --peer to ask a human.")
		return alreadySaid("no session called %s", peer)
	}
	if resp.StatusCode >= 300 {
		if answer.Error != "" {
			return fmt.Errorf("atrium refused: %s", answer.Error)
		}
		return fmt.Errorf("atrium refused: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	if !answer.Recorded {
		fmt.Fprintf(out, "atrium has no card for %s, so there was nowhere to put that.\n", agent)
		return nil
	}
	if answer.Peer != "" {
		// What the asker needs to know next, and the part a model gets wrong
		// on its own: nobody has been interrupted and no answer is coming back
		// in this turn.
		fmt.Fprintf(out, "asked %s. it arrives on that session's next tool call or at "+
			"the end of its turn, and the answer comes back the same way.\n", answer.Peer)
		if answer.Waiting {
			fmt.Fprintf(out, "  your card says you are waiting on %s, so it is visible "+
				"if no answer comes.\n", answer.Peer)
		}
		return nil
	}
	if answer.Waiting {
		fmt.Fprintf(out, "asked, and %s is now waiting for somebody.\n", agent)
		return nil
	}
	// Said plainly, because the difference matters to whoever is reading this
	// in a transcript later: the question was recorded and nobody was
	// interrupted.
	fmt.Fprintf(out, "asked. the card says what you need, and is not filed as waiting.\n")
	return nil
}
