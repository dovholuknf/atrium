package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	basedetach "github.com/dovholuknf/atrium/internal/base/detach"
	"github.com/dovholuknf/atrium/internal/statusline"
	"github.com/spf13/cobra"
)

// newStatusline draws Claude Code's status line from the JSON on stdin and posts the usage percents to the room. It is
// a runner hook only for the exit rule: a status line that fails draws nothing, so every error is a shorter line.
func newStatusline() *cobra.Command {
	var postURL, postBody string
	c := &cobra.Command{
		Use:   "statusline",
		Short: "Print the Claude Code status line. Run by Claude Code, not by hand.",
		Long: "Reads Claude Code's status JSON on stdin, prints the line, and posts the 5h, weekly and context percents " +
			"to the room from a detached copy of this command, so a room that is down never slows the draw.\n\n" +
			"Set it as statusLine.command in settings.json, as `<atrium> statusline`.",
		SilenceUsage:       true,
		SilenceErrors:      true,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		Annotations:        map[string]string{runnerHook: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if postURL != "" {
				statusline.Post(postURL, postBody)
				return nil
			}
			in, _ := io.ReadAll(cmd.InOrStdin())
			home, _ := os.UserHomeDir()
			fmt.Fprint(cmd.OutOrStdout(), statusline.Render(in, home, time.Now(), spawnPost))
			return nil
		},
	}
	c.Flags().StringVar(&postURL, "post-url", "", "the detached post, not for hands")
	c.Flags().StringVar(&postBody, "post-body", "", "the detached post, not for hands")
	_ = c.Flags().MarkHidden("post-url")
	_ = c.Flags().MarkHidden("post-body")
	return c
}

// spawnPost starts the post as its own process with no stdio, so Claude Code's read of the line ends with this one.
func spawnPost(url string, body []byte) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if p, err := basedetach.Start(exe, []string{"statusline", "--post-url", url, "--post-body", string(body)}, ""); err == nil {
		_ = p.Release()
	}
}
