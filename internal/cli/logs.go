package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/oslog"
)

// newLogs is `atrium logs`: the last lines of the rolling log the hub, room and pty host all write.
func newLogs() *cobra.Command {
	var component string
	var n int
	var follow bool
	c := &cobra.Command{
		Use:   "logs",
		Short: "Show the last lines atrium's hub, room and pty host logged.",
		Long: "Show the last lines of the rolling log under the atrium folder. The same lines go to journald on Linux, " +
			"the unified log on macOS (subsystem-less, tag atrium) and the Application Event Log on Windows.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := logsDir()
			lines, err := oslog.Tail(dir, component, n)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, l := range lines {
				fmt.Fprintln(out, l)
			}
			if !follow {
				return nil
			}
			return oslog.Follow(cmd.Context(), filepath.Join(dir, oslog.FileName), component, out, 500*time.Millisecond)
		},
	}
	c.Flags().StringVar(&component, "component", "", "only lines of this component: hub, room, ptyhost, link, supervisor")
	c.Flags().IntVarP(&n, "lines", "n", 50, "how many lines to show, 0 for all")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	_ = os.Stdout
	return c
}
