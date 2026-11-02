package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// `atrium new <name>`: gwt's `new` from a checkout. Run it inside any clone or worktree, and the room fetches origin,
// makes a worktree named <name> off origin's default branch (or --from, or origin/<name> when the remote has it) and
// starts a card there. The clone you are in is never checked out or pulled. `POST /v1/new`, internal/api/newwork.go.

type newOpts struct {
	boardURL string
	from     string
	prompt   string
	runner   string
	room     string
	repo     string
	asJSON   bool
	attach   bool
	twig     string
}

// newWorkAnswer is the half of the room's answer this command reads.
type newWorkReply struct {
	Card       string `json:"card"`
	Worktree   string `json:"worktree"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
	Created    bool   `json:"created"`
	Existed    bool   `json:"existed"`
	Hook       string `json:"hook"`
	HookError  string `json:"hook_error"`
	FetchError string `json:"fetch_error"`
	Patch      string `json:"patch"`
	CarryError string `json:"carry_error"`
}

func newNew() *cobra.Command {
	var o newOpts
	c := &cobra.Command{
		Use:   "new <name>",
		Short: "Start new work: a worktree named <name> off origin, and a card in it.",
		Long: "Run inside any checkout of the repo. The room fetches origin, then makes a worktree and a branch called " +
			"<name> from, in order: --from, origin/<name> when the remote already has that branch, else origin's " +
			"default branch. Your own clone is never checked out or pulled. The repo's setup hook runs in the new " +
			"worktree, and a card starts there. A name that already has a worktree answers it and its live card.\n\n" +
			"The repo is read from this checkout's origin remote, or --repo host/org/repo from anywhere.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := strings.TrimSpace(o.repo)
			if repo == "" {
				got, err := repoOfCheckout()
				if err != nil {
					return err
				}
				repo = got
			}
			return startNew(repo, strings.TrimSpace(args[0]), o)
		},
	}
	c.Flags().StringVar(&o.from, "from", "", "fork from this branch, tag or commit instead of origin's default")
	c.Flags().StringVar(&o.prompt, "prompt", "", "the card's first prompt")
	c.Flags().StringVar(&o.runner, "runner", "", "which configured runner starts the card")
	c.Flags().StringVar(&o.room, "room", "", "the room to make it on, instead of the one the hub picks")
	c.Flags().StringVar(&o.repo, "repo", "", "host/org/repo, instead of this checkout's origin")
	c.Flags().BoolVar(&o.attach, "attach", false, "attach this terminal to the card. ctrl+] leaves it")
	c.Flags().BoolVar(&o.asJSON, "json", false, "print the answer as json")
	c.Flags().StringVar(&o.boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

// repoOfCheckout is host/org/repo of the current directory's origin remote.
func repoOfCheckout() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("this directory has no origin remote to read the repo from. run it inside a checkout, or pass --repo host/org/repo")
	}
	return repoFromRemote(strings.TrimSpace(string(out)))
}

// repoFromRemote reads host/org/repo out of a remote url: https, ssh:// or scp-like.
func repoFromRemote(remote string) (string, error) {
	host, path := "", ""
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil {
			return "", fmt.Errorf("cannot read the repo from origin %q", remote)
		}
		host, path = u.Hostname(), u.Path
	case strings.Contains(remote, "@") && strings.Contains(remote, ":"):
		rest := remote[strings.Index(remote, "@")+1:]
		i := strings.Index(rest, ":")
		host, path = rest[:i], rest[i+1:]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("cannot read host/org/repo from origin %q. pass --repo host/org/repo", remote)
	}
	return strings.ToLower(host) + "/" + parts[0] + "/" + parts[1], nil
}

func startNew(repo, name string, o newOpts) error {
	board := boardAddress(o.boardURL)
	body, err := json.Marshal(map[string]string{"repo": repo, "name": name, "from": strings.TrimSpace(o.from),
		"prompt": o.prompt, "harness": o.runner, "twig": o.twig})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, board+"/v1/new", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(o.room) != "" {
		req.Header.Set("X-Atrium-Room", strings.TrimSpace(o.room))
	}
	// A fetch and a setup hook can take minutes. The room bounds both, and this bounds the wait.
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", board, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
			Step  string `json:"step"`
		}
		_ = json.Unmarshal(raw, &e)
		switch {
		case resp.StatusCode == http.StatusNotFound && e.Error == "":
			return fmt.Errorf("the board at %s cannot make new work yet. it needs a newer build", board)
		case e.Error != "" && e.Step != "":
			return fmt.Errorf("atrium refused at the %s step: %s", e.Step, e.Error)
		case e.Error != "":
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}
	var got newWorkReply
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}
	if o.asJSON {
		os.Stdout.Write(raw)
		fmt.Println()
	} else {
		printNew(board, got)
	}
	if o.attach && got.Card != "" {
		return attachTerminal(board, got.Card)
	}
	return nil
}

func printNew(board string, n newWorkReply) {
	verb := "new work"
	switch {
	case n.Existed && !n.Created && n.Card != "":
		verb = "already open"
	case n.Existed:
		verb = "reopened"
	}
	fmt.Printf("%s: %s %s\n", verb, n.Repo, n.Branch)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(w, "  %s\t%s\n", k, v)
		}
	}
	row("card", n.Card)
	row("worktree", n.Worktree)
	row("from", strings.TrimPrefix(n.Base, "refs/remotes/"))
	row("hook", n.Hook)
	row("hook failed", n.HookError)
	row("fetch failed", n.FetchError)
	row("patch not applied", n.Patch)
	row("carry failed", n.CarryError)
	row("board", strings.TrimRight(board, "/")+"/#term="+url.QueryEscape(n.Card))
	w.Flush()
}
