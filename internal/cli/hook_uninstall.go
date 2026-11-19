package cli

import (
	"fmt"
	"io"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
	"github.com/spf13/cobra"
)

func newHookUninstall() *cobra.Command {
	var runner, project string
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Take atrium's hooks out of a runner's settings.",
		Long: "Removes every hook command an atrium binary runs, and claude's status line when it is " +
			"`atrium statusline`, keeping a copy of the old file first and leaving everything else in it alone. " +
			"Running it again changes nothing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := claudeconf.TargetFor(runner)
			if target.ID != runner {
				return fmt.Errorf("unknown hook runner %q", runner)
			}
			target, err := projectTarget(target, project)
			if err != nil {
				return err
			}
			res, err := claudeconf.UninstallTarget(target)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(res.Removed) == 0 {
				fmt.Fprintf(out, "%s has no atrium hooks. nothing was written.\n", res.Path)
				return nil
			}
			pingHooksChanged()
			fmt.Fprintf(out, "wrote %s\n", res.Path)
			fmt.Fprintf(out, "the old settings are at %s\n", res.Backup)
			for _, r := range res.Removed {
				fmt.Fprintf(out, "  removed %s\n", r)
			}
			fmt.Fprintln(out, "sessions already running keep their old settings until restarted.")
			return nil
		},
	}
	c.Flags().StringVar(&runner, "runner", "claude", "hook target: claude or codex")
	c.Flags().StringVar(&project, "project", "", "take them out of <dir>/.claude/settings.local.json instead")
	return c
}

// wireHooksOnFirstRun installs claude's hooks when its settings have none of atrium's, and never fails the run.
func wireHooksOnFirstRun(out io.Writer, noHooks bool) {
	if noHooks || claudeconf.HasAtriumHooks(claudeconf.Claude) {
		return
	}
	exe := claudeconf.HookExe("")
	if exe == "" {
		fmt.Fprintln(out, "claude has no atrium hooks, and atrium cannot tell which binary to wire. run `atrium hook install`")
		return
	}
	rep, res, err := claudeconf.InstallOnlyTarget(claudeconf.Claude, exe, nil)
	switch {
	case err != nil:
		fmt.Fprintf(out, "claude has no atrium hooks, and wiring them failed: %v. run `atrium hook install`\n", err)
	case res.Backup != "":
		fmt.Fprintf(out, "wired atrium's hooks into %s, the old file is at %s. --no-hooks skips this\n", rep.Path, res.Backup)
	default:
		fmt.Fprintf(out, "wired atrium's hooks into %s. --no-hooks skips this\n", rep.Path)
	}
}
