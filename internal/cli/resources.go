package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/resources"
)

// newResources is `atrium resources`. See docs/fabric/f-003-resources-design.md.
func newResources() *cobra.Command {
	c := &cobra.Command{
		Use:   "resources",
		Short: "The inventory of machines and environments agents may use",
		Long: "The inventory is the file resources.md in atrium's state dir, edited by hand and read by agents\n" +
			"through atrium_resources. It names hosts, identities and commands, and never holds a token,\n" +
			"password or key.",
	}
	c.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a starter resources.md, and never overwrite one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := resources.Init(daemon.StateDir())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "wrote "+p)
			return nil
		},
	})
	return c
}
