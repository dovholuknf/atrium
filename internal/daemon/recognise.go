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
	"regexp"
	"strconv"
	"strings"
	"time"

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
// missing, the answer is a sentence saying so. See docs/scm-design.md.

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
	r, vars, err := d.st.MatchRecogniser(url)
	if err != nil {
		return nil, err
	}

	fetchErr := error(nil)
	if strings.TrimSpace(r.Fetch) != "" {
		facts, err := d.fetchFacts(context.Background(), r, vars)
		fetchErr = err
		if err == nil {
			// THE CAPTURES WIN.
			//
			// Fetched facts come off the network, from an issue tracker anybody
			// may have written into. The captures came off the URL the operator
			// pasted. A fetch that could redefine `repo` could move `cwd`,
			// which means the contents of an issue would choose the directory a
			// runner starts in. So a fact only fills a name the pattern left
			// empty, and never overwrites one it filled.
			for k, v := range facts {
				if _, taken := vars[k]; !taken {
					vars[k] = v
				}
			}
		}
		if err := d.st.RecogniserFetched(r.ID, fetchErr); err != nil {
			log.Printf("[atrium] recording the fetch for recogniser %s: %v", r.ID, err)
		}
	}

	out := r.Fill(vars)
	if fetchErr != nil {
		out.FetchError = firstLine(fetchErr.Error())
	}
	d.describeCwd(out)
	return out, nil
}

// describeCwd looks at the directory the templates named and says what it
// found.
//
// ATRIUM DOES NOT CREATE IT. This is the point in the flow where a tool that
// knew git would run a clone, and the reason this one does not is that it would
// be a second, worse implementation of something already on the PATH and
// already better at it. What atrium contributes is knowing which card the
// directory belongs to.
//
// So the answer to a missing directory is a sentence naming the path, which the
// dialog shows beside a field the operator can point somewhere else. Make it
// with whatever makes worktrees here, then press start.
func (d *Daemon) describeCwd(out *store.Resolved) {
	// A HOLE IN THE PATH IS ANSWERED BEFORE THE FILESYSTEM IS. A directory
	// still called `.../{branch}` does not exist for an uninteresting reason,
	// and saying "no such directory" about it sends somebody off to create one
	// with a brace in its name.
	//
	// Only a hole in the PATH. A `{title}` nothing filled in leaves a thin card
	// and is named in `Missing` for the dialog to show, but it has no bearing
	// on whether the work can start.
	if holes := placeholdersIn(out.Cwd); len(holes) > 0 {
		out.Problem = fmt.Sprintf(
			"the directory still says %s, because nothing filled it in. "+
				"type over it, or add a fetch to the recogniser that knows",
			strings.Join(holes, " and "))
		return
	}
	if strings.TrimSpace(out.Cwd) == "" {
		out.Problem = "this recogniser does not say where the work happens. pick a directory"
		return
	}
	path := filepath.FromSlash(out.Cwd)
	fi, err := os.Stat(path)
	if err == nil && fi.IsDir() {
		out.CwdExists = true
		return
	}
	if err == nil {
		out.Problem = out.Cwd + " is a file, not a directory"
		return
	}
	// Not an error and not a refusal. The URL resolved, and the checkout for it
	// is not on this machine yet, which is a thing to go and do.
	out.Problem = out.Cwd + " is not here yet. make the worktree, then start it"
}

// placeholdersIn finds the `{name}` holes still standing in a filled-in value.
//
// Read out of the RESULT rather than out of the row's Missing list, because the
// question here is about one field. A recogniser can leave a hole in its title
// and none in its path, and only one of those stops the work starting.
func placeholdersIn(s string) []string {
	var out []string
	for _, m := range placeholder.FindAllString(s, -1) {
		out = append(out, m)
	}
	return out
}

// placeholder is the same `{name}` the store's templates use. Duplicated as one
// expression rather than exported, because what this package needs is to spot a
// hole and what that one needs is to fill one.
var placeholder = regexp.MustCompile(`\{[a-zA-Z_][a-zA-Z0-9_]*\}`)

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
	runCtx, cancel := context.WithTimeout(ctx, recogniserFetchTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Dir = r.FetchCwd
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
