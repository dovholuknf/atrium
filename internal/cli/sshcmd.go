package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/sshtarget"
)

// newSSH is `atrium ssh`. See docs/rnd/ssh-across-rooms.md. It edits the list of addresses this room offers to other
// rooms and holds no credential.
func newSSH() *cobra.Command {
	var dir string
	c := &cobra.Command{
		Use:   "ssh",
		Short: "The addresses this room offers to other rooms over ssh",
	}
	c.PersistentFlags().StringVar(&dir, "dir", "", "where this machine's room keeps its certificate")
	where := func() string { return orDefault(dir, roomDir()) }

	var from, user, note string
	var replace bool
	add := &cobra.Command{
		Use:   "add <name> <host:port>",
		Short: "Offer a host:port, and name the rooms that may reach it",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := sshtarget.Target{Name: args[0], Addr: args[1], User: user, Note: note, From: []string{}}
			for _, f := range strings.Split(from, ",") {
				if f = strings.TrimSpace(f); f != "" {
					t.From = append(t.From, f)
				}
			}
			if err := sshtarget.Add(where(), t, replace); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(t.From) == 0 {
				fmt.Fprintf(out, "%s is offered to no room yet. add --from <room> or --from '*'\n", t.Name)
				return nil
			}
			fmt.Fprintf(out, "%s is offered at %s to %s\n", t.Name, t.Addr, strings.Join(t.From, ","))
			return nil
		},
	}
	add.Flags().StringVar(&from, "from", "", "rooms that may reach it, comma separated, or '*'")
	add.Flags().StringVar(&user, "user", "", "the login name to write into the generated ssh config")
	add.Flags().StringVar(&note, "note", "", "one line saying what it is")
	add.Flags().BoolVar(&replace, "replace", false, "change a target that already exists")

	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Stop offering a target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := sshtarget.Remove(where(), args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "removed "+args[0])
			return nil
		},
	}

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List what this room offers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := sshtarget.Load(where())
			if err != nil {
				return err
			}
			for _, t := range s.Targets {
				fl := strings.Join(t.From, ",")
				if fl == "" {
					fl = "(nobody)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", t.Name, t.Addr, fl, t.User, t.Note)
			}
			return nil
		},
	}
	c.AddCommand(add, rm, ls, sshConfigCmd(), sshSetupCmd())
	return c
}
