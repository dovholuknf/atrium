package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shape the orchestrator is told to end a turn with, which went unread.
const orchestratorTurn = `Batch two is merged on claude/main.

Open Questions:

---

1. Should sa21 land before sa25?
2. Do you want the tray built now,
   or after the chip has been used for a day?
3. Is 3 seconds the right dwell?
`

func TestOpenQuestionsReadsTheOrchestratorsBlock(t *testing.T) {
	got, block := openQuestions(orchestratorTurn)
	if !block {
		t.Fatal("the block was not found")
	}
	want := []string{
		"Should sa21 land before sa25?",
		"Do you want the tray built now, or after the chip has been used for a day?",
		"Is 3 seconds the right dwell?",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestOpenQuestionsShapes(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		want  []string
		block bool
	}{
		{"no block at all", "done. nothing to ask.", nil, false},
		{"bold heading, parens", "**Open Questions:**\n1) one\n2) two", []string{"one", "two"}, true},
		{"markdown heading", "## Open questions\n\n1. one", []string{"one"}, true},
		{"prose after the list ends it", "Open Questions:\n1. one\n\nI will wait.", []string{"one"}, true},
		{"the last block wins",
			"Open Questions:\n1. old\n\nquoted above. now:\n\nOpen Questions:\n1. new", []string{"new"}, true},
		{"a heading with nothing readable", "Open Questions:\n\nnone that I can phrase yet", nil, true},
		{"crlf line ends", "Open Questions:\r\n\r\n---\r\n\r\n1. one\r\n2. two\r\n", []string{"one", "two"}, true},
		{"the words mid-sentence are not a heading", "I have open questions: see below.\n1. no", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, block := openQuestions(c.text)
			if block != c.block || strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("got %q block=%v, want %q block=%v", got, block, c.want, c.block)
			}
		})
	}
}

func TestOpenQuestionsAreBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("Open Questions:\n")
	for i := 0; i < 30; i++ {
		b.WriteString("1. " + strings.Repeat("é", 400) + "\n")
	}
	got, _ := openQuestions(b.String())
	if len(got) != turnQuestionMax {
		t.Fatalf("kept %d, want %d", len(got), turnQuestionMax)
	}
	for _, q := range got {
		if len(q) > turnQuestionLen {
			t.Fatalf("a question of %d bytes", len(q))
		}
		if !strings.HasSuffix(q, "é") {
			t.Fatal("cut in the middle of a character")
		}
	}
}

// A transcript line per entry, the way Claude Code writes them. The last
// assistant entry with text is the turn's last message, and a tool result
// after it does not hide it.
func TestLastAssistantTextReadsTheTranscriptTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	entry := func(role string, content any) string {
		raw, _ := json.Marshal(map[string]any{"type": role, "message": map[string]any{"role": role, "content": content}})
		return string(raw)
	}
	lines := []string{
		strings.Repeat("x", transcriptTail), // pushes the start off the tail
		entry("user", "do it"),
		entry("assistant", []map[string]any{{"type": "text", "text": "old turn"}}),
		entry("assistant", []map[string]any{
			{"type": "thinking", "thinking": "hmm"},
			{"type": "text", "text": orchestratorTurn},
		}),
		entry("assistant", []map[string]any{{"type": "tool_use", "name": "Bash"}}),
		`not json at all`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := lastAssistantText(path)
	if !strings.Contains(got, "Should sa21 land") {
		t.Fatalf("got %q", got)
	}
	if lastAssistantText(filepath.Join(dir, "missing.jsonl")) != "" {
		t.Fatal("a missing transcript produced text")
	}
}

// The payload's own message wins over the transcript, and neither leaves the
// hook: only the questions are posted.
func TestTheStopHookPostsOnlyTheQuestions(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	payload, _ := json.Marshal(map[string]any{
		"cwd": "/w", "session_id": "s1", "hook_event_name": "Stop",
		"last_assistant_message": orchestratorTurn,
	})
	restore := withStdin(t, string(payload))
	defer restore()
	t.Setenv("ATRIUM_PERM_GATE", "")

	if got := turnEnded(srv.URL, "end", "orchestrator"); got != keepGoing {
		t.Fatalf("the hook printed %q", got)
	}
	qs, _ := body["questions"].([]any)
	if len(qs) != 3 || body["questions_block"] != true || body["questions_known"] != true {
		t.Fatalf("posted %v", body)
	}
	for k, v := range body {
		if s, ok := v.(string); ok && strings.Contains(s, "Batch two") {
			t.Fatalf("the message itself was posted under %q", k)
		}
	}
}

// No message and no transcript: the questions are unknown, and the turn still
// ends normally.
func TestTheStopHookSaysUnknownWithNoText(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	restore := withStdin(t, `{"cwd":"/w","hook_event_name":"Stop","transcript_path":"/nope/t.jsonl"}`)
	defer restore()
	t.Setenv("ATRIUM_PERM_GATE", "")

	if got := turnEnded(srv.URL, "end", "orchestrator"); got != keepGoing {
		t.Fatalf("the hook printed %q", got)
	}
	if body["questions_known"] != false {
		t.Fatalf("posted %v", body)
	}
}
