package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// postPermission parks a request on /permission the way the dotfiles hook
// does, and hands back the decoded answer when it arrives.
func postPermission(ctx context.Context, d *Daemon, body string) <-chan PermissionResponse {
	out := make(chan PermissionResponse, 1)
	go func() {
		defer close(out)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://"+d.opts.AgentAddr+"/permission", strings.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		var got PermissionResponse
		if json.NewDecoder(resp.Body).Decode(&got) == nil {
			out <- got
		}
	}()
	return out
}

// parkedID waits for the request to be in the store AND registered as live,
// since the handler writes the row a moment before it parks.
func parkedID(t *testing.T, d *Daemon, taskID string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pend, err := d.st.PendingForTask(taskID)
		if err == nil && len(pend) > 0 && d.liveStoreIDs()[pend[0].ID] {
			return pend[0].ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the request never reached the store with a hook parked on it")
	return ""
}

func permTask(t *testing.T, d *Daemon, name string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// A decision on the board wakes the hook parked on it, with the exact fields
// the dotfiles hook reads: decision, reason, and a rewritten command.
func TestBoardDecisionWakesParkedPermission(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "perm-wake")

	answer := postPermission(context.Background(), d,
		`{"agent":"perm-wake","tool":"Bash","command":"rm -rf build","dedup_key":"k1"}`)
	id := parkedID(t, d, task.ID)

	if _, err := d.decide(id, "approve", "narrower, please", "rm -rf build/tmp"); err != nil {
		t.Fatal(err)
	}
	select {
	case got, ok := <-answer:
		if !ok {
			t.Fatal("the hook got no parseable answer")
		}
		if got.Decision != "approve" || got.Reason != "narrower, please" || got.Command != "rm -rf build/tmp" {
			t.Fatalf("the hook was answered %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a board decision did not wake the parked hook")
	}
	if d.liveStoreIDs()[id] {
		t.Fatal("an answered request is still counted as live")
	}
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedAt == nil || p.Decision != "approve" {
		t.Fatalf("the decision was not recorded: %+v", p)
	}
}

// A block carries its guidance as the reason, and an unchanged command is not
// echoed back, so the hook runs nothing it did not ask for.
func TestBoardBlockCarriesReasonAndNoCommand(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "perm-block")

	answer := postPermission(context.Background(), d,
		`{"agent":"perm-block","command":"git push --force"}`)
	id := parkedID(t, d, task.ID)

	if _, err := d.decide(id, "block", "push to a branch instead", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-answer:
		if got.Decision != "block" || got.Reason != "push to a branch instead" || got.Command != "" {
			t.Fatalf("the hook was answered %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a block did not wake the parked hook")
	}
	// No tool in the body means Bash, which is what the oldest hooks send.
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tool != "Bash" {
		t.Fatalf("a request with no tool was recorded as %q", p.Tool)
	}
}

// The orphan reaper's input: a hook that hangs up stops counting as live.
func TestDroppedPermissionLeavesLiveSet(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "perm-drop")

	ctx, hangUp := context.WithCancel(context.Background())
	answer := postPermission(ctx, d, `{"agent":"perm-drop","command":"sleep 100"}`)
	id := parkedID(t, d, task.ID)

	hangUp()
	<-answer
	deadline := time.Now().Add(3 * time.Second)
	for d.liveStoreIDs()[id] {
		if time.Now().After(deadline) {
			t.Fatal("a request whose hook hung up is still counted as live")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Nobody is parked, so a late decision is recorded without waking anything.
	if d.decideByStoreID(id, "approve", "", "") {
		t.Fatal("a decision claimed to wake a hook that had gone")
	}
}

// The endpoint's refusals are part of the contract too. The hook fails open on
// anything that is not an answer, so these must stay errors, not answers.
func TestPermissionEndpointRefusals(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	base := "http://" + d.opts.AgentAddr

	resp, err := http.Get(base + "/permission")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /permission answered %d", resp.StatusCode)
	}

	resp, err = http.Post(base+"/permission", "application/json", strings.NewReader(`{"command":"ls"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a request with no agent answered %d", resp.StatusCode)
	}

	// The v1 submit loop is gone. Nothing may answer it as if it were there.
	resp, err = http.Post(base+"/submit", "application/json",
		strings.NewReader(`{"agent":"x","kind":"greeting","content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/submit answered %d", resp.StatusCode)
	}
}
