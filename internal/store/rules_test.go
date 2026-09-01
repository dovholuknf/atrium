package store

import "testing"

func TestDefaultPrefixIsToolAware(t *testing.T) {
	cases := []struct{ tool, command, want string }{
		// Commands: the verb, not its arguments.
		{"Bash", "go build -o build.claude/ ./...", "go build"},
		{"Bash", "go test ./internal/store/", "go test"},
		{"Bash", "ls", "ls"},
		{"Bash", "curl -s http://localhost:7778/v1/tasks", "curl"},
		// File tools name a path, so the useful unit is the directory. Two
		// leading words here would pin a rule to one file.
		{"Edit", "D:/git/github/dovholuknf/atrium/internal/api/api.go <- (replace edit)",
			"D:/git/github/dovholuknf/atrium/internal/api/"},
		{"Write", `D:\git\github\dovholuknf\atrium\README.md <- (write)`,
			"D:/git/github/dovholuknf/atrium/"},
	}
	for _, c := range cases {
		if got := DefaultPrefix(c.tool, c.command); got != c.want {
			t.Errorf("DefaultPrefix(%q, %q) = %q, want %q", c.tool, c.command, got, c.want)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, command string
		want             bool
	}{
		// Plain text is a prefix, so it never needs a trailing star.
		{"go build", "go build -o build.claude/ ./...", true},
		{"go build", "go install ./...", false},
		{"D:/git/github/dovholuknf/atrium/", "D:/git/github/dovholuknf/atrium/go.mod <- (edit)", true},
		{"D:/git/github/dovholuknf/atrium/", "C:/Windows/System32/hosts <- (edit)", false},
		// A star matches anything, including path separators.
		{"go * -o build.claude/*", "go build -o build.claude/ ./...", true},
		{"*/internal/*.go <- *", "D:/git/github/dovholuknf/atrium/internal/api/api.go <- (replace edit)", true},
		{"*/internal/*.go <- *", "D:/git/github/dovholuknf/atrium/README.md <- (replace edit)", false},
		{"curl -s http://localhost:*", "curl -s http://localhost:7778/v1/tasks", true},
		{"curl -s http://localhost:*", "curl -s http://example.com/", false},
		// A glob is anchored at both ends, unlike a prefix.
		{"go build *", "go build", false},
		{"go build*", "go build", true},
		// `?` is exactly one character.
		{"port 777?", "port 7778", true},
		{"port 777?", "port 77780", false},
		// Regex metacharacters in a pattern are literal.
		{"cost is $5.00 (net)", "cost is $5.00 (net) today", true},
		{"cost is $5x00 (net)", "cost is $5.00 (net) today", false},
	}
	for _, c := range cases {
		if got := matchPattern(c.pattern, c.command); got != c.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", c.pattern, c.command, got, c.want)
		}
	}
}

// A narrow rule has to beat a broad one, or a repo-wide approve would swallow
// a deliberate block on one directory.
func TestMostSpecificRuleWins(t *testing.T) {
	s := open(t)
	if _, err := s.AddRule("Edit", "D:/git/atrium/", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule("Edit", "D:/git/atrium/secrets/", "block", "not that one", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.MatchRule("Edit", "D:/git/atrium/secrets/keys.txt <- (edit)", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Decision != "block" {
		t.Fatalf("broad rule beat the narrow one: %+v", got)
	}
	got, err = s.MatchRule("Edit", "D:/git/atrium/go.mod <- (edit)", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Decision != "approve" {
		t.Fatalf("repo rule did not apply outside the narrow one: %+v", got)
	}
}

// Wildcards must not win specificity by padding themselves with stars.
func TestGlobDoesNotOutrankLongerLiteral(t *testing.T) {
	s := open(t)
	if _, err := s.AddRule("Bash", "go test ./internal/store/", "block", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule("Bash", "go *********", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.MatchRule("Bash", "go test ./internal/store/ -run TestX", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Decision != "block" {
		t.Fatalf("padded glob outranked the literal rule: %+v", got)
	}
}

func TestAddRuleRejectsMatchEverything(t *testing.T) {
	s := open(t)
	if _, err := s.AddRule("Bash", "*", "approve", "", ""); err == nil {
		t.Fatal("a bare wildcard was accepted, which silently approves every command")
	}
	if _, err := s.AddRule("Bash", "**?*", "approve", "", ""); err == nil {
		t.Fatal("an all-wildcard pattern was accepted")
	}
	if _, err := s.AddRule("Bash", "go *", "approve", "", ""); err != nil {
		t.Fatalf("a pattern with a literal part should be allowed: %v", err)
	}
}

func TestRuleUpsertAndForget(t *testing.T) {
	s := open(t)
	first, err := s.AddRule("Bash", "go build", "approve", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Clicking always twice must not create a duplicate.
	second, err := s.AddRule("Bash", "go build", "block", "changed my mind", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatal("repeating a rule created a duplicate instead of updating it")
	}
	rules, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Decision != "block" {
		t.Fatalf("upsert did not take: %+v", rules)
	}
	if err := s.DeleteRule(first.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.MatchRule("Bash", "go build ./...", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("forgotten rule still matched")
	}
}

func TestRuleCountsHits(t *testing.T) {
	s := open(t)
	if _, err := s.AddRule("Bash", "go build", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.MatchRule("Bash", "go build ./...", ""); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Hits != 3 {
		t.Fatalf("hit count wrong: %+v", rules)
	}
}
