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

func newAsk() *cobra.Command {
	var name, hubURL string
	var working bool

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
			"--working if you are carrying on and would like an answer when somebody has one: " +
			"a session still working does not belong in a bucket of things needing attention.\n\n" +
			"Which session this is comes from $ATRIUM_AGENT_NAME, or the current directory's " +
			"name, exactly like the hooks.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ask := strings.TrimSpace(strings.Join(args, " "))
			if ask == "" {
				ask = pipedRecap()
			}
			if ask == "" {
				return fmt.Errorf("say what you need. an empty ask is what waiting already means")
			}
			return reportStuck(cmd.OutOrStdout(), hubURL, name, ask, !working)
		},
	}
	c.Flags().BoolVar(&working, "working", false,
		"you are carrying on rather than stopping, so do not file the card as waiting")
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself (default: $ATRIUM_AGENT_NAME, or the directory name)")
	c.Flags().StringVar(&hubURL, "url", "",
		"atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

func reportStuck(out io.Writer, hubURL, name, ask string, blocked bool) error {
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

	body, err := json.Marshal(map[string]any{
		"agent":   agent,
		"task_id": os.Getenv("ATRIUM_TASK_ID"),
		"ask":     ask,
		"blocked": blocked,
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
	if resp.StatusCode >= 300 {
		return fmt.Errorf("atrium refused: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var answer struct {
		Recorded bool `json:"recorded"`
		Waiting  bool `json:"waiting"`
	}
	_ = json.Unmarshal(raw, &answer)

	if !answer.Recorded {
		fmt.Fprintf(out, "atrium has no card for %s, so there was nowhere to put that.\n", agent)
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
