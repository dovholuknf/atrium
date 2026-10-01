package daemon

import (
	"bytes"
	"encoding/json"
	"os"
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

// A line still being written is not skipped: the read stops before it, and the
// next read takes it whole.
func TestOutputAtWaitsForAWholeLine(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	transcriptReply(t, f.path, "m1", f.base, false, textBlock("first"))
	if !d.outputMoved(f.task) {
		t.Fatal("the first reply did not move output_at")
	}
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": f.base.Add(time.Minute).Format(time.RFC3339Nano),
		"message": map[string]any{"id": "m2", "content": []any{textBlock("second")}},
	})
	half := len(b) / 2
	appendRaw(t, f.path, b[:half])
	if d.outputMoved(f.task) {
		t.Fatal("half a line moved output_at")
	}
	appendRaw(t, f.path, append(b[half:], '\n'))
	if !d.outputMoved(f.task) {
		t.Fatal("the line, once whole, was never read")
	}
	if got := d.outputAtFor(f.task.ID); got != f.base.Add(time.Minute).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("output_at is %q", got)
	}
}

func appendRaw(t *testing.T, path string, b []byte) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.Write(b); err != nil {
		t.Fatal(err)
	}
}

// A LINE OVER THE SCANNER'S LIMIT, a pasted image stored base64, does not freeze
// output_at: the read is capped at the tail and moves past it.
func TestOutputAtGetsPastAHugeLine(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	transcriptReply(t, f.path, "m1", f.base, false, textBlock("before the paste"))
	if !d.outputMoved(f.task) {
		t.Fatal("the first reply did not move output_at")
	}
	huge := append([]byte(`{"type":"user","message":{"content":"`), bytes.Repeat([]byte("A"), 9<<20)...)
	appendRaw(t, f.path, append(huge, []byte("\"}}\n")...))
	if d.outputMoved(f.task) {
		t.Fatal("a user line moved output_at")
	}
	transcriptReply(t, f.path, "m2", f.base.Add(time.Minute), false, textBlock("after the paste"))
	if !d.outputMoved(f.task) {
		t.Fatal("the reply after a 9 MB line never moved output_at")
	}
	if got := d.outputAtFor(f.task.ID); got != f.base.Add(time.Minute).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("output_at is %q", got)
	}
	d.output.mu.Lock()
	off := d.output.seen[f.task.ID].offset
	d.output.mu.Unlock()
	if info, _ := os.Stat(f.path); off != info.Size() {
		t.Fatalf("the offset is %d of %d", off, info.Size())
	}
}

// A check armed before Close never runs.
func TestCloseStopsAPendingOutputCheck(t *testing.T) {
	f := newUsageFix(t)
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	transcriptReply(t, f.path, "m1", f.base, false, textBlock("hello"))
	d.outputSoon(f.task.ID)
	d.stopOutput()
	time.Sleep(outputCheckDelay + 200*time.Millisecond)
	if d.outputAtFor(f.task.ID) != "" {
		t.Fatal("a check armed before Close read the transcript")
	}
	d.outputSoon(f.task.ID)
	if len(d.output.pending) != 0 {
		t.Fatal("a check was armed after Close")
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
