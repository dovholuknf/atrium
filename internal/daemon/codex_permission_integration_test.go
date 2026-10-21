//go:build integration

package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestCodexPermissionReachesBoardAndKeepsRunner(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, _, err := d.st.Register(store.Observed{WireName: "codex-native", Runner: "codex-fast"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	answer := postPermission(ctx, d, `{"agent":"codex-native","runner":"codex-fast","tool":"Bash",`+
		`"command":"echo native","tool_use_id":"native-1"}`)
	id := parkedID(t, d, task.ID)
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runner != "codex-fast" || got.Status != store.StatusNeedsPermission {
		t.Fatalf("native request card: %+v", got)
	}
	if _, err := d.decide(id, "block", "human says stop", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-answer:
		if got.Decision != "block" || got.Reason != "human says stop" {
			t.Fatalf("answer: %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("native hook never received decision")
	}
	_, replay, err := d.onPermRequest(PermissionRequest{Agent: "codex-native", Runner: "codex-fast",
		Tool: "Bash", Command: "echo native", ToolUseID: "native-1"})
	if err != nil || replay == nil || replay.Decision != "block" {
		t.Fatalf("replay: %+v, %v", replay, err)
	}
}

func TestPermissionReplayRetainsApprovedEdit(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "codex-edit-replay")
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	body := `{"agent":"codex-edit-replay","runner":"codex","tool":"Bash",` +
		`"command":"echo original","tool_use_id":"edit-1"}`
	answer := postPermission(ctx, d, body)
	id := parkedID(t, d, task.ID)
	if _, err := d.decide(id, "approve", "use the edit", "echo edited"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case got := <-answer:
			if got.Decision != "approve" || got.Command != "echo edited" {
				t.Fatalf("attempt %d lost the approved edit: %+v", attempt, got)
			}
		case <-ctx.Done():
			t.Fatal("edited decision never arrived")
		}
		if attempt == 0 {
			answer = postPermission(ctx, d, body)
		}
	}
}
