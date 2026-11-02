package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/worktrees"
)

// `atrium update` is gwt update: it runs git here, on this machine, in the repository's worktrees.
func newUpdate() *cobra.Command {
	var repo string
	var asJSON bool
	c := &cobra.Command{
		Use:   "update",
		Short: "Fast-forward every clean worktree whose upstream is still live.",
		Long: "Fetches with prune, then for each worktree of the repository fast-forwards its branch to its upstream. " +
			"A dirty worktree, a locked one, a detached one, and a branch with no upstream or whose upstream is gone " +
			"are skipped, and every skip says why. Nothing is ever merged but a fast-forward.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := repo
			if dir == "" {
				var err error
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}
			res, err := worktrees.Updates(context.Background(), dir, nil)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return json.NewEncoder(out).Encode(res)
			}
			for _, u := range res {
				if u.Skipped != "" {
					fmt.Fprintf(out, "skipped  %s (%s): %s\n", u.Path, u.Branch, u.Skipped)
					continue
				}
				fmt.Fprintf(out, "ok       %s (%s): %s\n", u.Path, u.Branch, u.Done)
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "a directory inside any checkout of the repository (default: the current directory)")
	c.Flags().BoolVar(&asJSON, "json", false, "print the answer as json")
	return c
}
