package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/spf13/cobra"
)

// WHAT VERSION THIS IS, which nothing could answer until now.
//
// It is the first thing every packaging format asks for. Scoop compares it to
// decide whether an update exists, a deb and an rpm refuse to install one
// package over another without it, Homebrew names the bottle with it, and
// `restart_atrium` swapping a binary over another has no way to say whether
// anything changed. `docs/backlog.md` has the rest under "Proper packaging".
//
// It also answers a question that comes up without any of that: this daemon
// has been running since some morning weeks ago, and the only way to find out
// what it is was to compare file timestamps.

// Version is stamped at build time and is `dev` when it is not.
//
// SET BY THE LINKER, not by editing this file:
//
//	go build -ldflags "-X github.com/dovholuknf/atrium/internal/cli.Version=v0.4.1"
//
// A constant edited by hand is a constant that is wrong between the edit and
// the tag, and wrong again on every build off a branch. `dev` is the honest
// answer for a binary built from a working tree, and saying so is worth more
// than a number nobody set.
var Version = "dev"

// Commit is stamped the same way, and falls back to what Go recorded.
//
// Go embeds the revision in the build info when building inside a repository,
// which covers `go build` and `go install` with no flags at all. The linker
// value wins because a release build knows things the compiler does not, such
// as whether the tree was clean.
var Commit = ""

func versionInfo() (version, commit, dirty string) {
	version, commit = Version, Commit
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit, ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "modified"
			}
		}
	}
	return version, commit, dirty
}

// VersionLine is one line, for a log or a header.
func VersionLine() string {
	version, commit, dirty := versionInfo()
	out := "atrium " + version
	if commit != "" {
		if len(commit) > 12 {
			commit = commit[:12]
		}
		out += " (" + commit
		if dirty != "" {
			out += ", " + dirty
		}
		out += ")"
	}
	return out
}

func newVersion() *cobra.Command {
	var short bool
	c := &cobra.Command{
		Use:   "version",
		Short: "What this binary is.",
		Long: "Prints the version, the commit it was built from, the board it carries, and " +
			"the platform.\n\nThe board hash is the same one `/v1/health` reports and the " +
			"same one a popped-out window compares itself against, so two binaries with the " +
			"same version and different board hashes is a thing you can see rather than a " +
			"thing you deduce.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			version, commit, dirty := versionInfo()
			if short {
				fmt.Fprintln(out, version)
				return nil
			}
			fmt.Fprintf(out, "atrium   %s\n", version)
			if commit != "" {
				line := commit
				if dirty != "" {
					line += " (" + dirty + ")"
				}
				fmt.Fprintf(out, "commit   %s\n", line)
			}
			// The board is embedded and versioned separately in practice: a
			// build id that moved without the version moving means somebody
			// rebuilt from a working tree, which is exactly what `dev` says.
			fmt.Fprintf(out, "board    %s\n", api.BuildID)
			fmt.Fprintf(out, "platform %s/%s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Fprintf(out, "go       %s\n", strings.TrimPrefix(runtime.Version(), "go"))
			return nil
		},
	}
	c.Flags().BoolVar(&short, "short", false, "just the version, for a script")
	return c
}
