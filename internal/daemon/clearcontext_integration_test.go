//go:build integration

package daemon

import (
	"net/http"
	"os"
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
