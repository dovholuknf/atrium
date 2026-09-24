// Package cli wires the cobra subcommands: status, watch, serve.
package cli

import (
	"context"
	"errors"
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

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/server"
	"github.com/dovholuknf/atrium/internal/state"
)

// errAlreadySaid marks a failure the command has already explained, in its own
// words, on its own output.
//
// The failure it prevents: `atrium ask --peer nobody-here "x"` printed a
// refusal that named every handle that WOULD have worked, and then cobra
// printed `Error: ...`, twelve lines of flag usage, and the same sentence
// again. The one paragraph somebody wrote scrolled off the top, replaced by
// flag descriptions for a command that was typed correctly except for one
// argument. So a path that has spoken wraps this, and both cobra and Execute
// stay quiet.
//
// NOT a blanket silence. Only the paths that print a human sentence carry it,
// because a command that fails without saying anything must still be reported
// or a real failure becomes an exit code and nothing else.
var errAlreadySaid = errors.New("already reported")

// alreadySaid tags an error as one whose message was already printed. The text
// is kept for tests and for anything that inspects the error, never for the
// terminal.
func alreadySaid(format string, a ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, a...), errAlreadySaid)
}

// speaksForItself wraps a RunE that MIGHT return an already-reported error.
//
// Cobra reads SilenceUsage and SilenceErrors AFTER RunE returns, so setting
// them here scopes the silence to the one call that earned it. Setting them on
// the command up front would also swallow the usage and the message for every
// other way that command can fail.
func speaksForItself(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if errors.Is(err, errAlreadySaid) {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		}
		return err
	}
}

// Execute runs the cobra root. Returns the exit code.
func Execute() int {
	return run(os.Args[1:])
}

// run is Execute for any argument list, so a test can run the exact command a
// hooks file holds through the same exit-code rule.
func run(args []string) int {
	root := newRoot()
	root.SetArgs(args)
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	// A runner shows a non-zero hook as `Hook failed` after every command, or
	// blocks the call. Whatever went wrong parsing a hook line, the session
	// goes on.
	if isRunnerHook(cmd) {
		return 0
	}
	// Still a failure, still exit 1. The command said why already.
	if !errors.Is(err, errAlreadySaid) {
		fmt.Fprintln(os.Stderr, "atrium:", err)
	}
	return 1
}

// runnerHook marks a subcommand a runner's hooks file runs. See isRunnerHook.
const runnerHook = "atrium/runner-hook"

// isRunnerHook reports whether cmd is one a runner runs as a hook. Its
// children, `hook install` and `hook status`, are typed by people and keep
// their failures.
func isRunnerHook(cmd *cobra.Command) bool {
	return cmd != nil && cmd.Annotations[runnerHook] == "true"
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "atrium",
		Short: "Single pane of glass for many claude-code sessions.",
		Long: "Atrium is a read-only aggregator over the state that the gwt session hooks write " +
			"to disk. It exposes the state via a CLI table, a tail-able event stream, and an MCP server.",
	}
	root.AddCommand(newStatus(), newWatch(), newServe(), newDaemon(),
		newJoin(), newLeave(), newStop(), newLaunch(), newPreview(), newHook(), newSession(), newTurn(),
		newName(), newFinish(), newPeers(), newTell(), newControl(), newVersion(), newAsk(),
		newAnswer(), newRoom(), newOpen(), newDispatch(), newReplayCmd())
	return root
}

// ── daemon ──────────────────────────────────────────────────────────────────

func newDaemon() *cobra.Command {
	var agentAddr, humanAddr, dbPath, shutdownToken, locationFile, boardDir string
	var timeoutSec int
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run the v2 daemon: durable state, an agent listener, and the board.",
		Long: "Serves two listeners. Hooks POST to the agent address. Humans get the JSON API, " +
			"the SSE stream, and the board on the human address. State is durable, so restarting " +
			"does not wipe what you were doing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemon(cmd.Context(), daemon.Options{
				AgentAddr:     agentAddr,
				HumanAddr:     humanAddr,
				DBPath:        dbPath,
				LongPoll:      time.Duration(timeoutSec) * time.Second,
				ShutdownToken: shutdownToken,
				LocationFile:  locationFile,
				BoardDir:      boardDir,
			})
		},
	}
	c.Flags().StringVar(&agentAddr, "addr", ":7777", "agent-facing listen address")
	c.Flags().StringVar(&humanAddr, "http", ":7778", "human-facing listen address (API and board)")
	c.Flags().StringVar(&dbPath, "db", "", "sqlite path (default: alongside the rest of atrium's state)")
	c.Flags().IntVar(&timeoutSec, "long-poll", 60, "longest shutdown waits on parked agent requests, in seconds (capped at 5)")
	c.Flags().StringVar(&shutdownToken, "shutdown-token", "",
		"require this token on POST /v1/shutdown (default: loopback only, no token accepted)")
	// Every daemon writes where it is listening, and hooks read that file to
	// find one. So a second daemon started to try something out TAKES the
	// address from the one you actually use, and every hook in every live
	// session starts arriving at the wrong place. Naming another file is how
	// to run one without doing that.
	c.Flags().StringVar(&locationFile, "location-file", "",
		"where to record this daemon's address (default: the machine's one place for it). "+
			"name another to run a second daemon without stealing the first one's hooks")
	// The board is compiled in, so a one-line change to a stylesheet costs a
	// rebuild, an install, and a restart that takes down every supervised
	// terminal on the machine. Pointing this at `internal/api/web` makes it a
	// browser refresh instead.
	c.Flags().StringVar(&boardDir, "board-dir", "",
		"serve the board from this directory instead of the copy built into the binary, "+
			"so a change to the page needs a refresh rather than a restart")
	return c
}

func runDaemon(ctx context.Context, opts daemon.Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Checked here rather than at first request, because the symptom of a
	// wrong directory is a board that answers 404 for every file, which reads
	// as a broken daemon rather than as a typo.
	if dir := strings.TrimSpace(opts.BoardDir); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
			return fmt.Errorf("--board-dir %s has no index.html in it, so it is not a board", abs)
		}
		opts.BoardDir = abs
		fmt.Printf("board served from %s\n", filepath.ToSlash(abs))
	}

	d, err := daemon.New(opts)
	if err != nil {
		// Tier one: a store that will not open is not something to run without.
		return fmt.Errorf("refusing to start: %w", err)
	}
	defer d.Close()
	return d.Run(ctx)
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
