package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// pagingBase is the clock of the generated transcripts.
var pagingBase = time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

// withWindows sets the read sizes for one test.
func withWindows(t *testing.T, window, reach int64) {
	t.Helper()
	w, r := repliesWindow, repliesReach
	repliesWindow, repliesReach = window, reach
	t.Cleanup(func() { repliesWindow, repliesReach = w, r })
}

func pagingReplyLine(id string, at time.Time, text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"id": id, "content": []map[string]any{textBlock(text)}},
	})
	return string(b) + "\n"
}

func pagingPromptLine(at time.Time, text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "user", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"content": text},
	})
	return string(b) + "\n"
}

// lockstepTranscript is count turns: prompt i at base+2i, then reply i at base+2i+1,
// the reply written in blocks lines under one message id, pad bytes of noise after each turn.
func lockstepTranscript(t *testing.T, count, blocks, pad int) string {
	t.Helper()
	var sb strings.Builder
	noise := ""
	if pad > 0 {
		noise = `{"type":"system","pad":"` + strings.Repeat("p", pad) + `"}` + "\n"
	}
	for i := 0; i < count; i++ {
		at := pagingBase.Add(time.Duration(2*i) * time.Second)
		sb.WriteString(pagingPromptLine(at, fmt.Sprintf("p%d", i)))
		for b := 0; b < blocks; b++ {
			sb.WriteString(pagingReplyLine(fmt.Sprintf("m%d", i), at.Add(time.Second+time.Duration(b)*time.Millisecond),
				fmt.Sprintf("r%d-%c", i, 'a'+b)))
		}
		sb.WriteString(noise)
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantReplyText(i, blocks int) string {
	parts := make([]string, blocks)
	for b := range parts {
		parts[b] = fmt.Sprintf("r%d-%c", i, 'a'+b)
	}
	return strings.Join(parts, "\n\n")
}

// walk pages a transcript from the first page back to more=false. Each list is
// walked from its own oldest at, since the two lists thin out at different depths.
func walk(t *testing.T, path string, n int, prompted bool) ([]Reply, []Prompt, int) {
	t.Helper()
	var (
		replies []Reply
		prompts []Prompt
		pages   int
	)
	r, p, more, err := readTranscriptPage(path, n)
	for ; ; pages++ {
		if err != nil {
			t.Fatal(err)
		}
		replies = append(append([]Reply{}, r...), replies...)
		prompts = append(append([]Prompt{}, p...), prompts...)
		if !more {
			return replies, prompts, pages + 1
		}
		var before time.Time
		switch {
		case prompted && len(p) > 0:
			before = p[0].At
		case !prompted && len(r) > 0:
			before = r[0].At
		default:
			t.Fatalf("the walk cannot continue: page %d, %d replies, %d prompts", pages, len(r), len(p))
		}
		if pages > 500 {
			t.Fatal("the walk does not end")
		}
		r, p, more, err = readTranscriptBefore(path, n, before)
	}
}

// checkWalk takes both walks of one transcript: replies from the reply walk,
// prompts from the prompt walk.
func checkWalk(t *testing.T, path string, n, count, blocks int) int {
	t.Helper()
	replies, _, pages := walk(t, path, n, false)
	_, prompts, _ := walk(t, path, n, true)
	if len(replies) != count || len(prompts) != count {
		t.Fatalf("got %d replies and %d prompts, want %d each", len(replies), len(prompts), count)
	}
	for i := 0; i < count; i++ {
		if replies[i].Text != wantReplyText(i, blocks) {
			t.Fatalf("reply %d is %q, want %q", i, replies[i].Text, wantReplyText(i, blocks))
		}
		if prompts[i].Text != fmt.Sprintf("p%d", i) {
			t.Fatalf("prompt %d is %q", i, prompts[i].Text)
		}
	}
	return pages
}

func TestRepliesPageCapIs50(t *testing.T) {
	f := newUsageFix(t)
	for i := 0; i < 60; i++ {
		transcriptReply(t, f.path, fmt.Sprintf("m%d", i), f.base.Add(time.Duration(i)*time.Second), false,
			textBlock(fmt.Sprintf("answer %d", i)))
	}
	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}
	for _, ask := range []int{50, 80} {
		v, err := d.repliesFor(f.task.ID, ask)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Replies) != 50 || !v.More {
			t.Fatalf("n=%d gave %d replies, more=%v", ask, len(v.Replies), v.More)
		}
		if v.Replies[0].Text != "answer 10" || v.Replies[49].Text != "answer 59" {
			t.Fatalf("oldest first, got %q to %q", v.Replies[0].Text, v.Replies[49].Text)
		}
	}
	v, _ := d.repliesFor(f.task.ID, 3)
	if !v.More {
		t.Fatal("a first page with older replies left must say more")
	}
}

func TestRepliesFirstPageMoreWhenWindowStartsMidFile(t *testing.T) {
	withWindows(t, 2048, 1<<20)
	path := lockstepTranscript(t, 40, 1, 0)
	_, _, more, err := readTranscriptPage(path, 50)
	if err != nil || !more {
		t.Fatalf("a window that did not reach byte 0 must say more: %v %v", more, err)
	}
	withWindows(t, 1<<20, 1<<20)
	path = lockstepTranscript(t, 4, 1, 0)
	if r, _, more, _ := readTranscriptPage(path, 50); more || len(r) != 4 {
		t.Fatalf("the whole file, got %d replies, more=%v", len(r), more)
	}
}

// Every window size puts a seam somewhere else: no reply lost, none doubled,
// every multi-block reply whole.
func TestRepliesBeforeWalksAcrossSeams(t *testing.T) {
	path := lockstepTranscript(t, 60, 3, 0)
	for window := int64(700); window < 1100; window += 53 {
		withWindows(t, window, 1<<30)
		for _, n := range []int{1, 7, 50} {
			checkWalk(t, path, n, 60, 3)
		}
	}
}

func TestRepliesBeforeLastPageSaysNoMore(t *testing.T) {
	withWindows(t, 1024, 1<<30)
	path := lockstepTranscript(t, 30, 1, 0)
	before := pagingBase.Add(100 * time.Second)
	r, p, more, err := readTranscriptBefore(path, 50, before)
	if err != nil || more || len(r) != 30 || len(p) != 30 {
		t.Fatalf("got %d replies %d prompts more=%v %v", len(r), len(p), more, err)
	}
	r, p, more, _ = readTranscriptBefore(path, 10, before)
	if !more || len(r) != 10 || r[9].Text != "r29-a" {
		t.Fatalf("a short page of a longer file: %d more=%v", len(r), more)
	}
	// strictly older: a before that is a reply's own time excludes it.
	r, _, _, _ = readTranscriptBefore(path, 50, pagingBase.Add(time.Second))
	if len(r) != 0 {
		t.Fatalf("before the first reply there is nothing, got %d", len(r))
	}
}

// 40 turns spread over a file many times the reach: the bound stops a page, says
// more, and the next page continues from the oldest at.
func TestRepliesBeforeStopsAtTheReach(t *testing.T) {
	withWindows(t, 2048, 4096)
	path := lockstepTranscript(t, 40, 1, 900)
	info, _ := os.Stat(path)
	if info.Size() < 8*4096 {
		t.Fatalf("the fixture is %d bytes", info.Size())
	}
	before := pagingBase.Add(time.Hour)
	r, _, more, err := readTranscriptBefore(path, 50, before)
	if err != nil || !more {
		t.Fatalf("a page stopped by the reach must say more: %v %v", more, err)
	}
	if len(r) == 0 || len(r) >= 40 {
		t.Fatalf("the reach should cut the page short, got %d", len(r))
	}
	if pages := checkWalk(t, path, 50, 40, 1); pages < 5 {
		t.Fatalf("expected several bounded pages, got %d", pages)
	}
}

func TestRepliesFirstPageHasNoMoreForTheScreen(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "codexer2", Worktree: "/tmp/codexer2", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.repliesPage(task.ID, 3, pagingBase)
	if err != nil || v.Source != "screen" || v.More {
		t.Fatalf("%+v %v", v, err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"more":false`) {
		t.Fatalf("%s", raw)
	}
}

func TestRepliesRouteParsesBefore(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "codexer3", Worktree: "/tmp/codexer3", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	get := func(before string) int {
		resp, err := http.Get(ts.URL + "/v1/tasks/" + task.ID + "/replies?n=2&before=" + url.QueryEscape(before))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, ok := range []string{"2026-09-30T08:00:00Z", "2026-09-30T08:00:00.123456789Z", "2026-09-30T08:00:00+02:00"} {
		if code := get(ok); code != http.StatusOK {
			t.Fatalf("before=%s answered %d", ok, code)
		}
	}
	for _, bad := range []string{"yesterday", "2026-09-30", "123"} {
		if code := get(bad); code != http.StatusBadRequest {
			t.Fatalf("before=%s answered %d, want 400", bad, code)
		}
	}
}
