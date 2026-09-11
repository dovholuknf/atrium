package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Choosing a model at launch, and the two ways it goes wrong quietly.
//
// The whole feature is one flag on a command line, so the risk is not that it
// fails. It is that it succeeds at the wrong thing: a session started on the
// default after being asked for something else, or one that reverts on a
// restart. Neither shows up on screen. What you see is output that reads
// differently, or a bill.

func claudeWithModel() *store.Harness {
	return &store.Harness{
		ID: "claude", Label: "claude code", Cmd: "claude",
		ResumeArgs: []string{"--resume", "{resume}"},
		PromptArgs: []string{"{prompt}"},
		ModelArgs:  []string{"--model", "{model}"},
	}
}

// The ordinary case, and the ORDER, which is the part that is easy to get
// wrong and impossible to see.
func TestAModelBecomesTheArgumentsTheRunnerDeclared(t *testing.T) {
	args, logged, err := runnerArgs(claudeWithModel(), "", "", "claude-fable-5-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "--model" || args[1] != "claude-fable-5-1" {
		t.Fatalf("args came out as %q", args)
	}
	// The command line is what somebody reads back to find out what was
	// started. A model missing from it is the question this feature exists to
	// answer, unanswered.
	if !strings.Contains(logged, "claude-fable-5-1") {
		t.Fatalf("the audit log does not record which model: %q", logged)
	}
}

// THE MODEL GOES BEFORE THE PROMPT. A prompt is a bare positional argument for
// both runners that take one, so a flag after it is read as part of the
// instruction rather than as a flag, and the session starts on the default
// having been told to work on "--model claude-fable-5-1".
func TestTheModelComesBeforeThePrompt(t *testing.T) {
	args, _, err := runnerArgs(claudeWithModel(), "", "fix the parser", "claude-opus-5")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "claude-opus-5", "fix the parser"}
	if len(args) != len(want) {
		t.Fatalf("args came out as %q", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args came out as %q, wanted %q", args, want)
		}
	}
}

// A RESUMED SESSION KEEPS ITS MODEL. Resume arguments REPLACE the base
// arguments, so a model appended before that substitution would be thrown
// away, and every card would revert on the first restart.
func TestAResumedSessionStillGetsItsModel(t *testing.T) {
	args, _, err := runnerArgs(claudeWithModel(), "sess-7", "", "claude-opus-5")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--resume", "sess-7", "--model", "claude-opus-5"}
	if len(args) != len(want) {
		t.Fatalf("args came out as %q, wanted %q", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args came out as %q, wanted %q", args, want)
		}
	}
}

// A RUNNER THAT CANNOT TAKE A MODEL IS REFUSED, NOT QUIETLY IGNORED.
//
// A shell has no model and would try to execute the flag. Starting it on the
// default after being asked for something else is invisible until the output
// is wrong, and by then nobody remembers which session was which.
func TestARunnerWithNoModelArgumentsRefuses(t *testing.T) {
	shell := &store.Harness{ID: "shell", Label: "shell", Cmd: "pwsh"}
	_, _, err := runnerArgs(shell, "", "", "claude-opus-5")
	if err == nil {
		t.Fatal("a shell accepted a model")
	}
	if !strings.Contains(err.Error(), "{model}") {
		t.Fatalf("the refusal does not say how to fix it: %v", err)
	}
}

// Arguments that never mention the model would run, and run on whatever they
// spell. The same refusal `{resume}` gets, for the same reason.
func TestModelArgumentsWithoutThePlaceholderAreRefused(t *testing.T) {
	h := claudeWithModel()
	h.ModelArgs = []string{"--model", "sonnet"}
	_, _, err := runnerArgs(h, "", "", "claude-opus-5")
	if err == nil {
		t.Fatal("model arguments that ignore the model were accepted")
	}
	if !strings.Contains(err.Error(), "{model}") {
		t.Fatalf("the refusal does not say how to fix it: %v", err)
	}
}

// Nothing changes for a launch that names no model, which is every launch that
// existed before this.
func TestNoModelChangesNothing(t *testing.T) {
	h := claudeWithModel()
	h.Args = []string{"--verbose"}
	args, logged, err := runnerArgs(h, "", "", "   ")
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
