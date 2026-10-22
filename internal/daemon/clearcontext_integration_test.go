//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestClearContextWritesTheHandoffAndClearsWhenTheTurnEnds(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	if err := os.WriteFile(dir+"/BRIEF.md", []byte("brief"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := string(handoffBody) + "\nwritten by clear context"
	path, code, err := d.ClearContext(task, text)
	if err != nil {
		t.Fatalf("refused with %d: %v", code, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != text {
		t.Fatalf("the handoff file holds %q, %v", got, err)
	}
	if strings.Contains(f.written(), limitPrompt) {
		t.Fatal("the limit prompt was typed for a handoff already written")
	}
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.wake.sawSession(task.ID, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), "read the handoff at "+path) })
	if w := f.written(); !strings.Contains(w, "re-read "+strings.ReplaceAll(dir, "\\", "/")+"/BRIEF.md") {
		t.Fatalf("the wake does not name the brief: %q", w)
	}
}

func TestClearContextIsRefusedWithTheReason(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)
	if _, code, err := d.ClearContext(task, "  \n"); err == nil || code != http.StatusConflict {
		t.Fatalf("an empty handoff got %d, %v", code, err)
	}
	codex, _, err := d.st.Register(store.Observed{WireName: "other", Worktree: t.TempDir(), Runner: "codex", PID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, code, err := d.ClearContext(codex, string(handoffBody)); err == nil || code != http.StatusConflict ||
		!strings.Contains(err.Error(), "not claude") {
		t.Fatalf("a runner that is not claude got %d, %v", code, err)
	}
	if _, err := os.Stat(d.handoffPath(codex)); err == nil {
		t.Fatal("a handoff was written for a refused call")
	}

	d2 := testDaemon(t)
	other, _, err := d2.st.Register(store.Observed{WireName: "bare", Worktree: t.TempDir(), Runner: "claude", PID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, code, err := d2.ClearContext(other, string(handoffBody)); err == nil || code != http.StatusConflict ||
		!strings.Contains(err.Error(), "terminal") {
		t.Fatalf("a card with no terminal got %d, %v", code, err)
	}
}

func TestClearContextRefusesWhileACycleIsUnderWay(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)
	if _, _, err := d.ClearContext(task, string(handoffBody)); err != nil {
		t.Fatal(err)
	}
	if _, code, err := d.ClearContext(task, string(handoffBody)); err == nil || code != http.StatusConflict {
		t.Fatalf("a second call got %d, %v", code, err)
	}
}

func postHandoff(d *Daemon, id, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/new-context", strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleNewContext(rec, req)
	return rec
}

func TestNewContextEndpointWithAHandoffBodyQueuesTheClear(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)

	if rec := postHandoff(d, task.ID, `{"handoff":"  "}`); rec.Code != http.StatusConflict {
		t.Fatalf("an empty handoff answered %d: %s", rec.Code, rec.Body)
	}
	body, _ := json.Marshal(map[string]string{"handoff": string(handoffBody)})
	rec := postHandoff(d, task.ID, string(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out["queued"] != true || out["path"] != d.handoffPath(task) {
		t.Fatalf("answered %v, %v", out, err)
	}
	if got, err := os.ReadFile(d.handoffPath(task)); err != nil || string(got) != string(handoffBody) {
		t.Fatalf("the handoff file holds %q, %v", got, err)
	}
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool {
		return strings.Contains(f.written(), "/clear")
	})
}

func TestAResidentCardCyclesByToolAndStaysOpen(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.st.SetTags(task.ID, []string{ResidentTag}); err != nil {
		t.Fatal(err)
	}
	rec := postHandoff(d, task.ID, `{"handoff":`+strconv.Quote(string(handoffBody))+`}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body)
	}
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.wake.sawSession(task.ID, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), "cleared at your own request") })
	until(t, "the chip to go", func() bool { return d.newContextFor(task.ID) == nil })
	got, err := d.st.Get(task.ID)
	if err != nil || got == nil {
		t.Fatalf("the card is gone: %v", err)
	}
	if got.Status == store.StatusDead || got.Status == store.StatusDone {
		t.Fatalf("the card was closed: %s", got.Status)
	}
	if d.sup.get(task.ID) == nil {
		t.Fatal("the card's terminal was closed")
	}
}
