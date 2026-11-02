package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// `atrium twig <name>`: gwt's `twig` from inside a worktree. `POST /v1/new` with twig, internal/api/twig.go. It goes
// to this machine's room, never another, since the room reads the worktree's files.

func newTwig() *cobra.Command {
	var o newOpts
	c := &cobra.Command{
		Use:   "twig <name>",
		Short: "Branch off this worktree's HEAD into a new worktree <name>, carrying its uncommitted work, and a card in it.",
		Long: "Run inside a worktree. The room makes a branch and a worktree called <name> from this worktree's local " +
			"HEAD, applies its tracked changes there unstaged (`git diff HEAD --binary`) and copies its untracked " +
			"files. This worktree is left as it is. A patch that does not apply is saved beside the new worktree and " +
			"named. The repo's setup hook runs and a card starts there. A name that is already a branch or a worktree " +
			"is refused.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
			if err != nil {
				return fmt.Errorf("this directory is not in a git worktree. run it inside the worktree to twig from")
			}
			o.twig = filepath.ToSlash(strings.TrimSpace(string(top)))
			repo, err := repoOfCheckout()
			if err != nil {
				return err
			}
			return startNew(repo, strings.TrimSpace(args[0]), o)
		},
	}
	c.Flags().StringVar(&o.prompt, "prompt", "", "the card's first prompt")
	c.Flags().StringVar(&o.runner, "runner", "", "which configured runner starts the card")
	c.Flags().BoolVar(&o.attach, "attach", false, "attach this terminal to the card. ctrl+] leaves it")
	c.Flags().BoolVar(&o.asJSON, "json", false, "print the answer as json")
	c.Flags().StringVar(&o.boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}
