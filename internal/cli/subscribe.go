package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// Hearing a hub event, for the calling session's own card. The tool and the commands share one call, which goes to this
// room, which asks the hub over its link. Nothing here names a card but the caller's own: ATRIUM_TASK_ID. See
// docs/fabric/land-webhook-design.md, 3.

const subscribeWait = 60 * time.Second

const subscribeToolDesc = "Ask to hear a hub event on YOUR card, or stop hearing it. Only your own card: you cannot " +
	"subscribe another one, and a card that should hear landings is told to call this, or the operator adds it on the " +
	"board.\n\n" +
	"- `subscribe`: `kind` (today `land`: a landing on a branch was pushed, not pushed or stopped), `repo` as " +
	"owner/name and `branch` to hear only that one, either left out for any. `wake` resumes your card when it is " +
	"parked, at the cost of a cold start. Twice is one subscription.\n" +
	"- `unsubscribe`: the same `kind`, `repo` and `branch`, or the `id` of one.\n" +
	"- `list`: your subscriptions.\n\n" +
	"An event arrives as a short fyi from `land@<hub>`. A card that is parked without `wake` gets the newest one for " +
	"each repo and branch when it is back, for up to a day."

// SubscribeInput is the atrium_subscribe tool's input.
type SubscribeInput struct {
	Action string `json:"action" jsonschema:"subscribe, unsubscribe or list"`
	Kind   string `json:"kind,omitempty" jsonschema:"the kind of event. today: land"`
	Repo   string `json:"repo,omitempty" jsonschema:"owner/name to hear only that repository. empty for any"`
	Branch string `json:"branch,omitempty" jsonschema:"the branch landed on, to hear only that one. empty for any"`
	Wake   bool   `json:"wake,omitempty" jsonschema:"true to resume your card when it is parked and an event arrives"`
	ID     string `json:"id,omitempty" jsonschema:"unsubscribe: the id of one subscription, from list"`
}

func subscribeCall(ctx context.Context, in SubscribeInput) (map[string]any, error) {
	out := map[string]any{}
	switch strings.TrimSpace(in.Action) {
	case "subscribe", "unsubscribe", "list":
	default:
		return out, fmt.Errorf("say the action: subscribe, unsubscribe or list")
	}
	path, err := myCardPath("/subscriptions")
	if err != nil {
		return out, err
	}
	err = askFor(ctx, subscribeWait, http.MethodPost, path, in, &out)
	if olderRoom(err) {
		return out, fmt.Errorf("this room predates subscriptions. update the room")
	}
	return out, err
}

func subscribeHandler(ctx context.Context, _ *mcp.CallToolRequest, in SubscribeInput) (*mcp.CallToolResult, map[string]any, error) {
	out, err := subscribeCall(ctx, in)
	return nil, out, err
}

func addSubscribeTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_subscribe", Description: subscribeToolDesc}, subscribeHandler)
}

func subscribeFlags(c *cobra.Command, in *SubscribeInput, withWake bool) {
	c.Flags().StringVar(&in.Repo, "repo", "", "owner/name to hear only that repository (default any)")
	c.Flags().StringVar(&in.Branch, "branch", "", "the branch landed on, to hear only that one (default any)")
	if withWake {
		c.Flags().BoolVar(&in.Wake, "wake", false, "resume this card when it is parked and an event arrives")
	}
}

func printSubscriptions(c *cobra.Command, out map[string]any) {
	subs, _ := out["subscriptions"].([]any)
	if len(subs) == 0 {
		fmt.Fprintln(c.OutOrStdout(), "no subscriptions")
		return
	}
	for _, v := range subs {
		m, _ := v.(map[string]any)
		wake := ""
		if w, _ := m["wake"].(bool); w {
			wake = "  wake"
		}
		fmt.Fprintf(c.OutOrStdout(), "%v  %v  repo %v  branch %v%s\n", m["id"], m["kind"], anyWord(m["repo"]), anyWord(m["branch"]), wake)
	}
}

func anyWord(v any) string {
	if s, _ := v.(string); s != "" {
		return s
	}
	return "any"
}

func newSubscribe() *cobra.Command {
	in := SubscribeInput{Action: "subscribe"}
	c := &cobra.Command{
		Use:   "subscribe <kind>",
		Short: "Have this session's card hear a hub event, such as land.",
		Long: "Asks the hub to tell this session's card when something happens: `atrium subscribe land --repo " +
			"dovholuknf/atrium --branch main`. The card is the one $ATRIUM_TASK_ID names. The same as the " +
			"atrium_subscribe tool.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			in.Kind = args[0]
			out, err := subscribeCall(c.Context(), in)
			if err != nil {
				return err
			}
			if already, _ := out["already"].(bool); already {
				fmt.Fprintln(c.OutOrStdout(), "already subscribed")
			} else {
				fmt.Fprintln(c.OutOrStdout(), "subscribed")
			}
			return nil
		},
	}
	subscribeFlags(c, &in, true)
	return c
}

func newUnsubscribe() *cobra.Command {
	in := SubscribeInput{Action: "unsubscribe"}
	c := &cobra.Command{
		Use:   "unsubscribe <kind>",
		Short: "Stop this session's card hearing a hub event.",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			in.Kind = args[0]
			out, err := subscribeCall(c.Context(), in)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "removed %v\n", out["removed"])
			return nil
		},
	}
	subscribeFlags(c, &in, false)
	c.Flags().StringVar(&in.ID, "id", "", "the id of one subscription, from `atrium subscriptions`")
	return c
}

func newSubscriptions() *cobra.Command {
	return &cobra.Command{
		Use:   "subscriptions",
		Short: "List what this session's card hears.",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			out, err := subscribeCall(c.Context(), SubscribeInput{Action: "list"})
			if err != nil {
				return err
			}
			printSubscriptions(c, out)
			return nil
		},
	}
}
