package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The one-time tidy of done worker cards. See internal/daemon/archiveworkers.go
// for what it will and will not touch.

func newArchiveWorkers() *cobra.Command {
	var dryRun bool
	var boardURL string
	c := &cobra.Command{
		Use:   "archive-workers [--dry-run]",
		Short: "Archive the done worker cards that piled up. Never a director.",
		Long: "Takes finished WORKER cards off the board: done, launched by an agent, no live runner, not " +
			"pinned and not a fixture. Never a director, the orchestrator, or a card an agent did not " +
			"launch. Archives only: the cards keep their history, and no worktree or branch is removed.\n\n" +
			"--dry-run lists what would go and changes nothing.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runArchiveWorkers(cmd.OutOrStdout(), boardURL, dryRun)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be archived and change nothing")
	c.Flags().StringVar(&boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func runArchiveWorkers(out io.Writer, boardURL string, dryRun bool) error {
	body, _ := json.Marshal(map[string]bool{"dry_run": dryRun})
	url := boardAddress(boardURL) + "/v1/tasks/archive-workers"
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("atrium answered %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var ans struct {
		DryRun   bool `json:"dry_run"`
		Archived []struct {
			Card  string `json:"card"`
			Title string `json:"title"`
		} `json:"archived"`
		Kept int `json:"kept"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return fmt.Errorf("could not read the answer: %w", err)
	}
	verb := "archived"
	if ans.DryRun {
		verb = "would archive"
	}
	for _, a := range ans.Archived {
		fmt.Fprintf(out, "%s %s  %s\n", verb, a.Card, a.Title)
	}
	fmt.Fprintf(out, "%s %d worker card(s), left %d alone (pinned, fixture or still running)\n",
		verb, len(ans.Archived), ans.Kept)
	return nil
}
