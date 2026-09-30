package daemon

import (
	"testing"
	"time"
)

// output_at moves when the transcript gains a reply with text, and only then: a
// tool call with no text, or a subagent's reply, leaves it where it was.
func TestOutputAtMovesOnANewReply(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}

	if d.outputMoved(f.task) || d.outputAtFor(f.task.ID) != "" {
		t.Fatal("a card with no transcript has an output time")
	}

	transcriptReply(t, f.path, "m1", f.base, false, textBlock("looking at the tests"))
	if !d.outputMoved(f.task) {
		t.Fatal("the first reply did not move output_at")
	}
	want := f.base.UTC().Format(time.RFC3339Nano)
	if got := d.outputAtFor(f.task.ID); got != want {
		t.Fatalf("output_at is %q, want %q", got, want)
	}
	if d.outputMoved(f.task) {
		t.Fatal("an unchanged transcript moved output_at")
	}

	transcriptReply(t, f.path, "m2", f.base.Add(time.Second), false,
		map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "go test"}})
	transcriptReply(t, f.path, "m3", f.base.Add(2*time.Second), true, textBlock("a subagent's words"))
	if d.outputMoved(f.task) {
		t.Fatal("a tool call or a subagent reply moved output_at")
	}

	// Mid-turn: a second reply with text before the turn ends.
	transcriptReply(t, f.path, "m4", f.base.Add(3*time.Second), false, textBlock("three tests fail, fixing"))
	if !d.outputMoved(f.task) {
		t.Fatal("a mid-turn reply did not move output_at")
	}
	if got := d.outputAtFor(f.task.ID); got != f.base.Add(3*time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("output_at is %q after the second reply", got)
	}

	d.forgetOutput(map[string]bool{})
	if d.outputAtFor(f.task.ID) != "" {
		t.Fatal("a closed card kept its output time")
	}
}

// A card that is not Claude has no transcript to read.
func TestOutputAtIsClaudeOnly(t *testing.T) {
	f := newUsageFix(t)
	transcriptReply(t, f.path, "m1", f.base, false, textBlock("hello"))
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	task := *f.task
	task.Runner = "codex"
	if d.outputMoved(&task) {
		t.Fatal("a codex card read a transcript")
	}
}
