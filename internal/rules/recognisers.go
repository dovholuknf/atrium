package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A recogniser is a row that turns a URL into a filled-in launch dialog.
//
// The same shape as a harness and a source, and the same reason. A harness row
// says how to start a runner without atrium knowing what claude is. A source
// row says how to find work without atrium knowing what GitHub is. A
// recogniser row says what a URL MEANS without atrium knowing what a pull
// request is.
//
// ATRIUM LEARNS NOTHING ABOUT ANY SOURCE SYSTEM. A recogniser is a pattern and
// a mapping, and whoever wrote the row did the understanding. That is the rule
// that lets this serve a ticketing system nobody has thought of yet, and it is
// the rule that keeps the binary from growing a `case "github"`. There is no
// built-in row: an atrium with an empty recogniser table recognises nothing,
// which is correct.
//
// It also does not learn git. A recogniser produces a PATH, and how that path
// came into existence is somebody else's business. Where the path does not
// exist, resolution says so and stops, rather than making one. See
// docs/runtime/scm-design.md, part one.

// Recogniser is one row: a pattern, and the templates it fills in.
type Recogniser struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	// Rank orders the table, lowest first, and it is load bearing rather than
	// cosmetic.
	//
	// `.../pull/5/files` and `.../pull/5` are the same pull request, and a
	// generic "any repo on a known host" pattern will happily swallow both. So
	// the specific rows have to be asked first, and the shrug at the bottom is
	// what catches everything else. Ties break on id, so a table where nobody
	// set a rank is at least stable.
	Rank int `json:"rank"`
	// Pattern is a Go regular expression with named groups. The groups are the
	// variables every template below can read, plus `url` for the whole match
	// and whatever a fetch adds.
	//
	// The names that matter to a card are `host`, `org` and `repo`: those three
	// go onto the card as themselves, which is how two forks of one repo stop
	// landing in the same pile. `num` is the convention for an issue or pull
	// request number and atrium does nothing with it beyond substitution.
	Pattern string `json:"pattern"`

	// Every one of these is a template over the captures. Missing variables are
	// left standing as `{name}` and reported, never guessed at and never
	// silently blanked: a directory with a hole in it is a directory nobody
	// meant, and a card that says `{branch}` is one somebody can fix.
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Tags   string `json:"tags"`
	Cwd    string `json:"cwd"`
	Prompt string `json:"prompt"`
	Branch string `json:"branch"`
	Window string `json:"window"`
	Theme  string `json:"theme"`

	// DefaultRepo is where a link that names no repository opens, "host/org/repo", for a support ticket or a forum
	// topic. Empty is a scratch folder of the card's own. Not a template: the link's text never chooses a repo.
	DefaultRepo string `json:"default_repo"`

	// Fetch is an OPTIONAL argv that prints more facts as JSON on stdout.
	//
	// THIS IS THE WHOLE EXTENSIBILITY STORY AND IT HOLDS NO CREDENTIAL. The
	// pattern gives you an issue number. Turning that into a title needs an
	// authenticated network call, and atrium does not make one: it runs a
	// command the operator wrote, exactly as a source does, and reads what it
	// printed. `gh` already has a token, in the keyring it already uses.
	//
	// The command and its arguments are templated the same way everything else
	// here is, so `gh issue view {num} --repo {org}/{repo}` is the whole
	// configuration.
	Fetch     string   `json:"fetch"`
	FetchArgs []string `json:"fetch_args"`
	FetchCwd  string   `json:"fetch_cwd"`

	Notes string `json:"notes"`

	// What atrium observed, which the operator does not get to type. Same
	// observed-versus-overrides rule the cards follow.
	LastError  string     `json:"last_error"`
	Failures   int        `json:"failures"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Resolved is what a URL turned into: everything a launch dialog needs, and an
// account of what could not be worked out.
type Resolved struct {
	// Recogniser and Label name the row that answered, because the first
	// question about a wrong answer is which row gave it.
	Recogniser string `json:"recogniser"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	// Vars is every variable that was in scope, captures and fetched facts
	// together. Shown when a row is being written, which is the only time
	// anybody wants it.
	Vars map[string]string `json:"vars"`

	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Tags   []string `json:"tags"`
	Cwd    string   `json:"cwd"`
	Prompt string   `json:"prompt"`
	Repo   string   `json:"repo"`
	Org    string   `json:"org"`
	Host   string   `json:"host"`
	Branch string   `json:"branch"`
	Window string   `json:"window"`
	Theme  string   `json:"theme"`
	// DefaultRepo is the row's, unchanged.
	DefaultRepo string `json:"default_repo"`

	// Missing names every `{placeholder}` no variable answered, sorted. The
	// placeholder is still there in the text, so the dialog shows a hole rather
	// than a plausible wrong value.
	Missing []string `json:"missing,omitempty"`
	// CwdExists is whether the directory is there. Filled in by DescribeCwd,
	// with the stat of whoever can look at the filesystem.
	CwdExists bool `json:"cwd_exists"`
	// Problem is the one sentence a human has to read. Empty when there is
	// nothing to say.
	Problem string `json:"problem,omitempty"`
	// FetchError is what the fetch command said when it broke. Never fatal: see
	// `RecogniserFetched`.
	FetchError string `json:"fetch_error,omitempty"`
}

// ErrNoRecogniser is what a URL nothing matches comes back as.
//
// An error rather than an empty result, because "no row wanted this" and "a row
// matched and filled in nothing" are different answers and the caller says
// different things about them.
var ErrNoRecogniser = errors.New("no recogniser matches this")

// CheckRecogniser is the save rule without a store: an id, a pattern that compiles, a label. The hub's table holds
// its rows to it as well. The bookkeeping a save never takes from a request is left as it came, for the caller to
// keep or drop.
func CheckRecogniser(r Recogniser) (Recogniser, error) {
	r.ID = strings.TrimSpace(r.ID)
	if r.ID == "" {
		return r, errors.New("a recogniser needs an id")
	}
	r.Pattern = strings.TrimSpace(r.Pattern)
	if r.Pattern == "" {
		return r, errors.New("a recogniser needs a pattern to match urls against")
	}
	if _, err := regexp.Compile(r.Pattern); err != nil {
		return r, errors.New("that pattern is not a valid regular expression: " + err.Error())
	}
	if r.Label == "" {
		r.Label = r.ID
	}
	r.DefaultRepo = strings.Trim(strings.TrimSpace(r.DefaultRepo), "/")
	if r.DefaultRepo != "" && !RepoSlug(r.DefaultRepo) {
		return r, errors.New("the default repo is host/org/repo, like github.com/openziti/ziti, or empty for none")
	}
	if r.FetchArgs == nil {
		r.FetchArgs = []string{}
	}
	return r, nil
}

// PRTag is the tag that makes a recogniser's link a pull request, which the open verb reviews.
const PRTag = "pull-request"

// IsPRRow is whether a row's tags template names PRTag.
func IsPRRow(tags string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == PRTag {
			return true
		}
	}
	return false
}

// repoSegment is one part of a "host/org/repo": no part is . or .., so a path made from it cannot climb out.
var repoSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

// RepoSlug is whether s is "host/org/repo" and nothing else.
func RepoSlug(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if !repoSegment.MatchString(p) {
			return false
		}
	}
	return true
}

// MatchRecogniserIn is MatchRecogniser over rows held somewhere other than this store, which is the hub's table.
// The rows are asked in rank order whatever order they came in.
func MatchRecogniserIn(rows []*Recogniser, url string) (*Recogniser, map[string]string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil, errors.New("no url to recognise")
	}
	rows = append([]*Recogniser(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Rank != rows[j].Rank {
			return rows[i].Rank < rows[j].Rank
		}
		return rows[i].ID < rows[j].ID
	})
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			// Saving compiles it, so a row that does not compile got here
			// another way: an import, or a hand-edited database. Skipping is
			// the only sane answer, since one bad row must not stop the rows
			// below it from being asked.
			continue
		}
		m := re.FindStringSubmatch(url)
		if m == nil {
			continue
		}
		vars := map[string]string{"url": url}
		for i, name := range re.SubexpNames() {
			if name == "" || i >= len(m) {
				continue
			}
			vars[name] = m[i]
		}
		// `url` is the URL, always, and a capture group called `url` does not
		// get to redefine it. Every template that builds a prompt says
		// "{url}" meaning the link, and a row that quietly changed what that
		// means would produce cards pointing at the wrong place.
		vars["url"] = url
		return r, vars, nil
	}
	return nil, nil, ErrNoRecogniser
}

// placeholder is `{name}` and nothing cleverer.
//
// No expressions, no defaults, no filters. A template language grows one
// feature at a time until it is a programming language nobody can debug inside
// a text box, and the thing that needs computing already has a place to be
// computed: the fetch command.
var placeholder = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// Fill turns the row's templates into a resolution.
//
// UNKNOWN PLACEHOLDERS ARE LEFT STANDING AND REPORTED. The two alternatives are
// both worse. Blanking `{branch}` turns `feature/{branch}` into `feature/`,
// which is a directory somebody will create by accident. Failing the whole
// resolution throws away the five fields that did work because the sixth did
// not. Leaving the hole visible means the dialog shows exactly what atrium
// could not work out, next to a field the operator can type into.
func (r *Recogniser) Fill(vars map[string]string) *Resolved {
	missing := map[string]bool{}
	fill := func(tmpl string) string {
		return placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
			key := m[1 : len(m)-1]
			if v := vars[key]; v != "" {
				return v
			}
			missing[key] = true
			return m
		})
	}

	out := &Resolved{
		Recogniser: r.ID,
		Label:      r.Label,
		URL:        vars["url"],
		Vars:       vars,
		Kind:       oneLine(fill(r.Kind)),
		Title:      oneLine(fill(r.Title)),
		Cwd:        oneLine(fill(r.Cwd)),
		Branch:     oneLine(fill(r.Branch)),
		Window:     oneLine(fill(r.Window)),
		Theme:      oneLine(fill(r.Theme)),
		// The prompt is the one field a newline belongs in. An issue body is
		// several paragraphs and flattening it would be atrium mangling
		// somebody else's words on the way past.
		Prompt: fill(r.Prompt),
		Repo:   strings.TrimSpace(vars["repo"]),
		Org:    strings.TrimSpace(vars["org"]),
		Host:   strings.TrimSpace(vars["host"]),

		DefaultRepo: r.DefaultRepo,
	}
	// Split on commas and never on spaces, the same rule the tag field on a
	// card follows. A tag with a space in it is one tag.
	for _, t := range strings.Split(fill(r.Tags), ",") {
		if t = oneLine(t); t != "" {
			out.Tags = append(out.Tags, t)
		}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	for k := range missing {
		out.Missing = append(out.Missing, k)
	}
	sort.Strings(out.Missing)
	return out
}

// FillPrompt fills the prompt from the URL's captures only. LINKED TEXT NEVER REACHES A PROMPT (design
// docs/rnd/card-lifecycle-design.md section 11): an issue body is written by anybody, and a prompt is what the runner
// obeys. A placeholder naming a fetched fact becomes "", and anything else stays `{name}`, as Fill leaves it.
func (r *Recogniser) FillPrompt(captures map[string]string, fetched map[string]bool) string {
	return placeholder.ReplaceAllStringFunc(r.Prompt, func(m string) string {
		key := m[1 : len(m)-1]
		if v := captures[key]; v != "" {
			return v
		}
		if fetched[key] {
			return ""
		}
		return m
	})
}

// Resolve is the whole resolution once a fetch has run: the facts merged under the captures, the templates filled,
// the prompt from the captures alone. The room and the hub both resolve through here, so a link reads the same
// whichever of them answered.
//
// THE CAPTURES WIN. Fetched facts come off the network, from a tracker anybody may have written into. The captures
// came off the URL the operator pasted. A fetch that could redefine `repo` could move `cwd`, so a fact only fills a
// name the pattern did not capture, and never overwrites one it did.
func (r *Recogniser) Resolve(vars, facts map[string]string, fetchErr error) *Resolved {
	captures := make(map[string]string, len(vars))
	all := make(map[string]string, len(vars)+len(facts))
	for k, v := range vars {
		captures[k], all[k] = v, v
	}
	fetched := map[string]bool{}
	for k, v := range facts {
		if _, taken := vars[k]; !taken {
			all[k] = v
			fetched[k] = true
		}
	}
	out := r.Fill(all)
	// The title and the rest may use the facts. The prompt is the runner's orders, so only the URL fills it.
	out.Prompt = r.FillPrompt(captures, fetched)
	if fetchErr != nil {
		out.FetchError = FirstLine(fetchErr.Error())
	}
	return out
}

// DescribeCwd says what stat found at the directory the templates named: CwdExists, or the one sentence in Problem.
// This package looks at no disk, so the caller hands it the stat of whichever machine it is answering for. The room
// and the hub share it, so the sentence is the same wherever the link was recognised.
//
// ATRIUM DOES NOT CREATE THE DIRECTORY. Making a worktree is `gwt`'s job, so a missing one is a sentence naming the
// path, beside a field the operator can point somewhere else.
func DescribeCwd(out *Resolved, stat func(string) (fs.FileInfo, error)) {
	// A HOLE IN THE PATH IS ANSWERED BEFORE THE FILESYSTEM IS. A directory still called `.../{branch}` does not exist
	// for an uninteresting reason, and saying "no such directory" about it sends somebody off to make one with a
	// brace in its name. Only a hole in the PATH: a `{title}` nothing filled in is named in Missing and stops nothing.
	if holes := placeholder.FindAllString(out.Cwd, -1); len(holes) > 0 {
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
	fi, err := stat(filepath.FromSlash(out.Cwd))
	if err == nil && fi.IsDir() {
		out.CwdExists = true
		return
	}
	if err == nil {
		out.Problem = out.Cwd + " is a file, not a directory"
		return
	}
	// Not an error and not a refusal. The URL resolved, and the checkout for it is not on this machine yet.
	out.Problem = out.Cwd + " is not here yet. make the worktree, then start it"
}

// FillArgv substitutes into a command and its arguments.
//
// Separate from `Fill` because an argv is not a card field: nothing is
// collapsed to one line, nothing is reported as missing, and an unfilled
// placeholder is left exactly as written. The command sees `{num}` and says
// what it thinks of it, which is a better error than atrium guessing.
//
// EVERY ARGUMENT STAYS ITS OWN ARGUMENT. Nothing here joins them into a command
// string, so a repo name with a space in it is one argument rather than a
// quoting problem for whichever shell got involved.
func FillArgv(cmd string, args []string, vars map[string]string) (string, []string) {
	sub := func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(m string) string {
			if v := vars[m[1:len(m)-1]]; v != "" {
				return v
			}
			return m
		})
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, sub(a))
	}
	return sub(cmd), out
}

// oneLine keeps a fetched value from turning one field into three.
//
// A title fetched from an issue tracker is somebody else's text, and somebody
// else's text has newlines in it. Every field but the prompt is a single line
// on a card or a path on a filesystem, and neither survives one.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// FirstLine keeps a failure to one readable line on a row.
func FirstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
