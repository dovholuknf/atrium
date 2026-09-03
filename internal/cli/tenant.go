package cli

import (
	"fmt"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/spf13/cobra"
)

// Naming an atrium, so its cards cannot be confused with another's.
//
// A subcommand rather than a flag on `daemon`, because this is a decision made
// once and then never again. A flag would have to be passed on every start,
// and a start that forgot it would register a whole board of cards under the
// wrong names.

func newName() *cobra.Command {
	var dbPath string
	c := &cobra.Command{
		Use:   "name [<name>]",
		Short: "Name this atrium, once, so its cards are unique across machines.",
		Long: "Every session on this machine registers under this name, so two machines with " +
			"the same directory layout cannot claim each other's cards.\n\n" +
			"Immutable. Changing it would orphan every card already registered under the old " +
			"name, and those cards are the history atrium exists to keep.\n\n" +
			"With no argument, says what this atrium is currently called.\n\n" +
			"Only needed by an atrium that will be part of something larger. One machine has " +
			"nothing to collide with.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := dbPath
			if path == "" {
				// The same file the daemon opens, so naming an atrium from a
				// terminal and running it are talking about one board.
				path = daemon.DefaultDBPath()
			}
			st, err := store.Open(path)
			if err != nil {
				return err
			}
			defer st.Close()

			out := cmd.OutOrStdout()
			if len(args) == 0 {
				if name := st.Tenant(); name != "" {
					fmt.Fprintf(out, "this atrium is called %q\n", name)
					fmt.Fprintf(out, "its sessions register as %s\n", st.Qualify("<session>"))
					return nil
				}
				fmt.Fprintln(out, "this atrium has no name, so its sessions register under "+
					"whatever they call themselves.")
				fmt.Fprintln(out, "name it with: atrium name <name>")
				return nil
			}

			want := store.NormalizeTenant(args[0])
			if want == "" {
				return fmt.Errorf("%q leaves nothing usable. letters, digits, - and _", args[0])
			}
			if want != args[0] {
				fmt.Fprintf(out, "using %q\n", want)
			}
			if err := st.SetTenant(want); err != nil {
				return err
			}
			fmt.Fprintf(out, "this atrium is called %q. sessions register as %s\n",
				want, st.Qualify("<session>"))
			fmt.Fprintln(out, "cards registered before now keep the names they already have.")
			return nil
		},
	}
	c.Flags().StringVar(&dbPath, "db", "", "state file (default: the same one the daemon uses)")
	return c
}
