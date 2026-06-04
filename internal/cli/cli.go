// Package cli wires the cobra subcommands: status, watch, serve.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/server"
	"github.com/dovholuknf/atrium/internal/state"
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
	root.AddCommand(newStatus(), newWatch(), newServe())
	return root
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
