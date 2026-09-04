package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Running an action on a card.
//
// Delivery is the existing message queue and the tests are about the two ends
// of it: that the right thing is said, and that the board cannot run an action
// it would not have offered.

func actionCard(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// No terminal, so it queues. This is the ordinary case: a session atrium did
// not launch has no pty for atrium to type into.
func TestAnActionQueuesWhenThereIsNoTerminal(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-1")
	action, err := d.st.SaveCardAction(store.CardAction{
		Label: "run the tests", Prompt: "run the tests please", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := d.runAction(task.ID, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Delivered != "queued" {
		t.Fatalf("it was delivered %q", res.Delivered)
	}

	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d messages were queued", len(pending))
	}
	if pending[0].Text != "run the tests please" {
		t.Fatalf("what was queued is %q", pending[0].Text)
	}
}

// The board decides which actions to show and the daemon decides which it will
// run, and they have to agree. A board that offered the right ones and ran any
// of them would be a filter that only worked when it was looked at.
func TestAnActionThatDoesNotBelongOnACardIsRefused(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-2")
	if err := d.st.SetTags(task.ID, []string{"docs"}); err != nil {
		t.Fatal(err)
	}
	action, err := d.st.SaveCardAction(store.CardAction{
		Label: "go things", Prompt: "go test ./...", Enabled: true, Tag: "go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.runAction(task.ID, action.ID); err == nil {
		t.Fatal("an action was run on a card it is not offered on")
	}
}

func TestADisabledActionCannotBeRun(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-3")
	action, err := d.st.SaveCardAction(store.CardAction{
		Label: "off", Prompt: "x", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runAction(task.ID, action.ID); err == nil {
		t.Fatal("a disabled action was run")
	}
}

// `and exit` with no terminal to send keys to says so rather than silently
// doing half the job. Half the reason the button was pressed is the exit.
func TestExitingWithoutATerminalSaysSo(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-4")
	action, err := d.st.SaveCardAction(store.CardAction{
		Label: "wrap up", Prompt: "commit and stop", Enabled: true, After: store.AfterExit,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := d.runAction(task.ID, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Exiting {
		t.Fatal("it claims to be quitting a session it cannot reach")
	}
	if res.Note == "" {
		t.Fatal("an exit that could not be attempted said nothing about it")
	}
	if !strings.Contains(res.Note, "cannot be made to quit") {
		t.Fatalf("the note is unhelpful: %q", res.Note)
	}
	// The prompt still went. Being unable to quit is no reason to withhold the
	// instruction to wrap up.
	pending, err := d.st.PendingMessages(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatal("the prompt was withheld because the exit could not be attempted")
	}
}

// What was sent, and which action sent it, both land in the history. An action
// is a prompt somebody chose, and six months later "why did it start running
// the tests" is a question with an answer.
func TestRunningAnActionIsRecorded(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-5")
	action, err := d.st.SaveCardAction(store.CardAction{
		Label: "run the tests", Prompt: "run them", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.runAction(task.ID, action.ID); err != nil {
		t.Fatal(err)
	}

	events, err := d.st.Events(task.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == store.EventPrompted && strings.Contains(string(e.Payload), action.ID) {
			if !strings.Contains(string(e.Payload), "run the tests") {
				t.Fatalf("the history does not say which action: %s", e.Payload)
			}
			return
		}
	}
	t.Fatal("running an action left nothing in the card's history")
}

// An action id that names nothing is a refusal, not a panic.
func TestAnUnknownActionIsRefused(t *testing.T) {
	d := testDaemon(t)
	task := actionCard(t, d, "act-6")
	if _, err := d.runAction(task.ID, "no-such-action"); err == nil {
		t.Fatal("an unknown action was accepted")
	}
}
