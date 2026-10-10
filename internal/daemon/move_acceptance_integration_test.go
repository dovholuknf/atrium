//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// mv drives one route of a daemon's move handler by hand, as the hub will.
func mv(t *testing.T, d *Daemon, step string, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	d.moveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/move/"+step, bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func mvOK(t *testing.T, d *Daemon, step string, body map[string]any) map[string]any {
	t.Helper()
	code, out := mv(t, d, step, body)
	if code != http.StatusOK {
		t.Fatalf("%s: %d %+v", step, code, out)
	}
	return out
}

func gitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
			t.Skipf("git: %v %s", err, out)
		}
	}
}

// THE M1 ACCEPTANCE, two rooms on one machine, driven by hand through the room routes.
func TestAWorkerMovesBetweenRoomsThroughTheRoomRoutes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	a, fa := roomDaemon(t)
	a.opts.Room = "sg3"
	b, _, cancel, errCh := startDaemonWith(t, func(o *Options) { o.Room = "m1mini" })
	defer func() {
		cancel()
		<-errCh
	}()
	// A's relay is room B: a say lands on B as it would from the hub.
	fa.set(func(s RelaySay) (RelayResult, error) {
		code, out := say(t, b, map[string]string{"from": s.From + "@sg3", "to": s.To, "text": s.Text})
		return RelayResult{OK: code < 400, Delivered: "queued", To: s.To + "@" + s.Room, Card: s.Room + "~" + s.To,
			Code: code, Error: strings.TrimSpace(strings.Join([]string{asString(out["error"])}, ""))}, nil
	})

	// The old card, a pinned worker in a git worktree, and a child that reports to it by handle.
	wt := filepath.Join(t.TempDir(), "atrium-worktrees", "old")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRepo(t, wt)
	old, _, err := a.st.Register(store.Observed{WireName: "old", Worktree: wt, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.SetPinned(old.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := a.st.SetPinSlot(old.ID, 2); err != nil {
		t.Fatal(err)
	}
	child := peerCard(t, a, "kid")
	if err := a.st.SetLauncher(child.ID, old.WireName, old.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.st.SetReportTo(child.ID, "old"); err != nil {
		t.Fatal(err)
	}
	peerCard(t, a, "sender")
	if err := os.WriteFile(filepath.Join(wt, "BRIEF.md"), []byte("brief"), 0o644); err != nil {
		t.Fatal(err)
	}

	// PREPARE. Freeze, check, bundle.
	mvOK(t, a, "freeze", map[string]any{"id": old.ID, "move_id": "mv1", "lease_seconds": 600})
	mvOK(t, a, "freeze", map[string]any{"id": old.ID, "move_id": "mv1"}) // repeat is the same freeze
	if out := mvOK(t, a, "check", map[string]any{"id": old.ID, "room": "m1mini"}); out["ok"] != true {
		t.Fatalf("check %+v", out)
	}
	// Typing is refused while it is frozen. Its says and notices wait, once each.
	if !a.frozenForMove(old.ID) || !a.holdingMessages(old.ID) {
		t.Fatal("the old card is not held")
	}
	if code, out := say(t, a, map[string]string{"from": "sender", "to": "old", "text": "during the freeze"}); code != 200 {
		t.Fatalf("%d %+v", code, out)
	}
	if got := pendingFrom(t, a, old.ID); len(got) != 0 {
		t.Fatalf("a frozen card has a deliverable message: %+v", got)
	}
	mvOK(t, a, "park", map[string]any{"id": old.ID, "move_id": "mv1"})

	bundle := mvOK(t, a, "bundle", map[string]any{"id": old.ID})
	files, _ := bundle["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("bundle %+v", bundle)
	}

	// B: write the files, launch the successor.
	newWt := filepath.Join(t.TempDir(), "atrium-worktrees", "old")
	if err := os.MkdirAll(newWt, 0o755); err != nil {
		t.Fatal(err)
	}
	recv := map[string]any{"cwd": newWt, "conversation": bundle["conversation"], "files": bundle["files"]}
	mvOK(t, b, "receive", recv)
	if bs, err := os.ReadFile(filepath.Join(newWt, "BRIEF.md")); err != nil || string(bs) != "brief" {
		t.Fatalf("BRIEF.md on B: %q %v", bs, err)
	}
	h := slowHarness(t, b)
	req := LaunchRequest{Harness: h, Cwd: newWt, Title: "old", Repo: "dovholuknf/atrium",
		MovedFrom: "sg3~" + old.ID, Pinned: true, PinOrder: 2, SpawnedBy: "boss@sg3", SpawnedByID: "sg3~BOSS"}
	nc, err := b.Launch(req)
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	defer func() { _ = b.StopRunner(nc.ID) }()
	if again, err := b.Launch(req); err != nil || again.ID != nc.ID {
		t.Fatalf("a repeat launch made another card: %v %v", again, err)
	}
	nc = mustGet(t, b, nc.ID)
	if nc.MovedFrom != "sg3~"+old.ID || !nc.Pinned || nc.PinOrder != 2 {
		t.Fatalf("successor %+v", nc)
	}
	if nc.DisplayRepo() != "dovholuknf/atrium" {
		t.Fatalf("label %q", nc.DisplayRepo())
	}

	// CUT OVER. moved_to, then the queue forwarded by id (twice, delivered once), then the close.
	mvOK(t, a, "moved", map[string]any{"id": old.ID, "move_id": "mv1", "to": "m1mini~" + nc.ID})
	q := mvOK(t, a, "queue", map[string]any{"id": old.ID})["queue"].([]any)
	if len(q) != 1 {
		t.Fatalf("queue %+v", q)
	}
	m := q[0].(map[string]any)
	for i := 0; i < 2; i++ {
		out := mvOK(t, b, "deliver", map[string]any{"id": nc.ID, "msg_id": m["id"], "from": m["from_peer"], "text": m["text"]})
		if out["acked"] != true || (out["duplicate"] == true) != (i == 1) {
			t.Fatalf("delivery %d: %+v", i, out)
		}
	}
	if code, _ := mv(t, a, "release", map[string]any{"id": old.ID, "move_id": "mv1"}); code != http.StatusConflict {
		t.Fatal("released with the queue not acked")
	}
	mvOK(t, a, "ack", map[string]any{"id": old.ID, "msg_ids": []string{m["id"].(string)}})
	mvOK(t, a, "release", map[string]any{"id": old.ID, "move_id": "mv1"})
	mvOK(t, a, "release", map[string]any{"id": old.ID, "move_id": "mv1"}) // idempotent
	mvOK(t, b, "adopt", map[string]any{"id": nc.ID, "alias": "oldy", "pinned": true, "pin_order": 2, "gated": true})
	if code, _ := mv(t, a, "undo", map[string]any{"id": old.ID, "move_id": "mv1"}); code != http.StatusConflict {
		t.Fatal("undid a card that has moved")
	}

	// A say to the old card now arrives, once, on the new one, and the sender is told where it went.
	code, out := say(t, a, map[string]string{"from": "sender", "to": old.ID, "text": "after the move"})
	if code != 200 || out["forwarded"] != "moved to m1mini~"+nc.ID {
		t.Fatalf("%d %+v", code, out)
	}
	if got := fa.says(); len(got) != 1 || got[0].Room != "m1mini" || got[0].To != nc.ID || got[0].Text != "after the move" {
		t.Fatalf("relay got %+v", got)
	}

	// The child's report goes to the new card: its launcher is re-pointed to B.
	child = mustGet(t, a, child.ID)
	if l := a.launcherOf(child); l != nil {
		t.Fatalf("launcher is still local: %+v", l)
	}
	child = mustGet(t, a, child.ID)
	if child.SpawnedByID != "m1mini~"+nc.ID {
		t.Fatalf("child launcher %q", child.SpawnedByID)
	}
	if got := mustGet(t, a, old.ID); got.Status != store.StatusDone || got.Alias != "" || !a.st.MovedHandleHeld("old") {
		t.Fatalf("old card %+v", got)
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// An undo after the launch leaves the old card as it was, its queue replayed once.
func TestAnUndoLeavesTheOldCardAsItWas(t *testing.T) {
	a, _ := roomDaemon(t)
	a.opts.Room = "sg3"
	peerCard(t, a, "sender")
	old := peerCard(t, a, "old")
	mvOK(t, a, "freeze", map[string]any{"id": old.ID, "move_id": "mv9", "lease_seconds": 600})
	say(t, a, map[string]string{"from": "sender", "to": "old", "text": "queued while moving"})
	if f, _ := a.st.Frozen(old.ID); f == nil {
		t.Fatal("not frozen")
	}
	mvOK(t, a, "undo", map[string]any{"id": old.ID, "move_id": "mv9"})
	mvOK(t, a, "undo", map[string]any{"id": old.ID, "move_id": "mv9"}) // idempotent
	if a.frozenForMove(old.ID) {
		t.Fatal("still frozen")
	}
	if got := pendingFrom(t, a, old.ID); len(got) != 1 || got[0].Text != "queued while moving" {
		t.Fatalf("after the undo %+v", got)
	}
}

// A card whose hub never came back undoes its own freeze at the lease.
func TestAnExpiredLeaseUndoesTheFreeze(t *testing.T) {
	a, _ := roomDaemon(t)
	peerCard(t, a, "sender")
	old := peerCard(t, a, "old")
	mvOK(t, a, "freeze", map[string]any{"id": old.ID, "move_id": "mv7", "lease_seconds": 1})
	say(t, a, map[string]string{"from": "sender", "to": "old", "text": "held"})
	a.sweepFreezes(time.Now())
	if !a.frozenForMove(old.ID) {
		t.Fatal("undone before the lease")
	}
	a.sweepFreezes(time.Now().Add(time.Minute))
	if a.frozenForMove(old.ID) || len(pendingFrom(t, a, old.ID)) != 1 {
		t.Fatal("the lease did not undo the freeze")
	}
	if code, _ := mv(t, a, "moved", map[string]any{"id": old.ID, "move_id": "mv7", "to": "x~y"}); code != http.StatusConflict {
		t.Fatal("moved_to after the self-undo")
	}
}

// receive writes only what the allowlist names.
func TestReceiveRefusesANameOutsideTheAllowlist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	b, _ := roomDaemon(t)
	cwd := t.TempDir()
	for _, bad := range []string{"../evil.md", "x.sh", ".hidden", "BRIEF.md/../../x"} {
		code, _ := mv(t, b, "receive", map[string]any{"cwd": cwd, "files": []map[string]any{{"name": bad, "data": "eA=="}}})
		if code != http.StatusBadRequest {
			t.Fatalf("%q was accepted: %d", bad, code)
		}
	}
	code, _ := mv(t, b, "receive", map[string]any{"cwd": cwd, "memory": []map[string]any{{"name": "../m.md", "data": "eA=="}}})
	if code != http.StatusBadRequest {
		t.Fatal("a memory name outside was accepted")
	}
}
