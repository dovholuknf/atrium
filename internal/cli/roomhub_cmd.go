package cli

import (
	"errors"
	"fmt"
	"net"

	"github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/spf13/cobra"
)

// roomHubCmd is `atrium room hub [addr]`, the address this room dials its hub at.
//
// It exists because the hub moves: a DHCP lease changes and the address in room.json, which
// the join string put there, is wrong for good. Editing the file by hand is what this
// replaces. It writes room.json and nothing else, so it works on a stopped room and on one that
// cannot reach its hub, which is when it is needed. A running room that cannot reach its hub reads it
// again before its next attempt, and an attached one at its next start.
func roomHubCmd() *cobra.Command {
	var dir string
	c := &cobra.Command{
		Use:   "hub [host:port]",
		Short: "Show, or change, the address this room dials its hub at",
		Long: "With no argument this prints the address in room.json. With one it writes that address\n" +
			"there, after copying the old file to room.json.bak-<time> beside it. Only direct rooms\n" +
			"have one. A room that cannot reach its hub tries it at its next attempt, and an attached\n" +
			"one at its next start.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := transport.Keys{Dir: orDefault(dir, roomDir())}
			out := cmd.OutOrStdout()
			saved, err := keys.Joined()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Fprintln(out, saved.Hub)
				return nil
			}
			addr := hostPort(args[0])
			if _, port, err := net.SplitHostPort(addr); err != nil || port == "" {
				return errors.New("the hub's address is host:port, such as 192.168.102.41:7779")
			}
			backup, err := keys.SetHub(addr)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "hub: %s (was %s)\nthe old room.json is at %s\n"+
				"a room that cannot reach its hub tries it at its next attempt. an attached one takes it at its\n"+
				"next start: restart it with `atrium room --detach`, or ask the hub to restart it.\n",
				addr, saved.Hub, backup)
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "where this room keeps its certificate")
	return c
}
