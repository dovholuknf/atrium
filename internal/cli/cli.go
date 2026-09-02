// Package cli wires the cobra subcommands: status, watch, serve.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/agent"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/hub"
	"github.com/dovholuknf/atrium/internal/server"
	"github.com/dovholuknf/atrium/internal/state"
	"github.com/dovholuknf/atrium/internal/tui"
)

// Execute runs the cobra root. Returns the exit code.
func Execute() int {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "atrium:", err)
		return 1
	}
	return 0
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "atrium",
		Short: "Single pane of glass for many claude-code sessions.",
		Long: "Atrium is a read-only aggregator over the state that the gwt session hooks write " +
			"to disk. It exposes the state via a CLI table, a tail-able event stream, and an MCP server.",
	}
	root.AddCommand(newStatus(), newWatch(), newServe(), newHub(), newAgent(), newDaemon(),
		newJoin(), newLeave())
	return root
}

// ── daemon ──────────────────────────────────────────────────────────────────

func newDaemon() *cobra.Command {
	var agentAddr, humanAddr, dbPath string
	var timeoutSec int
	var withTUI bool
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run the v2 daemon: durable state, an agent listener, and the board.",
		Long: "Serves two listeners. Agents POST to the agent address exactly as they do against " +
			"`atrium hub`. Humans get the JSON API, the SSE stream, and the board on the human " +
			"address. State is durable, so restarting no longer wipes what you were doing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), daemon.Options{
				AgentAddr:  agentAddr,
				HumanAddr:  humanAddr,
				DBPath:     dbPath,
				LongPoll:   time.Duration(timeoutSec) * time.Second,
			}, withTUI)
		},
	}
	c.Flags().StringVar(&agentAddr, "addr", ":7777", "agent-facing listen address")
	c.Flags().StringVar(&humanAddr, "http", ":7778", "human-facing listen address (API and board)")
	c.Flags().StringVar(&dbPath, "db", "", "sqlite path (default: alongside the rest of atrium's state)")
	c.Flags().IntVar(&timeoutSec, "long-poll", 60, "agent long-poll timeout in seconds")
	c.Flags().BoolVar(&withTUI, "tui", false, "also attach the terminal UI in this process")
	return c
}

func runDaemon(ctx context.Context, opts daemon.Options, withTUI bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	d, err := daemon.New(opts)
	if err != nil {
		// Tier one: a store that will not open is not something to run without.
		return fmt.Errorf("refusing to start: %w", err)
	}
	defer d.Close()

	if !withTUI {
		return d.Run(ctx)
	}

	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()

	p := tui.New(ctx, d.Hub())
	if _, err := p.Run(); err != nil {
		cancel()
		return err
	}
	cancel()
	select {
	case err := <-runErr:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}

// ── hub ─────────────────────────────────────────────────────────────────────

func newHub() *cobra.Command {
	var addr string
	var timeoutSec int
	var simple bool
	c := &cobra.Command{
		Use:   "hub",
		Short: "Run the HTTP broker + terminal UI: agents POST here, you type prompts in.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHub(cmd.Context(), addr, time.Duration(timeoutSec)*time.Second, simple)
		},
	}
	c.Flags().StringVar(&addr, "addr", ":7777", "HTTP listen address (host:port)")
	c.Flags().IntVar(&timeoutSec, "long-poll", 60, "hub-side long-poll timeout in seconds")
	c.Flags().BoolVar(&simple, "simple", false, "use the plain stdin TUI instead of the Bubble Tea full-screen UI")
	return c
}

func runHub(ctx context.Context, addr string, longPoll time.Duration, simple bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	h := hub.New(longPoll)

	if simple {
		fmt.Fprintf(os.Stdout, "[atrium hub] listening on %s (long-poll %s)\n", addr, longPoll)
		errCh := make(chan error, 1)
		go func() { errCh <- h.Serve(ctx, addr) }()
		go func() { errCh <- h.RunTUI(ctx, os.Stdout, os.Stdin) }()
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			return err
		}
	}

	// Bubble Tea full-screen UI. Server runs in a goroutine; quitting the UI
	// cancels ctx which shuts down the server.
	serveErr := make(chan error, 1)
	go func() { serveErr <- h.Serve(ctx, addr) }()

	p := tui.New(ctx, h)
	if _, err := p.Run(); err != nil {
		cancel()
		return err
	}
	cancel()
	select {
	case err := <-serveErr:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}

// ── agent ───────────────────────────────────────────────────────────────────

func newAgent() *cobra.Command {
	var url, name string
	c := &cobra.Command{
		Use:   "agent",
		Short: "Run the claude-side MCP server. Wire this into a claude session's mcpServers config.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved := resolveAgentName(name)
			if resolved == "" {
				return fmt.Errorf("could not determine agent name; pass --name explicitly")
			}
			return runAgent(cmd.Context(), url, resolved)
		},
	}
	c.Flags().StringVar(&url, "url", "http://localhost:7777", "atrium hub base URL")
	c.Flags().StringVar(&name, "name", "", "agent name (defaults to the leaf of the current working dir)")
	return c
}

func resolveAgentName(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if v := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")); v != "" {
		return v
	}
	base := ""
	if cwd, err := os.Getwd(); err == nil {
		b := filepath.Base(cwd)
		if b != "" && b != "." && b != string(filepath.Separator) {
			base = b
		}
	}
	if base == "" {
		return ""
	}
	// Append a short PID-derived suffix so two agents in the same dir get
	// distinct wire names. Modulo keeps it readable. Use /rename in the hub
	// for a friendlier display name.
	return fmt.Sprintf("%s-%d", base, os.Getpid()%100000)
}

func runAgent(ctx context.Context, url, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	s := agent.New(url, name)
	return s.Run(ctx, &mcp.StdioTransport{})
}

// ── status ──────────────────────────────────────────────────────────────────

func newStatus() *cobra.Command {
	var onlyNeedsInput, onlyAlive bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Print the current state of every known claude-code session.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.OutOrStdout(), onlyNeedsInput, onlyAlive)
		},
	}
	c.Flags().BoolVar(&onlyNeedsInput, "needs-input", false, "show only sessions waiting on a prompt")
	c.Flags().BoolVar(&onlyAlive, "alive", false, "show only sessions whose pid is currently running")
	return c
}

func runStatus(w io.Writer, onlyNeedsInput, onlyAlive bool) error {
	sessions, err := state.ReadAll()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tBRANCH\tWINDOW\tPID\tWORKTREE")
	for _, s := range sessions {
		if onlyNeedsInput && s.State != "needs-input" {
			continue
		}
		if onlyAlive && s.PID == 0 {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			padState(s.State), pickLabel(s), defaultStr(s.WindowName, "-"), s.PID, s.WorktreePath)
	}
	return tw.Flush()
}

func pickLabel(s state.Session) string {
	if s.Label != "" {
		return s.Label
	}
	return s.Branch
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func padState(s string) string {
	if s == "" {
		return "idle"
	}
	return s
}

// ── watch ───────────────────────────────────────────────────────────────────

func newWatch() *cobra.Command {
	var tailN int
	c := &cobra.Command{
		Use:   "watch",
		Short: "Tail the state log, printing each new transition.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatch(cmd.OutOrStdout(), tailN)
		},
	}
	c.Flags().IntVar(&tailN, "tail", 20, "lines of existing log to print before following")
	return c
}

func runWatch(w io.Writer, tailN int) error {
	path := state.StateLogPath()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// Print last N lines then follow.
	all, err := state.TailEvents()
	if err != nil {
		return err
	}
	start := 0
	if len(all) > tailN {
		start = len(all) - tailN
	}
	for _, ev := range all[start:] {
		fmt.Fprintln(w, ev.Raw)
	}

	// Seek to end and tail.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			fmt.Fprint(w, string(buf[:n]))
		}
		if err != nil && err != io.EOF {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ── serve ───────────────────────────────────────────────────────────────────

func newServe() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run as an MCP server over stdio.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context())
		},
	}
}

func runServe(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	s := server.New()
	return s.Run(ctx, &mcp.StdioTransport{})
}
