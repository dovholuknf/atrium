package store

import "testing"

// The case that drove this. Expressing "let it touch anything in this folder"
// as a command glob means writing a pattern that also accounts for the quoting
// around the path, and `rm -f "C:/x/*"` fails against `rm -f "C:/x/y.db"` over
// the closing quote alone. Silently. A path rule says what was meant.
func TestPathRuleMatchesAQuotedPathMidCommand(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "C:/Users/claude/AppData/Local/Temp/claude",
		"approve", "my own scratch space", ""); err != nil {
		t.Fatalf("add: %v", err)
	}

	cmds := []string{
		`rm -f "C:/Users/claude/AppData/Local/Temp/claude/x/smoke.db"`,
		`rm -f C:/Users/claude/AppData/Local/Temp/claude/x/smoke.db`,
		`cat "C:\Users\claude\AppData\Local\Temp\claude\x\notes.txt"`,
		`ls C:/Users/claude/AppData/Local/Temp/claude`,
	}
	for _, cmd := range cmds {
		r, err := s.MatchRule("Bash", cmd, "")
		if err != nil {
			t.Fatal(err)
		}
		if r == nil {
			t.Errorf("no rule matched:\n  %s", cmd)
		}
	}
}

// The trailing separator is what stops a sibling directory with the same start
// from being swept in.
func TestPathRuleDoesNotMatchASiblingPrefix(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "D:/tmp", "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `rm -rf D:/tmpfiles/everything`, "")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatalf("a rule for D:/tmp also covered D:/tmpfiles")
	}
}

// Windows paths are case insensitive, and a rule that failed over a capital
// letter would look broken for no visible reason.
func TestPathRuleIgnoresCase(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "D:/Git/Atrium", "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `go test d:/git/atrium/internal/...`, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("a path rule failed on letter case alone")
	}
}

// A path short enough to be a drive or a filesystem root covers the whole
// machine. That is a decision to make on purpose, not by pasting.
func TestPathRuleRefusesTheWholeMachine(t *testing.T) {
	s := open(t)
	for _, p := range []string{"/", "C:/", `C:\`, "  /  "} {
		if _, err := s.AddPathRule("Bash", p, "approve", "", ""); err == nil {
			t.Errorf("a rule for %q was accepted", p)
		}
	}
}

// A narrow rule has to beat a broad one, or turning on a folder would override
// a specific block inside it.
func TestNarrowPathRuleBeatsABroadOne(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "D:/git", "approve", "everything I work on", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPathRule("Bash", "D:/git/production", "block", "not from here", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `rm -rf D:/git/production/data`, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.Decision != "block" {
		t.Fatalf("the broad rule won: %+v", r)
	}
}

// The two kinds are different rules even with the same text, because they
// answer different requests.
func TestPathAndCommandRulesCoexist(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "D:/git/atrium", "approve", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRule("Bash", "D:/git/atrium", "block", "", ""); err != nil {
		t.Fatal(err)
	}
	rules, err := s.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("%d rules, wanted 2: one kind overwrote the other", len(rules))
	}
}

// Existing rules predate the column, so they have to keep behaving as the
// command patterns they were.
func TestRulesWithoutAKindStayCommandRules(t *testing.T) {
	s := open(t)
	r, err := s.AddRule("Bash", "go build", "approve", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindCommand {
		t.Fatalf("kind is %q, wanted command", r.Kind)
	}
	got, err := s.MatchRule("Bash", "go build ./...", "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("a plain prefix rule stopped matching")
	}
}

// A path rule has to cover the file tools too, since their command IS a path.
// That is most of the value: "stop asking about edits in this repo".
func TestPathRuleCoversFileTools(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Edit", "D:/git/atrium", "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Edit", `D:/git/atrium/internal/store/rules.go <- (replace edit)`, "")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("a path rule did not cover an edit inside it")
	}
}

// The case that decides whether folder rules are useful at all.
//
// Commands are written relative to where the session is: `rm ./tmp/x.db`,
// `go test ./...`, `cat notes.txt`. None of those contain a path a rule could
// match on its own, so without resolving against the session's working
// directory a folder rule would keep prompting in exactly the workflow it
// exists to simplify.
func TestPathRuleCoversRelativeCommandsFromInside(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		`rm ./tmp/x.db`,
		`go test ./...`,
		`cat notes.txt`,
		`go build -o build.claude/ ./...`,
	} {
		r, err := s.MatchRule("Bash", cmd, repo)
		if err != nil {
			t.Fatal(err)
		}
		if r == nil {
			t.Errorf("no match for a command run inside the folder:\n  %s", cmd)
		}
	}
}

// Working inside an allowed folder does not license reaching out of it.
func TestPathRuleDoesNotCoverReachingOutside(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		`rm -rf C:/Windows/System32`,
		`cat /etc/passwd`,
		`cp secrets.txt D:/elsewhere/`,
	} {
		r, err := s.MatchRule("Bash", cmd, repo)
		if err != nil {
			t.Fatal(err)
		}
		if r != nil {
			t.Errorf("a folder rule approved something outside it:\n  %s", cmd)
		}
	}
}

// A relative path that walks upward is not inside anything.
func TestPathRuleRefusesUpwardEscapes(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		`rm -rf ../../important`,
		`cat ..\..\secrets.txt`,
		`ls ..`,
	} {
		r, err := s.MatchRule("Bash", cmd, repo)
		if err != nil {
			t.Fatal(err)
		}
		if r != nil {
			t.Errorf("a folder rule covered a command that climbed out of it:\n  %s", cmd)
		}
	}
}

// A session working somewhere else gets nothing from the rule, even for a
// command that names no path at all.
func TestPathRuleIgnoresSessionsElsewhere(t *testing.T) {
	s := open(t)
	if _, err := s.AddPathRule("Bash", "D:/git/atrium", "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", "go test ./...", "D:/git/something-else")
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("a folder rule answered for a session working outside it")
	}
}

// A command can name a path inside the folder AND one outside it. Matching on
// the first one it finds inside would approve exfiltrating out of an allowed
// folder, so reaching out has to be checked before reaching in.
func TestPathRuleRefusesAMixOfInsideAndOutside(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		`cp D:/git/atrium/secrets.txt C:/Windows/out.txt`,
		`cat D:/git/atrium/notes.md /etc/passwd`,
		`mv "D:/git/atrium/x" "D:/elsewhere/x"`,
	} {
		for _, cwd := range []string{"", repo} {
			r, err := s.MatchRule("Bash", cmd, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if r != nil {
				t.Errorf("approved a command reaching out of the folder (cwd=%q):\n  %s", cwd, cmd)
			}
		}
	}
}

// Splitting on whitespace breaks `"C:/Program Files/x"` into two tokens that
// neither look absolute, so a command reaching outside reads as if it named no
// absolute path at all and the working-directory rule approves it.
func TestPathRuleSeesQuotedPathsWithSpaces(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	outside := []string{
		`cat "C:/Program Files/secret.txt"`,
		`cp "C:/Program Files/a b/x" .`,
		`type 'C:/Users/someone else/notes.txt'`,
	}
	for _, cmd := range outside {
		r, err := s.MatchRule("Bash", cmd, repo)
		if err != nil {
			t.Fatal(err)
		}
		if r != nil {
			t.Errorf("a quoted path outside the folder was approved:\n  %s", cmd)
		}
	}

	// The same shape inside the folder still matches.
	r, err := s.MatchRule("Bash", `cat "D:/git/atrium/a folder/notes.txt"`, repo)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("a quoted path inside the folder stopped matching")
	}
}

// A path hidden behind a flag is still a path.
func TestPathRuleSeesPathsAttachedToFlags(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `go build --output=C:/Windows/evil.exe ./...`, repo)
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("a path behind a flag escaped the folder check")
	}
}

// A literal cd out of the folder is visible in the command text, so it is
// caught even though nothing here simulates a shell.
func TestPathRuleCatchesALiteralCdOut(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `cd /elsewhere && rm -rf x`, repo)
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Fatal("a command naming a directory outside the folder was approved")
	}
}

// A url is not a path on this machine, so it must not be read as one reaching
// outside the folder either.
func TestPathRuleIgnoresURLs(t *testing.T) {
	s := open(t)
	const repo = "D:/git/atrium"
	if _, err := s.AddPathRule("Bash", repo, "approve", "", ""); err != nil {
		t.Fatal(err)
	}

	r, err := s.MatchRule("Bash", `curl https://example.com/x -o ./out.txt`, repo)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("a url was mistaken for a path outside the folder")
	}
}
