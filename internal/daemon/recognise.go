package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/linkfetch"
	"github.com/dovholuknf/atrium/internal/store"
)

// Turning a pasted URL into a filled-in launch dialog.
//
// The store owns the table, the patterns and the templates, all of which are
// pure and testable without a process or a filesystem. This file owns the two
// things that are neither: running the operator's fetch command, and looking at
// the directory the templates named.
//
// IT STOPS AT THE DIALOG. Recognising a URL fills a form in. It does not start
// a runner, and it does not make a directory. Starting work is a decision and
// the launch dialog already exists to take it. Making a worktree is `gwt`'s job
// and atrium has no business owning a checkout layout. Where the directory is
// missing, the answer is a sentence saying so. See docs/runtime/scm-design.md.

const (
	// recogniserFetchLimit bounds what one fetch may print.
	//
	// A fetch answering a single issue number with more than a megabyte is
	// reporting a repository, and reading it all in to find that out is the
	// failure the bound exists to prevent. Same number as a source, and
	// bounded while reading rather than after, for the same reason.
	recogniserFetchLimit = 1 << 20

	// recogniserFetchTimeout bounds one fetch.
	//
	// Half a minute, where a source gets two. A source runs on a timer and
	// nobody is waiting. This runs because somebody pasted a URL and is
	// watching a dialog not open. A fetch that takes longer than this has
	// already failed as far as the person is concerned.
	recogniserFetchTimeout = 30 * time.Second
)

// Recognise resolves one URL against the recogniser table.
//
// Returns store.ErrNoRecogniser when no row wanted it, which the caller reports
// as "nothing recognises this" rather than as a broken request. Every other
// difficulty comes back INSIDE the resolution: a fetch that failed, a
// placeholder nothing filled in, a directory that is not there. All three are
// things a human reads and acts on, and none of them is a reason to withhold
// the four fields that did resolve.
func (d *Daemon) Recognise(url string) (*store.Resolved, error) {
	r, vars, fromHub, err := d.matchRecogniser(url)
	if err != nil {
		return nil, err
	}

	var facts map[string]string
	fetchErr := error(nil)
	if strings.TrimSpace(r.Fetch) != "" {
		facts, fetchErr = d.fetchFacts(context.Background(), r, vars)
		// A hub's row is not in this room's table, so there is nothing here to record it on.
		if !fromHub {
			if err := d.st.RecogniserFetched(r.ID, fetchErr); err != nil {
				log.Printf("[atrium] recording the fetch for recogniser %s: %v", r.ID, err)
			}
		}
	}
	// The captures win over the facts, and a failed fetch adds none. See store.Recogniser.Resolve.
	if fetchErr != nil {
		facts = nil
	}
	out := r.Resolve(vars, facts, nil)
	if fetchErr != nil {
		out.FetchError = firstLine(fetchErr.Error())
	}
	// ATRIUM DOES NOT CREATE THE DIRECTORY. See store.DescribeCwd.
	store.DescribeCwd(out, os.Stat)
	return out, nil
}

// matchRecogniser matches url against the HUB'S rows when this room has a hub, and against its own table otherwise.
//
// The table is the hub's (link/recognisers.go), so a paste the hub places on any room is recognised the same way.
// A hub that cannot be asked right now, or one older than the route, leaves the room on its own table, with a log
// line: recognising a link runs nothing and reads no forge, so the rule that a room never falls back to a forge of its
// own does not reach it. fromHub says which table answered.
func (d *Daemon) matchRecogniser(url string) (r *store.Recogniser, vars map[string]string, fromHub bool, err error) {
	if hf := d.HubForge(); hf != nil {
		ctx, cancel := context.WithTimeout(context.Background(), recogniserFetchTimeout)
		var got struct {
			Recognisers []*store.Recogniser `json:"recognisers"`
		}
		err := hf.Call(ctx, forge.HubRecognisersPath, struct{}{}, &got)
		cancel()
		if err == nil {
			r, vars, err := store.MatchRecogniserIn(got.Recognisers, url)
			return r, vars, true, err
		}
		log.Printf("[atrium] the hub's recognisers could not be read (%v), so this room's own are used", err)
	}
	r, vars, err = d.st.MatchRecogniser(url)
	return r, vars, false, err
}

// fetchFacts runs the row's fetch command and reads what it printed.
//
// The argv is templated first, so `gh issue view {num} --repo {org}/{repo}` is
// the whole configuration. Every argument stays its own argv element and
// nothing is joined into a command string: a template that expanded to a value
// with a quote in it would otherwise become a shell's problem rather than the
// command's.
//
// ATRIUM HOLDS NO CREDENTIAL HERE and there is nowhere in the row to put one.
// `gh` already has a token in the keyring it already uses, which is the same
// arrangement sources are built on.
func (d *Daemon) fetchFacts(ctx context.Context, r *store.Recogniser,
	vars map[string]string) (map[string]string, error) {

	name, args := store.FillArgv(r.Fetch, r.FetchArgs, vars)
	if name == forgeFetch {
		return d.forgeFacts(ctx, args, vars)
	}
	// A slug-only link names no number, and following it to where it lands does. See linkfetch.
	if name == linkfetch.Redirect {
		return linkfetch.Follow(ctx, vars["url"], args)
	}
	// A ROOM WITH A HUB RUNS NO FORGE CLI: only the hub talks to the forge. See hubforge.go.
	if forgeCLIs[strings.ToLower(filepath.Base(name))] && d.HubForge() != nil {
		return nil, fmt.Errorf("this room reads the forge only through its hub, so it does not run %s. set the "+
			"recogniser's fetch to `forge` with the argument pr or issue", name)
	}
	runCtx, cancel := context.WithTimeout(ctx, recogniserFetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = r.FetchCwd
	// Fetched behind a dialog the operator is already looking at, so a console
	// appearing over it is the same interruption a source's was. See
	// hideWindow.
	hideWindow(cmd)
	// The daemon's own environment, minus the markers that would make a child
	// think it is inside the session atrium was started from. Exactly what a
	// source and a launched runner get, and for the same reason.
	cmd.Env = childEnv(nil, nil)

	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: recogniserFetchLimit + 1}
	cmd.Stderr = &limitedWriter{w: &errOut, left: 8 << 10}

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("took longer than %s and was stopped", recogniserFetchTimeout)
	}
	if out.Len() > recogniserFetchLimit {
		return nil, fmt.Errorf("printed more than the %d byte limit and was stopped",
			recogniserFetchLimit)
	}
	if err != nil {
		if errOut.Len() > 0 {
			return nil, fmt.Errorf("%v: %s", err, firstLine(errOut.String()))
		}
		return nil, err
	}

	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		// A fetch with nothing to add is not a failure. The captures already
		// filled the dialog in.
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("printed something that is not a json object of facts: %w", err)
	}
	return flattenFacts(raw), nil
}

// flattenFacts turns one JSON object into variables a template can read.
//
// A template substitutes text, so every fact has to become text. Strings,
// numbers and booleans are obvious. Anything else is written back out as JSON
// rather than dropped, because a fact that vanished silently is a `{labels}`
// nobody can explain, and seeing `[{"name":"bug"}]` land in a field is what
// teaches an operator to pipe their fetch through `jq` once.
//
// Null is dropped. A null is the tracker saying it does not have one, which is
// the same answer as not printing the key, and turning it into the four letters
// "null" would put them on a card.
func flattenFacts(raw map[string]any) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch t := v.(type) {
		case nil:
			continue
		case string:
			out[k] = t
		case bool:
			out[k] = strconv.FormatBool(t)
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		default:
			b, err := json.Marshal(t)
			if err != nil {
				continue
			}
			out[k] = string(b)
		}
	}
	return out
}

// forgeFetch is the built-in fetch: `forge pr` or `forge issue` reads the pull request or issue the URL names through
// the hub's forge, or the room's own when it has no hub. The facts carry gh's names, so templates written for
// `gh pr view --json title,headRefName,baseRefName` and `gh issue view --json title,body` read the same.
const forgeFetch = forge.FactsFetch

// forgeCLIs are the commands a room with a hub does not run as a fetch.
var forgeCLIs = map[string]bool{"gh": true, "gh.exe": true, "bb": true, "bb.exe": true, "glab": true, "glab.exe": true}

func (d *Daemon) forgeFacts(ctx context.Context, args []string, vars map[string]string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, recogniserFetchTimeout)
	defer cancel()
	return forge.Facts(ctx, args, vars, func(host string) (forge.Forge, error) {
		if hf := d.HubForge(); hf != nil {
			return hf, nil
		}
		return d.prr.forgeFor(host)
	})
}
