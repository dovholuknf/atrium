package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/shepherd"
)

// `atrium shepherd run <item>`: a program that takes one backlog item from launch to a held packet, spending no model
// tokens of its own. The logic is internal/shepherd, and this is the wiring: the real hub, exec and disk behind the
// interfaces the machine runs on. See docs/rnd/script-minion.md and docs/changes/rnd-shepherd-core.md.
//
// HIDDEN FROM --help until accept-land lands: a shepherd that holds a packet and has no verb to accept it is half a
// command, and a person who found it would be left holding the packet.

func newShepherd() *cobra.Command {
	c := &cobra.Command{
		Use:    "shepherd",
		Short:  "Take one backlog item from launch to a held packet",
		Hidden: true,
	}
	c.AddCommand(newShepherdRun())
	return c
}

func newShepherdRun() *cobra.Command {
	var board, repo, worktrees, bin string
	var pollMax time.Duration
	c := &cobra.Command{
		Use:   "run <item>",
		Short: "Check, make a worktree, launch a worker, wait, collect, stage, and hold the packet",
		Long: "Runs the phases for one backlog item and stops with a packet held for a person. The row on the hub is\n" +
			"written before each phase, so a shepherd that was killed resumes where it was by running this again.\n" +
			"Nothing lands: accepting is not built here.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				repo = wd
			}
			repo = filepath.ToSlash(repo)
			if worktrees == "" {
				worktrees = filepath.ToSlash(filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees"))
			}
			if bin == "" {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				bin = exe
			}
			hub := &shepherd.HubHTTP{Board: orDefault(board, defaultBoardAddr())}
			env := &shepherd.Env{
				Store: hub, Hub: hub, Runner: shepherd.ExecRunner{}, Files: shepherd.DiskFiles{},
				Out: cmd.OutOrStdout(), Sleep: shepherd.SleepCtx,
				Cfg: shepherd.Config{Repo: repo, Worktrees: worktrees, Atrium: bin, PollMax: pollMax,
					Rules: shepherd.DefaultRules},
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			st, err := shepherd.New(env).Run(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", st.ItemID, st.Phase)
			if st.LastError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (%s)", st.LastError)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			if st.Phase == shepherd.PhaseFailed || st.Phase == shepherd.PhaseRefused || st.Phase == shepherd.PhaseException {
				return fmt.Errorf("the shepherd stopped in %s", st.Phase)
			}
			return nil
		},
	}
	boardFlag(c, &board)
	c.Flags().StringVar(&repo, "repo", "", "the main checkout (default: the current directory)")
	c.Flags().StringVar(&worktrees, "worktrees", "", "where worktrees go (default: <repo>-worktrees beside it)")
	c.Flags().StringVar(&bin, "atrium", "", "the atrium binary to launch through (default: this one)")
	c.Flags().DurationVar(&pollMax, "poll-max", time.Minute, "the longest wait between two reads of the hub")
	return c
}
