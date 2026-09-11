package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Resume arguments that never mention the id.
//
// Found on a live database: codex configured as `resume --last`, which runs,
// succeeds, and picks up whichever codex conversation that machine saw last.
// The card's own id is dropped on the floor, so the terminal comes up holding
// somebody else's work and nothing anywhere says a substitution did not
// happen.

func codexLike(resume ...string) *store.Harness {
	return &store.Harness{
		ID: "codex", Label: "codex", Cmd: "codex",
		ResumeArgs: resume,
		PromptArgs: []string{"{prompt}"},
	}
}

// The refusal. Better a launch that does not happen than a card showing a
// conversation it has nothing to do with.
func TestResumeRefusesArgumentsThatDiscardTheID(t *testing.T) {
	_, _, err := runnerArgs(codexLike("resume", "--last"), "01a077cc-43d4-7e50-913a-011da04baf63", "", "")
	if err == nil {
		t.Fatal("resume arguments with no {resume} were accepted, so the wrong conversation opens")
	}
	if !strings.Contains(err.Error(), "{resume}") {
		t.Fatalf("the refusal does not say how to fix it: %v", err)
	}
}

// And the shipped codex spelling is accepted, with the id where codex wants it.
func TestCodexResumesWithTheCardsOwnID(t *testing.T) {
	id := "01a077cc-43d4-7e50-913a-011da04baf63"
	args, _, err := runnerArgs(codexLike("resume", "{resume}"), id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "resume" || args[1] != id {
		t.Fatalf("codex would be started as %q", args)
	}
}

// A launch that is not resuming is untouched by any of this. The check has to
// sit inside the resume branch, or every ordinary start of a runner with no
// resume arguments would start failing.
func TestAPlainStartDoesNotNeedResumeArguments(t *testing.T) {
	if _, _, err := runnerArgs(codexLike(), "", "", ""); err != nil {
		t.Fatalf("starting a runner fresh asked for resume arguments: %v", err)
	}
}
