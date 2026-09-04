package cli

import "testing"

// The two hooks that decide for themselves whether to say anything.
//
// Everything else on the hook path posts unconditionally, which is what makes
// it cheap. These two cannot, and both of them are quiet by default, because a
// badge that appears when nothing is wanted is a badge you learn to ignore and
// that costs the ones that matter.

func TestOnlyNotificationsThatWantAHumanAreReported(t *testing.T) {
	for _, kind := range []string{"idle_prompt", "agent_needs_input", "elicitation_dialog"} {
		if !wantsAHuman(hookInput{NotificationType: kind}) {
			t.Fatalf("%s does want a human and was filtered out", kind)
		}
	}
	for _, kind := range []string{
		// atrium's own gate is what put this on screen. Reporting it back is
		// the card telling itself something it already knows.
		"permission_prompt",
		"agent_completed", "tool_use", "background_task", "auto_compact",
		// An unrecognised kind stays silent, so a new notification type in a
		// future release does not turn into noise on upgrade.
		"something_invented_next_year", "",
	} {
		if wantsAHuman(hookInput{NotificationType: kind}) {
			t.Fatalf("%q was reported as wanting a human", kind)
		}
	}
}

// The field has been spelled two ways. Reading both costs nothing and guessing
// wrong costs the whole hook.
func TestEitherSpellingOfTheNotificationTypeIsRead(t *testing.T) {
	if !wantsAHuman(hookInput{Type: "idle_prompt"}) {
		t.Fatal("the `type` spelling was ignored")
	}
	if !wantsAHuman(hookInput{NotificationType: "idle_prompt"}) {
		t.Fatal("the `notification_type` spelling was ignored")
	}
	// The specific field wins when both are there, because it is the one that
	// names what it holds.
	if wantsAHuman(hookInput{NotificationType: "tool_use", Type: "idle_prompt"}) {
		t.Fatal("a general field overruled the specific one")
	}
}

// Case and whitespace are the runner's, not a decision.
func TestNotificationTypeIsReadLoosely(t *testing.T) {
	if !wantsAHuman(hookInput{NotificationType: "  Idle_Prompt "}) {
		t.Fatal("a notification type was missed over case or spacing")
	}
}

// A failed tool and a finished tool mean the same thing to a badge that only
// says what is running now, so they map onto one activity event. They keep
// separate arguments, because a registered command is matched back to the hook
// that wrote it by that word.
func TestAFailedToolReportsTheSameThingAsAFinishedOne(t *testing.T) {
	if hookEvents["tool-failed"] != hookEvents["tool-end"] {
		t.Fatalf("tool-failed reports %q and tool-end reports %q",
			hookEvents["tool-failed"], hookEvents["tool-end"])
	}
	if hookEvents["tool-failed"] != "tool-end" {
		t.Fatalf("a failed tool reports %q", hookEvents["tool-failed"])
	}
}

// Every argument the hook subcommand accepts has to be a word no other
// argument contains, because that is how a command already in settings.json is
// matched back to the hook it belongs to.
func TestHookArgumentsAreDistinctWords(t *testing.T) {
	for a := range hookEvents {
		for b := range hookEvents {
			if a == b {
				continue
			}
			if len(a) > len(b) && a[:len(b)] == b {
				t.Fatalf("%q starts with %q, so a registered %q command matches both", a, b, a)
			}
		}
	}
}
