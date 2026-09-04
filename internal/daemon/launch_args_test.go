package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Handing a runner its first instruction. See docs/intake-design.md: this is
// the whole of intake layer 0 on the daemon side, and everything else in that
// document needs it.

func claudeLike() *store.Harness {
	return &store.Harness{
		ID: "claude", Label: "claude code", Cmd: "claude",
		ResumeArgs: []string{"--resume", "{resume}"},
		PromptArgs: []string{"{prompt}"},
	}
}

// A prompt is one argv element however many words it has. Splitting it would
// hand the runner a dozen arguments it has no flags for.
func TestPromptStaysOneArgument(t *testing.T) {
	text := `investigate #4211 and say "why" it regressed`
	args, _, err := runnerArgs(claudeLike(), "", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 {
		t.Fatalf("a prompt became %d arguments: %q", len(args), args)
	}
	if args[0] != text {
		t.Fatalf("the prompt was rewritten on the way through: %q", args[0])
	}
}

// The audit log records what was started, not what was asked for. A prompt in
// there is a paragraph in a column meant to hold a command line, and for a
// support case it is a customer's words in atrium's own database.
func TestTheLoggedCommandOmitsThePrompt(t *testing.T) {
	_, logged, err := runnerArgs(claudeLike(), "", "a very long instruction about a customer")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged, "customer") {
		t.Fatalf("the prompt reached the audit log: %q", logged)
	}
	if !strings.Contains(logged, "claude") {
		t.Fatalf("the logged command does not name the runner: %q", logged)
	}
}

// Resuming and prompting together is refused rather than guessed at.
func TestPromptAndResumeAreRefusedTogether(t *testing.T) {
	_, _, err := runnerArgs(claudeLike(), "sess-1", "do the thing")
	if err == nil {
		t.Fatal("a resume and a prompt were accepted together")
	}
	if !strings.Contains(err.Error(), "already has its instruction") {
		t.Fatalf("the refusal does not explain itself: %v", err)
	}
}

// A runner with no way to take a prompt says so, instead of starting a session
// that will never read it. A shell is the case: it would try to execute it.
func TestARunnerThatCannotTakeAPromptSaysSo(t *testing.T) {
	shell := &store.Harness{ID: "shell", Label: "shell", Cmd: "pwsh", Args: []string{"-NoLogo"}}
	_, _, err := runnerArgs(shell, "", "run the tests")
	if err == nil {
		t.Fatal("a shell accepted an opening prompt")
	}
	if !strings.Contains(err.Error(), "{prompt}") {
		t.Fatalf("the refusal does not say how to fix it: %v", err)
	}
}

// A prompt is appended to the runner's own arguments rather than replacing
// them. Ollama's model name is in Args and losing it starts the wrong model.
func TestPromptIsAppendedNotSubstituted(t *testing.T) {
	h := &store.Harness{
		ID: "ollama", Label: "ollama", Cmd: "ollama",
		Args: []string{"run", "llama3"}, PromptArgs: []string{"{prompt}"},
	}
	args, _, err := runnerArgs(h, "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[0] != "run" || args[1] != "llama3" || args[2] != "hello" {
		t.Fatalf("args came out as %q", args)
	}
}

// Nothing changes for a launch that supplies no prompt, which is every launch
// that existed before this.
func TestNoPromptChangesNothing(t *testing.T) {
	h := claudeLike()
	h.Args = []string{"--verbose"}
	args, logged, err := runnerArgs(h, "", "   ")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != "--verbose" {
		t.Fatalf("args came out as %q", args)
	}
	if logged != "claude --verbose" {
		t.Fatalf("logged command is %q", logged)
	}
}

// Resuming still works, and its own arguments still substitute.
func TestResumeIsUnchanged(t *testing.T) {
	args, logged, err := runnerArgs(claudeLike(), "sess-9", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "--resume" || args[1] != "sess-9" {
		t.Fatalf("resume args came out as %q", args)
	}
	if !strings.Contains(logged, "sess-9") {
		t.Fatalf("the resumed command line does not record which session: %q", logged)
	}
}
