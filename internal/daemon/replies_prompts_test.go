package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// transcriptUser appends one user line. extra fields go on the record.
func transcriptUser(t *testing.T, path string, at time.Time, content any, extra map[string]any) {
	t.Helper()
	rec := map[string]any{"type": "user", "timestamp": at.Format(time.RFC3339Nano), "message": map[string]any{"content": content}}
	for k, v := range extra {
		rec[k] = v
	}
	b, _ := json.Marshal(rec)
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

// WHAT WAS SAID TO A CARD, from every source, beside its replies: the operator's
// words wherever they were typed, a peer's message, a slash command. Never a tool
// result, a notification, a meta line or a subagent's prompt.
func TestRepliesCarryThePromptsFromEverySource(t *testing.T) {
	f := newUsageFix(t)
	at := func(s int) time.Time { return f.base.Add(time.Duration(s) * time.Second) }
	human := map[string]any{"origin": map[string]any{"kind": "human"}}
	transcriptUser(t, f.path, at(0), "<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>", nil)
	transcriptUser(t, f.path, at(1), "<local-command-stdout></local-command-stdout>", nil)
	transcriptUser(t, f.path, at(2), "The command below was run directly", map[string]any{"isMeta": true})
	transcriptUser(t, f.path, at(3), "We connected?", human)
	transcriptReply(t, f.path, "m1", at(4), false, textBlock("Yes."))
	transcriptUser(t, f.path, at(5), []any{map[string]any{"type": "tool_result", "content": "ok"}}, nil)
	transcriptUser(t, f.path, at(6), "<task-notification>\n<task-id>x</task-id>", map[string]any{"origin": map[string]any{"kind": "task-notification"}})
	transcriptUser(t, f.path, at(7), "[atrium] review says: ROOM DEPLOY OK", human)
	transcriptUser(t, f.path, at(8), "a subagent's brief", map[string]any{"isSidechain": true})
	transcriptUser(t, f.path, at(9), []any{textBlock("look at this"), map[string]any{"type": "image"},
		textBlock("<system-reminder>injected</system-reminder>")}, human)

	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	v, err := d.repliesFor(f.task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Replies) != 1 || v.Replies[0].Text != "Yes." {
		t.Fatalf("the replies changed: %+v", v.Replies)
	}
	want := []Prompt{
		{At: at(0), Text: "/clear", Kind: PromptCommand},
		{At: at(3), Text: "We connected?", Kind: PromptOperator},
		{At: at(7), Text: "[atrium] review says: ROOM DEPLOY OK", Kind: PromptPeer},
		{At: at(9), Text: "look at this", Kind: PromptOperator},
	}
	if len(v.Prompts) != len(want) {
		t.Fatalf("got %d prompts, want %d: %+v", len(v.Prompts), len(want), v.Prompts)
	}
	for i, w := range want {
		g := v.Prompts[i]
		if g.Text != w.Text || g.Kind != w.Kind || !g.At.Equal(w.At.UTC()) {
			t.Errorf("prompt %d is %+v, want %+v", i, g, w)
		}
	}

	// Bounded by n, the newest kept.
	v, _ = d.repliesFor(f.task.ID, 2)
	if len(v.Prompts) != 2 || v.Prompts[1].Text != "look at this" {
		t.Fatalf("n=2 gave %+v", v.Prompts)
	}
}

// A command with arguments keeps them.
func TestACommandPromptKeepsItsArgs(t *testing.T) {
	p, ok := promptOf("<command-name>/model</command-name>\n<command-args>opus</command-args>")
	if !ok || p.Text != "/model opus" || p.Kind != PromptCommand {
		t.Fatalf("got %+v %v", p, ok)
	}
}
