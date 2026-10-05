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

// Saying the handoff is written, in a context cycle.
//
// Atrium types the limit prompt when a card passes its context limit. The agent
// wraps up, writes its handoff to the path the prompt names, and runs this. Only
// then does atrium type /clear and the wake line. Nothing is cleared without it.
//
// A command for the same reason as `finish`: every runner can run one. Unlike the
// hooks it is not silent, and a refusal exits non-zero, since an agent that thinks
// the ack landed when it did not would wait on a clear that never comes.

func newReady() *cobra.Command {
	var name, hubURL string
	c := &cobra.Command{
		Use:   "ready",
		Short: "Say your handoff is written, so atrium can clear your context.",
		Long: "Run this when atrium's limit prompt asked for it, after the handoff is written to " +
			"the path the prompt names. Atrium stores the handoff on your card, types /clear when " +
			"this turn ends, then tells the new session to read the handoff and continue.\n\n" +
			"Refused, with a non-zero exit, when no context cycle is waiting on this card or the " +
			"handoff file is missing or empty.\n\n" +
			"Which session this is comes from $ATRIUM_AGENT_NAME, or the current directory's " +
			"name, exactly like `atrium finish`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return reportReady(cmd.OutOrStdout(), hubURL, name)
		},
	}
	c.Flags().StringVar(&name, "name", "",
		"what this session calls itself (default: $ATRIUM_AGENT_NAME, or the directory name)")
	c.Flags().StringVar(&hubURL, "url", "",
		"atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

func reportReady(out io.Writer, hubURL, name string) error {
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" {
		if cwd, err := os.Getwd(); err == nil {
			agent = filepath.Base(cwd)
		}
	}
	taskID := os.Getenv("ATRIUM_TASK_ID")
	if agent == "" && taskID == "" {
		return fmt.Errorf("could not work out which session this is. pass --name")
	}
	body, err := json.Marshal(map[string]any{"agent": agent, "task_id": taskID})
	if err != nil {
		return err
	}

	url := hubAddress(hubURL) + "/ready"
	client := &http.Client{Timeout: finishTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var answer struct {
		Error   string `json:"error"`
		Path    string `json:"path"`
		Stored  bool   `json:"stored"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &answer)
	if resp.StatusCode >= 300 {
		why := answer.Error
		if why == "" {
			why = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("atrium refused: %s", why)
	}
	fmt.Fprintln(out, answer.Message)
	if !answer.Stored {
		fmt.Fprintf(out, "  the handoff could not be stored on the card. it is still at %s\n", answer.Path)
	}
	return nil
}
