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

// Saying the work is finished.
//
// This is the one thing an agent could not tell atrium. Everything it reported
// landed in `needs-input`, so the board could not tell "go and look at the
// result" from "answer me", and only a human moving a card by hand ever
// produced `done`.
//
// A command rather than a tool on purpose. A command is the one channel every
// runner already has: an agent that can run `ls` can run this, with no MCP
// server, no tool description and no cooperation from the harness. That
// matters more here than anywhere else, because this has to work for codex and
// for a bare shell and not only for the runner that happens to have a tool
// surface.
//
// Unlike the hooks, this one is NOT silent. It is run deliberately rather than
// on a hot path, by somebody or something that wants to know whether it
// worked, so it says so and exits non-zero when it did not.

// finishTimeout is generous, because this is not on any hot path. It runs once
// at the end of a piece of work.
const finishTimeout = 5 * time.Second

func newFinish() *cobra.Command {
	var (
		recap, name, hubURL, status string
		sha, noCommit, ask          string
		handBack                    bool
	)
	c := &cobra.Command{
		Use:   "finish [recap]",
		Short: "Say this session's work is finished, and what it did.",
		Long: "Moves this session's card to done and records what it says it did.\n\n" +
			"This is the one thing an agent could not tell atrium. Without it every report " +
			"lands in ready, so the board cannot tell \"go and look at the result\" from " +
			"\"answer me\", and only a human moving a card by hand produces done.\n\n" +
			"The recap is two or three sentences: what changed and what is worth knowing. Not a " +
			"transcript, and not a summary of how you got there. It can be the first argument, " +
			"the --recap flag, or piped in on stdin.\n\n" +
			"Which session this is comes from $ATRIUM_AGENT_NAME, or the current directory's " +
			"name, exactly like the hooks.\n\n" +
			"A session another session launched reports with this too, and its launcher is told. " +
			"--status done needs --sha (or --no-commit saying why there is none). --status " +
			"blocked and --status question need --ask. --status progress says you are stopping " +
			"on purpose while something runs. The same report as the atrium_report tool.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if recap == "" && len(args) > 0 {
				recap = strings.Join(args, " ")
			}
			if recap == "" {
				recap = pipedRecap()
			}
			if handBack {
				status = "needs-input"
			}
			return reportFinished(cmd.OutOrStdout(), hubURL, name, finishReport{
				Recap: recap, Status: status, SHA: sha, NoCommit: noCommit, Ask: ask,
			})
		},
	}
	c.Flags().StringVar(&status, "status", "",
		"done (the default), blocked, question or progress")
	c.Flags().StringVar(&sha, "sha", "", "for done: the commit the work landed as")
	c.Flags().StringVar(&noCommit, "no-commit", "", "for done with no commit: why there is none")
	c.Flags().StringVar(&ask, "ask", "", "for blocked or question: what you need, and from whom")
	c.Flags().StringVar(&recap, "recap", "",
		"what this session did, in two or three sentences")
	c.Flags().BoolVar(&handBack, "hand-back", false,
		"hand the work back without claiming it is finished. the card goes to ready, "+
			"with the recap attached")
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself (default: $ATRIUM_AGENT_NAME, or the directory name)")
	c.Flags().StringVar(&hubURL, "url", "",
		"atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

// pipedRecap reads a recap from stdin when something piped one in.
//
// Nothing is read from a terminal. A human typing `atrium finish` at a prompt
// with no argument means "mark it done, I have nothing to add", and waiting
// for them to type EOF would look like a hang.
func pipedRecap() string {
	if interactive() {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<16))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// finishReport is what `atrium finish` sends beside which session it is.
type finishReport struct {
	Recap, Status, SHA, NoCommit, Ask string
}

func reportFinished(out io.Writer, hubURL, name string, rep finishReport) error {
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
		"agent":     agent,
		"task_id":   os.Getenv("ATRIUM_TASK_ID"),
		"recap":     rep.Recap,
		"status":    rep.Status,
		"sha":       rep.SHA,
		"no_commit": rep.NoCommit,
		"ask":       rep.Ask,
	})
	if err != nil {
		return err
	}

	url := hubAddress(hubURL) + "/finish"
	client := &http.Client{Timeout: finishTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("atrium refused: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var answer struct {
		Recorded bool   `json:"recorded"`
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		// A worker's report, checked and passed on. See daemon/finish.go.
		Unverified   bool `json:"unverified"`
		LauncherTold bool `json:"launcher_told"`
	}
	_ = json.Unmarshal(raw, &answer)

	// An agent atrium has never heard of is not a failure, and saying so
	// plainly is better than a success message about a card that does not
	// exist.
	if !answer.Recorded {
		fmt.Fprintf(out, "atrium has no card for %s, so there was nothing to mark finished.\n", agent)
		return nil
	}
	fmt.Fprintf(out, "%s is %s\n", agent, orDefaultWord(answer.Status, "done"))
	if answer.Unverified {
		fmt.Fprintln(out, "  that commit is not in this worktree, so the card is flagged.")
	}
	if answer.LauncherTold {
		fmt.Fprintln(out, "  your launcher has been told.")
	}
	if rep.Recap == "" {
		fmt.Fprintln(out, "  no recap. the card will say so, which is worse than a short one.")
	}
	fmt.Fprintf(out, "  card %s\n", answer.TaskID)
	return nil
}

func orDefaultWord(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
