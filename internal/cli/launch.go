package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
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
	model, effort                              string
	// Extra argv and KEY=VALUE environment for the runner, used as given.
	args, env []string
	// What the caller already knows about the work. See `LaunchRequest`:
	// atrium derives none of it, and every one is optional.
	repo, org, host, branch, window, theme string
	ifRunning, reportTo                    string
	base                                   string
	home                                   bool
	// onto is a card to start the runner on, by handle, alias, name@room or id.
	onto  string
	tags  []string
	quiet bool
	// item is the backlog item the launch works on: an id, or new:<title>. See link.ResolveItem.
	item string
	// isolated starts the card as an isolated item. See daemon.ItemTag.
	isolated bool
	// itemOwner is who owns the item: empty, human, or a single-holder role. See daemon.ItemOwnerTag.
	itemOwner string
	// step is the step of its item the card is for: designed, approved, building and so on. See link.StepTagPrefix.
	step string
	// lean and superlean start a claude runner without the operator's setup. See docs/runtime/superlean-design.md.
	lean, superlean bool
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
	c.Flags().StringVar(&o.model, "model", "",
		"which model this session runs on, for this session only. the runner has to "+
			"declare how it takes one, and a runner that cannot is refused rather than "+
			"started on its default")
	c.Flags().StringVar(&o.effort, "effort", "",
		"the thinking effort, such as low or high, for this session only. passed in the shape the "+
			"runner declares and not checked. a runner that cannot take one is refused")
	c.Flags().StringArrayVar(&o.args, "arg", nil,
		"an extra argument for the runner, used as given. repeat it for more, in order")
	c.Flags().StringArrayVar(&o.env, "env", nil,
		"an extra KEY=VALUE for the runner's environment. repeat it for more. "+
			"the card shows the names, never the values")
	c.Flags().StringVar(&o.source, "source", "",
		"the system this came from, such as github or zendesk. atrium never interprets it")
	c.Flags().StringVar(&o.externalID, "external", "",
		"that system's own identifier for this work, such as openziti/ziti#4211")
	c.Flags().StringVar(&o.itemURL, "item-url", "", "the way back to the thing itself")
	c.Flags().StringVar(&o.repo, "repo", "", "which repository this work is in")
	c.Flags().StringVar(&o.org, "org", "", "which organisation owns it, so two forks are told apart")
	c.Flags().StringVar(&o.host, "host", "", "which forge it is on, such as github.com")
	c.Flags().StringVar(&o.branch, "branch", "", "which branch this directory is on, or with --repo as host/owner/repo the branch of the worktree atrium makes")
	c.Flags().StringVar(&o.base, "base", "", "with --repo as host/owner/repo: where a new branch starts (default claude/main)")
	c.Flags().BoolVar(&o.home, "home", false, "run in a new empty folder atrium makes under the room's card root, for scratch work")
	c.Flags().StringVar(&o.window, "window", "",
		"which pile this belongs to, and what the board groups by. free text: "+
			"active-work, pull-requests, tangent, a repo name, anything")
	c.Flags().StringVar(&o.theme, "theme", "",
		"the terminal palette this session comes up in, when the caller keeps that map")
	c.Flags().StringVar(&o.ifRunning, "if-running", "",
		"what to do when this directory already has a card: skip to hand it back and "+
			"start nothing, adopt to continue it. empty starts another one")
	c.Flags().StringVar(&o.reportTo, "report-to", "",
		"who this card reports to: a handle, alias or card id on this room, such as review. its reports "+
			"and silent-stop notices go there. an unknown name refuses the launch")
	c.Flags().StringVar(&o.onto, "onto", "",
		"start the runner on this existing card, named by its handle, alias, name@room or id. "+
			"without --cwd it runs in the card's own directory")
	c.Flags().StringVar(&o.itemOwner, "item-owner", "", "with --isolated: who owns it. empty is the creating session, human, or orchestrator, merger or director")
	c.Flags().StringVar(&o.item, "item", "",
		"REQUIRED for a new session: the backlog item this works on, by id (f-new-thing), or new:\"a title\" to file one "+
			"under the dept: tag in --tags. not needed with --resume or --onto, whose card already exists")
	c.Flags().StringVar(&o.step, "step", "",
		"with --item: the step of the item this card is for (designed, building, tested, reviewed, landed). a card with none "+
			"is a builder. a designer is --step designed, and its finishing makes the design wait for the human to approve it")
	c.Flags().BoolVar(&o.isolated, "isolated", false,
		"start the card isolated: its ending never mentions a launcher, its opening prompt comes from atrium, "+
			"it has no atrium_say, atrium_peers, atrium_task or atrium_report, and atrium refuses any say from it")
	c.Flags().BoolVar(&o.lean, "lean", false,
		"start a claude runner lean: no user CLAUDE.md, memory, skills or agents, only its brief, the repo, "+
			"atrium's hooks and the atrium-control and mercurius MCP servers. claude only")
	c.Flags().BoolVar(&o.superlean, "superlean", false,
		"start a claude runner superlean: lean, and only the tools Read, Edit, Write, Grep, Glob, Bash and ToolSearch, "+
			"no mercurius, and a one paragraph worker prompt. for a bounded change. implies --lean. claude only")
	c.Flags().BoolVar(&o.quiet, "quiet", false, "print only the card id")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

// parseEnvPairs turns repeated --env KEY=VALUE flags into the map the launch
// carries. A value may hold `=`, and only the first one splits.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("--env %q is not KEY=VALUE", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

func launchAgent(o launchOpts) error {
	onto := strings.TrimSpace(o.onto)
	if onto != "" && strings.TrimSpace(o.cwd) == "" {
		// The card knows its directory. The daemon uses it when none is sent.
		return launchBody(o, "")
	}
	if strings.TrimSpace(o.cwd) == "" && (o.home || daemon.RepoIsRef(o.repo)) {
		return launchBody(o, "")
	}
	if strings.TrimSpace(o.cwd) != "" && (o.home || daemon.RepoIsRef(o.repo)) {
		return fmt.Errorf("pass --cwd for a directory you made, or --repo / --home for one atrium makes, not both")
	}
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

	return launchBody(o, filepath.ToSlash(abs))
}

// launchBody posts the launch. `cwd` empty onto a card means the card's own.
func launchBody(o launchOpts, cwd string) error {
	env, err := parseEnvPairs(o.env)
	if err != nil {
		return err
	}

	fields := map[string]any{
		"effort":      o.effort,
		"args":        o.args,
		"env":         env,
		"harness":     o.harness,
		"cwd":         cwd,
		"task_id":     strings.TrimSpace(o.onto),
		"title":       o.title,
		"why":         o.why,
		"resume":      o.resume,
		"tags":        o.tags,
		"prompt":      o.prompt,
		"model":       o.model,
		"source":      o.source,
		"external_id": o.externalID,
		"url":         o.itemURL,
		"repo":        o.repo,
		"org":         o.org,
		"host":        o.host,
		"branch":      o.branch,
		"base":        o.base,
		"home":        o.home,
		"window":      o.window,
		"theme":       o.theme,
		"if_running":  o.ifRunning,
		"report_to":   o.reportTo,
	}
	// EVERY NEW LAUNCH NAMES AN ITEM, refused before anything is posted. A resume or a launch onto a card that exists
	// is the same work continuing, so it names none.
	if o.resume == "" && strings.TrimSpace(o.onto) == "" || strings.TrimSpace(o.item) != "" {
		id, filed, err := link.ResolveItem(context.Background(), boardHubCall(o.boardURL), o.item,
			link.DeptOfTags(o.tags), "", "")
		if err != nil {
			return err
		}
		if filed && !o.quiet {
			fmt.Printf("filed backlog item %s\n", id)
		}
		fields["tags"] = link.WithWorkTag(o.tags, id)
	}
	if s := strings.TrimSpace(o.step); s != "" {
		tags, _ := fields["tags"].([]string)
		if tags == nil {
			tags = o.tags
		}
		withStep, err := link.WithStepTag(tags, s)
		if err != nil {
			return err
		}
		fields["tags"] = withStep
	}
	// ONLY WHEN ASKED FOR, so a launch without the flag posts what it always did.
	if o.lean || o.superlean {
		fields["lean"] = true
	}
	if o.superlean {
		fields["superlean"] = true
	}
	if o.isolated {
		fields["item"] = true
		if so := strings.TrimSpace(o.itemOwner); so != "" {
			fields["tags"] = append(append([]string{}, fields["tags"].([]string)...), link.ItemOwnerTag+so)
		}
		fields["prompt"] = link.WithItemReportLine(o.prompt)
	}
	body, err := json.Marshal(fields)
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
	if h := resp.Header.Get("X-Atrium-Handle"); h != "" {
		fmt.Printf("  onto  %s\n", h)
	}
	if task.PID > 0 {
		fmt.Printf("  pid   %d\n", task.PID)
	}
	fmt.Printf("  card  %s\n", task.ID)
	fmt.Printf("  board %s\n", boardAddress(boardURL))
	return nil
}
