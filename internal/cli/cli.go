// Package cli wires the cobra subcommands.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
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
	return runRoot(newRoot(), args)
}

// runRoot is run for any root.
func runRoot(root *cobra.Command, args []string) int {
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
		fmt.Fprintln(os.Stderr, root.Name()+":", err)
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
		Long: "Atrium runs claude-code sessions in real terminals, keeps their state durable, and " +
			"shows every one of them on one board.",
	}
	root.AddCommand(newDaemon(),
		newJoin(), newLeave(), newStop(), newLaunch(), newTaskCmd(), newExitCmd(), newNewContextCmd(), newPreview(), newHook(), newSession(), newTurn(),
		newName(), newFinish(), newPeers(), newTell(), newControl(), newVersion(), newAsk(),
		newAnswer(), newOpen(), newDispatch(), newReplayCmd(), newRequirements(), newMerged(), newArchiveWorkers(),
		newPtyHost())
	// The atrium and its rooms, which were `atrium2` until the two binaries
	// became one. See docs/fabric/one-atrium-plan.md.
	backups := hubBackupsCmd("atrium-")
	backups.AddCommand(hubRestoreCmd("atrium-"))
	root.AddCommand(newRun(), roomCmd(), hubRoomsCmd("rooms", "atrium-"), backups, dbCmd(), ledgerCmd(), usageCmd())
	for _, c := range root.Commands() {
		switch c.Name() {
		case "run", "room":
			quietUsage(c)
		}
	}
	return root
}

// quietUsage keeps a long-running command's failure to the one line that says
// why. `atrium room` failing to bind a port is not a flag mistake, and twenty
// lines of usage after it push the reason off the screen. A flag that does not
// parse still prints the usage, because cobra reports that before RunE runs.
func quietUsage(c *cobra.Command) {
	for _, sub := range append([]*cobra.Command{c}, c.Commands()...) {
		if sub.RunE == nil {
			continue
		}
		run := sub.RunE
		sub.RunE = func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return run(cmd, args)
		}
	}
}

// ── daemon ──────────────────────────────────────────────────────────────────

func newDaemon() *cobra.Command {
	var agentAddr, humanAddr, dbPath, shutdownToken, locationFile, boardDir, startedBy string
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
				StartedBy:     startedBy,
			})
		},
	}
	startedByFlag(c, &startedBy)
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
