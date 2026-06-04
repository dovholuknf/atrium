// Package server wires the Atrium MCP server. Three tools:
//   - snapshot          : returns every known session's current state
//   - wait_for_change   : long-polls until any session transitions or a timeout
//   - focus_session     : brings a session's wt window to the foreground
package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/state"
)

const Version = "v0.0.0-dev"

// New returns an MCP server with the three tools registered.
func New() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "atrium", Version: Version}, nil)
	RegisterTools(s)
	return s
}

// RegisterTools attaches the tool handlers. Separated so tests can drive a
// server with a custom registration.
func RegisterTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "snapshot",
		Description: "Return every known claude-code session and its current state. " +
			"Reads <WORKTREE_ROOT>\\sessions\\*.json. Best-effort: malformed entries are skipped. " +
			"Use this when you want a one-shot view of who's idle / thinking / waiting on input.",
	}, snapshotHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "wait_for_change",
		Description: "Long-poll until any session transitions to a new state (or the timeout fires). " +
			"Pass 'since' as an RFC3339 timestamp to only consider events after that point. " +
			"Default timeout is 30 seconds; max 300 seconds. Returns the change record or " +
			"{timed_out:true} when no event arrives before the deadline.",
	}, waitForChangeHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "focus_session",
		Description: "Bring the wt window hosting the given session to the foreground. " +
			"Best-effort: returns {focused:false} if the session has no recorded WindowName " +
			"or if wt.exe is not on PATH.",
	}, focusSessionHandler)
}

// ── snapshot ────────────────────────────────────────────────────────────────

type SnapshotInput struct{}

type SnapshotOutput struct {
	Sessions  []state.Session `json:"sessions"`
	ReadAt    string          `json:"read_at"`
	SessionsN int             `json:"sessions_count"`
}

func snapshotHandler(ctx context.Context, _ *mcp.CallToolRequest, _ SnapshotInput) (*mcp.CallToolResult, any, error) {
	sessions, err := state.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read sessions: %w", err)
	}
	return nil, SnapshotOutput{
		Sessions:  sessions,
		ReadAt:    time.Now().UTC().Format(time.RFC3339Nano),
		SessionsN: len(sessions),
	}, nil
}

// ── wait_for_change ─────────────────────────────────────────────────────────

type WaitForChangeInput struct {
	Since          string `json:"since,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type WaitForChangeOutput struct {
	TimedOut bool          `json:"timed_out"`
	Change   *ChangeRecord `json:"change,omitempty"`
}

type ChangeRecord struct {
	At           string `json:"at"`
	State        string `json:"state"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
}

func waitForChangeHandler(ctx context.Context, _ *mcp.CallToolRequest, in WaitForChangeInput) (*mcp.CallToolResult, any, error) {
	timeout := resolveTimeout(in.TimeoutSeconds)
	deadline := time.Now().Add(timeout)

	var since time.Time
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339Nano, in.Since)
		if err != nil {
			return nil, nil, fmt.Errorf("parse 'since': %w", err)
		}
		since = t
	} else {
		// no cursor: only return events that arrive AFTER this call starts.
		since = time.Now()
	}

	// Cheap polling loop. The state log is small and the poll interval is short
	// enough that the human-visible latency stays under a second. Upgrade to
	// fsnotify if this ever becomes a hot path.
	const tick = 500 * time.Millisecond
	for {
		events, err := state.TailEvents()
		if err == nil {
			for _, ev := range events {
				if ev.Timestamp.After(since) {
					return nil, WaitForChangeOutput{
						Change: &ChangeRecord{
							At:           ev.Timestamp.Format(time.RFC3339Nano),
							State:        ev.State,
							Branch:       ev.Branch,
							WorktreePath: ev.WorktreePath,
						},
					}, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return nil, WaitForChangeOutput{TimedOut: true}, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(tick):
		}
	}
}

func resolveTimeout(req int) time.Duration {
	const defaultTimeout = 30 * time.Second
	const maxTimeout = 300 * time.Second
	if req <= 0 {
		if v := os.Getenv("ATRIUM_LONG_POLL_TIMEOUT"); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return capDuration(d, maxTimeout)
			}
		}
		return defaultTimeout
	}
	return capDuration(time.Duration(req)*time.Second, maxTimeout)
}

func capDuration(d, max time.Duration) time.Duration {
	if d > max {
		return max
	}
	return d
}

// ── focus_session ───────────────────────────────────────────────────────────

type FocusSessionInput struct {
	SessionID string `json:"session_id"`
}

type FocusSessionOutput struct {
	Focused bool   `json:"focused"`
	Window  string `json:"window,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func focusSessionHandler(ctx context.Context, _ *mcp.CallToolRequest, in FocusSessionInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, nil, errors.New("session_id is required")
	}
	sessions, err := state.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read sessions: %w", err)
	}
	var match *state.Session
	for i := range sessions {
		if sessions[i].ID == in.SessionID || sessions[i].ClaudeSessionID == in.SessionID {
			match = &sessions[i]
			break
		}
	}
	if match == nil {
		return nil, FocusSessionOutput{Focused: false, Reason: "no session with that id"}, nil
	}
	if match.WindowName == "" {
		return nil, FocusSessionOutput{Focused: false, Reason: "session has no WindowName"}, nil
	}
	if _, err := exec.LookPath("wt.exe"); err != nil {
		return nil, FocusSessionOutput{Focused: false, Window: match.WindowName, Reason: "wt.exe not on PATH"}, nil
	}
	cmd := exec.CommandContext(ctx, "wt.exe", "-w", match.WindowName, "focus-tab")
	if err := cmd.Run(); err != nil {
		return nil, FocusSessionOutput{
			Focused: false,
			Window:  match.WindowName,
			Reason:  fmt.Sprintf("wt invocation failed: %v", err),
		}, nil
	}
	return nil, FocusSessionOutput{Focused: true, Window: match.WindowName}, nil
}
