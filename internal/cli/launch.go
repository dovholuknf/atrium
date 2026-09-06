package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Starting an atrium-managed agent from a script.
//
// The inversion that makes this worth having: atrium does not need to learn how
// to make a git worktree. Whatever already does that can make the directory the
// way it likes and then hand it over, and atrium supervises from there.
//
// `POST /v1/launch` was already the whole mechanism. This exists because a shell
// script should not have to build JSON and parse a response to use it.

// launchOpts is what the flags collect. A struct rather than nine positional
// arguments, which is the point at which a parameter list stops being readable
// and starts being a place to transpose two strings.
type launchOpts struct {
	boardURL, harness, cwd, title, why, resume string
	prompt, source, externalID, itemURL        string
	// What the caller already knows about the work. See `LaunchRequest`:
	// atrium derives none of it, and every one is optional.
	repo, org, host, branch, window, theme string
	ifRunning                              string
	tags                                   []string
	quiet                                  bool
}

func newLaunch() *cobra.Command {
	var o launchOpts
	c := &cobra.Command{
		Use:   "launch",
		Short: "Start a runner under atrium, in a directory you have already prepared.",
		Long: "Starts a runner and puts it on the board, supervised, gated and attachable from the " +
			"browser. Atrium does not create the directory: make it however you like, then point " +
			"this at it.\n\n" +
			"Whatever already knows how to turn an issue or a ticket into a worktree can hand one " +
			"over with --tags, --prompt and --external, and atrium learns nothing about that " +
			"system.\n\n" +
			"Prints the new card's id, so a script can hold on to it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return launchAgent(o)
		},
	}
	c.Flags().StringVar(&o.harness, "runner", "claude", "which configured runner to start")
	c.Flags().StringVar(&o.cwd, "cwd", "", "where to run it (default: the current directory)")
	c.Flags().StringVar(&o.title, "title", "", "what to show on the card")
	c.Flags().StringVar(&o.why, "why", "", "what this is for, read back later")
	c.Flags().StringVar(&o.resume, "resume", "",
		"a session id to pick up instead of starting a new conversation")
	c.Flags().StringSliceVar(&o.tags, "tags", nil,
		"what you call this work, comma separated. free text, and what grouping and filtering use")
	c.Flags().StringVar(&o.prompt, "prompt", "",
		"the first instruction the runner gets. not available with --resume: "+
			"that conversation already has one")
	c.Flags().StringVar(&o.source, "source", "",
		"the system this came from, such as github or zendesk. atrium never interprets it")
	c.Flags().StringVar(&o.externalID, "external", "",
		"that system's own identifier for this work, such as openziti/ziti#4211")
	c.Flags().StringVar(&o.itemURL, "item-url", "", "the way back to the thing itself")
	c.Flags().StringVar(&o.repo, "repo", "", "which repository this work is in")
	c.Flags().StringVar(&o.org, "org", "", "which organisation owns it, so two forks are told apart")
	c.Flags().StringVar(&o.host, "host", "", "which forge it is on, such as github.com")
	c.Flags().StringVar(&o.branch, "branch", "", "which branch this directory is on")
	c.Flags().StringVar(&o.window, "window", "",
		"which pile this belongs to, and what the board groups by. free text: "+
			"active-work, pull-requests, tangent, a repo name, anything")
	c.Flags().StringVar(&o.theme, "theme", "",
		"the terminal palette this session comes up in, when the caller keeps that map")
	c.Flags().StringVar(&o.ifRunning, "if-running", "",
		"what to do when this directory already has a card: skip to hand it back and "+
			"start nothing, adopt to continue it. empty starts another one")
	c.Flags().BoolVar(&o.quiet, "quiet", false, "print only the card id")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func launchAgent(o launchOpts) error {
	if strings.TrimSpace(o.cwd) == "" {
		got, err := os.Getwd()
		if err != nil {
			return err
		}
		o.cwd = got
	}
	abs, err := filepath.Abs(o.cwd)
	if err != nil {
		return err
	}
	// Checked here so the error names the directory the caller passed, rather
	// than coming back from the daemon about a path it resolved differently.
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}
	// Refused here as well as in the daemon, because the daemon's refusal
	// arrives after a round trip and this one names the two flags.
	if o.prompt != "" && o.resume != "" {
		return fmt.Errorf("--prompt and --resume cannot both be given: " +
			"a resumed conversation already has its instruction")
	}

	body, err := json.Marshal(map[string]any{
		"harness":     o.harness,
		"cwd":         filepath.ToSlash(abs),
		"title":       o.title,
		"why":         o.why,
		"resume":      o.resume,
		"tags":        o.tags,
		"prompt":      o.prompt,
		"source":      o.source,
		"external_id": o.externalID,
		"url":         o.itemURL,
		"repo":        o.repo,
		"org":         o.org,
		"host":        o.host,
		"branch":      o.branch,
		"window":      o.window,
		"theme":       o.theme,
		"if_running":  o.ifRunning,
	})
	if err != nil {
		return err
	}

	boardURL, harness := o.boardURL, o.harness
	quiet := o.quiet
	url := boardAddress(boardURL) + "/v1/launch"
	// Long enough to cover the daemon waiting for the runner to prove it
	// started, which is two seconds plus whatever the runner takes to load.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}

	var task struct {
		ID           string `json:"id"`
		DisplayTitle string `json:"display_title"`
		PID          int    `json:"pid"`
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}

	if quiet {
		fmt.Println(task.ID)
		return nil
	}
	fmt.Printf("started %s as %s\n", harness, task.DisplayTitle)
	if task.PID > 0 {
		fmt.Printf("  pid   %d\n", task.PID)
	}
	fmt.Printf("  card  %s\n", task.ID)
	fmt.Printf("  board %s\n", boardAddress(boardURL))
	return nil
}
