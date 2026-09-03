package store

import (
	"strings"
	"testing"
)

// A tenant name becomes part of a database key, so it is folded to one form
// and stripped of anything that would make the split ambiguous.
func TestNormalizeTenant(t *testing.T) {
	cases := map[string]string{
		"SG4":                    "sg4",
		"  laptop  ":             "laptop",
		"my machine":             "mymachine",
		"build-box_2":            "build-box_2",
		"--edge--":               "edge",
		"":                       "",
		"///":                    "",
		"a/b":                    "ab",
		"café":                   "caf",
		strings.Repeat("x", 100): strings.Repeat("x", 32),
	}
	for in, want := range cases {
		if got := NormalizeTenant(in); got != want {
			t.Errorf("NormalizeTenant(%q) = %q, wanted %q", in, got, want)
		}
	}
}

// The separator has to be absent from a tenant name, or splitting a qualified
// name back apart is guesswork.
func TestNormalizeTenantNeverContainsTheSeparator(t *testing.T) {
	for _, in := range []string{"a/b", "//x//", "one/two/three"} {
		if strings.Contains(NormalizeTenant(in), TenantSep) {
			t.Fatalf("NormalizeTenant(%q) kept the separator", in)
		}
	}
}

// An unnamed atrium changes nothing. One machine has nothing to collide with,
// and renaming every card on a board that will never federate would be a
// migration for no benefit.
func TestQualifyIsANoOpWithoutAName(t *testing.T) {
	s := open(t)
	if got := s.Qualify("atrium"); got != "atrium" {
		t.Fatalf("an unnamed atrium qualified a name to %q", got)
	}
}

// Named, every session carries the machine's name.
func TestQualifyPrefixes(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	if got := s.Qualify("atrium"); got != "sg4/atrium" {
		t.Fatalf("qualified to %q, wanted sg4/atrium", got)
	}
}

// A session that reconnects registers again, and it must not accumulate
// prefixes into sg4/sg4/atrium.
func TestQualifyIsIdempotent(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	once := s.Qualify("atrium")
	if twice := s.Qualify(once); twice != once {
		t.Fatalf("qualifying twice gave %q, wanted %q", twice, once)
	}
}

// The name is immutable. Accepting a change would orphan every card already
// registered under the old one.
func TestTenantCannotChange(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	// The same name again is not a change.
	if err := s.SetTenant("SG4"); err != nil {
		t.Fatalf("setting the same name again was refused: %v", err)
	}
	err := s.SetTenant("laptop")
	if err == nil {
		t.Fatal("the tenant name was allowed to change")
	}
	if !strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("the refusal does not say what would be lost: %v", err)
	}
	if got := s.Tenant(); got != "sg4" {
		t.Fatalf("a refused change still altered the name to %q", got)
	}
}

// A name that leaves nothing usable is a mistake worth naming.
func TestSetTenantRefusesNothing(t *testing.T) {
	s := open(t)
	for _, in := range []string{"", "   ", "///", "---"} {
		if err := s.SetTenant(in); err == nil {
			t.Fatalf("SetTenant(%q) was accepted", in)
		}
	}
}

// The whole point: two machines with the same directory layout cannot claim
// each other's cards. Registration is where a wire name becomes a stored one,
// so that is where the prefix has to be applied.
func TestRegisterQualifiesTheWireName(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	task, created, err := s.Register(Observed{
		WireName: "dotfiles", Worktree: "/work/dotfiles", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("the first registration did not create a card")
	}
	if task.WireName != "sg4/dotfiles" {
		t.Fatalf("stored as %q, wanted sg4/dotfiles", task.WireName)
	}
	// The title drops the prefix: on a board with one atrium it is the same on
	// every row and says nothing.
	if task.DisplayTitle() != "dotfiles" {
		t.Fatalf("the title is %q, wanted dotfiles", task.DisplayTitle())
	}
}

// A hook says the name the session knows. It has to find the card anyway.
func TestLookupsQualifyToo(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	made, _, err := s.Register(Observed{
		WireName: "atrium", Worktree: "/work/atrium", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Unqualified, the way a hook sends it.
	got, err := s.GetByWireName("atrium")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != made.ID {
		t.Fatal("an unqualified lookup did not find the card")
	}
	// Qualified, the way the board holds it.
	got, err = s.GetByWireName("sg4/atrium")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != made.ID {
		t.Fatal("a qualified lookup did not find the card")
	}
}

// Two atriums, same session name, two cards. This is the collision the prefix
// exists to prevent, tested by standing in for the second machine.
func TestTwoTenantsDoNotCollide(t *testing.T) {
	s := open(t)
	if err := s.SetTenant("sg4"); err != nil {
		t.Fatal(err)
	}
	mine, _, err := s.Register(Observed{
		WireName: "repo", Worktree: "/work/repo", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The other machine's session, already qualified by its own atrium, which
	// is exactly what would arrive over a forum link.
	theirs, created, err := s.Register(Observed{
		WireName: "vps1/repo", Worktree: "/work/repo", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("the other machine's session was matched onto this machine's card")
	}
	if theirs.ID == mine.ID {
		t.Fatal("two machines share one card, which is the bug this prevents")
	}
}

// Stripping the prefix back off, for showing a name without repeating the
// machine on every row.
func TestLocalName(t *testing.T) {
	cases := map[string]string{
		"sg4/dotfiles": "dotfiles",
		"dotfiles":     "dotfiles",
		"":             "",
		"sg4/a/b":      "a/b",
	}
	for in, want := range cases {
		if got := LocalName(in); got != want {
			t.Errorf("LocalName(%q) = %q, wanted %q", in, got, want)
		}
	}
}
