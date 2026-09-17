// Command atrium2 is the hub and the room.
//
// A SEPARATE BINARY, DELIBERATELY. `atrium` is running right now with agents
// attached to it, and the whole point of this work is that changing the board
// must not disturb them. Shipping the split inside the same executable would
// mean the first thing anybody testing it did was restart the thing it exists
// to avoid restarting.
//
// Two commands:
//
//	atrium2 hub            serve the board. Prints one line to paste.
//	atrium2 join <line>    run the agents here and attach them to that hub.
//
// That is the whole setup. See `docs/hub-room-plan.md`.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is stamped by the linker, the same way the main binary's is.
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "atrium2",
		Short: "A hub that serves the board, and rooms that run the agents",
		Long: "Atrium, split in two along the line where the cost is.\n\n" +
			"The HUB serves the board and holds nothing. Restart it as often as you like.\n" +
			"A ROOM holds the database and the running agents. It stays up for days.\n\n" +
			"The room dials the hub, so the hub can come and go without the room noticing,\n" +
			"and a room behind a firewall needs nothing opened.\n\n" +
			"  atrium2 hub             on the machine you want the board on\n" +
			"  atrium2 join <string>   on the machine your agents run on\n",
		SilenceUsage: true,
	}
	root.AddCommand(hubCmd(), joinCmd(), roomCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "atrium2:", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "What this binary is",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	}
}
