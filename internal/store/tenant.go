package store

import (
	"errors"
	"fmt"
	"strings"
)

// This atrium's name for itself, and what it does to a wire name.
//
// `wire_name` is UNIQUE on the task table and is the first thing registration
// matches, so a collision does not error: it silently hands one session
// another's card, its history and its permission rules. On one machine that
// cannot happen, because the names come from directories that cannot collide.
// Across machines it happens the first time two containers run the same image
// in the same working directory, which is the normal case rather than an
// unlucky one.
//
// The fix is a prefix nobody else can claim. Asked for once, immutable after,
// and only needed by an atrium that is going to be part of something larger.

// SettingTenant is where the name is kept.
const SettingTenant = "tenant"

// TenantSep divides a tenant from the name a session chose for itself.
//
// A slash, because a wire name already reads like a path. Excluded from a
// tenant name by NormalizeTenant, so splitting on it is unambiguous: a tenant
// called `a/b` would otherwise produce names indistinguishable from tenant `a`
// on a machine with a directory called `b`.
const TenantSep = "/"

// Tenant returns this atrium's name, or empty when it has never been named.
func (s *Store) Tenant() string {
	v, err := s.Setting(SettingTenant)
	if err != nil {
		return ""
	}
	return v
}

// SetTenant names this atrium, once.
//
// A change is refused rather than accepted. Accepting one would orphan every
// card already registered under the old name, and those cards are the history
// the daemon exists to keep.
func (s *Store) SetTenant(name string) error {
	name = NormalizeTenant(name)
	if name == "" {
		return errors.New("a tenant name has to have something in it")
	}
	if have := s.Tenant(); have != "" && have != name {
		return fmt.Errorf("this atrium is already called %q, and that cannot change: "+
			"every card registered under it would be orphaned", have)
	}
	return s.SetSetting(SettingTenant, name)
}

// NormalizeTenant puts a name into the one form everything else relies on:
// lower case, trimmed, and carrying nothing that would make a prefixed wire
// name ambiguous.
func NormalizeTenant(in string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(in)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-_")
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}

// Qualify prefixes a session's own name with this atrium's.
//
// Idempotent, so a session that reconnects and registers again does not become
// `sg4/sg4/atrium`.
//
// An unnamed atrium returns the name untouched. One machine has nothing to
// collide with, and renaming every card on a board that will never federate
// would be a migration for no benefit.
func (s *Store) Qualify(name string) string {
	t := s.Tenant()
	if t == "" || name == "" {
		return name
	}
	if strings.HasPrefix(name, t+TenantSep) {
		return name
	}
	return t + TenantSep + name
}

// LocalName strips the tenant back off, for showing a card without repeating
// the machine's name on every row of a board that only has one.
func LocalName(name string) string {
	if i := strings.Index(name, TenantSep); i >= 0 {
		return name[i+len(TenantSep):]
	}
	return name
}
