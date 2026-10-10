//go:build integration

package store

import (
	"fmt"
	"strings"
	"testing"
)

func TestBranchAliasIsItsLastSegmentAsAHandle(t *testing.T) {
	t.Parallel()
	for branch, want := range map[string]string{
		"claude/u-foo":                      "u-foo",
		"win32crypto-e2e":                   "win32crypto-e2e",
		"claude/Alias_Default":              "alias_default",
		"feature/a b+c":                     "a-b-c",
		"claude/-lead":                      "lead",
		"claude/trail-":                     "trail",
		"claude/" + strings.Repeat("x", 40): strings.Repeat("x", MaxAliasLen),
		// None to give.
		"":              "",
		"main":          "",
		"master":        "",
		"claude/main":   "",
		"HEAD":          "",
		"atrium":        "",
		"claude/atrium": "",
		"claude/":       "",
		"+++":           "",
	} {
		if got := BranchAlias(branch); got != want {
			t.Errorf("BranchAlias(%q) = %q, want %q", branch, got, want)
		}
	}
}

func TestADefaultAliasComesFromTheBranch(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	// No title prefix: the branch.
	a := aliasCard(t, s, "one")
	if got, err := s.GiveDefaultAliasFor(a.ID, "fix the build", "claude/u-foo"); err != nil || got != "u-foo" {
		t.Fatalf("branch default = %q, %v", got, err)
	}
	// Taken in the room: -2, then -3.
	b := aliasCard(t, s, "two")
	if got, _ := s.GiveDefaultAliasFor(b.ID, "", "u-foo"); got != "u-foo-2" {
		t.Fatalf("second = %q", got)
	}
	c := aliasCard(t, s, "three")
	if got, _ := s.GiveDefaultAliasFor(c.ID, "", "other/u-foo"); got != "u-foo-3" {
		t.Fatalf("third = %q", got)
	}
	// The suffix fits inside the length limit.
	long := strings.Repeat("y", MaxAliasLen)
	l1, l2 := aliasCard(t, s, "l1"), aliasCard(t, s, "l2")
	s.GiveDefaultAliasFor(l1.ID, "", long)
	got, _ := s.GiveDefaultAliasFor(l2.ID, "", long)
	if len(got) > MaxAliasLen || !strings.HasSuffix(got, "-2") {
		t.Fatalf("long suffixed = %q", got)
	}

	// The title prefix wins over the branch.
	d := aliasCard(t, s, "four")
	if got, _ := s.GiveDefaultAliasFor(d.ID, "sa89: typing gate", "claude/u-bar"); got != "sa89" {
		t.Fatalf("title did not win: %q", got)
	}
	// A title default somebody holds is not swapped for the branch.
	e := aliasCard(t, s, "five")
	if got, _ := s.GiveDefaultAliasFor(e.ID, "sa89: again", "claude/u-baz"); got != "" {
		t.Fatalf("title clash fell back to the branch: %q", got)
	}

	// An alias a person set always wins.
	f := aliasCard(t, s, "six")
	if err := s.SetAlias(f.ID, "mine"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GiveDefaultAliasFor(f.ID, "", "claude/u-qux"); got != "" {
		t.Fatalf("overrode an alias: %q", got)
	}
	if read, _ := s.Get(f.ID); read.Alias != "mine" {
		t.Fatalf("alias is %q", read.Alias)
	}

	// An ended card no longer holds its alias.
	if err := s.SetStatus(a.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	g := aliasCard(t, s, "seven")
	if got, _ := s.GiveDefaultAliasFor(g.ID, "", "claude/u-foo"); got != "u-foo" {
		t.Fatalf("a done card still held its alias: %q", got)
	}

	// No branch, main, master, atrium: nothing, and no note.
	for i, br := range []string{"", "main", "master", "claude/atrium"} {
		h := aliasCard(t, s, fmt.Sprintf("none-%d", i))
		if got, err := s.GiveDefaultAliasFor(h.ID, "", br); err != nil || got != "" {
			t.Errorf("branch %q gave %q, %v", br, got, err)
		}
		if read, _ := s.Get(h.ID); read.Alias != "" || read.AliasNote != "" {
			t.Errorf("branch %q left alias %q note %q", br, read.Alias, read.AliasNote)
		}
	}

	// A branch equal to another card's handle takes a suffix.
	holder := aliasCard(t, s, "taken-handle")
	i := aliasCard(t, s, "eight")
	got, _ = s.GiveDefaultAliasFor(i.ID, "", "claude/"+LocalName(holder.WireName))
	if got == LocalName(holder.WireName) || got == "" {
		t.Fatalf("a handle was taken as an alias: %q", got)
	}
}
