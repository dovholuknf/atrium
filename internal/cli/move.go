package cli

import (
	"errors"
	"time"

	"github.com/dovholuknf/atrium/internal/roommove"
	"github.com/spf13/cobra"
)

type moveOpts struct {
	boardURL, from, to, fromURL, toURL, repo string
	wip, fresh, noCapture, check             bool
	lease                                    int
	wait                                     time.Duration
	undo, resume, stopAfter                  string
}

func (o moveOpts) opts() roommove.Opts {
	return roommove.Opts{BoardURL: boardAddress(o.boardURL), From: o.from, To: o.to, FromURL: o.fromURL,
		ToURL: o.toURL, Repo: o.repo, Wip: o.wip, Fresh: o.fresh, NoCapture: o.noCapture, Check: o.check,
		Lease: o.lease, Wait: o.wait, StopAfter: o.stopAfter}
}

func newMove() *cobra.Command {
	var o moveOpts
	c := &cobra.Command{
		Use:   "move <card> --from <room> --to <room>",
		Short: "Move a card, its branch and its conversation, to another room.",
		Long: "Freezes the card on --from, commits nothing unless --wip, carries its branch as a git bundle, its " +
			"conversation, its project memory and its BRIEF.md and HANDOFF files, parks it, and resumes it on --to " +
			"as a new card with moved_from. Then the old card gets moved_to, its held messages are forwarded, the new " +
			"card takes its alias and pin, and the old card is done.\n\n" +
			"Through the hub (--url, the default board) or straight to each room's board (--from-url, --to-url), " +
			"which needs no hub. --undo <move id> rolls a move back that did not reach the cut-over, and --resume " +
			"<move id> finishes one that did.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch {
			case o.undo != "":
				return roommove.Undo(out, o.undo, o.opts())
			case o.resume != "":
				return roommove.Resume(out, o.resume, o.opts())
			}
			if len(args) != 1 {
				return errors.New("say which card")
			}
			return roommove.Run(out, args[0], o.opts())
		},
	}
	c.Flags().StringVar(&o.from, "from", "", "the room the card is on (or give the card as room~id)")
	c.Flags().StringVar(&o.to, "to", "", "the room to move it to")
	c.Flags().StringVar(&o.fromURL, "from-url", "", "the source room's own board, instead of the hub")
	c.Flags().StringVar(&o.toURL, "to-url", "", "the destination room's own board, instead of the hub")
	c.Flags().StringVar(&o.repo, "repo", "", "host/org/repo, instead of the card's origin remote")
	c.Flags().BoolVar(&o.wip, "wip", false, "commit a dirty tree as `WIP: move to <room>` instead of refusing")
	c.Flags().BoolVar(&o.fresh, "fresh", false, "do not carry the conversation: start from the handoff")
	c.Flags().BoolVar(&o.noCapture, "no-capture", false, "do not ask the card to write its handoff first")
	c.Flags().BoolVar(&o.check, "check", false, "check only, move nothing")
	c.Flags().IntVar(&o.lease, "lease", 1800, "seconds the source holds the freeze before it undoes it on its own")
	c.Flags().DurationVar(&o.wait, "wait", time.Minute, "how long to wait for the new card to start")
	c.Flags().StringVar(&o.undo, "undo", "", "roll back the move with this id")
	c.Flags().StringVar(&o.resume, "resume", "", "finish the move with this id from where it stopped")
	c.Flags().StringVar(&o.stopAfter, "stop-after", "", "stop the driver after this step, as a crash would (frozen, checked, worktree, parked, launched, moved). for testing")
	_ = c.Flags().MarkHidden("stop-after")
	c.Flags().StringVar(&o.boardURL, "url", "", "the hub's board (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}
