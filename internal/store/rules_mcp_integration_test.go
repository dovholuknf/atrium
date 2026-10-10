//go:build integration

package store

import "testing"

const (
	mSearch = "mcp__mercurius__discourse_discourse_search"
	mCreate = "mcp__mercurius__discourse_discourse_create_user"
	mGetU   = "mcp__mercurius__discourse_discourse_get_user"
	mOpen   = "mcp__mercurius__mercurius_open_session"
)

// answer is the decision a request for tool gets, or "" when no rule answers.
func answer(t *testing.T, s *Store, tool, scope string) string {
	t.Helper()
	r, err := s.MatchRule(tool, `{"query":"x"}`, scope)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		return ""
	}
	return r.Decision
}

func addMCP(t *testing.T, s *Store, tool, decision, scope string) *Rule {
	t.Helper()
	r, err := s.AddMCPRule(tool, decision, "", scope)
	if err != nil {
		t.Fatalf("AddMCPRule(%s): %v", tool, err)
	}
	return r
}

func TestMCPExactRule(t *testing.T) {
	s := open(t)
	addMCP(t, s, mSearch, "approve", "")
	if got := answer(t, s, mSearch, ""); got != "approve" {
		t.Errorf("the named tool got %q", got)
	}
	if got := answer(t, s, mCreate, ""); got != "" {
		t.Errorf("another tool of the same server got %q", got)
	}
}

func TestMCPServerGlob(t *testing.T) {
	s := open(t)
	addMCP(t, s, "mcp__mercurius__*", "approve", "")
	for _, tool := range []string{mSearch, mOpen} {
		if got := answer(t, s, tool, ""); got != "approve" {
			t.Errorf("%s got %q", tool, got)
		}
	}
	for _, tool := range []string{"mcp__mercurius-staging__x", "mcp__atrium-control__atrium_say", "Bash"} {
		if got := answer(t, s, tool, ""); got != "" {
			t.Errorf("%s should not be covered, got %q", tool, got)
		}
	}
}

func TestMCPPrefixGlob(t *testing.T) {
	s := open(t)
	addMCP(t, s, "mcp__mercurius__discourse_discourse_get_*", "approve", "")
	if got := answer(t, s, mGetU, ""); got != "approve" {
		t.Errorf("a get_ tool got %q", got)
	}
	if got := answer(t, s, mCreate, ""); got != "" {
		t.Errorf("a create_ tool got %q", got)
	}
}

// The five rows of the table in section 5 of the design.
func TestMCPPrecedence(t *testing.T) {
	const (
		server   = "mcp__mercurius__*"
		create   = "mcp__mercurius__discourse_discourse_create_*"
		readers  = "mcp__mercurius__discourse_discourse_*"
		createEq = "mcp__mercurius__discourse_discourse_create_user*"
	)
	cases := []struct {
		name         string
		allow, deny  string
		request, ans string
	}{
		{"exact deny beats a server allow", server, mCreate, mCreate, "block"},
		{"the same pair still allows a read", server, mCreate, mSearch, "approve"},
		{"a deny glob beats a server allow", server, create, mCreate, "block"},
		{"a narrow allow beats a broad deny", readers, server, mSearch, "approve"},
		{"a tie goes to the deny", createEq, mCreate, mCreate, "block"},
	}
	for _, c := range cases {
		for _, denyFirst := range []bool{false, true} {
			s := open(t)
			if denyFirst {
				addMCP(t, s, c.deny, "block", "")
				addMCP(t, s, c.allow, "approve", "")
			} else {
				addMCP(t, s, c.allow, "approve", "")
				addMCP(t, s, c.deny, "block", "")
			}
			if got := answer(t, s, c.request, ""); got != c.ans {
				t.Errorf("%s (deny first %v): got %q, want %q", c.name, denyFirst, got, c.ans)
			}
		}
	}
}

func TestEqualSpecificityBlockWinsWhicheverIsInsertedFirst(t *testing.T) {
	for _, blockFirst := range []bool{false, true} {
		s := open(t)
		add := func(decision string) {
			if _, err := s.AddRule("Bash", "go build*", decision, "", ""); err != nil {
				t.Fatal(err)
			}
		}
		// Same reach, different text: `go build*` and `go build?*` both have eight
		// literal characters once wildcards are not counted.
		addQ := func(decision string) {
			if _, err := s.AddRule("Bash", "go build?*", decision, "", ""); err != nil {
				t.Fatal(err)
			}
		}
		if blockFirst {
			add("block")
			addQ("approve")
		} else {
			addQ("approve")
			add("block")
		}
		r, err := s.MatchRule("Bash", "go build ./...", "")
		if err != nil {
			t.Fatal(err)
		}
		if r == nil || r.Decision != "block" {
			t.Errorf("block first %v: an equal-reach approve beat the block: %+v", blockFirst, r)
		}
	}
}

func TestMCPGlobIsNotACandidateForOtherTools(t *testing.T) {
	s := open(t)
	// A rule for a tool that is not MCP and contains a star must stay literal:
	// the glob path is for mcp__ names only.
	if _, err := s.AddRule("Bash", "go *", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	addMCP(t, s, "mcp__mercurius__*", "block", "")
	r, err := s.MatchRule("Bash", "go build ./...", "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Decision != "approve" || r.Tool != "Bash" {
		t.Fatalf("an MCP glob reached a Bash request: %+v", r)
	}
}

func TestAddMCPRuleValidation(t *testing.T) {
	s := open(t)
	for _, bad := range []string{"mcp__*", "mcp__m*__x", "mcp__", "*", "mcp__m__t(x)", "Bash", "mcp____t", "mcp__m__"} {
		if _, err := s.AddMCPRule(bad, "approve", "", ""); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	r, err := s.AddMCPRule("mcp__mercurius", "approve", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "mcp__mercurius__*" || r.Prefix != "*" {
		t.Errorf("a bare server should be stored as mcp__mercurius__*, got %+v", r)
	}
}

func TestMCPRuleScope(t *testing.T) {
	s := open(t)
	addMCP(t, s, mSearch, "approve", "D:/git/one")
	if got := answer(t, s, mSearch, "D:/git/one"); got != "approve" {
		t.Errorf("the scoped worktree got %q", got)
	}
	if got := answer(t, s, mSearch, "D:/git/two"); got != "" {
		t.Errorf("another worktree got %q", got)
	}
	addMCP(t, s, mOpen, "approve", "")
	if got := answer(t, s, mOpen, "D:/git/two"); got != "approve" {
		t.Errorf("an unscoped rule got %q", got)
	}
}

func TestMCPRuleIsIdempotent(t *testing.T) {
	s := open(t)
	first := addMCP(t, s, "mcp__mercurius__*", "approve", "")
	second := addMCP(t, s, "mcp__mercurius__*", "block", "")
	if first.ID != second.ID {
		t.Fatal("adding the same rule twice made a second row")
	}
	rules, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Decision != "block" {
		t.Fatalf("the second add should have updated the first: %+v", rules)
	}
}

// What the old approve-forever path wrote: an exact MCP tool with a prefix cut
// from the compacted JSON of one call.
func TestOldMCPRuleWithAPrefixStillMatchesAndIsMarked(t *testing.T) {
	s := open(t)
	if _, err := s.AddRule(mSearch, `{"query":"ziti`, "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	r, err := s.MatchRule(mSearch, `{"query":"ziti docs"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Decision != "approve" {
		t.Fatalf("the old rule stopped matching: %+v", r)
	}
	if r.Note != NoteExactInput {
		t.Errorf("the old rule is not marked: %q", r.Note)
	}
	if r, _ := s.MatchRule(mSearch, `{"query":"other"}`, ""); r != nil {
		t.Errorf("a different input matched: %+v", r)
	}
	rules, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Note != NoteExactInput {
		t.Errorf("the list does not carry the note: %+v", rules)
	}
	addMCP(t, s, mSearch, "approve", "")
	rules, _ = s.Rules()
	for _, ru := range rules {
		if ru.Prefix == "*" && ru.Note != "" {
			t.Errorf("a tool rule was marked exact-input: %+v", ru)
		}
	}
}
