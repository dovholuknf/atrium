package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
// IT OPENS THE LINK. `POST /v1/open` makes the worktree, the review and the card
// in one call, or answers the card the link already has, and this prints where
// the card is. The same verb the board's paste and ctrl-alt-r call. See
// internal/api/open.go and docs/rnd/card-lifecycle-design.md section 3.
//
// `--show` is the old print and starts nothing. Writing a recogniser is a loop
// of paste, look, adjust the template, and a verb that opened a card every time
// round that loop would be unusable. A link the verb does not open (a bare repo
// page) is shown the same way, and `--start` launches it from what it resolved
// to, as before.

type openOpts struct {
	boardURL string
	runner   string
	why      string
	room     string
	repo     string
	show     bool
	start    bool
	attach   bool
	asJSON   bool
}

func newOpen() *cobra.Command {
	var o openOpts
	c := &cobra.Command{
		Use:   "open [url]",
		Short: "Open a link as a card: its worktree, its review and a session, or the card it already has.",
		Long: "Opens a link the way a paste on the board does, on the room the hub picks. A pull request gets " +
			"its worktree, its review and a card in the worktree. An issue or a branch gets a worktree on its " +
			"branch and a card. A support ticket or forum topic names no repo, so it opens in the recogniser's " +
			"default repo, or the one --repo names, or with --repo none in a scratch folder of the card's own. " +
			"A link that already has a live card answers that card. Prints the card's address on the board. " +
			"With no url the clipboard is read.\n\n" +
			"--attach attaches this terminal to the card. Ctrl+] leaves it, and the card keeps running.\n\n" +
			"--show only prints what the recogniser table made of the url and starts nothing, which is the " +
			"loop for writing a recogniser. A link that names no piece of work (a repo page) is shown the same " +
			"way, and --start launches it from what it resolved to. No directory is created for one of those.",
		Args: cobra.MaximumNArgs(1),
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
			link := ""
			if len(args) == 1 {
				link = args[0]
			} else {
				got, err := readClipboard()
				if err != nil {
					return err
				}
				if !strings.HasPrefix(got, "http://") && !strings.HasPrefix(got, "https://") || strings.ContainsAny(got, " \t\r\n") {
					return fmt.Errorf("the clipboard does not hold a link. pass the url instead")
				}
				link = got
			}
			if o.show {
				return showURL(link, o)
			}
			return openLink(link, o)
		},
	}
	c.Flags().BoolVar(&o.show, "show", false, "only print what the url resolves to, and start nothing")
	c.Flags().BoolVar(&o.start, "start", false,
		"for a link that is not opened as a card yet, start a runner on what it resolved to")
	c.Flags().BoolVar(&o.attach, "attach", false, "attach this terminal to the card. ctrl+] leaves it")
	c.Flags().StringVar(&o.why, "why", "", "why it is being opened, kept on the review and the card")
	c.Flags().StringVar(&o.room, "room", "", "the room to open it on, instead of the one the hub picks")
	c.Flags().StringVar(&o.repo, "repo", "",
		"for a link that names no repo: host/org/repo to open it in, or none for a scratch folder (default: the recogniser's)")
	c.Flags().StringVar(&o.runner, "runner", "", "which configured runner starts the card (default: the review recipe's, or claude for --start)")
	c.Flags().BoolVar(&o.asJSON, "json", false, "print the answer as json")
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

// opened is the open verb's answer.
type opened struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Card     string `json:"card"`
	PR       string `json:"pr"`
	Worktree string `json:"worktree"`
	Repo     string `json:"repo"`
	Created  bool   `json:"created"`
	Room     string `json:"room"`
	Title    string `json:"title"`
}

// openLink sends the link to the open verb and prints the card, or attaches to it.
func openLink(link string, o openOpts) error {
	board := boardAddress(o.boardURL)
	body, err := json.Marshal(map[string]string{"url": strings.TrimSpace(link), "why": o.why, "harness": o.runner,
		"repo": strings.TrimSpace(o.repo)})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, board+"/v1/open", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(o.room) != "" {
		req.Header.Set("X-Atrium-Room", strings.TrimSpace(o.room))
	}
	// A clone and a fetch can take minutes on a slow link. The room bounds the git, and this bounds the wait.
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", board, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
			Step  string `json:"step"`
		}
		_ = json.Unmarshal(raw, &e)
		switch {
		case e.Code == "not_openable" || e.Code == "not_a_pr":
			// Not opened as a card: shown as before, and --start launches it from the resolution. not_a_pr is an
			// older room's.
			fmt.Println("that link names no piece of work to open as a card. this is what the url resolves to:")
			fmt.Println()
			return showURL(link, o)
		case resp.StatusCode == http.StatusNotFound && e.Error == "":
			return fmt.Errorf("the board at %s cannot open links yet. it needs a newer build", board)
		case e.Error != "" && e.Step != "":
			return fmt.Errorf("atrium refused at the %s step: %s", e.Step, e.Error)
		case e.Error != "":
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}
	var got opened
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}
	if o.asJSON {
		os.Stdout.Write(raw)
		fmt.Println()
	} else {
		printOpened(board, got)
	}
	if o.attach && got.Card != "" {
		return attachTerminal(board, got.Card)
	}
	return nil
}

// printOpened says what was opened and where the card is on the board.
func printOpened(board string, o opened) {
	verb := "opened"
	if !o.Created {
		verb = "already open"
	}
	where := ""
	if o.Room != "" {
		where = " on " + o.Room
	}
	fmt.Printf("%s: %s%s\n", verb, o.Key, where)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(w, "  %s\t%s\n", k, v)
		}
	}
	row("card", o.Card)
	row("title", o.Title)
	row("kind", o.Kind)
	row("repo", o.Repo)
	row("worktree", o.Worktree)
	row("review", o.PR)
	row("board", strings.TrimRight(board, "/")+"/#term="+url.QueryEscape(o.Card))
	w.Flush()
}

// showURL prints what the recogniser table made of a url, and launches it with --start.
func showURL(url string, o openOpts) error {
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
	runner := o.runner
	if runner == "" {
		runner = "claude"
	}
	return launchAgent(launchOpts{
		boardURL: o.boardURL, harness: runner, cwd: got.Cwd,
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
