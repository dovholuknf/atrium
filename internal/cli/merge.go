package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// `atrium merge`: the merge queue from a shell, over /_hub/merge, reached as the backlog is (backlogCall). See
// internal/link/merge.go and docs/rnd/merge-queue.md.

func mergeCmd() *cobra.Command {
	var board, branch, repo string
	var list, all bool
	c := &cobra.Command{
		Use:   "merge",
		Short: "Land this branch on claude/main through the hub's merge queue, or --list the queue",
		Long: "Asks the hub to merge the current branch at HEAD onto claude/main. Push it to the hub first. The hub\n" +
			"merges it, the merge room runs scripts/test-touched.sh on the merge, and on green claude/main moves and\n" +
			"the branch is deleted. The card that asked and its launcher are told how it ended.\n\n" +
			"--list shows the open requests first, then the latest, --all up to 500.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return mergeList(cmd.OutOrStdout(), board, repo, all)
			}
			return mergeAsk(cmd.OutOrStdout(), board, repo, branch)
		},
	}
	boardFlag(c, &board)
	c.Flags().StringVar(&branch, "branch", "", "the claude/* branch to merge (default: the current one, at the hub's tip of it)")
	c.Flags().StringVar(&repo, "repo", "", "<host>/<owner>/<repo> (default: this worktree's hub or origin remote)")
	c.Flags().BoolVar(&list, "list", false, "list the queue instead of asking")
	c.Flags().BoolVar(&all, "all", false, "with --list, up to 500 requests")
	c.AddCommand(mergeShowCmd(&board), mergeCancelCmd(&board), mergeRoomCmd(&board))
	return c
}

func mergeAsk(w io.Writer, board, repo, branch string) error {
	sha := ""
	if repo == "" || branch == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dir, _ := os.Getwd()
		hr, hb, hs, err := mergeHere(ctx, dir)
		if err != nil {
			return fmt.Errorf("%v. name them with --branch and --repo", err)
		}
		if repo == "" {
			repo = hr
		}
		if branch == "" {
			branch, sha = hb, hs
		}
	}
	by, room := backlogFiler()
	var out struct {
		Request  hubstore.MergeRequest `json:"request"`
		Existing bool                  `json:"existing"`
	}
	err := backlogCall(board, http.MethodPost, "/_hub/merge", map[string]string{
		"repo": repo, "branch": branch, "sha": sha, "by": by, "room": room}, &out)
	if err != nil {
		return err
	}
	m := out.Request
	if out.Existing {
		fmt.Fprintf(w, "request %d already merges %s, and is %s\n", m.N, m.Branch, m.State)
		return nil
	}
	fmt.Fprintf(w, "queued %s at %s as request %d. atrium merge show %d\n", m.Branch, shortSHA(m.SHA), m.N, m.N)
	return nil
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// mergeHere is the repository, branch and sha of the worktree at dir. The repository is the one its hub remote names,
// else its origin's.
func mergeHere(ctx context.Context, dir string) (repo, branch, sha string, err error) {
	g := gitsync.Default
	branch, err = g.Git(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if branch = strings.TrimSpace(branch); err != nil || branch == "" {
		return "", "", "", errors.New("this is not a worktree on a branch")
	}
	if sha, err = g.Git(ctx, dir, "rev-parse", "HEAD"); err != nil {
		return "", "", "", errors.New("this worktree has no commit")
	}
	sha = strings.TrimSpace(sha)
	if u, err := g.Git(ctx, dir, "remote", "get-url", "hub"); err == nil {
		if name := mergeRepoOfHubURL(strings.TrimSpace(u)); name != "" {
			return name, branch, sha, nil
		}
	}
	if u, err := g.Git(ctx, dir, "remote", "get-url", "origin"); err == nil {
		if ref, err := gitsync.ParseURL(strings.TrimSpace(u)); err == nil {
			return ref.Name(), branch, sha, nil
		}
	}
	return "", "", "", errors.New("this worktree has no hub or origin remote that names a repository")
}

// mergeRepoOfHubURL is the repository a hub remote's URL names, http://<host>/git/hub/<host>/<owner>/<repo>.git, or "".
func mergeRepoOfHubURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	_, rest, ok := strings.Cut(u.Path, gitsync.StorePrefix)
	if !ok {
		return ""
	}
	ref, err := gitsync.ParseName(strings.TrimSuffix(strings.Trim(rest, "/"), ".git"))
	if err != nil {
		return ""
	}
	return ref.Name()
}

func mergeList(w io.Writer, board, repo string, all bool) error {
	q := url.Values{}
	if repo != "" {
		q.Set("repo", repo)
	}
	if all {
		q.Set("all", "1")
	}
	var out struct {
		Requests []hubstore.MergeRequest `json:"requests"`
		Held     map[string]string       `json:"held"`
		Room     string                  `json:"room"`
		Why      string                  `json:"why"`
	}
	if err := backlogCall(board, http.MethodGet, "/_hub/merge?"+q.Encode(), nil, &out); err != nil {
		return err
	}
	if out.Room != "" {
		fmt.Fprintf(w, "merge room: %s\n", out.Room)
	} else {
		fmt.Fprintf(w, "merge room: none. %s\n", out.Why)
	}
	for r, why := range out.Held {
		fmt.Fprintf(w, "held: %s: %s\n", r, why)
	}
	if len(out.Requests) == 0 {
		fmt.Fprintln(w, "no requests")
	}
	for _, m := range out.Requests {
		fmt.Fprintln(w, mergeRow(m))
	}
	return nil
}

// mergeRow is one request on one line: number, state, branch, what it came to, when.
func mergeRow(m hubstore.MergeRequest) string {
	what := ""
	switch m.State {
	case hubstore.MergeLanded:
		what = shortSHA(m.Landed)
	case hubstore.MergeRed:
		what = strings.Join(m.Failing, ", ")
	case hubstore.MergeTesting:
		what = "on " + m.Room
	}
	if what == "" {
		what = m.Reason
	}
	at := m.AskedAt
	if m.EndedAt != "" {
		at = m.EndedAt
	}
	return strings.TrimRight(fmt.Sprintf("%-5d %-9s %-40s %-20s %s", m.N, m.State, m.Branch, at, what), " ")
}

func mergeShowCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <n>",
		Short: "One request, with its test output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || n <= 0 {
				return errors.New("a request is a number")
			}
			var m hubstore.MergeRequest
			if err := backlogCall(*board, http.MethodGet, "/_hub/merge/"+strconv.FormatInt(n, 10), nil, &m); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "#%d %s  %s at %s  [%s]\n", m.N, m.Repo, m.Branch, shortSHA(m.SHA), m.State)
			fmt.Fprintf(w, "asked by %s@%s", orDefault(m.Requester, "?"), orDefault(m.RequesterRoom, "?"))
			if m.Launcher != "" {
				fmt.Fprintf(w, ", launched by %s@%s", m.Launcher, m.LauncherRoom)
			}
			fmt.Fprintln(w)
			for _, l := range [][2]string{{"asked", m.AskedAt}, {"started", m.StartedAt}, {"tested", m.TestedAt},
				{"ended", m.EndedAt}, {"base", m.Base}, {"candidate", m.Candidate}, {"landed", m.Landed},
				{"room", m.Room}, {"reason", m.Reason}, {"failing", strings.Join(m.Failing, ", ")}} {
				if l[1] != "" {
					fmt.Fprintf(w, "%-10s %s\n", l[0], l[1])
				}
			}
			if m.Output != "" {
				fmt.Fprintf(w, "\n%s\n", strings.TrimRight(m.Output, "\n"))
			}
			return nil
		},
	}
}

func mergeCancelCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <n>",
		Short: "Take back a request that is still queued",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || n <= 0 {
				return errors.New("a request is a number")
			}
			by, room := backlogFiler()
			if by != "" && room != "" {
				by += "@" + room
			}
			var m hubstore.MergeRequest
			if err := backlogCall(*board, http.MethodPost, "/_hub/merge/"+strconv.FormatInt(n, 10)+"/cancel",
				map[string]string{"by": by}, &m); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "request %d is %s\n", m.N, m.State)
			return nil
		},
	}
}

func mergeRoomCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "room [name|-]",
		Short: "Show the merge room, or set it (- unsets it). Setting is from the hub's machine only",
		Long:  "The merge room runs the queue's tests. Unset, the queue uses the room on the hub's own machine.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct{ Room, Using, Why string }
			var err error
			if len(args) == 0 {
				err = backlogDirect(orDefault(*board, backlogHubAddr()), http.MethodGet, "/_hub/merge/room", nil, &out)
			} else {
				name := args[0]
				if name == "-" {
					name = ""
				}
				by, _ := backlogFiler()
				err = backlogDirect(orDefault(*board, backlogHubAddr()), http.MethodPost, "/_hub/merge/room",
					map[string]string{"room": name, "by": orDefault(by, "operator")}, &out)
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "configured: %s\n", orDefault(out.Room, "(none: the room on the hub's machine)"))
			if out.Using != "" {
				fmt.Fprintf(w, "using: %s\n", out.Using)
			} else {
				fmt.Fprintf(w, "using: none. %s\n", out.Why)
			}
			return nil
		},
	}
}
