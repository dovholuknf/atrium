package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

type tangentOpts struct {
	boardURL, runner, prompt, why string
	quiet                         bool
}

func newTangent() *cobra.Command {
	var o tangentOpts
	c := &cobra.Command{
		Use:   "tangent <name>",
		Short: "Start a card in a named scratch folder, no repo, reused when the name is already there.",
		Long: "Makes <git.scm_root>/scratch/tangents/<name> and starts a card in it in the tangent window. " +
			"The name is made a folder name: every run of other characters becomes one dash. A folder of " +
			"that name already there is worked in again, and closing the card leaves it. A live card " +
			"already in it is answered rather than a second one started.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tangentAgent(o, args[0])
		},
	}
	c.Flags().StringVar(&o.runner, "runner", "", "which configured runner to start (default: the room's)")
	c.Flags().StringVar(&o.prompt, "prompt", "", "the first instruction the runner gets")
	c.Flags().StringVar(&o.why, "why", "", "what this is for, read back later")
	c.Flags().BoolVar(&o.quiet, "quiet", false, "print only the card id")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func tangentAgent(o tangentOpts, name string) error {
	body, err := json.Marshal(map[string]any{"name": name, "harness": o.runner, "prompt": o.prompt, "why": o.why})
	if err != nil {
		return err
	}
	url := boardAddress(o.boardURL) + "/v1/tangent"
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}
	var task struct {
		ID           string `json:"id"`
		DisplayTitle string `json:"display_title"`
		Worktree     string `json:"worktree"`
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}
	if o.quiet {
		fmt.Println(task.ID)
		return nil
	}
	fmt.Printf("tangent %s\n", task.DisplayTitle)
	fmt.Printf("  dir   %s\n", task.Worktree)
	fmt.Printf("  card  %s\n", task.ID)
	fmt.Printf("  board %s\n", boardAddress(o.boardURL))
	return nil
}
