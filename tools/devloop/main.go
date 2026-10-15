// devloop runs the repeat orchestration chores around atrium: landing a branch, pruning a finished one, collecting
// screenshots and summarizing usage. It is its own program on purpose. It talks to atrium only over the hub HTTP API,
// git and ssh, and imports nothing from the atrium module, so it can be lifted into its own repo.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const usageText = `devloop <command> [flags]

  land <branch> [--item <id>]          fetch hub, put the branch on claude/main, deploy the hub, mark the item done
  prune <branch> --room <room>         exit the card, remove the worktree and branches on the room, local and hub
  shots --room <room> <remote dir>     copy PNGs from a room's directory to D:\tmp\shots\<room>
  usage --since <RFC3339> [--until]    says, tokens and cards CSVs plus a summary

Each step prints one line. Any failure exits non-zero.`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errw io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(out, usageText)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var err error
	switch args[0] {
	case "land":
		err = cmdLand(args[1:], out, realGit{})
	case "prune":
		err = cmdPrune(args[1:], out)
	case "shots":
		err = cmdShots(args[1:], out)
	case "usage":
		err = cmdUsage(args[1:], out)
	default:
		fmt.Fprintf(errw, "devloop: unknown command %q\n%s\n", args[0], usageText)
		return 2
	}
	if err != nil {
		fmt.Fprintf(errw, "devloop %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

// step prints the one line a step is allowed.
func step(out io.Writer, format string, a ...any) {
	fmt.Fprintf(out, format+"\n", a...)
}

// splitArgs separates positionals from flags so a branch can come before or after its flags. A flag in valued is
// followed by its value, which stays with it. Everything else starting with "-" is taken as a boolean or --k=v.
func splitArgs(args []string, valued ...string) (pos, flags []string) {
	takes := map[string]bool{}
	for _, v := range valued {
		takes[v] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(a, "=") && takes[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return pos, flags
}
