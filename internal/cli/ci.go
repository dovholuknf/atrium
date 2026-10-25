package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/base/forge"
)

// `atrium ci`: atrium_ci from a terminal, asked through the same door (roomCIDoor). A log or an artifact's file is
// written to a file and its path printed, because a log is read with an editor or grep, not scrolled past.

func ciCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ci",
		Short: "A repository's CI, read through the hub: runs, run, log, artifacts, artifact",
		Long: "Read only, and answered by the hub, which has gh, as atrium_ci is. <repo> is <owner>/<repo> or\n" +
			"<host>/<owner>/<repo>. A log is written to a file and its path printed.",
	}
	c.AddCommand(ciRunsCmd(), ciRunCmd(), ciLogCmd(), ciArtifactsCmd(), ciArtifactCmd())
	return c
}

func ciAsk(repo string, a forge.HubCIAsk) (forge.HubCI, error) {
	return roomCIDoor().AskRepo(context.Background(), nil, repo, a)
}

func ciID(s, what string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s is a number, not %q", what, s)
	}
	return n, nil
}

var ciFileUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func ciWrite(out, name, text string) (string, error) {
	if out == "" {
		dir := filepath.Join(os.TempDir(), "atrium-ci")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		out = filepath.Join(dir, strings.Trim(ciFileUnsafe.ReplaceAllString(name, "-"), "-")+".log")
	}
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return "", err
	}
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	}
	return filepath.ToSlash(out), nil
}

func ciRunsCmd() *cobra.Command {
	var branch, sha string
	var limit int
	c := &cobra.Command{
		Use:   "runs <repo>",
		Short: "The runs, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ans, err := ciAsk(args[0], forge.HubCIAsk{Action: forge.CIRuns, Branch: branch, SHA: sha, Limit: limit})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(ans.Runs) == 0 {
				fmt.Fprintln(w, "no runs")
			}
			for _, r := range ans.Runs {
				sha := r.Head
				if len(sha) > 10 {
					sha = sha[:10]
				}
				fmt.Fprintf(w, "%-12d %-11s %-10s %-10s %-24s %s  %s\n", r.ID, r.Status, orDefault(r.Conclusion, "-"), sha,
					r.Branch, r.Workflow, r.Created)
			}
			return nil
		},
	}
	c.Flags().StringVar(&branch, "branch", "", "only runs of this branch")
	c.Flags().StringVar(&sha, "sha", "", "only runs of this commit")
	c.Flags().IntVar(&limit, "limit", 0, "how many, default 10, at most 50")
	return c
}

func ciRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <repo> <run-id>",
		Short: "One run's jobs and their steps",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ciID(args[1], "a run id")
			if err != nil {
				return err
			}
			ans, err := ciAsk(args[0], forge.HubCIAsk{Action: forge.CIRun, RunID: id})
			if err != nil {
				return err
			}
			if ans.Run == nil {
				return fmt.Errorf("the hub answered no run %d", id)
			}
			w := cmd.OutOrStdout()
			r := ans.Run
			fmt.Fprintf(w, "run %d  %s  %s/%s  %s  %s\n", r.ID, r.Workflow, r.Status, orDefault(r.Conclusion, "-"), r.Branch, r.URL)
			for _, j := range r.Jobs {
				fmt.Fprintf(w, "\njob %d  %s  %s/%s\n", j.ID, j.Name, j.Status, orDefault(j.Conclusion, "-"))
				for _, s := range j.Steps {
					fmt.Fprintf(w, "  %3d  %-10s %s\n", s.Number, orDefault(s.Conclusion, s.Status), s.Name)
				}
			}
			return nil
		},
	}
}

func ciLogCmd() *cobra.Command {
	var run, job int64
	var tail int
	var failedOnly bool
	var out string
	c := &cobra.Command{
		Use:   "log <repo> --run <id> | --job <id>",
		Short: "The end of a job's log, or of a run's failed jobs, written to a file",
		Long: "The last --tail lines (default 400, at most 5000) within 64 KiB, as atrium_ci answers them. A run's log is\n" +
			"its failed steps unless --failed-only=false. The file is --out, or one under the temp folder, and its\n" +
			"path is printed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (run == 0) == (job == 0) {
				return fmt.Errorf("name one of --run or --job")
			}
			a := forge.HubCIAsk{Action: forge.CILogs, RunID: run, JobID: job, Tail: tail}
			if cmd.Flags().Changed("failed-only") {
				a.FailedOnly = &failedOnly
			}
			ans, err := ciAsk(args[0], a)
			if err != nil {
				return err
			}
			if ans.Log == nil {
				return fmt.Errorf("the hub answered no log")
			}
			name := args[0] + "-run-" + strconv.FormatInt(run, 10)
			if job != 0 {
				name = args[0] + "-job-" + strconv.FormatInt(job, 10)
			}
			path, err := ciWrite(out, name, ans.Log.Text)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, path)
			note := fmt.Sprintf("%d lines", ans.Log.Lines)
			if ans.Log.FailedOnly {
				note += ", failed steps only"
			}
			if ans.Log.Truncated {
				note += ", the end of a longer log"
			}
			fmt.Fprintln(w, note)
			return nil
		},
	}
	c.Flags().Int64Var(&run, "run", 0, "a run's failed jobs")
	c.Flags().Int64Var(&job, "job", 0, "one job")
	c.Flags().IntVar(&tail, "tail", 0, "the last N lines, default 400, at most 5000")
	c.Flags().BoolVar(&failedOnly, "failed-only", false, "only the failed steps. default true for a run and false for a job")
	c.Flags().StringVar(&out, "out", "", "the file to write, instead of one under the temp folder")
	return c
}

func ciArtifactsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "artifacts <repo> <run-id>",
		Short: "A run's artifacts: name, size, expiry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ciID(args[1], "a run id")
			if err != nil {
				return err
			}
			ans, err := ciAsk(args[0], forge.HubCIAsk{Action: forge.CIArtifacts, RunID: id})
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(ans.Artifacts) == 0 {
				fmt.Fprintln(w, "no artifacts")
			}
			for _, a := range ans.Artifacts {
				state := ""
				if a.Expired {
					state = "expired"
				}
				fmt.Fprintf(w, "%-40s %12d  %s\n", a.Name, a.Size, state)
			}
			return nil
		},
	}
}

func ciArtifactCmd() *cobra.Command {
	var file, out string
	var tail int
	c := &cobra.Command{
		Use:   "artifact <repo> <run-id> <name>",
		Short: "Have the hub download an artifact, and list its files or write the end of one to a file",
		Long: "The hub downloads the artifact into a folder on the hub's machine and lists its files. --file reads the\n" +
			"last --tail lines of one of them into --out, or a file under the temp folder, and prints its path.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ciID(args[1], "a run id")
			if err != nil {
				return err
			}
			ans, err := ciAsk(args[0], forge.HubCIAsk{Action: forge.CIArtifact, RunID: id, Name: args[2], File: file, Tail: tail})
			if err != nil {
				return err
			}
			d := ans.Download
			if d == nil {
				return fmt.Errorf("the hub answered no download")
			}
			w := cmd.OutOrStdout()
			if file != "" {
				path, err := ciWrite(out, args[0]+"-run-"+args[1]+"-"+args[2]+"-"+file, d.Text)
				if err != nil {
					return err
				}
				fmt.Fprintln(w, path)
				if d.Truncated {
					fmt.Fprintln(w, "the end of a longer file")
				}
				return nil
			}
			fmt.Fprintf(w, "%s, %d bytes, on the hub at %s\n", d.Name, d.Bytes, d.Dir)
			for _, f := range d.Files {
				fmt.Fprintf(w, "  %12d  %s\n", f.Size, f.Path)
			}
			if d.More {
				fmt.Fprintln(w, "  and more files")
			}
			if d.Note != "" {
				fmt.Fprintln(w, d.Note)
			}
			return nil
		},
	}
	c.Flags().StringVar(&file, "file", "", "a path from the list, to read its last lines")
	c.Flags().IntVar(&tail, "tail", 0, "with --file, the last N lines, default 400, at most 5000")
	c.Flags().StringVar(&out, "out", "", "with --file, the file to write, instead of one under the temp folder")
	return c
}
