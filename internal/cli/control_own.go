package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_port and atrium_own: what a card's agent starts, recorded on the card's inventory so closing the card stops
// and frees it. The room does the work (internal/api/cardports.go). Item r-card-procs-ports.

type PortInput struct {
	Count int `json:"count,omitempty" jsonschema:"how many ports, 1 (the default) to 20"`
}

type PortOutput struct {
	Ports []int  `json:"ports"`
	Range string `json:"range"`
}

const portToolDesc = "Get free TCP ports for something you are about to start: a test controller, a dev server, a " +
	"router. The room hands them out from its own range, skips ports Windows has reserved and ports another card " +
	"holds, checks each with a bind, and records them on your card, so no other card is given them until yours is " +
	"closed. Use these instead of picking a number yourself. Previews keep 50000-50999, as before. Bind them on " +
	"127.0.0.1 unless you must listen wider."

const ownToolDesc = "Record on your card something you started, so closing the card stops or frees it.\n\n" +
	"- `proc`: `ref` is the pid of a long-running process you started in the background (a controller, a server, " +
	"a watcher). The room reads its start time now, and closing the card stops it with every process it started, " +
	"only while that pid is still the same process. Record it right after you start it.\n" +
	"- `dir`: `ref` is an absolute path to a folder inside your worktree or the scratch folder.\n" +
	"- `port`: `ref` is a port number you were given some other way.\n\n" +
	"Not for builds, tests or anything you wait on. A process you start and do not record is not stopped when " +
	"the card closes."

type OwnInput struct {
	Kind string `json:"kind" jsonschema:"proc, dir or port"`
	Ref  string `json:"ref" jsonschema:"the pid, the absolute folder path, or the port number"`
}

const overlayToolDesc = "Run a throwaway ziti overlay of your own for a test, held on your card: closing the card " +
	"stops it and deletes it, PKI and identities with it. Never the operator's network, never the board's share. " +
	"Have as many as you need.\n\n" +
	"- `up` {name}: a `ziti edge quickstart` (controller and router) on two of your card's ports. Answers the " +
	"controller URL, the admin password file and the log. Waits up to two minutes for the controller.\n" +
	"- `identity` {overlay, name, roles}: a Device identity, created and enrolled. Answers its .json file. Make " +
	"services and policies yourself with the admin login, as `ziti edge login <controller> -u admin -p <password> " +
	"--cli-identity <name>` keeps the operator's default login untouched.\n" +
	"- `tunnel` {overlay, identity, mode, services}: `ziti tunnel host`, or `proxy` with a port per service from your " +
	"card. `tun` needs admin or root and atrium never elevates: you get the command back, so stop and ask clint to " +
	"run it in an elevated shell, saying what it is for and how to stop it.\n" +
	"- `down` {overlay}: stop its tunnelers and controller and delete its folder.\n\n" +
	"`overlay` may be left out when the card has one."

type OverlayInput struct {
	Action   string   `json:"action" jsonschema:"up, identity, tunnel or down"`
	Name     string   `json:"name,omitempty" jsonschema:"up: the overlay's name (default o1, o2...). identity: the identity's name"`
	Overlay  string   `json:"overlay,omitempty" jsonschema:"which overlay, when the card has more than one"`
	Identity string   `json:"identity,omitempty" jsonschema:"tunnel: the identity that runs it"`
	Roles    []string `json:"roles,omitempty" jsonschema:"identity: role attributes for policies"`
	Mode     string   `json:"mode,omitempty" jsonschema:"tunnel: host (default), proxy or tun"`
	Services []string `json:"services,omitempty" jsonschema:"tunnel proxy: the services to listen for, one port each"`
}

func addOwnTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_port", Description: portToolDesc}, portHandler)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_own", Description: ownToolDesc}, ownHandler)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_overlay", Description: overlayToolDesc}, overlayHandler)
}

func overlayHandler(ctx context.Context, _ *mcp.CallToolRequest, in OverlayInput) (*mcp.CallToolResult, map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(in.Action) == "" {
		return nil, out, fmt.Errorf("say the action: up, identity, tunnel or down")
	}
	path, err := myCardPath("/overlay")
	if err != nil {
		return nil, out, err
	}
	err = askFor(ctx, 3*time.Minute, http.MethodPost, path, in, &out)
	return nil, out, err
}

// myCardPath is a path under the calling session's own card, or an error for a session with no card.
func myCardPath(tail string) (string, error) {
	me := meID()
	if me == "" {
		return "", fmt.Errorf("this session has no card (ATRIUM_TASK_ID is not set), so there is nothing to record it on")
	}
	return "/v1/tasks/" + url.PathEscape(me) + tail, nil
}

func portHandler(ctx context.Context, _ *mcp.CallToolRequest, in PortInput) (*mcp.CallToolResult, PortOutput, error) {
	out := PortOutput{}
	path, err := myCardPath("/ports")
	if err != nil {
		return nil, out, err
	}
	count := in.Count
	if count == 0 {
		count = 1
	}
	err = askFor(ctx, time.Minute, http.MethodPost, path, map[string]int{"count": count}, &out)
	return nil, out, err
}

func ownHandler(ctx context.Context, _ *mcp.CallToolRequest, in OwnInput) (*mcp.CallToolResult, map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(in.Kind) == "" || strings.TrimSpace(in.Ref) == "" {
		return nil, out, fmt.Errorf("say the kind (proc, dir or port) and the ref")
	}
	path, err := myCardPath("/own")
	if err != nil {
		return nil, out, err
	}
	err = askFor(ctx, 30*time.Second, http.MethodPost, path,
		map[string]string{"kind": strings.TrimSpace(in.Kind), "ref": strings.TrimSpace(in.Ref)}, &out)
	return nil, out, err
}
