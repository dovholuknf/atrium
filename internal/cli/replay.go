package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/spf13/cobra"
)

// `atrium replay` renders a captured terminal stream the way an attach would.
//
// THE POINT IS TO TAKE THE DAEMON OUT OF THE LOOP. Every change to how
// scrollback renders used to mean a build, a wind-down, a restart with every
// session on the board interrupted, and then reading the result by eye out of
// a pane. That loop is slow enough that this rendering was twice declared
// fixed on the strength of tests written beside it, and twice reverted the
// moment somebody looked at it.
//
// With a captured stream and this command the loop is: change the renderer,
// run it, read the file. Nothing restarts and nothing is interrupted.
//
// WHERE A STREAM COMES FROM, in the order they cost nothing to get:
//
//   - `ATRIUM_TAP_DIR=<dir>` in the daemon's environment writes every byte out
//     of every pty to `<dir>/<card-id>.tap`, BEFORE the ring, the collapse,
//     and any renderer. It is the only source that can distinguish "the
//     renderer lost it" from "the runner never printed it".
//   - `GET /v1/tasks/{id}/scrollback/raw?collapse=0` is a live card's ring.
//     Without `collapse=0` it has already had repeated in-place frames folded
//     down, which is right for a pane and wrong for an investigation.
//   - `~/.atrium/scrollback/<card-id>.scrollback` is what a card held before
//     the last clean stop.

func newReplayCmd() *cobra.Command {
	var mode, out string
	var cols, rows int
	var stats, all bool

	c := &cobra.Command{
		Use:   "replay [file]",
		Short: "Render a captured terminal stream the way an attach would",
		Long: "Reads a captured terminal byte stream and writes what the board's " +
			"terminal would show.\n\n" +
			"Reads stdin when no file is given, writes stdout when --out is not.\n\n" +
			"THE SIZE IS NOT OPTIONAL in any meaningful sense. A terminal user " +
			"interface composes for a specific grid: hard line breaks at the column " +
			"it was told, and absolute cursor moves to rows it worked out itself. " +
			"Replaying into a different grid is the difference between reading the " +
			"output and reading its wreckage. Take the size off the card's " +
			"`last_cols`, or off the `X-Atrium-Cols` and `X-Atrium-Rows` headers " +
			"that `/scrollback/raw` answers with.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				b   []byte
				err error
			)
			if len(args) == 1 && args[0] != "-" {
				b, err = os.ReadFile(args[0])
			} else {
				b, err = io.ReadAll(cmd.InOrStdin())
			}
			if err != nil {
				return err
			}

			// Every mode, for comparing them. One file per mode beside the
			// output path, because the whole question is usually which of them
			// is least wrong on this particular stream.
			if all {
				if out == "" {
					return fmt.Errorf("--all writes one file per mode, so it needs --out")
				}
				for _, m := range daemon.ReplayModes {
					r := daemon.Replay(b, m, cols, rows)
					p := out + "." + m + ".txt"
					if err := os.WriteFile(p, r, 0o644); err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "%-7s %s\n", m, statLine(p, r))
				}
				return nil
			}

			if !validMode(mode) {
				return fmt.Errorf("no mode called %q. the ones there are: %s",
					mode, strings.Join(daemon.ReplayModes, ", "))
			}
			r := daemon.Replay(b, mode, cols, rows)
			if out != "" {
				if err := os.WriteFile(out, r, 0o644); err != nil {
					return err
				}
			} else if _, err := cmd.OutOrStdout().Write(r); err != nil {
				return err
			}
			// To stderr, always, so it is there when the rendering went to
			// stdout and is being piped into something.
			if stats || out != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "%-7s %s\n", mode, statLine(out, r))
			}
			return nil
		},
	}

	c.Flags().StringVar(&mode, "mode", "screen",
		"how to render it: "+strings.Join(daemon.ReplayModes, ", "))
	c.Flags().BoolVar(&all, "all", false, "write one file per mode, named after --out")
	c.Flags().StringVar(&out, "out", "", "write here instead of stdout")
	c.Flags().IntVar(&cols, "cols", 0, "the width the stream was COMPOSED at (default 80)")
	c.Flags().IntVar(&rows, "rows", 0, "the height it was composed at (default 24)")
	c.Flags().BoolVar(&stats, "stats", false, "count lines, blanks and padded lines, to stderr")
	return c
}

func validMode(m string) bool {
	for _, k := range daemon.ReplayModes {
		if k == m {
			return true
		}
	}
	return false
}

// statLine is the one line that says whether a change helped, without reading
// the output. Padded lines are the flattener's signature, since it has to put
// something where a cursor move was and what it has is spaces. Blank lines are
// the screen model's, since a repaint scrolls empty grid rows into history.
func statLine(path string, out []byte) string {
	st := daemon.StatsFor(out)
	where := path
	if where == "" {
		where = "(stdout)"
	}
	return fmt.Sprintf("%7d bytes  %5d lines  %4d blank  %4d padded  %s",
		st.Bytes, st.Lines, st.Blank, st.Padded, where)
}
