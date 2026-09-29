package daemon

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The board's `? N` click: dismiss the set it was drawn from, refuse a stale
// one, and reject a body that names nothing.
func TestDismissQuestionsRoute(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{WireName: "orch", Worktree: "/tmp/atrium-seen", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	seenStop(t, d, "orch", []string{"land sa21 first?"})
	v := seenCard(t, d, task.ID)
	url := "http://" + d.opts.HumanAddr + "/v1/tasks/" + task.ID + "/questions/dismiss"
	dismiss := func(url string, body any) (int, map[string]bool) {
		t.Helper()
		resp := postJSON(t, url, body)
		defer resp.Body.Close()
		var out map[string]bool
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if code, _ := dismiss(url, map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("a body with no questions_at answered %d, not 400", code)
	}
	if code, _ := dismiss(url, map[string]any{"questions_at": "soon"}); code != http.StatusBadRequest {
		t.Fatalf("an unparseable questions_at answered %d, not 400", code)
	}
	missing := "http://" + d.opts.HumanAddr + "/v1/tasks/nosuchcard/questions/dismiss"
	if code, _ := dismiss(missing, map[string]any{"questions_at": v.QuestionsAt}); code != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d, not 404", code)
	}

	older := v.QuestionsAt.Add(-time.Minute)
	if code, out := dismiss(url, map[string]any{"questions_at": older}); code != 200 || out["dismissed"] || !out["stale"] {
		t.Fatalf("an older set: %d %v", code, out)
	}
	if got := seenCard(t, d, task.ID); got.OpenCount() != 1 {
		t.Fatalf("a stale dismiss removed the questions: %+v", got)
	}

	if code, out := dismiss(url, map[string]any{"questions_at": v.QuestionsAt}); code != 200 || !out["dismissed"] || out["stale"] {
		t.Fatalf("the shown set: %d %v", code, out)
	}
	got := seenCard(t, d, task.ID)
	if got.OpenCount() != 0 || got.AnsweredVia != store.SeenDismissed || !got.Unseen {
		t.Fatalf("after dismissing: %+v", got)
	}
}
