//go:build integration

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

func writePaging(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
	return writePaging(t, sb.String())
}

func wantReplyText(i, blocks int) string {
	parts := make([]string, blocks)
	for b := range parts {
		parts[b] = fmt.Sprintf("r%d-%c", i, 'a'+b)
	}
	return strings.Join(parts, "\n\n")
}

// walk pages a transcript by next_before alone, from the first page to more=false,
// and returns the pages' union oldest first. A reply or prompt seen twice fails.
func walk(t *testing.T, path string, n int) ([]Reply, []Prompt, int) {
	t.Helper()
	var (
		replies []Reply
		prompts []Prompt
		seenR   = map[string]bool{}
		seenP   = map[string]bool{}
		pages   int
	)
	pg, err := readTranscriptPage(path, n)
	for ; ; pages++ {
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.replies) > n+3 || len(pg.prompts) > n+3 {
			t.Fatalf("page %d holds %d replies and %d prompts for n=%d", pages, len(pg.replies), len(pg.prompts), n)
		}
		for _, r := range pg.replies {
			if seenR[r.Text] {
				t.Fatalf("reply %q came twice", r.Text)
			}
			seenR[r.Text] = true
		}
		for _, p := range pg.prompts {
			if seenP[p.Text] {
				t.Fatalf("prompt %q came twice", p.Text)
			}
			seenP[p.Text] = true
		}
		replies = append(append([]Reply{}, pg.replies...), replies...)
		prompts = append(append([]Prompt{}, pg.prompts...), prompts...)
		if !pg.more {
			return replies, prompts, pages + 1
		}
		if pg.next.IsZero() {
			t.Fatalf("page %d says more with no next_before", pages)
		}
		if pages > 2000 {
			t.Fatal("the walk does not end")
		}
		before := pg.next
		pg, err = readTranscriptBefore(path, n, before)
		if err == nil && pg.more && !pg.next.Before(before) {
			t.Fatalf("page %d: next_before %v is not older than before %v", pages+1, pg.next, before)
		}
	}
}

func checkWalk(t *testing.T, path string, n, count, blocks int) int {
	t.Helper()
	replies, prompts, pages := walk(t, path, n)
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
		if len(v.Replies) != 50 || !v.More || v.NextBefore == "" {
			t.Fatalf("n=%d gave %d replies, more=%v next=%q", ask, len(v.Replies), v.More, v.NextBefore)
		}
		if v.Replies[0].Text != "answer 10" || v.Replies[49].Text != "answer 59" {
			t.Fatalf("oldest first, got %q to %q", v.Replies[0].Text, v.Replies[49].Text)
		}
		if next, err := time.Parse(time.RFC3339Nano, v.NextBefore); err != nil || !next.Equal(v.Replies[0].At) {
			t.Fatalf("next_before %q should be the oldest kept reply %v", v.NextBefore, v.Replies[0].At)
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
	pg, err := readTranscriptPage(path, 50)
	if err != nil || !pg.more || pg.next.IsZero() {
		t.Fatalf("a window that did not reach byte 0 must say more: %+v %v", pg, err)
	}
	withWindows(t, 1<<20, 1<<20)
	path = lockstepTranscript(t, 4, 1, 0)
	if pg, _ := readTranscriptPage(path, 50); pg.more || len(pg.replies) != 4 || !pg.next.IsZero() {
		t.Fatalf("the whole file, got %+v", pg)
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
	pg, err := readTranscriptBefore(path, 50, before)
	if err != nil || pg.more || len(pg.replies) != 30 || len(pg.prompts) != 30 || !pg.next.IsZero() {
		t.Fatalf("got %d replies %d prompts more=%v %v", len(pg.replies), len(pg.prompts), pg.more, err)
	}
	pg, _ = readTranscriptBefore(path, 10, before)
	if !pg.more || len(pg.replies) != 10 || pg.replies[9].Text != "r29-a" {
		t.Fatalf("a short page of a longer file: %d more=%v", len(pg.replies), pg.more)
	}
	// strictly older: a before that is a reply's own time excludes it.
	pg, _ = readTranscriptBefore(path, 50, pagingBase.Add(time.Second))
	if len(pg.replies) != 0 {
		t.Fatalf("before the first reply there is nothing, got %d", len(pg.replies))
	}
}

// 40 turns spread over a file many times the reach: the bound stops a page, says
// more, and the next page continues from next_before.
func TestRepliesBeforeStopsAtTheReach(t *testing.T) {
	withWindows(t, 2048, 4096)
	path := lockstepTranscript(t, 40, 1, 900)
	info, _ := os.Stat(path)
	if info.Size() < 8*4096 {
		t.Fatalf("the fixture is %d bytes", info.Size())
	}
	pg, err := readTranscriptBefore(path, 50, pagingBase.Add(time.Hour))
	if err != nil || !pg.more || pg.next.IsZero() {
		t.Fatalf("a page stopped by the reach must say more with a next_before: %+v %v", pg, err)
	}
	if len(pg.replies) == 0 || len(pg.replies) >= 40 {
		t.Fatalf("the reach should cut the page short, got %d", len(pg.replies))
	}
	if pages := checkWalk(t, path, 50, 40, 1); pages < 5 {
		t.Fatalf("expected several bounded pages, got %d", pages)
	}
}

// Prompts dense in one stretch, replies dense in another: the lists thin out at
// different depths, and walking by next_before alone still loses and repeats nothing.
func TestRepliesBeforeWalksUnevenLists(t *testing.T) {
	var sb strings.Builder
	at := func(i int) time.Time { return pagingBase.Add(time.Duration(i) * time.Second) }
	i := 0
	// oldest: 70 prompts and 3 replies
	for k := 0; k < 70; k++ {
		sb.WriteString(pagingPromptLine(at(i), fmt.Sprintf("early-p%d", k)))
		i++
		if k%25 == 0 {
			sb.WriteString(pagingReplyLine(fmt.Sprintf("e%d", k), at(i), fmt.Sprintf("early-r%d", k)))
			i++
		}
	}
	// then 70 replies and 3 prompts
	for k := 0; k < 70; k++ {
		sb.WriteString(pagingReplyLine(fmt.Sprintf("l%d", k), at(i), fmt.Sprintf("late-r%d", k)))
		i++
		if k%25 == 0 {
			sb.WriteString(pagingPromptLine(at(i), fmt.Sprintf("late-p%d", k)))
			i++
		}
	}
	path := writePaging(t, sb.String())
	for _, window := range []int64{600, 1500, 4000, 1 << 20} {
		withWindows(t, window, 1<<30)
		for _, n := range []int{1, 5, 20, 50} {
			replies, prompts, _ := walk(t, path, n)
			if len(replies) != 73 || len(prompts) != 73 {
				t.Fatalf("window %d n=%d: %d replies and %d prompts, want 73 each", window, n, len(replies), len(prompts))
			}
		}
	}
	// and under a reach that stops pages short
	withWindows(t, 600, 1500)
	for _, n := range []int{3, 50} {
		replies, prompts, _ := walk(t, path, n)
		if len(replies) != 73 || len(prompts) != 73 {
			t.Fatalf("reach: n=%d: %d replies and %d prompts, want 73 each", n, len(replies), len(prompts))
		}
	}
}

// A stretch of the file with no record in it, longer than the reach: the page reads
// on past the reach to stand on a record, and the cursor advances. Past the hard cap
// it answers no more, never a cursor that stays where it was.
func TestRepliesBeforeReadsPastTheReachToFindARecord(t *testing.T) {
	var sb strings.Builder
	at := func(i int) time.Time { return pagingBase.Add(time.Duration(i) * time.Second) }
	for k := 0; k < 5; k++ {
		sb.WriteString(pagingPromptLine(at(2*k), fmt.Sprintf("old-p%d", k)))
		sb.WriteString(pagingReplyLine(fmt.Sprintf("o%d", k), at(2*k+1), fmt.Sprintf("old-r%d", k)))
	}
	for k := 0; sb.Len() < 320<<10; k++ {
		sb.WriteString(`{"type":"summary","summary":"` + strings.Repeat("s", 280) + `"}` + "\n")
	}
	for k := 0; k < 5; k++ {
		sb.WriteString(pagingPromptLine(at(100+2*k), fmt.Sprintf("new-p%d", k)))
		sb.WriteString(pagingReplyLine(fmt.Sprintf("n%d", k), at(100+2*k+1), fmt.Sprintf("new-r%d", k)))
	}
	path := writePaging(t, sb.String())
	between := at(50)
	withWindows(t, 4096, 4096)

	pg, err := readTranscriptBefore(path, 50, between)
	if err != nil || len(pg.replies) != 5 || len(pg.prompts) != 5 || pg.more {
		t.Fatalf("got %d replies %d prompts more=%v %v", len(pg.replies), len(pg.prompts), pg.more, err)
	}
	// the whole file by next_before alone: each cursor strictly older than the last.
	if replies, prompts, _ := walk(t, path, 3); len(replies) != 10 || len(prompts) != 10 {
		t.Fatalf("walk: %d replies and %d prompts, want 10 each", len(replies), len(prompts))
	}

	// a cap shorter than the gap: no record older than before is within it.
	c := repliesCap
	repliesCap = 100 << 10
	t.Cleanup(func() { repliesCap = c })
	pg, err = readTranscriptBefore(path, 50, between)
	if err != nil || pg.more || !pg.next.IsZero() || len(pg.replies) != 0 || len(pg.prompts) != 0 {
		t.Fatalf("past the cap the answer is no more: %+v %v", pg, err)
	}
}

// The first page's 50-cap keeps a tie together: the entry cut off at the floor's
// time would be on neither page.
func TestRepliesFirstPageCapKeepsTies(t *testing.T) {
	var sb strings.Builder
	for k := 0; k < 56; k++ {
		sb.WriteString(pagingReplyLine(fmt.Sprintf("m%d", k), pagingBase.Add(time.Duration(k/2)*time.Second),
			fmt.Sprintf("r%d", k)))
	}
	path := writePaging(t, sb.String())
	pg, err := readTranscriptPage(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	// 56 replies, pairs sharing a time. The 50 newest start at r6, which ties r7's pair.
	if !pg.more || pg.replies[0].Text != "r6" || len(pg.replies) != 50 {
		t.Fatalf("got %d replies from %q more=%v", len(pg.replies), pg.replies[0].Text, pg.more)
	}
	if replies, _, _ := walk(t, path, 50); len(replies) != 56 {
		t.Fatalf("walk: %d replies, want 56", len(replies))
	}
	// an odd cut: 7 newest of pairs would start mid-pair, so the pair stays whole.
	pg, _ = readTranscriptPage(path, 7)
	if pg.replies[0].Text != "r48" || len(pg.replies) != 8 {
		t.Fatalf("n=7 gave %d replies from %q", len(pg.replies), pg.replies[0].Text)
	}
}

// Entries sharing the cut's time stay on one page.
func TestRepliesCutKeepsTiesTogether(t *testing.T) {
	var sb strings.Builder
	for k := 0; k < 12; k++ {
		sb.WriteString(pagingReplyLine(fmt.Sprintf("m%d", k), pagingBase.Add(time.Duration(k/4)*time.Second),
			fmt.Sprintf("r%d", k)))
	}
	path := writePaging(t, sb.String())
	pg, err := readTranscriptPage(path, 6)
	if err != nil {
		t.Fatal(err)
	}
	// the 6 newest are r6..r11, and r4..r7 share a time, so r4 and r5 come too.
	if len(pg.replies) != 8 || pg.replies[0].Text != "r4" || !pg.more {
		t.Fatalf("got %d replies from %q, more=%v", len(pg.replies), pg.replies[0].Text, pg.more)
	}
	older, _ := readTranscriptBefore(path, 6, pg.next)
	if len(older.replies) != 4 || older.more {
		t.Fatalf("the rest: %d replies, more=%v", len(older.replies), older.more)
	}
}

func TestRepliesFirstPageHasNoMoreForTheScreen(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "codexer2", Worktree: "/tmp/codexer2", Runner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.repliesPage(task.ID, 3, pagingBase)
	if err != nil || v.Source != "screen" || v.More || v.NextBefore != "" {
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
