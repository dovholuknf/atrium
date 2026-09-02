package store

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Rule is a standing answer to a permission request. Deciding a request
// "forever" writes one of these, and every later request that matches is
// answered without asking again.
//
// A pattern is either a plain prefix or a glob. Plain text matches by prefix,
// so `go build` covers every later `go build` without silently covering
// `go install`. A pattern containing `*` or `?` is matched as a glob against
// the whole command instead, the way Claude Code's own permission patterns
// read: `go * -o build.claude/*`, `*/internal/*.go <- *`.
//
// Prefix stays the default because a rule is only ever created from a request
// the operator actually saw, and turning that into a wildcard would
// widen it beyond what they agreed to.
type Rule struct {
	ID       string `json:"id"`
	Tool     string `json:"tool"`
	Prefix   string `json:"prefix"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Scope    string `json:"scope"`
	// Kind is how Prefix is read. See the Rule kinds below.
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int       `json:"hits"`
}

// Rule kinds.
//
// KindCommand reads Prefix as a command shape: a literal prefix, or a glob when
// it contains a wildcard. This is what "always" writes, because a rule made
// from a request the operator actually read should cover that shape of request
// and nothing wider.
//
// KindPath reads Prefix as a directory: "let it work in here". See matchPath
// for what that covers, which is wider than "names a path inside it".
//
// As a command glob the same thing means hand-writing a pattern that accounts
// for the quoting around the path. `rm -f "C:/x/*"` fails against
// `rm -f "C:/x/y.db"` over the closing quote alone, and fails silently.
const (
	KindCommand = "command"
	KindPath    = "path"
)

// fileTools name a path rather than run a command, so a useful standing rule
// for them covers a directory, not a single file.
var fileTools = map[string]bool{
	"Edit": true, "Write": true, "Read": true, "MultiEdit": true, "NotebookEdit": true,
}

// DefaultPrefix picks the part of a request a "forever" rule should cover.
//
// The right answer depends on the tool. A command like
// `go build -o build.claude/ ./...` should become `go build`, which covers
// every later build while leaving `go install` to ask on its own. A file edit
// is not a command at all: its "command" is a path, so two leading words would
// pin the rule to one file and it would ask again on the very next edit. File
// tools get the containing directory instead: "stop asking about edits in this
// repo".
func DefaultPrefix(tool, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if fileTools[tool] {
		// The hook formats these as `<path> <- (replace edit)`.
		path := strings.Fields(command)[0]
		path = strings.ReplaceAll(path, `\`, "/")
		if i := strings.LastIndex(path, "/"); i > 0 {
			return path[:i+1]
		}
		return path
	}
	fields := strings.Fields(command)
	if len(fields) == 1 {
		return fields[0]
	}
	// A leading flag or path fragment is not a verb, so keep one word in that
	// case rather than gluing an argument onto the rule.
	if strings.HasPrefix(fields[1], "-") {
		return fields[0]
	}
	return fields[0] + " " + fields[1]
}

// IsGlob reports whether a pattern should be matched as a glob rather than as
// a prefix.
func IsGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?")
}

// globRE turns a glob into an anchored regexp. Unlike path.Match, `*` crosses
// path separators, because these patterns are written against whole commands
// and Windows paths, where refusing to cross `/` would surprise everyone.
func globRE(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`\A`)
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(`\z`)
	return regexp.Compile(b.String())
}

// globCache keeps compiled patterns, since MatchRule runs on every gated tool
// call and the rule set changes rarely.
var globCache sync.Map // pattern -> *regexp.Regexp

// normalizePath puts a path in the form matching compares against: forward
// slashes, lowercased, no surrounding quotes.
//
// Lowercased because Windows paths are case insensitive, and a rule failing
// over a capital letter gives no visible reason.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.Trim(p, `"'`)
	return strings.ToLower(p)
}

// under reports whether path names something inside dir, or dir itself. The
// trailing separator stops `C:/tmp` from covering `C:/tmpfiles`.
func under(dir, path string) bool {
	d, p := normalizePath(dir), normalizePath(path)
	if d == "" || p == "" {
		return false
	}
	d = strings.TrimSuffix(d, "/")
	return p == d || strings.HasPrefix(p, d+"/")
}

// tokenize splits a command into arguments, keeping a quoted run whole.
//
// Splitting on whitespace is not enough. `copy "C:/Program Files/x" .` becomes
// `"C:/Program` and `Files/x"`, and neither is recognisable as an absolute
// path, so a command reaching outside an allowed folder reads as if it names no
// absolute path at all. Under the working-directory rule below, that approves
// it.
func tokenize(command string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// absolutePaths pulls the absolute path-looking tokens out of a command.
//
// Rough: a shell command is not parseable without a shell. It only has to
// answer "does this reach somewhere the rule does not cover", where a false
// positive costs a prompt and a false negative costs an unwanted approval, so
// it errs toward finding more.
func absolutePaths(command string) []string {
	var out []string
	for _, t := range tokenize(strings.ReplaceAll(command, `\`, "/")) {
		// A url is not a path on this machine.
		if strings.Contains(t, "://") {
			continue
		}
		// A flag with its value attached hides a path: `--output=C:/x`. Checked
		// before the leading dash is skipped, or the path is never seen.
		if i := strings.IndexByte(t, '='); i >= 0 && i+1 < len(t) {
			if rest := t[i+1:]; isAbsolutePath(rest) {
				out = append(out, rest)
				continue
			}
		}
		// A bare flag is not a path.
		if strings.HasPrefix(t, "-") {
			continue
		}
		if isAbsolutePath(t) {
			out = append(out, t)
		}
	}
	return out
}

// isAbsolutePath reports whether a token names a path from a root: `/x` or
// `C:/x`. Anything else is relative and belongs to the working directory.
func isAbsolutePath(t string) bool {
	if strings.HasPrefix(t, "/") {
		return true
	}
	return len(t) > 2 && t[1] == ':' && t[2] == '/'
}

// escapesUpward reports whether a command uses `..` to climb out of wherever it
// is. A relative path is only safely inside a folder if it never walks up.
func escapesUpward(command string) bool {
	c := strings.ReplaceAll(command, `\`, "/")
	return strings.Contains(c, "../") || strings.HasSuffix(c, "/..") ||
		strings.Contains(c, " .. ") || strings.HasSuffix(c, " ..")
}

// matchPath reports whether a folder rule for dir answers this request.
//
// Three questions, in this order:
//
//  1. Does the command reach outside dir? Any absolute path it names that is
//     not inside dir refuses the whole command. This has to come first: a
//     command can name a path inside and a path outside at once, and
//     `cp D:/repo/secrets C:/Windows/out` must not be approved because its
//     source happens to be in an allowed folder.
//
//  2. Does it name an absolute path inside dir? `rm -f "C:/tmp/x.db"` under a
//     rule for `C:/tmp`.
//
//  3. Is the session working inside dir, without climbing out? Commands are
//     written relative to where the session is, and `rm ./tmp/x.db`,
//     `go test ./...` and `cat notes.txt` name no path a rule could match.
//     Without this a folder rule keeps prompting in the workflow it exists to
//     simplify. A command containing `..` is refused, since a relative path
//     that walks upward is not inside anything.
//
// It reads the command text and does not expand it, so `$HOME/x` or a command
// substitution reaching outside is not seen.
func matchPath(dir, command, cwd string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	abs := absolutePaths(command)

	// Reaching out refuses the command outright, whatever else it names.
	inside := false
	for _, p := range abs {
		if !under(dir, p) {
			return false
		}
		inside = true
	}
	if inside {
		return true
	}

	// No absolute paths at all, so it depends where the session is.
	if cwd == "" || !under(dir, cwd) {
		return false
	}
	return !escapesUpward(command)
}

func matchPattern(pattern, command string) bool {
	if !IsGlob(pattern) {
		return strings.HasPrefix(command, pattern)
	}
	if cached, ok := globCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re != nil && re.MatchString(command)
	}
	re, err := globRE(pattern)
	if err != nil {
		// An uncompilable pattern matches nothing rather than everything.
		globCache.Store(pattern, (*regexp.Regexp)(nil))
		return false
	}
	globCache.Store(pattern, re)
	return re.MatchString(command)
}

// matches reports whether this rule answers a request for command. cwd is the
// session's working directory, used only by a folder rule, which without it
// would miss `rm ./tmp/x.db`.
func (r *Rule) matches(command, cwd string) bool {
	// Paths arrive with either slash style, so compare on one.
	cmd := strings.ReplaceAll(command, `\`, "/")
	if r.Kind == KindPath {
		return matchPath(r.Prefix, cmd, cwd)
	}
	return matchPattern(r.Prefix, cmd)
}

// specificity ranks matching rules so a narrow one beats a broad one. Literal
// characters are what make a pattern narrow, so wildcards do not count toward
// it, and a glob never outranks a longer literal prefix by padding itself with
// stars.
//
// A path rule is ranked by the length of its directory, so a rule for one
// worktree beats a rule for the drive it lives on.
func (r *Rule) specificity() int {
	return specificity(r.Prefix)
}

func specificity(pattern string) int {
	return len(pattern) - strings.Count(pattern, "*") - strings.Count(pattern, "?")
}

// MatchRule finds the standing answer for a request, if there is one. The most
// specific matching rule wins, so a narrow rule can override a broad one.
func (s *Store) MatchRule(tool, command, scope string) (*Rule, error) {
	var out *Rule
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT `+ruleColumns+`
			 FROM perm_rule WHERE tool = ? AND (scope = '' OR scope = ?)`, tool, scope)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRule(rows)
			if err != nil {
				return err
			}
			// scope is the worktree the request came from, so it doubles as the
			// session's working directory. A folder rule needs it: commands are
			// written relative to where the session is.
			if !r.matches(command, scope) {
				continue
			}
			if out == nil || r.specificity() > out.specificity() {
				out = r
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if out != nil {
		// Hit counting is best effort: a rule that cannot be counted is still
		// a rule, and failing the request over a statistic would be silly.
		_ = s.guard(func() error {
			_, err := s.db.Exec(`UPDATE perm_rule SET hits = hits + 1 WHERE id = ?`, out.ID)
			return err
		})
		out.Hits++
	}
	return out, nil
}

const ruleColumns = `id, tool, prefix, decision, reason, scope, kind, created_at, hits`

func scanRule(sc interface{ Scan(...any) error }) (*Rule, error) {
	var (
		r       Rule
		created string
	)
	if err := sc.Scan(&r.ID, &r.Tool, &r.Prefix, &r.Decision, &r.Reason, &r.Scope,
		&r.Kind, &created, &r.Hits); err != nil {
		return nil, err
	}
	if r.Kind == "" {
		r.Kind = KindCommand
	}
	var err error
	if r.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &r, nil
}

// AddRule stores a standing answer. Repeating an existing rule updates it
// rather than failing, so clicking "always" twice is harmless.
func (s *Store) AddRule(tool, prefix, decision, reason, scope string) (*Rule, error) {
	return s.addRule(tool, prefix, decision, reason, scope, KindCommand, false)
}

// AddBroadRule is AddRule without the guard against a pattern that matches
// everything. Import uses it, because Claude Code writes a bare tool name to
// mean "always allow this tool" and refusing to carry that across would make
// an import silently incomplete. Nothing in the UI reaches this path.
func (s *Store) AddBroadRule(tool, prefix, decision, reason, scope string) (*Rule, error) {
	return s.addRule(tool, prefix, decision, reason, scope, KindCommand, true)
}

// AddPathRule stores "this tool may work under this directory". See matchPath.
func (s *Store) AddPathRule(tool, dir, decision, reason, scope string) (*Rule, error) {
	return s.addRule(tool, dir, decision, reason, scope, KindPath, false)
}

func (s *Store) addRule(tool, prefix, decision, reason, scope, kind string, allowMatchAll bool) (*Rule, error) {
	if strings.TrimSpace(prefix) == "" {
		return nil, errors.New("a rule needs a pattern to match on")
	}
	if kind == "" {
		kind = KindCommand
	}
	if kind == KindPath {
		// Normalised on the way in, so a rule typed with backslashes matches a
		// command written with forward ones.
		prefix = strings.ReplaceAll(strings.Trim(strings.TrimSpace(prefix), `"'`), `\`, "/")
		// `/` or `C:/` covers the whole machine, which should not happen by
		// pasting a short path.
		if len(strings.Trim(prefix, "/:")) < 2 {
			return nil, errors.New("that path covers the whole machine, so name something narrower")
		}
	}
	if kind == KindCommand && IsGlob(prefix) {
		if _, err := globRE(prefix); err != nil {
			return nil, errors.New("that wildcard pattern is not valid: " + err.Error())
		}
		// A bare `*` answers every request for the tool forever, which should
		// not happen by fat-fingering a pattern.
		if specificity(prefix) == 0 && !allowMatchAll {
			return nil, errors.New("a pattern of only wildcards would match everything, so name something literal")
		}
	}
	r := &Rule{
		ID: newID(), Tool: tool, Prefix: prefix, Decision: decision,
		Reason: reason, Scope: scope, Kind: kind, CreatedAt: now(),
	}
	err := s.guard(func() error {
		// Kind is part of the identity: the same string is both a valid command
		// pattern and a valid directory, and they answer different requests.
		existing, err := s.ruleBy(`tool = ? AND prefix = ? AND scope = ? AND kind = ?`,
			tool, prefix, scope, kind)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing != nil {
			if _, err := s.db.Exec(
				`UPDATE perm_rule SET decision = ?, reason = ? WHERE id = ?`,
				decision, reason, existing.ID); err != nil {
				return err
			}
			existing.Decision, existing.Reason = decision, reason
			r = existing
			return nil
		}
		_, err = s.db.Exec(
			`INSERT INTO perm_rule (id, tool, prefix, decision, reason, scope, kind, created_at, hits)
			 VALUES (?,?,?,?,?,?,?,?,0)`,
			r.ID, r.Tool, r.Prefix, r.Decision, r.Reason, r.Scope, r.Kind, ts(r.CreatedAt))
		return err
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ruleBy(where string, args ...any) (*Rule, error) {
	row := s.db.QueryRow(`SELECT `+ruleColumns+`
		FROM perm_rule WHERE `+where+` LIMIT 1`, args...)
	return scanRule(row)
}

// Rules lists every standing answer, most used first.
func (s *Store) Rules() ([]*Rule, error) {
	var out []*Rule
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT ` + ruleColumns + `
			FROM perm_rule ORDER BY hits DESC, created_at ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRule(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// DeleteRule removes a standing answer, so the next matching request asks again.
func (s *Store) DeleteRule(id string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`DELETE FROM perm_rule WHERE id = ?`, id)
		return err
	})
}
