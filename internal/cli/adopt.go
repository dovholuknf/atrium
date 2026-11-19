package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// A scan never enters these: they hold no project of their own and can be huge.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, ".git": true, ".cache": true}

// It does not enter a checkout, so a repo's own subfolders are never taken for projects.
func scanRepos(root string, depth int) []string {
	var out []string
	var walk func(dir string, left int)
	walk = func(dir string, left int) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			out = append(out, filepath.ToSlash(dir))
			return
		}
		if left <= 0 {
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() && !skipDirs[e.Name()] && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), left-1)
			}
		}
	}
	walk(root, depth)
	sort.Strings(out)
	return out
}

func newAdopt() *cobra.Command {
	var boardURL string
	var scan bool
	var depth int
	var dry bool
	c := &cobra.Command{
		Use:   "adopt <folder>...",
		Short: "Put the repos you already have on the new agent dialog's repo list. Starts nothing.",
		Long: "Each folder is a repo or a worktree to adopt, or with --scan a folder to look in for them, " +
			"--depth folders down (default 2). Adopted repos show in the repo field of the board's new agent " +
			"dialog, and a card made from one gets its own worktree under the card root. No card is made and no " +
			"agent starts. Running it again adds nothing.\n\n" +
			"Run it on the machine the repos are on: it adds them to that machine's room.\n\n" +
			"  atrium adopt ~/git/myproject\n  atrium adopt --scan ~/git",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dirs []string
			for _, a := range args {
				abs, err := filepath.Abs(a)
				if err != nil {
					return err
				}
				if scan {
					found := scanRepos(abs, depth)
					if len(found) == 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "nothing under %s\n", filepath.ToSlash(abs))
					}
					dirs = append(dirs, found...)
				} else {
					dirs = append(dirs, filepath.ToSlash(abs))
				}
			}
			if dry {
				for _, d := range dirs {
					fmt.Fprintln(cmd.OutOrStdout(), "would adopt", d)
				}
				return nil
			}
			if len(dirs) == 0 {
				return nil
			}
			body, _ := json.Marshal(map[string]any{"dirs": dirs})
			resp, err := (&http.Client{Timeout: 30 * time.Second}).Post(boardAddress(boardURL)+"/v1/adopt", "application/json", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("no room answered: %w. start one with `atrium run`", err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			var out struct {
				Results []struct{ Dir, Status, Branch, Why string } `json:"results"`
				Error   string                                      `json:"error"`
			}
			if err := json.Unmarshal(raw, &out); err != nil || resp.StatusCode != http.StatusOK {
				return fmt.Errorf("the room refused: %s", strings.TrimSpace(string(raw)))
			}
			n := 0
			for _, r := range out.Results {
				line := r.Status + " " + r.Dir
				if r.Branch != "" {
					line += " (" + r.Branch + ")"
				}
				if r.Why != "" {
					line += ": " + r.Why
				}
				if r.Status == "adopted" {
					n++
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d adopted. they are in the repo list of the new agent dialog\n", n)
			return nil
		},
	}
	c.Flags().StringVar(&boardURL, "url", "", "the room's board address (default: $ATRIUM_BOARD_URL or the recorded one)")
	c.Flags().BoolVar(&scan, "scan", false, "each folder is a place to look for repos, not a repo")
	c.Flags().IntVar(&depth, "depth", 2, "with --scan, how many folders down to look")
	c.Flags().BoolVar(&dry, "dry-run", false, "list what would be adopted and change nothing")
	return c
}
