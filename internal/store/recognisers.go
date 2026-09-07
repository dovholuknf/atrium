package store

import (
	"encoding/json"
	"errors"
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
// docs/scm-design.md, part one.

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

	// Missing names every `{placeholder}` no variable answered, sorted. The
	// placeholder is still there in the text, so the dialog shows a hole rather
	// than a plausible wrong value.
	Missing []string `json:"missing,omitempty"`
	// CwdExists is whether the directory is there. Filled in by whoever can
	// look at the filesystem, which is not this package.
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

const recogniserColumns = `id, label, enabled, rank, pattern, kind, title, tags, cwd,
	prompt, branch, window_name, theme, fetch_cmd, fetch_args, fetch_cwd, notes,
	last_error, failures, last_used_at, created_at`

func scanRecogniser(sc interface{ Scan(...any) error }) (*Recogniser, error) {
	var (
		r                 Recogniser
		enabled           int
		args              string
		lastUsed, created string
	)
	if err := sc.Scan(&r.ID, &r.Label, &enabled, &r.Rank, &r.Pattern, &r.Kind, &r.Title,
		&r.Tags, &r.Cwd, &r.Prompt, &r.Branch, &r.Window, &r.Theme, &r.Fetch, &args,
		&r.FetchCwd, &r.Notes, &r.LastError, &r.Failures, &lastUsed, &created); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &r.FetchArgs); err != nil {
		return nil, err
	}
	if r.FetchArgs == nil {
		r.FetchArgs = []string{}
	}
	if lastUsed != "" {
		t, err := parseTS(lastUsed)
		if err != nil {
			return nil, err
		}
		r.LastUsedAt = &t
	}
	var err error
	if r.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &r, nil
}

// Recognisers lists every row, in the order they are asked: most specific
// first, which is what rank means.
func (st *Store) Recognisers() ([]*Recogniser, error) {
	var out []*Recogniser
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT ` + recogniserColumns +
			` FROM recogniser ORDER BY rank ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRecogniser(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// RecogniserByID returns one row.
func (st *Store) RecogniserByID(id string) (*Recogniser, error) {
	var r *Recogniser
	err := st.guard(func() error {
		row := st.db.QueryRow(`SELECT `+recogniserColumns+` FROM recogniser WHERE id = ?`, id)
		got, err := scanRecogniser(row)
		if err != nil {
			return err
		}
		r = got
		return nil
	})
	return r, err
}

// SaveRecogniser creates or replaces a row.
//
// THE PATTERN IS COMPILED HERE AND THE SAVE IS REFUSED IF IT DOES NOT. A bad
// regular expression that is only discovered when somebody pastes a URL is a
// feature that appears broken at the moment it is being relied on, and the
// person who typed the pattern is long gone. Refusing at save time puts the
// error in front of the person who can fix it.
//
// The failure bookkeeping is not writable, the same as a source: it is what
// atrium observed, and the operator does not get to type it.
func (st *Store) SaveRecogniser(r Recogniser) (*Recogniser, error) {
	r.ID = strings.TrimSpace(r.ID)
	if r.ID == "" {
		return nil, errors.New("a recogniser needs an id")
	}
	r.Pattern = strings.TrimSpace(r.Pattern)
	if r.Pattern == "" {
		return nil, errors.New("a recogniser needs a pattern to match urls against")
	}
	if _, err := regexp.Compile(r.Pattern); err != nil {
		return nil, errors.New("that pattern is not a valid regular expression: " + err.Error())
	}
	if r.Label == "" {
		r.Label = r.ID
	}
	args, err := json.Marshal(orEmptySlice(r.FetchArgs))
	if err != nil {
		return nil, err
	}

	err = st.guard(func() error {
		created := ts(now())
		wasEnabled := false
		row := st.db.QueryRow(`SELECT created_at, enabled FROM recogniser WHERE id = ?`, r.ID)
		var enabledNow int
		if err := row.Scan(&created, &enabledNow); err == nil {
			wasEnabled = enabledNow != 0
		}
		enabled := 0
		if r.Enabled {
			enabled = 1
		}
		// Turning one back on is the operator saying they fixed it.
		clear := r.Enabled && !wasEnabled
		_, err := st.db.Exec(`INSERT INTO recogniser
			(id, label, enabled, rank, pattern, kind, title, tags, cwd, prompt, branch,
			 window_name, theme, fetch_cmd, fetch_args, fetch_cwd, notes, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, rank = excluded.rank,
				pattern = excluded.pattern, kind = excluded.kind, title = excluded.title,
				tags = excluded.tags, cwd = excluded.cwd, prompt = excluded.prompt,
				branch = excluded.branch, window_name = excluded.window_name,
				theme = excluded.theme, fetch_cmd = excluded.fetch_cmd,
				fetch_args = excluded.fetch_args, fetch_cwd = excluded.fetch_cwd,
				notes = excluded.notes,
				failures   = CASE WHEN ? THEN 0  ELSE recogniser.failures   END,
				last_error = CASE WHEN ? THEN '' ELSE recogniser.last_error END`,
			r.ID, r.Label, enabled, r.Rank, r.Pattern, r.Kind, r.Title, r.Tags, r.Cwd,
			r.Prompt, r.Branch, r.Window, r.Theme, r.Fetch, string(args), r.FetchCwd,
			r.Notes, created, clear, clear)
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.RecogniserByID(r.ID)
}

// DeleteRecogniser removes a row. The cards it filled in stay: they are work,
// and deleting the thing that recognised the URL does not make it not work.
func (st *Store) DeleteRecogniser(id string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`DELETE FROM recogniser WHERE id = ?`, id)
		return err
	})
}

// RecogniserFetched records what a fetch command did.
//
// A FETCH THAT FAILS NEVER SWITCHES THE ROW OFF, and this is deliberately
// unlike a source. A source is a timer nobody is watching, so a broken one
// retrying forever is a process spawned every fifteen minutes to produce an
// error into an empty room, and switching it off is a kindness. A recogniser is
// a verb somebody just typed, with the board open in front of them. Switching
// the row off would mean the NEXT paste silently matches nothing while they
// watch, which turns "gh was not logged in" into "atrium is broken".
//
// So the reason goes on the row where the settings screen shows it, the count
// says how long it has been going on, and the URL still resolves from its
// captures. Losing the fetched title is a worse card. Losing the card is worse
// than that.
func (st *Store) RecogniserFetched(id string, fetchErr error) error {
	n := ts(now())
	if fetchErr == nil {
		return st.guard(func() error {
			_, err := st.db.Exec(`UPDATE recogniser SET
				last_used_at = ?, last_error = '', failures = 0 WHERE id = ?`, n, id)
			return err
		})
	}
	reason := firstLineOf(fetchErr.Error())
	return st.guard(func() error {
		_, err := st.db.Exec(`UPDATE recogniser SET
			last_used_at = ?, last_error = ?, failures = failures + 1
			WHERE id = ?`, n, reason, id)
		return err
	})
}

// MatchRecogniser finds the first enabled row whose pattern matches, and
// returns it with the variables its captures produced.
//
// FIRST MATCH WINS, in rank order. That is the whole dispatch: the specific
// rows are asked before the generic ones, and the generic one at the bottom is
// a shrug rather than an error, because a URL on a known host that matches
// nothing is still worth opening a dialog for.
//
// The url is trimmed and nothing else. No scheme is added, no trailing slash is
// removed, nothing is lowercased. Every one of those would be atrium deciding
// what a URL means, which is the one thing it must not do: a pattern author who
// wants to accept a bare host writes `(?:https?://)?` and has said so.
func (st *Store) MatchRecogniser(url string) (*Recogniser, map[string]string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil, errors.New("no url to recognise")
	}
	rows, err := st.Recognisers()
	if err != nil {
		return nil, nil, err
	}
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
