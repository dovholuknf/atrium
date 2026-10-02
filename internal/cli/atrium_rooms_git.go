package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/spf13/cobra"
)

// `atrium rooms git ...`: asking the hub to move code, and saying which repositories it
// moves. See docs/rnd/git-sync-design.md.
//
// `sync` and `collect` are requests to the RUNNING hub over its loopback board, because the
// hub is the thing that holds the links to the rooms. `repos` writes the hub's own store,
// the way every other verb here does, and the hub reads it on its next pass.
//
// NO VERB TAKES A BRANCH, A URL OR A REFSPEC. A repository is a name and a checkout on this
// machine, and the branch it mirrors is the integration branch.

func roomGitCmd(prefix string) *cobra.Command {
	c := &cobra.Command{
		Use:   "git",
		Short: "Move code between this hub and its rooms: sync, collect, and which repositories",
		Long: "The hub mirrors each repository in `repos` into a bare copy and keeps every attached room's\n" +
			"clone on claude/main by itself. These verbs do what the hub would do on its timers, now.\n\n" +
			"EVERY TRANSFER IS A FETCH. Nothing is pushed to a room or to the hub.",
	}
	c.AddCommand(gitSyncCmd(prefix), gitCollectCmd(prefix), gitStatusCmd(prefix), gitReposCmd(prefix),
		gitInitCmd(prefix), gitStoreCmd(prefix), gitSettingsCmd(prefix))
	return c
}

// hubPost does one request against the running hub's loopback board and prints a refusal
// as the sentence it is.
func hubCall(f *hubStoreFlags, method, path string, body any, out any) error {
	addr := orDefault(f.board, defaultBoardAddr())
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// A first clone over an overlay is minutes.
	res, err := (&http.Client{Timeout: gitsync.CommandBound + 2*time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("the hub is not answering at %s (is it running?): %w", addr, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = res.Status
		}
		if res.StatusCode == http.StatusNotFound {
			e.Error = "this hub has no git sync (its build predates it)"
		}
		return fmt.Errorf("%s", e.Error)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func gitSyncCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	var init bool
	c := &cobra.Command{
		Use:   "sync <room> [<name>]",
		Short: "Bring a room's clone up to date with claude/main",
		Long: "Asks the room to fetch claude/main from this hub and move its claude/main and hub-main.\n" +
			"With no <name> every repository in `repos` is synced.\n\n" +
			"Each answers ok, absent (no clone, and --init was not given, so nothing ran), behind (fetched, but\n" +
			"git refused a move: a worktree holds claude/main, or hub-main is checked out with changes),\n" +
			"failed (nothing fetched) or unsupported (the room's build predates git sync).\n\n" +
			"--init makes the clone when the room has none. It is only ever given by a person.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			var out struct {
				Results []gitsync.SyncResult `json:"results"`
			}
			if err := hubCall(&f, http.MethodPost, "/_hub/git/sync",
				map[string]any{"room": args[0], "name": name, "init": init}, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(out.Results) == 0 {
				fmt.Fprintln(w, "the hub has no repositories. `"+atriumCmd("rooms git repos add")+"` adds one")
			}
			bad := false
			for _, r := range out.Results {
				fmt.Fprintf(w, "%-12s %-8s %-40s %s\n", r.Room, r.State, r.Name, strings.TrimSpace(short12(r.SHA)+" "+r.Detail))
				bad = bad || r.State != "ok"
			}
			if bad {
				return fmt.Errorf("not every repository is ok")
			}
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	c.Flags().BoolVar(&init, "init", false, "make the clone when the room has none")
	return c
}

func short12(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func gitCollectCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "collect <room>",
		Short: "Fetch a room's claude/* branches now, for the hub's checkout to merge",
		Long: "Fetches the room's claude/* branches (never claude/main) into the hub, prunes what the room\n" +
			"deleted, and puts them in the hub's checkout as refs/remotes/<room>/claude/*. Nothing under\n" +
			"refs/heads is written.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out gitsync.CollectResult
			if err := hubCall(&f, http.MethodPost, "/_hub/git/collect", map[string]any{"room": args[0]}, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			failed := false
			for _, r := range out.Repos {
				line := fmt.Sprintf("%-40s %d branches, moved=%v, delivered=%v", r.Name, r.Refs, r.Moved, r.Delivered)
				if r.Error != "" {
					line += "  ERROR: " + r.Error
					failed = true
				}
				fmt.Fprintln(w, line)
			}
			if failed {
				return fmt.Errorf("not every repository was collected")
			}
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

func gitStatusCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "status",
		Short: "What the hub last did: each mirror, and each room's sync state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var out gitsync.HubStatus
			if err := hubCall(&f, http.MethodGet, "/_hub/git/status", nil, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(out.Repos) == 0 {
				fmt.Fprintln(w, "the hub has no repositories")
			}
			for _, m := range out.Mirror {
				line := fmt.Sprintf("mirror  %-40s %-10s %s", m.Name, m.Branch, short12(m.SHA))
				if m.Error != "" {
					line += "  ERROR: " + m.Error
				}
				fmt.Fprintln(w, line)
			}
			for room, st := range out.Rooms {
				fmt.Fprintf(w, "room    %-12s %s\n", room, st.State)
				for _, r := range st.Repos {
					fmt.Fprintf(w, "          %-40s %-8s %s\n", r.Name, r.State, strings.TrimSpace(short12(r.SHA)+" "+r.Detail))
				}
			}
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

func gitReposCmd(prefix string) *cobra.Command {
	c := &cobra.Command{
		Use:   "repos",
		Short: "Which repositories the hub mirrors and serves",
		Long: "Each is a name (`<host>/<owner>/<repo>`, for example github/dovholuknf/atrium) and the checkout\n" +
			"on THIS machine that @merge writes. The branch is the integration branch, claude/main, and\n" +
			"cannot be anything else: a room's hub-main mirrors it, and a department branch would put\n" +
			"unmerged work under every room.",
	}
	c.AddCommand(gitReposLsCmd(prefix), gitReposAddCmd(prefix), gitReposRmCmd(prefix))
	return c
}

func gitReposLsCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "ls",
		Short: "List them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			repos, err := store.GitRepos()
			if err != nil {
				return err
			}
			if len(repos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no repositories. `"+atriumCmd("rooms git repos add")+" <name> <checkout>` adds one")
			}
			for _, r := range repos {
				fmt.Fprintf(cmd.OutOrStdout(), "%-40s %-12s %s\n", r.Name, r.Branch, r.Checkout)
			}
			return nil
		},
	}
	f.bind(c, prefix)
	return c
}

func gitReposAddCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "add <name> <checkout>",
		Short: "Mirror a repository, taking claude/main from a checkout on this machine",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			ck, err := filepath.Abs(args[1])
			if err != nil {
				return err
			}
			repos, err := store.GitRepos()
			if err != nil {
				return err
			}
			next := repos[:0:0]
			for _, r := range repos {
				if r.Name != args[0] {
					next = append(next, r)
				}
			}
			next = append(next, gitsync.Repo{Name: args[0], Checkout: filepath.ToSlash(ck), Branch: gitsync.IntegrationBranch})
			raw, _ := json.Marshal(next)
			if err := store.SetGitRepos(string(raw)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "the hub will mirror %s from %s within 30 seconds\n", args[0], ck)
			return nil
		},
	}
	f.bind(c, prefix)
	return c
}

func gitReposRmCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "rm <name>",
		Short: "Stop mirroring a repository. The bare copy is left where it is",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := f.open()
			if err != nil {
				return err
			}
			defer store.Close()
			repos, err := store.GitRepos()
			if err != nil {
				return err
			}
			next := repos[:0:0]
			for _, r := range repos {
				if r.Name != args[0] {
					next = append(next, r)
				}
			}
			if len(next) == len(repos) {
				return fmt.Errorf("%q is not in the list", args[0])
			}
			raw, _ := json.Marshal(next)
			if len(next) == 0 {
				raw = nil
			}
			return store.SetGitRepos(string(raw))
		},
	}
	f.bind(c, prefix)
	return c
}
