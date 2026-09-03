package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/spf13/cobra"
)

// join and leave let a session that is already running put itself on the board,
// or take itself off, without being restarted.
//
// Before this, whether a session was gated was decided once, by the hook
// reading its environment when the session started. A session that was not
// gated stayed ungated for its whole life, and the only way in was to start
// over. Joining writes the decision where the hook can see it, so it takes
// effect on the very next tool call.

func newJoin() *cobra.Command {
	var name, why, title, hubURL string
	c := &cobra.Command{
		Use:   "join",
		Short: "Put this session on the atrium board and start gating its tool calls.",
		Long: "Registers the session running this command, so it appears on the board and its tool " +
			"calls are gated through atrium from now on. Takes effect immediately: nothing has to " +
			"be restarted.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionEvent(hubURL, "join", name, title, why)
		},
	}
	c.Flags().StringVar(&name, "name", "", "what this session calls itself (default: the directory name)")
	c.Flags().StringVar(&title, "title", "", "what to show on the card")
	c.Flags().StringVar(&why, "why", "", "what this session is for, read back later")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

func newLeave() *cobra.Command {
	var name, hubURL string
	c := &cobra.Command{
		Use:   "leave",
		Short: "Take this session off the board and stop gating its tool calls.",
		Long: "Stops gating the session running this command and marks its card done. The card and " +
			"its history are kept, because what the session did is still worth having.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionEvent(hubURL, "leave", name, "", "")
		},
	}
	c.Flags().StringVar(&name, "name", "", "what this session calls itself (default: the directory name)")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

// agentName resolves what this session calls itself, matching the permission
// hook exactly. Both have to agree or a session would join under one name and
// be gated under another.
func agentName(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("ATRIUM_AGENT_NAME"); v != "" {
		return v
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return filepath.Base(cwd)
}

// hubAddress works out where the daemon is, most explicit first.
//
// The recorded address is what makes `atrium hook` work with no flag on a
// daemon started on a port that is not the default. A flag would have to be
// baked into every settings.json entry, and would then be wrong the moment the
// port changed.
//
// A recorded address left behind by a daemon that was killed points at nothing,
// and the caller gets a connection refused in milliseconds. Every caller of
// this already treats that as "atrium is down", which it is.
func hubAddress(override string) string {
	addr, _ := hubAddressFrom(override)
	return addr
}

// hubAddressFrom also says where the answer came from, for a caller reporting
// itself to a human. Nothing on the hot path reads the second value.
func hubAddressFrom(override string) (addr, source string) {
	if override != "" {
		return strings.TrimRight(override, "/"), "the --url flag"
	}
	if v := os.Getenv("ATRIUM_HUB_URL"); v != "" {
		return strings.TrimRight(v, "/"), "$ATRIUM_HUB_URL"
	}
	if v := recordedAgentAddr(); v != "" {
		return v, "a running daemon"
	}
	return "http://localhost:7777", "the default, since no daemon has recorded one"
}

// recordedAgentAddr reads the address the running daemon wrote down. Any
// failure returns empty, so the caller falls back to the default.
//
// The path comes from the daemon package rather than being spelled again here.
// Two copies of it would agree until one platform's rule changed.
func recordedAgentAddr() string {
	path, err := daemon.LocationPath()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var loc daemon.Location
	if err := json.Unmarshal(raw, &loc); err != nil {
		return ""
	}
	return strings.TrimRight(loc.Agent, "/")
}

func sessionEvent(hubURL, event, name, title, why string) error {
	agent := agentName(name)
	cwd, _ := os.Getwd()

	body, err := json.Marshal(map[string]any{
		"agent": agent,
		"event": event,
		"cwd":   filepath.ToSlash(cwd),
		"title": title,
		"why":   why,
		// The pid is left for the hook to fill in. This process is a child of
		// the session, not the session itself, and the next gated tool call
		// reports the real one.
		"runner": "claude",
	})
	if err != nil {
		return err
	}

	url := hubAddress(hubURL) + "/session"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("atrium is not reachable at %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("atrium refused the %s: %s", event, resp.Status)
	}

	switch event {
	case "join":
		fmt.Printf("joined atrium as %q. tool calls from this session are gated from now on.\n", agent)
	case "leave":
		fmt.Printf("left atrium. %q is no longer gated, and its card is marked done.\n", agent)
	}
	return nil
}
