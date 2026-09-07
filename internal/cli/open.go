package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// Handing atrium a URL.
//
// The counterpart to `atrium launch`, and the inversion is the same one. Launch
// says "here is a directory I have already prepared". This says "here is a link,
// work out what it is", and the working out is a row in a table somebody wrote
// rather than anything in this binary.
//
// It PRINTS by default and starts nothing. That is not timidity: writing a
// recogniser is a loop of paste, look, adjust the template, and a verb that
// launched a runner every time round that loop would be unusable. `--start` is
// the second half, for when the row is right.

type openOpts struct {
	boardURL string
	runner   string
	start    bool
	asJSON   bool
}

func newOpen() *cobra.Command {
	var o openOpts
	c := &cobra.Command{
		Use:   "open <url>",
		Short: "Work out what a url is, and show the card it would start.",
		Long: "Matches the url against the recogniser table and prints what it resolved to: the " +
			"directory, the title, the tags and the first instruction.\n\n" +
			"Atrium learns nothing about GitHub, Jira or anything else doing this. A recogniser " +
			"is a pattern and a set of templates, written by whoever understood the system.\n\n" +
			"Nothing is started without --start, and no directory is ever created: where the " +
			"worktree is missing this says so, and making one is the job of whatever already " +
			"makes worktrees here.",
		Args: cobra.ExactArgs(1),
		// The flag list is not the answer to "nothing recognises this url".
		//
		// Cobra prints usage on any error a RunE returns, which is right when
		// the mistake was in how the command was typed. The commonest failure
		// here is not that: it is a url no row wants yet, which is a fact about
		// the table and gets buried under twelve lines about flags.
		//
		// Errors too, because `Execute` already prints them with the `atrium:`
		// prefix and cobra printing its own copy first says the same sentence
		// twice.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return openURL(args[0], o)
		},
	}
	c.Flags().BoolVar(&o.start, "start", false,
		"start a runner on what it resolved to, instead of only printing it")
	c.Flags().StringVar(&o.runner, "runner", "claude", "which configured runner --start uses")
	c.Flags().BoolVar(&o.asJSON, "json", false, "print the resolution as json")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

// resolution is the half of the daemon's answer this command reads. Declared
// here rather than imported so the CLI does not pull the store in for a print.
type resolution struct {
	Recogniser string   `json:"recogniser"`
	Label      string   `json:"label"`
	URL        string   `json:"url"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Tags       []string `json:"tags"`
	Cwd        string   `json:"cwd"`
	Prompt     string   `json:"prompt"`
	Repo       string   `json:"repo"`
	Org        string   `json:"org"`
	Host       string   `json:"host"`
	Branch     string   `json:"branch"`
	Window     string   `json:"window"`
	Theme      string   `json:"theme"`
	Missing    []string `json:"missing"`
	CwdExists  bool     `json:"cwd_exists"`
	Problem    string   `json:"problem"`
	FetchError string   `json:"fetch_error"`
}

func openURL(url string, o openOpts) error {
	board := boardAddress(o.boardURL)
	body, err := json.Marshal(map[string]string{"url": strings.TrimSpace(url)})
	if err != nil {
		return err
	}
	// Long enough to cover a fetch command making a network call, which the
	// daemon caps at thirty seconds of its own.
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Post(board+"/v1/recognise", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", board, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("nothing recognises %s. add a row for it under runners, "+
			"or see scripts/recognisers for working examples", url)
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}

	var got resolution
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}
	if o.asJSON {
		os.Stdout.Write(raw)
		fmt.Println()
		return nil
	}
	printResolution(got)
	if !o.start {
		return nil
	}
	// REFUSED RATHER THAN CREATED. Atrium does not make the directory, and a
	// launch into one that is not there fails inside the daemon with a message
	// about a path it resolved itself. Saying it here names the recogniser that
	// asked for it.
	if !got.CwdExists {
		return fmt.Errorf("not starting: %s", got.Problem)
	}
	return launchAgent(launchOpts{
		boardURL: o.boardURL, harness: o.runner, cwd: got.Cwd,
		title: got.Title, prompt: got.Prompt, tags: got.Tags,
		source: got.Kind, itemURL: got.URL,
		repo: got.Repo, org: got.Org, host: got.Host, branch: got.Branch,
		window: got.Window, theme: got.Theme,
		// A URL pasted twice is one piece of work. Handing the card back is
		// what a script wants and what a person pasting the same link again
		// meant, and it is the difference between this and two runners writing
		// to one worktree.
		ifRunning: "skip",
	})
}

func printResolution(r resolution) {
	fmt.Printf("%s recognised it as %s\n\n", r.Recogniser, r.Label)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		fmt.Fprintf(w, "  %s\t%s\n", k, v)
	}
	row("directory", r.Cwd)
	row("title", r.Title)
	row("tags", strings.Join(r.Tags, ", "))
	row("repo", strings.TrimSpace(r.Org+"/"+r.Repo))
	row("host", r.Host)
	row("branch", r.Branch)
	row("window", r.Window)
	row("theme", r.Theme)
	row("kind", r.Kind)
	w.Flush()

	if strings.TrimSpace(r.Prompt) != "" {
		fmt.Printf("\nfirst instruction:\n%s\n", indentBlock(r.Prompt))
	}
	if r.FetchError != "" {
		fmt.Printf("\nthe fetch command failed, so anything it would have added is missing:\n  %s\n",
			r.FetchError)
	}
	if r.Problem != "" {
		fmt.Printf("\n%s\n", r.Problem)
	}
}

func indentBlock(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
