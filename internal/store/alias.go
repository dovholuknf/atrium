package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A card's alias: the short name the operator mentions it by.
//
// Handles are made up by the board, `dotfiles-41800` or
// `sa84-merger-owns-claude-main-workers-rep`, and nobody types those. An alias
// is what somebody would type: `@dotfiles`, `@sa89`. It is accepted wherever a
// handle is (`atrium_say`, `atrium_peers`, `atrium tell`), and it is unique
// among LIVE cards. A card that has ended keeps its alias as a record of what
// it was called, but no longer holds it, so the next worker launched as `sa89`
// can take it.
//
// Lowercase, a letter or digit first, then letters, digits, `.`, `_` or `-`, at
// most 32. Short enough to type, and nothing that reads as a path, a flag, or
// a card id. A leading `@` is accepted and dropped, since that is how it is
// written in a sentence.

// MaxAliasLen is the longest alias a card may carry.
const MaxAliasLen = 32

var aliasShape = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ErrAliasTaken is an alias another live card already holds. The error's text
// names the holder, which is the whole of what the refused caller needs.
var ErrAliasTaken = errors.New("alias taken")

// AliasTakenError says who holds the alias.
type AliasTakenError struct {
	Alias  string
	Holder *Task
	// ByHandle is whether the clash is with the holder's HANDLE rather than its
	// alias. A handle is matched first everywhere, so an alias equal to one
	// could never be reached.
	ByHandle bool
}

func (e *AliasTakenError) Error() string {
	who := e.Holder.Title
	if h := LocalName(e.Holder.WireName); h != "" {
		who = h + " (" + e.Holder.Title + ")"
	}
	if e.ByHandle {
		return fmt.Sprintf("%q is the handle of %s, so it cannot be another card's alias", e.Alias, who)
	}
	return fmt.Sprintf("@%s is already the alias of %s, card %s", e.Alias, who, e.Holder.ID)
}

func (e *AliasTakenError) Unwrap() error { return ErrAliasTaken }

// NormalizeAlias trims, drops a leading `@` and lowercases. It does not check
// the shape: see ValidAlias.
func NormalizeAlias(a string) string {
	a = strings.TrimSpace(a)
	a = strings.TrimPrefix(a, "@")
	return strings.ToLower(strings.TrimSpace(a))
}

// ValidAlias says what is wrong with an already normalized alias, or nil.
// Empty is valid: it clears the alias.
func ValidAlias(a string) error {
	switch {
	case a == "":
		return nil
	case len(a) > MaxAliasLen:
		return fmt.Errorf("an alias is at most %d characters, and %q is %d", MaxAliasLen, a, len(a))
	case !aliasShape.MatchString(a):
		return fmt.Errorf("%q is not an alias: use lowercase letters, digits, '.', '_' or '-', "+
			"starting with a letter or digit", a)
	}
	return nil
}

// liveClause is what a card holding an alias has to be: not ended, not
// archived.
const liveClause = `status NOT IN ('` + StatusDone + `', '` + StatusDead + `') AND archived_at = ''`

// SetAlias gives card id the alias, or clears it with "". A live card already
// holding it, or whose handle it is, refuses it with an *AliasTakenError.
//
// The check and the write are one transaction, so two cards asking for the
// same alias at once cannot both get it.
func (s *Store) SetAlias(id, alias string) error {
	alias = NormalizeAlias(alias)
	if err := ValidAlias(alias); err != nil {
		return err
	}
	if isReserved(alias) {
		return ErrReservedName
	}
	// A refusal is an answer, not a storage failure, so it is carried out of
	// `guard` in this variable. Returned through it, guard would halt the
	// store over somebody picking a name already in use.
	// Qualified BEFORE the transaction: it reads a setting, and the store has one
	// connection, which the transaction would be holding.
	handle := s.Qualify(alias)
	var taken *AliasTakenError
	err := s.guard(func() error {
		taken = nil
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if alias != "" {
			holder, err := getByOn(tx, `alias = ? AND id != ? AND `+liveClause, alias, id)
			switch {
			case err == nil:
				taken = &AliasTakenError{Alias: alias, Holder: holder}
				return nil
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
			holder, err = getByOn(tx, `wire_name = ? AND id != ? AND `+liveClause, handle, id)
			switch {
			case err == nil:
				taken = &AliasTakenError{Alias: alias, Holder: holder, ByHandle: true}
				return nil
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
		}
		// Whatever was noted about a default that could not be taken is moot
		// once somebody sets or clears it.
		res, err := tx.Exec(`UPDATE task SET alias = ?, alias_note = '' WHERE id = ?`, alias, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return tx.Commit()
	})
	if err != nil {
		return err
	}
	if taken != nil {
		return taken
	}
	return nil
}

// GetByAlias returns the card an alias means. A leading `@` is accepted.
//
// RESOLVING IS NOT HOLDING. A `done` card no longer holds its alias, so a new
// worker can take it, but a worker that reported done still sits at its prompt
// waiting to be sent back or exited, and the operator still calls it by that
// name. So a done card that is not archived answers to it, behind any live card
// with the same alias. A dead or archived card never does.
func (s *Store) GetByAlias(alias string) (*Task, error) {
	alias = NormalizeAlias(alias)
	if alias == "" {
		return nil, sql.ErrNoRows
	}
	var t *Task
	err := s.guard(func() error {
		// One query, live first, then newest. A `SetAlias` keeps two live cards
		// from holding one, but a card that ended can be reopened after another
		// took its alias, and then the one launched most recently is the one
		// being meant. A done fallback lookup would be two round trips for the
		// same ordering.
		got, err := s.getBy(`alias = ? AND status != '`+StatusDead+`' AND archived_at = '' `+
			`ORDER BY (status = '`+StatusDone+`'), created_at DESC`, alias)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
}

// DefaultAlias is the alias a card starts with, read off its title's prefix:
// the first word, ending at a colon. Two shapes give one:
//
//   - A prefix with a digit, `sa89` from `sa89: typing gate`: a worker's
//     number (item 35).
//   - A prefix that is a name, `saorch` from `saorch: merger, owns ...`: what a
//     resident session was called when it was named (backlog-2 item 47). This
//     one needs a space after the colon, so a board-made title such as
//     `main:dotfiles`, a branch and a folder, does not become `@main`. It must
//     also not be a word a title opens with to say what KIND of work it is
//     (`fix: ...`, `docs: ...`), which names nobody.
//
// A title that is just a sentence gives none.
func DefaultAlias(title string) string {
	title = strings.TrimSpace(title)
	i := strings.IndexAny(title, ": \t")
	if i <= 0 || title[i] != ':' {
		return ""
	}
	a := NormalizeAlias(title[:i])
	if ValidAlias(a) != nil {
		return ""
	}
	if strings.ContainsAny(a, "0123456789") {
		return a
	}
	named := len(title) > i+1 && (title[i+1] == ' ' || title[i+1] == '\t')
	if !named || len(a) < 2 || workKinds[a] {
		return ""
	}
	return a
}

// workKinds are title prefixes that say what kind of work a card is, not what
// it is called. `fix: typo` is not a card anybody mentions as `@fix`.
var workKinds = map[string]bool{
	"fix": true, "bug": true, "bugfix": true, "hotfix": true, "feat": true, "feature": true,
	"docs": true, "doc": true, "chore": true, "test": true, "tests": true, "refactor": true,
	"wip": true, "design": true, "spike": true, "proof": true, "review": true, "draft": true,
	"todo": true, "note": true, "notes": true, "idea": true, "perf": true, "build": true, "ci": true,
	"style": true, "revert": true, "release": true, "main": true, "master": true,
}

// GiveDefaultAlias gives card id the alias its title makes, when it has none
// yet. See DefaultAlias. A default another live card holds is not taken, and
// why is written to the card's `alias_note`, so the card can say so rather
// than just wearing nothing. Returns the alias given, or "".
//
// A clash is not an error here: a card starting without its default is fine.
func (s *Store) GiveDefaultAlias(id, title string) (string, error) {
	a := DefaultAlias(title)
	if a == "" {
		return "", nil
	}
	t, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if t.Alias != "" {
		return "", nil
	}
	err = s.SetAlias(id, a)
	var taken *AliasTakenError
	switch {
	case err == nil:
		return a, nil
	case errors.As(err, &taken):
		return "", s.setAliasNote(id, fmt.Sprintf("no alias: its default is taken. %s", taken.Error()))
	default:
		return "", err
	}
}

// setAliasNote records why a card wears no alias. SetAlias clears it.
func (s *Store) setAliasNote(id, note string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET alias_note = ? WHERE id = ?`, note, id)
		return err
	})
}

// settingAliasBackfill marks the one pass over the cards already on the board
// when a resident's name became a default. See BackfillDefaultAliases.
const settingAliasBackfill = "alias_default_backfill"

// BackfillDefaultAliases gives every live card with no alias the default its
// title makes, ONCE, so a card whose alias the operator cleared afterwards is
// not handed it back at the next start. Returns how many took one.
func (s *Store) BackfillDefaultAliases() (int, error) {
	if done, err := s.Setting(settingAliasBackfill); err != nil || done != "" {
		return 0, err
	}
	tasks, err := s.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range tasks {
		if t.Alias != "" || t.Status == StatusDone || t.Status == StatusDead || t.ArchivedAt != nil {
			continue
		}
		a, err := s.GiveDefaultAlias(t.ID, t.DisplayTitle())
		if err != nil {
			return n, err
		}
		if a != "" {
			n++
		}
	}
	return n, s.SetSetting(settingAliasBackfill, ts(now()))
}
