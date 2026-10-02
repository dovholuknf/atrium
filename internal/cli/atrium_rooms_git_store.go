package cli

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/spf13/cobra"
)

// `atrium rooms git init|store|settings`: the hub's own git store, where finished work lives and
// `main` is held. See docs/rnd/hub-forge-design.md section 3.1. The design calls these `atrium hub
// git ...`, and there is no `atrium hub`: the plan removed it (TestTheCollidingNamesLandWhereThePlanSays),
// and the hub's verbs are under `rooms`, beside `rooms git sync` and `repos`.
//
// `init` is a request to the RUNNING hub over its loopback board, because the hub holds the store
// and its per-repository locks, and refuses it from any other machine. `settings` writes the hub's
// own store the way `rooms git repos` does, and the hub reads it on its next use.

func gitInitCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "init <url>",
		Short: "Make the hub's bare repository for a forge URL, with main seeded once from a public repo",
		Long: "For a PUBLIC repository the forge's default branch is fetched once into main, whatever the forge\n" +
			"calls it. A second init is a no-op and leaves main alone. For a private repository, or when the\n" +
			"forge cannot be reached, an EMPTY repository is made and you push main to it as the operator.\n" +
			"After a network error, run init again and it seeds main if it is still absent.\n\n" +
			"The URL is https://github.com/<owner>/<repo> or git@github.com:<owner>/<repo>.git. A URL with a\n" +
			"username or a token in it is refused, and nothing of the URL is kept but the host, owner and name.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out gitsync.InitResult
			if err := hubCall(&f, http.MethodPost, "/_hub/git/init", map[string]string{"url": args[0]}, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			state := "already there"
			switch {
			case out.Created && out.Seeded:
				state = "created, seeded"
			case out.Created:
				state = "created, empty"
			case out.Seeded:
				state = "seeded"
			}
			main := "(empty)"
			if out.Main != "" {
				main = short12(out.Main)
			}
			fmt.Fprintf(w, "%s  %s  main %s\n", out.Repo, state, main)
			fmt.Fprintln(w, out.Note)
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

func gitStoreCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "store",
		Short: "List the repositories in the hub's own store (not `repos`, the git_repos mirrors)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct {
				Repos []gitsync.RepoView `json:"repos"`
			}
			if err := hubCall(&f, http.MethodGet, "/_hub/git/repos", nil, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(out.Repos) == 0 {
				fmt.Fprintln(w, "the hub's store is empty. `"+atriumCmd("rooms git init")+" <url>` makes a repository")
			}
			for _, r := range out.Repos {
				main := "(empty)"
				if r.Main.SHA != "" {
					main = short12(r.Main.SHA)
				}
				fmt.Fprintf(w, "%-40s main %-13s %d branches  %s\n", r.Host+"/"+r.Owner+"/"+r.Repo, main, len(r.Branches), r.URL)
			}
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

func gitReleaseCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	c := &cobra.Command{
		Use:   "release <repo> <branch>",
		Short: "Let go of a branch on the hub, so the next card to push a fast-forward owns it",
		Long: "A branch belongs to the card that first pushed it. The hub lets it go on its own when that card is\n" +
			"gone, dead, or has been done for 7 days, and asks the card's room when another card pushes. This lets\n" +
			"it go now. The branch itself and its history are not touched.\n\n" +
			"<repo> is <owner>/<repo> or <host>/<owner>/<repo>, and <branch> is the name without refs/heads/.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out struct {
				Note string `json:"note"`
			}
			if err := hubCall(&f, http.MethodPost, "/_hub/git/release",
				map[string]string{"repo": args[0], "branch": args[1]}, &out); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out.Note)
			return nil
		},
	}
	f.bind(c, prefix)
	f.bindBoard(c, prefix)
	return c
}

func gitSettingsCmd(prefix string) *cobra.Command {
	var f hubStoreFlags
	var storeDir, createOnPush string
	c := &cobra.Command{
		Use:   "settings",
		Short: "Show or set git.store and git.create_on_push",
		Long: "git.store is where the hub's bare repositories are. It defaults to <the hub's directory>/git, and\n" +
			"changing it moves nothing already in the old one. `--store ''` puts it back to the default.\n\n" +
			"git.create_on_push is whether a room's first push of a repository the hub does not have may create\n" +
			"it, under the same case check as init. It is off by default. The operator never makes a repository by a\n" +
			"push, only with init.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := f.open()
			if err != nil {
				return err
			}
			defer st.Close()
			if cmd.Flags().Changed("store") {
				if err := st.SetGitStore(storeDir); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("create-on-push") {
				switch strings.ToLower(strings.TrimSpace(createOnPush)) {
				case "on", "true", "yes":
					err = st.SetGitCreateOnPush(true)
				case "off", "false", "no":
					err = st.SetGitCreateOnPush(false)
				default:
					err = fmt.Errorf("--create-on-push is on or off")
				}
				if err != nil {
					return err
				}
			}
			path, err := st.GitStorePath(f.keys().Dir)
			if err != nil {
				return err
			}
			on, err := st.GitCreateOnPush()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "git.store           %s\n", path)
			fmt.Fprintf(w, "git.create_on_push  %s\n", map[bool]string{true: "on", false: "off"}[on])
			return nil
		},
	}
	c.Flags().StringVar(&storeDir, "store", "", "set git.store, an absolute path on this machine (empty is the default)")
	c.Flags().StringVar(&createOnPush, "create-on-push", "", "set git.create_on_push: on or off")
	f.bind(c, prefix)
	return c
}
