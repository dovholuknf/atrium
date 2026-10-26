package cli

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// Exit codes of `atrium land-check`: 0 is clean, landConflict a merge conflict, landNotFound no such branch.
const (
	landConflict = 1
	landNotFound = 2
)

func newLandCheck() *cobra.Command {
	var remote, base string
	c := &cobra.Command{
		Use:   "land-check <branch>",
		Short: "Fetch a hub branch and say whether it merges cleanly into claude/main.",
		Long: "Fetches the branch and the base from the hub remote, then prints the commits the branch is ahead " +
			"of the base, whether `git merge-tree` finds a conflict (and the files), and the diffstat. Nothing " +
			"is merged or checked out.\n\nExit code 0 is clean, 1 a conflict, 2 the branch was not found.",
		Args: cobra.ExactArgs(1),
		RunE: speaksForItself(func(cmd *cobra.Command, args []string) error {
			return landCheck(cmd.OutOrStdout(), remote, base, args[0])
		}),
	}
	c.Flags().StringVar(&remote, "remote", "hub", "the remote to fetch from")
	c.Flags().StringVar(&base, "base", "claude/main", "the branch to land into")
	return c
}

func gitOut(args ...string) (string, error) {
	c := exec.Command("git", args...)
	nowindow.Hide(c)
	b, err := c.Output()
	return string(b), err
}

func landCheck(out io.Writer, remote, base, branch string) error {
	if _, err := gitOut("fetch", "--quiet", remote, base, branch); err != nil {
		fmt.Fprintf(out, "not found: %s on %s (or %s)\n", branch, remote, base)
		return exitCodeError{landNotFound}
	}
	head, baseRef := remote+"/"+branch, remote+"/"+base
	ahead, err := gitOut("log", "--oneline", baseRef+".."+head)
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}
	n := 0
	if ahead != "" {
		n = strings.Count(ahead, "\n")
	}
	fmt.Fprintf(out, "%d commit(s) ahead of %s:\n%s", n, base, ahead)

	// merge-tree exits 1 on a conflict and 0 when clean, so a failed run is told apart by its output.
	merged, mergeErr := gitOut("merge-tree", "--write-tree", "--name-only", baseRef, head)
	conflict := false
	if mergeErr != nil {
		ee, ok := mergeErr.(*exec.ExitError)
		if !ok || ee.ExitCode() != 1 {
			return fmt.Errorf("git merge-tree: %w", mergeErr)
		}
		conflict = true
	}
	if conflict {
		fmt.Fprintln(out, "conflict: yes")
		lines := strings.Split(merged, "\n")
		for _, f := range lines[1:] {
			if f == "" {
				break
			}
			fmt.Fprintln(out, "  "+f)
		}
	} else {
		fmt.Fprintln(out, "conflict: no")
	}
	stat, err := gitOut("diff", "--stat", baseRef+"..."+head)
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	fmt.Fprint(out, stat)
	if conflict {
		return exitCodeError{landConflict}
	}
	return nil
}
