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
		res, err := tx.Exec(`UPDATE task SET alias = ? WHERE id = ?`, alias, id)
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

// GetByAlias returns the live card holding alias. A leading `@` is accepted.
func (s *Store) GetByAlias(alias string) (*Task, error) {
	alias = NormalizeAlias(alias)
	if alias == "" {
		return nil, sql.ErrNoRows
	}
	var t *Task
	err := s.guard(func() error {
		got, err := s.getBy(`alias = ? AND `+liveClause, alias)
		if err != nil {
			return err
		}
		t = got
		return nil
	})
	return t, err
}

// DefaultAlias is the alias a launched worker starts with: its title's
// prefix, `sa89` from `sa89: typing gate`, when the title has one. A prefix is
// the first word, ending at a colon, and it has to carry a digit, so a title
// that is just a sentence gives none.
func DefaultAlias(title string) string {
	title = strings.TrimSpace(title)
	i := strings.IndexAny(title, ": \t")
	if i <= 0 || title[i] != ':' {
		return ""
	}
	a := NormalizeAlias(title[:i])
	if ValidAlias(a) != nil || !strings.ContainsAny(a, "0123456789") {
		return ""
	}
	return a
}
