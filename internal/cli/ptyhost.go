package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/ptyhost"
)

// newPtyHost is `atrium ptyhost`: the process that owns the pseudo terminals so a daemon can restart without ending
// them. Hidden, and started by the room when the pty_host setting is on: ptyhost.Start launches it detached from a copy
// of the binary.
// See docs/terminal/ptyhost-protocol.md.
func newPtyHost() *cobra.Command {
	var stateDir, build, logPath string
	var idle time.Duration
	cmd := &cobra.Command{
		Use:    "ptyhost",
		Short:  "Own the pseudo terminals, so the daemon can restart without ending them.",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ptyhost.Run(ptyhost.RunOptions{StateDir: stateDir, Build: build, IdleExit: idle, Log: logPath})
		},
	}
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "the state dir this host belongs to, which names its channel")
	cmd.Flags().StringVar(&build, "build", "", "the build string reported to a daemon")
	cmd.Flags().StringVar(&logPath, "log", "", "append the host's log and any crash to this file")
	cmd.Flags().DurationVar(&idle, "idle-exit", ptyhost.DefaultIdleExit,
		"exit after this long with no pty and no daemon connected")
	_ = cmd.MarkFlagRequired("state-dir")
	return cmd
}
