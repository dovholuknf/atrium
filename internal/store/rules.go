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
// the operator actually saw, and quietly turning that into a wildcard would
// widen it beyond what they agreed to.
type Rule struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	Prefix    string    `json:"prefix"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	Hits      int       `json:"hits"`
}

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
// tools get the containing directory instead, which is what "stop asking about
// edits in this repo" actually means.
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

// specificity ranks matching rules so a narrow one beats a broad one. Literal
// characters are what make a pattern narrow, so wildcards do not count toward
// it, and a glob never outranks a longer literal prefix by padding itself with
// stars.
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
			`SELECT id, tool, prefix, decision, reason, scope, created_at, hits
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
			// Paths arrive with either slash style, so compare on one.
			if !matchPattern(r.Prefix, strings.ReplaceAll(command, `\`, "/")) {
				continue
			}
			if out == nil || specificity(r.Prefix) > specificity(out.Prefix) {
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

func scanRule(sc interface{ Scan(...any) error }) (*Rule, error) {
	var (
		r       Rule
		created string
	)
	if err := sc.Scan(&r.ID, &r.Tool, &r.Prefix, &r.Decision, &r.Reason, &r.Scope,
		&created, &r.Hits); err != nil {
		return nil, err
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
	return s.addRule(tool, prefix, decision, reason, scope, false)
}

// AddBroadRule is AddRule without the guard against a pattern that matches
// everything. Import uses it, because Claude Code writes a bare tool name to
// mean "always allow this tool" and refusing to carry that across would make
// an import quietly incomplete. Nothing in the UI reaches this path.
func (s *Store) AddBroadRule(tool, prefix, decision, reason, scope string) (*Rule, error) {
	return s.addRule(tool, prefix, decision, reason, scope, true)
}

func (s *Store) addRule(tool, prefix, decision, reason, scope string, allowMatchAll bool) (*Rule, error) {
	if strings.TrimSpace(prefix) == "" {
		return nil, errors.New("a rule needs a pattern to match on")
	}
	if IsGlob(prefix) {
		if _, err := globRE(prefix); err != nil {
			return nil, errors.New("that wildcard pattern is not valid: " + err.Error())
		}
		// A bare `*` would answer every request for the tool without ever
		// asking again, which is a decision to make deliberately rather than
		// by fat-fingering a pattern.
		if specificity(prefix) == 0 && !allowMatchAll {
			return nil, errors.New("a pattern of only wildcards would match everything, so name something literal")
		}
	}
	r := &Rule{
		ID: newID(), Tool: tool, Prefix: prefix, Decision: decision,
		Reason: reason, Scope: scope, CreatedAt: now(),
	}
	err := s.guard(func() error {
		existing, err := s.ruleBy(`tool = ? AND prefix = ? AND scope = ?`, tool, prefix, scope)
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
			`INSERT INTO perm_rule (id, tool, prefix, decision, reason, scope, created_at, hits)
			 VALUES (?,?,?,?,?,?,?,0)`,
			r.ID, r.Tool, r.Prefix, r.Decision, r.Reason, r.Scope, ts(r.CreatedAt))
		return err
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ruleBy(where string, args ...any) (*Rule, error) {
	row := s.db.QueryRow(`SELECT id, tool, prefix, decision, reason, scope, created_at, hits
		FROM perm_rule WHERE `+where+` LIMIT 1`, args...)
	return scanRule(row)
}

// Rules lists every standing answer, most used first.
func (s *Store) Rules() ([]*Rule, error) {
	var out []*Rule
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id, tool, prefix, decision, reason, scope, created_at, hits
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
