package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// transcriptReply appends one transcript line for an assistant message.
func transcriptReply(t *testing.T, path, id string, at time.Time, sidechain bool, blocks ...map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano), "isSidechain": sidechain,
		"message": map[string]any{"id": id, "content": blocks},
	})
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func textBlock(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// r-024: main-chain assistant text only, one reply per message id, oldest first,
// the last n, and each reply cut at replyTextMax.
func TestRepliesAreTheLastMainChainTextOldestFirst(t *testing.T) {
	f := newUsageFix(t)
	path := f.path
	base := f.base
	transcriptReply(t, path, "m1", base, false, textBlock("first answer"))
	transcriptReply(t, path, "m2", base.Add(time.Second), false,
		map[string]any{"type": "thinking", "thinking": "secret"},
		map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"}})
	transcriptReply(t, path, "m3", base.Add(2*time.Second), true, textBlock("a subagent's words"))
	transcriptReply(t, path, "m4", base.Add(3*time.Second), false, textBlock("second, part one"))
	transcriptReply(t, path, "m4", base.Add(3*time.Second), false, textBlock("second, part two"))
	transcriptReply(t, path, "m5", base.Add(4*time.Second), false, textBlock(strings.Repeat("x", replyTextMax+500)))

	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}

	v, err := d.repliesFor(f.task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "transcript" || len(v.Replies) != 3 {
		t.Fatalf("got %+v", v)
	}
	if v.Replies[0].Text != "first answer" {
		t.Fatalf("oldest first, got %q", v.Replies[0].Text)
	}
	if v.Replies[1].Text != "second, part one\n\nsecond, part two" {
		t.Fatalf("one reply per message id, got %q", v.Replies[1].Text)
	}
	if !v.Replies[2].Truncated || len(v.Replies[2].Text) > replyTextMax {
		t.Fatalf("a long reply is not cut: %d bytes, truncated=%v", len(v.Replies[2].Text), v.Replies[2].Truncated)
	}
	for _, r := range v.Replies {
		if strings.Contains(r.Text, "secret") || strings.Contains(r.Text, "subagent") || strings.Contains(r.Text, "Bash") {
			t.Fatalf("thinking, a tool call or a sidechain leaked: %q", r.Text)
		}
	}

	// n=1 is the last one. n over the cap is held to it.
	if v, _ := d.repliesFor(f.task.ID, 1); len(v.Replies) != 1 || !v.Replies[0].Truncated {
		t.Fatalf("n=1 gave %+v", v)
	}
	if v, _ := d.repliesFor(f.task.ID, 99); len(v.Replies) != 3 {
		t.Fatalf("there are 3 text replies, got %d", len(v.Replies))
	}
}

// The conversation the runner last started, not the stored resume id (r-021).
func TestRepliesFollowTheStartedSession(t *testing.T) {
	f := newUsageFix(t)
	dir := filepath.Dir(f.path)
	fresh := filepath.Join(dir, "s2.jsonl")
	f.u.transcript = func(cwd, id string) string {
		switch id {
		case "s1":
			return f.path
		case "s2":
			return fresh
		}
		return ""
	}
	transcriptReply(t, f.path, "old", f.base, false, textBlock("from before the clear"))
	transcriptReply(t, fresh, "new", f.base.Add(time.Second), false, textBlock("after the clear"))
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	d.ctx.started(f.task.ID, "s2")

	v, err := d.repliesFor(f.task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Replies) != 1 || v.Replies[0].Text != "after the clear" {
		t.Fatalf("read the old conversation: %+v", v)
	}
}

// A card with no readable transcript (codex here) answers from the screen, and
// an unknown card is 404 on the route.
func TestRepliesFallBackToTheScreenAndTheRouteAnswers404(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "codexer", Worktree: "/tmp/codexer", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.repliesFor(task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "screen" || v.Replies == nil {
		t.Fatalf("got %+v, want source screen with a (possibly empty) list", v)
	}

	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/tasks/01a0nosuchcard/replies")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d", resp.StatusCode)
	}
	resp, err = http.Get(ts.URL + "/v1/tasks/" + task.ID + "/replies?n=2")
	if err != nil {
		t.Fatal(err)
	}
	var body RepliesView
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body.Source != "screen" {
		t.Fatalf("the route answered %d %+v", resp.StatusCode, body)
	}
}
