package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

// A merge happened. Run by git's `post-merge` hook, so it follows the hook
// posture and then some: it NEVER fails a merge. Every error, from a room that
// is down to a branch that cannot be read, is written to stderr as one line and
// the exit code is still 0. See docs/rnd/merged-cull-design.md.

// mergedTimeout is short on purpose. A merge waits on this hook, and a room
// that does not answer at once is one that will be told next merge.
const mergedTimeout = 3 * time.Second

func newMerged() *cobra.Command {
	var into, boardURL string
	c := &cobra.Command{
		Use:   "merged --into <branch>",
		Short: "Tell the room a merge into a branch happened. Never fails.",
		Long: "Asks the room to find the workers whose branch this merge covered, mark the finished ones " +
			"and cull them after the grace period unless they are kept.\n\n" +
			"Run by git's post-merge hook (scripts/install-git-hooks.ps1). Best effort: any error is " +
			"logged and the exit code is 0, so a merge is never failed by atrium not listening.\n\n" +
			"--into defaults to the branch the current directory is on.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := reportMerged(cmd.OutOrStdout(), boardURL, into); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "atrium merged: %v\n", err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&into, "into", "", "the branch merged into (default: the branch HEAD is on)")
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func reportMerged(out io.Writer, boardURL, into string) error {
	into = strings.TrimSpace(into)
	if into == "" {
		git := exec.Command("git", "symbolic-ref", "--short", "-q", "HEAD")
		nowindow.Hide(git)
		b, err := git.Output()
		if err != nil || strings.TrimSpace(string(b)) == "" {
			return fmt.Errorf("no branch given and HEAD is not on one")
		}
		into = strings.TrimSpace(string(b))
	}
	body, err := json.Marshal(map[string]string{"into": into})
	if err != nil {
		return err
	}
	url := boardAddress(boardURL) + "/v1/merged"
	resp, err := (&http.Client{Timeout: mergedTimeout}).Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("atrium answered %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var ans struct {
		Marked []struct {
			Card string `json:"card"`
		} `json:"marked"`
		Off bool `json:"off"`
	}
	_ = json.Unmarshal(raw, &ans)
	switch {
	case ans.Off:
		fmt.Fprintln(out, "atrium: merged-cull is off")
	case len(ans.Marked) > 0:
		ids := make([]string, 0, len(ans.Marked))
		for _, m := range ans.Marked {
			ids = append(ids, m.Card)
		}
		fmt.Fprintf(out, "atrium: %d worker(s) merged into %s will be culled: %s\n",
			len(ans.Marked), into, strings.Join(ids, ", "))
	}
	return nil
}
