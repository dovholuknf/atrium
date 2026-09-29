package store

import (
	"errors"
	"strings"
	"testing"
)

func aliasCard(t *testing.T, s *Store, wire string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: wire, Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// An alias is set, read back on the card, and resolves to it, with or without
// the `@` it is written with in a sentence.
func TestAnAliasResolvesToItsCard(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "dotfiles-41800")
	if err := s.SetAlias(c.ID, "@Dotfiles"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != "dotfiles" {
		t.Fatalf("alias read back as %q", got.Alias)
	}
	for _, as := range []string{"dotfiles", "@dotfiles", " DotFiles "} {
		found, err := s.GetByAlias(as)
		if err != nil || found.ID != c.ID {
			t.Fatalf("%q did not resolve to the card: %v", as, err)
		}
	}
	if err := s.SetAlias(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByAlias("dotfiles"); err == nil {
		t.Fatal("a cleared alias still resolved")
	}
}

// UNIQUE AMONG LIVE CARDS, and the refusal names the holder.
func TestATakenAliasIsRefusedNamingTheHolder(t *testing.T) {
	s := openTestStore(t)
	a := aliasCard(t, s, "sa89-typing-gate")
	b := aliasCard(t, s, "sa90-other")
	if err := s.SetAlias(a.ID, "sa89"); err != nil {
		t.Fatal(err)
	}
	err := s.SetAlias(b.ID, "sa89")
	var taken *AliasTakenError
	if !errors.As(err, &taken) || !errors.Is(err, ErrAliasTaken) {
		t.Fatalf("a taken alias was not refused as taken: %v", err)
	}
	if taken.Holder.ID != a.ID || !strings.Contains(err.Error(), "sa89-typing-gate") {
		t.Fatalf("the refusal does not name the holder: %v", err)
	}
	// Refusing is an answer, not a storage failure.
	if h, _ := s.Halted(); h {
		t.Fatal("a refused alias halted the store")
	}
	// Setting a card's own alias again is not a clash with itself.
	if err := s.SetAlias(a.ID, "sa89"); err != nil {
		t.Fatalf("re-setting a card's own alias was refused: %v", err)
	}
}

// A card that has ended no longer holds its alias, so the next worker can take
// it, and resolution finds the live one.
func TestAnEndedCardGivesUpItsAlias(t *testing.T) {
	s := openTestStore(t)
	old := aliasCard(t, s, "sa89-first")
	if err := s.SetAlias(old.ID, "sa89"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(old.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetByAlias("sa89"); err == nil {
		t.Fatal("an ended card's alias still resolved")
	}
	fresh := aliasCard(t, s, "sa89-second")
	if err := s.SetAlias(fresh.ID, "sa89"); err != nil {
		t.Fatalf("an ended card's alias could not be taken: %v", err)
	}
	found, err := s.GetByAlias("sa89")
	if err != nil || found.ID != fresh.ID {
		t.Fatalf("resolved to %v, %v", found, err)
	}
}

// An alias equal to another card's handle could never be reached, since a
// handle is matched first. Refused, naming whose handle it is.
func TestAnAliasCannotBeAnotherCardsHandle(t *testing.T) {
	s := openTestStore(t)
	a := aliasCard(t, s, "atrium")
	b := aliasCard(t, s, "sa91-x")
	err := s.SetAlias(b.ID, "atrium")
	var taken *AliasTakenError
	if !errors.As(err, &taken) || !taken.ByHandle || taken.Holder.ID != a.ID {
		t.Fatalf("an alias equal to a live handle was not refused: %v", err)
	}
	// Its own handle is fine.
	if err := s.SetAlias(a.ID, "atrium"); err != nil {
		t.Fatalf("a card could not take its own handle as its alias: %v", err)
	}
}

func TestAnAliasHasAShape(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "shape")
	for _, bad := range []string{"has space", "-flag", "a/b", "@", strings.Repeat("x", MaxAliasLen+1), "ünï"} {
		if bad == "@" {
			// Normalizes to empty, which clears.
			if err := s.SetAlias(c.ID, bad); err != nil {
				t.Fatalf("a bare @ did not clear: %v", err)
			}
			continue
		}
		if err := s.SetAlias(c.ID, bad); err == nil {
			t.Fatalf("%q was accepted as an alias", bad)
		}
	}
	for _, good := range []string{"sa89", "dotfiles", "a.b_c-1", "9lives"} {
		if err := s.SetAlias(c.ID, good); err != nil {
			t.Fatalf("%q was refused: %v", good, err)
		}
	}
}

// A worker's default alias is its title's prefix, when it has one.
func TestDefaultAliasIsTheTitlePrefix(t *testing.T) {
	for title, want := range map[string]string{
		"sa89: typing gate and card aliases": "sa89",
		"SA90: loud":                         "sa90",
		"sa84-merger: owns claude/main":      "sa84-merger",
		"fix the build":                      "",
		"notes: a sentence":                  "",
		"sa89 typing gate":                   "",
		"":                                   "",
		": nothing before":                   "",
		// A resident's name, item 47.
		"saorch: merger, owns claude/main": "saorch",
		"Dotfiles: my shell":               "dotfiles",
		// A branch and a folder, as the board titles a card, is not a name.
		"main:dotfiles": "",
		"saorch:merger": "",
		// A kind of work is not a name either.
		"fix: the build":  "",
		"docs: a section": "",
		"x: one letter":   "",
	} {
		if got := DefaultAlias(title); got != want {
			t.Errorf("DefaultAlias(%q) = %q, want %q", title, got, want)
		}
	}
}

// The migration tolerates its column already being there, which is how a
// database that ran it once and lost the record comes back up.
func TestTheAliasMigrationToleratesItsColumn(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "kept")
	if err := s.SetAlias(c.ID, "kept"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0064_task_alias'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("the migration did not tolerate its column already being there: %v", err)
	}
	if got, _ := s.Get(c.ID); got.Alias != "kept" {
		t.Fatalf("the alias did not survive the migration running again: %q", got.Alias)
	}
}

// A resident's default is taken when free, and when another live card holds
// it the card says who, until somebody sets or clears its alias.
func TestADefaultThatClashesSaysWhy(t *testing.T) {
	s := openTestStore(t)
	holder := aliasCard(t, s, "sa84-merger")
	if err := s.SetAlias(holder.ID, "saorch"); err != nil {
		t.Fatal(err)
	}
	c := aliasCard(t, s, "saorch-2")
	got, err := s.GiveDefaultAlias(c.ID, "saorch: merger, owns claude/main")
	if err != nil || got != "" {
		t.Fatalf("a clashing default was given or failed: %q %v", got, err)
	}
	read, _ := s.Get(c.ID)
	if read.Alias != "" || !strings.Contains(read.AliasNote, "@saorch") ||
		!strings.Contains(read.AliasNote, holder.ID) {
		t.Fatalf("the card does not say why it has no alias: %q", read.AliasNote)
	}
	if err := s.SetAlias(c.ID, "saorch2"); err != nil {
		t.Fatal(err)
	}
	if read, _ := s.Get(c.ID); read.AliasNote != "" {
		t.Fatalf("setting an alias left the note: %q", read.AliasNote)
	}

	free := aliasCard(t, s, "dotfiles-41800")
	if got, err := s.GiveDefaultAlias(free.ID, "dotfiles: my shell"); err != nil || got != "dotfiles" {
		t.Fatalf("a free default was not given: %q %v", got, err)
	}
	// A card that has one keeps it.
	if got, _ := s.GiveDefaultAlias(free.ID, "other: name"); got != "" {
		t.Fatalf("a card with an alias was given another: %q", got)
	}
	if read, _ := s.Get(free.ID); read.Alias != "dotfiles" {
		t.Fatalf("the alias changed: %q", read.Alias)
	}
}

// The cards already on the board take their defaults once, and a card the
// operator cleared afterwards is not handed its default back.
func TestTheDefaultBackfillRunsOnce(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "sa84-merger-owns-claude-main")
	if err := s.SetOverrides(c.ID, map[string]string{"title": "saorch: merger"}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.BackfillDefaultAliases(); err != nil || n != 1 {
		t.Fatalf("backfill gave %d, %v", n, err)
	}
	if read, _ := s.Get(c.ID); read.Alias != "saorch" {
		t.Fatalf("the resident did not take its name: %q", read.Alias)
	}
	if err := s.SetAlias(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if n, err := s.BackfillDefaultAliases(); err != nil || n != 0 {
		t.Fatalf("a second backfill ran: %d, %v", n, err)
	}
	if read, _ := s.Get(c.ID); read.Alias != "" {
		t.Fatalf("a cleared alias came back: %q", read.Alias)
	}
}
