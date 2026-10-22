package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	answer, err := postReady(hubURL, name, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, answer.Message)
	if !answer.Stored {
		fmt.Fprintf(out, "  the handoff could not be stored on the card. it is still at %s\n", answer.Path)
	}
	return nil
}

type ReadyInput struct {
	Handoff string `json:"handoff" jsonschema:"the handoff as markdown: what you are doing and why, what is done and left, decisions and their reasons, branches, commits and files, how to check the work. at most 256 KB"`
}

type ReadyOutput struct {
	Ok      bool   `json:"ok"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func readyHandler(_ context.Context, _ *mcp.CallToolRequest, in ReadyInput) (*mcp.CallToolResult, ReadyOutput, error) {
	out := ReadyOutput{}
	if strings.TrimSpace(in.Handoff) == "" {
		return nil, out, fmt.Errorf("the handoff is empty: write what a fresh session needs to carry on")
	}
	if strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")) == "" && os.Getenv("ATRIUM_TASK_ID") == "" {
		return nil, out, fmt.Errorf("this session has no ATRIUM_AGENT_NAME to mean itself by, so atrium cannot tell which card is ready")
	}
	answer, err := postReady("", "", &in.Handoff)
	if err != nil {
		return nil, out, err
	}
	out.Ok, out.Path, out.Message = true, answer.Path, answer.Message
	return nil, out, nil
}

type readyAnswer struct {
	Error   string `json:"error"`
	Path    string `json:"path"`
	Stored  bool   `json:"stored"`
	Message string `json:"message"`
}

// postReady says the handoff is written, with its text when the session has no shell to write the file.
func postReady(hubURL, name string, handoff *string) (readyAnswer, error) {
	var answer readyAnswer
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" {
		if cwd, err := os.Getwd(); err == nil {
			agent = filepath.Base(cwd)
		}
	}
	taskID := ""
	if name == "" {
		taskID = os.Getenv("ATRIUM_TASK_ID")
	}
	if agent == "" && taskID == "" {
		return answer, fmt.Errorf("could not work out which session this is. pass --name")
	}
	req := map[string]any{"agent": agent, "task_id": taskID}
	if handoff != nil {
		req["handoff"] = *handoff
	}
	body, err := json.Marshal(req)
	if err != nil {
		return answer, err
	}

	url := hubAddress(hubURL) + "/ready"
	client := &http.Client{Timeout: finishTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return answer, fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = json.Unmarshal(raw, &answer)
	if resp.StatusCode >= 300 {
		why := answer.Error
		if why == "" {
			why = strings.TrimSpace(string(raw))
		}
		return answer, fmt.Errorf("atrium refused: %s", why)
	}
	return answer, nil
}

type ClearContextInput struct {
	Handoff string `json:"handoff" jsonschema:"the handoff as markdown: what you are doing and why, what is done and left, decisions and their reasons, branches, commits and files, how to check the work. at most 256 KB"`
}

func clearContextHandler(_ context.Context, _ *mcp.CallToolRequest, in ClearContextInput) (*mcp.CallToolResult, ReadyOutput, error) {
	out := ReadyOutput{}
	if strings.TrimSpace(in.Handoff) == "" {
		return nil, out, fmt.Errorf("the handoff is empty: write what a fresh session needs to carry on")
	}
	if strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")) == "" && os.Getenv("ATRIUM_TASK_ID") == "" {
		return nil, out, fmt.Errorf("this session has no ATRIUM_AGENT_NAME to mean itself by, so atrium cannot tell which card to clear")
	}
	answer, err := postClearContext(in.Handoff)
	if err != nil {
		return nil, out, err
	}
	out.Ok, out.Path, out.Message = true, answer.Path, answer.Message
	return nil, out, nil
}

func postClearContext(handoff string) (readyAnswer, error) {
	var answer readyAnswer
	who := os.Getenv("ATRIUM_TASK_ID")
	if who == "" {
		who = os.Getenv("ATRIUM_AGENT_NAME")
	}
	got, err := callCard("", http.MethodPost, who, "new-context", map[string]any{"handoff": handoff})
	if err != nil {
		return answer, err
	}
	_ = json.Unmarshal(got.body, &answer)
	return answer, nil
}
