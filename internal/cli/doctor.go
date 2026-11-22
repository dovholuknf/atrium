package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/setupcheck"
	"github.com/dovholuknf/atrium/internal/runnersetup"
	"github.com/spf13/cobra"
)

func newDoctor() *cobra.Command {
	var boardURL string
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check this machine's setup and say what to fix. Changes nothing.",
		Long: "Reads this machine and reports: whether the account atrium runs as is a standard user, whether " +
			"atrium runs from an installed path, which tools (git, go, node, a compiler) are on the PATH, and " +
			"whether each runner is installed, signed in and hooked up.\n\n" +
			"Each row is ok, warn, fail or n/a. A row that is not ok says who fixes it: atrium applies it, you " +
			"run a command, or an administrator does. Doctor only reads. It never writes, installs or restarts " +
			"anything, and has no flag that does.\n\n" +
			"It asks the room on this machine for its answer, because the room's account is the one that " +
			"matters. With no room running it reads this account's own setup and says so. Exits 1 when a row " +
			"fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := doctorReport(boardURL, localSetup)
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(rep); err != nil {
					return err
				}
			} else {
				printDoctor(cmd.OutOrStdout(), rep)
			}
			if rep.Failing > 0 {
				return alreadySaid("%d checks failed", rep.Failing)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON, the same shape the room serves at /v1/setup")
	c.Flags().StringVar(&boardURL, "url", "", "room board address (default: $ATRIUM_BOARD_URL or the recorded one)")
	return c
}

// doctorReport is the room's own answer when one comes, else this account's local one with a row saying no room answered.
func doctorReport(boardURL string, local func() setupcheck.Report) setupcheck.Report {
	addr := boardAddress(boardURL)
	row := setupcheck.Result{ID: "room.running", Label: "a room is running on this machine"}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(addr + "/v1/setup")
	if err == nil {
		defer resp.Body.Close()
		var rep setupcheck.Report
		if resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 1<<22)).Decode(&rep) == nil {
			row.State, row.Detail = setupcheck.OK, "answered at "+addr
			return withRoomRow(rep, row)
		}
	}
	row.State, row.Fix = setupcheck.Warn, setupcheck.FixExplain
	row.Command = "atrium run"
	row.Detail = "no room answered at " + addr + ", so this is the setup of the account running doctor"
	return withRoomRow(local(), row)
}

func withRoomRow(rep setupcheck.Report, row setupcheck.Result) setupcheck.Report {
	rows := rep.Checks
	rep.Checks, rep.Failing, rep.Warning = nil, 0, 0
	rep.Add(row)
	rep.Add(rows...)
	return rep
}

// localSetup is the setup checks with no room to ask: every runner atrium knows how to set up, found on this PATH.
func localSetup() setupcheck.Report {
	rep := setupcheck.Inspect(setupcheck.Env{})
	home, _ := os.UserHomeDir()
	self, _ := os.Executable()
	for _, a := range runnersetup.Adapters {
		env := runnersetup.Env{Home: home, AtriumExe: self}
		for _, cmd := range a.Cmds {
			if p, err := exec.LookPath(cmd); err == nil {
				env.Exe = p
				break
			}
		}
		rep.Add(runnersetup.Rows(a.ID, runnersetup.Inspect(a, env))...)
	}
	return rep
}

var fixWho = map[string]string{
	setupcheck.FixApply:   "apply it from the board's runners pane",
	setupcheck.FixExplain: "you run this",
	setupcheck.FixAdmin:   "an administrator runs this",
}

func printDoctor(out io.Writer, rep setupcheck.Report) {
	for _, c := range rep.Checks {
		fmt.Fprintf(out, "%-5s %-30s %s\n", c.State, c.ID, c.Detail)
		if c.State == setupcheck.OK || c.State == setupcheck.NA || c.Fix == "" {
			continue
		}
		fmt.Fprintf(out, "      fix: %s", fixWho[c.Fix])
		if c.Command != "" {
			fmt.Fprintf(out, ": %s", c.Command)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "\n%d failing, %d warning, %d checks on %s\n", rep.Failing, rep.Warning, len(rep.Checks), strings.TrimSpace(rep.OS))
}
