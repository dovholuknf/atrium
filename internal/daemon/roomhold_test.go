package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The room deploy hold. See roomhold.go and docs/rnd/room-deploy-hold-design.md.

// heldRoom is a room with a worker and a deployer, held for a deploy by the
// deployer.
func heldRoom(t *testing.T) (d *Daemon, worker, deployer *store.Task) {
	t.Helper()
	d = testDaemon(t)
	worker = peerCard(t, d, "worker")
	deployer = peerCard(t, d, "merge")
	h, err := d.startDeployHold(holdStart{By: "merge", ByCard: deployer.ID, Whys: []string{"r-hold-notices"}})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Holds(worker.ID) || h.Holds(deployer.ID) {
		t.Fatalf("hold %+v: want the worker held and the deployer not", h)
	}
	return d, worker, deployer
}

func TestADeployHoldRefusesAHeldCallAndNotTheDeployers(t *testing.T) {
	d, _, _ := heldRoom(t)

	id, auto := ask(t, d, "worker", "Bash", "go test ./...")
	if auto == nil || auto.Decision != "block" || !strings.Contains(auto.Reason, "is being redeployed by merge") ||
		!strings.Contains(auto.Reason, "r-hold-notices") {
		t.Fatalf("held call answered %+v", auto)
	}
	if len(auto.Reason) > maxHoldLine {
		t.Fatalf("refusal is %d bytes, over %d", len(auto.Reason), maxHoldLine)
	}
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != DecidedByDeployHold {
		t.Fatalf("recorded as decided by %q", p.DecidedBy)
	}
	if _, auto := ask(t, d, "merge", "Bash", "go build ./..."); auto != nil && strings.Contains(auto.Reason, "redeployed") {
		t.Fatalf("the deployer was held: %+v", auto)
	}
}

func TestAnExemptCardIsNotHeld(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "worker")
	watcher := peerCard(t, d, "watcher")
	h, err := d.startDeployHold(holdStart{By: "merge", Exempt: []string{"watcher"}})
	if err != nil {
		t.Fatal(err)
	}
	if h.Holds(watcher.ID) {
		t.Fatal("an exempt card is held")
	}
	if _, err := d.startDeployHold(holdStart{By: "merge", Exempt: []string{"a", "b", "c", "d"}}); err == nil {
		t.Fatal("four exempt cards were taken")
	}
}

func TestASecondHoldIsRefused(t *testing.T) {
	d, _, _ := heldRoom(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/hold", strings.NewReader(`{"action":"start","by":"merge"}`))
	w := httptest.NewRecorder()
	d.handleHold(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("second start answered %d: %s", w.Code, w.Body)
	}
}

func TestAnAgentsSayIsHeldAndTheOperatorsIsNot(t *testing.T) {
	d, worker, _ := heldRoom(t)

	out := sayViaMessage(t, d, "alice", worker.ID, "from alice")
	if w, _ := out["warning"].(string); !strings.Contains(w, "being redeployed") {
		t.Fatalf("an agent's say to a held card answered %v", out)
	}
	sayViaMessage(t, d, "", worker.ID, "from the operator")
	msgs, err := d.takeMessages(worker.ID, "permission")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "from the operator" {
		t.Fatalf("during the hold the hooks carried %+v, want the operator's only", msgs)
	}

	if _, err := d.liftDeployHold(HoldOutcomeCancelled, "merge", "build broke"); err != nil {
		t.Fatal(err)
	}
	msgs, err = d.takeMessages(worker.ID, "permission")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range msgs {
		texts = append(texts, m.Text)
	}
	got := strings.Join(texts, "\n")
	if !strings.Contains(got, "from alice") || !strings.Contains(got, "room deploy called off by merge: build broke") {
		t.Fatalf("after the lift the hooks carried %q", got)
	}
}

func TestALaunchOntoAHeldRoomIsRefusedAndTheDeployersIsNot(t *testing.T) {
	d, _, _ := heldRoom(t)
	if err := d.launchHeld(LaunchRequest{SpawnedBy: "worker"}); err == nil ||
		!strings.Contains(err.Error(), "launch after the wake") {
		t.Fatalf("a held card's launch answered %v", err)
	}
	if err := d.launchHeld(LaunchRequest{SpawnedBy: "merge"}); err != nil {
		t.Fatalf("the deployer's launch was refused: %v", err)
	}
}

// restartUnder saves a reopen list naming the worker, as the wind-down does.
func restartUnder(t *testing.T, d *Daemon, ids ...string) {
	t.Helper()
	rs := make([]*runner, 0, len(ids))
	for _, id := range ids {
		rs = append(rs, &runner{taskID: id})
	}
	d.saveReopen(rs)
}

func TestTheRoomLiftsItsHoldAtStartupOnANewBuild(t *testing.T) {
	d, worker, _ := heldRoom(t)
	restartUnder(t, d, worker.ID)
	// The binary that set the hold is not this one.
	d.build = "newbuild"

	d.liftAtStartup()
	if d.deployHold() != nil {
		t.Fatal("still held after the startup lift")
	}
	w := d.wake.get(worker.ID)
	if w == nil || !strings.Contains(w.Text, "room deploy done: this room is on build newbuild") {
		t.Fatalf("worker's wake = %+v", w)
	}
	// Messages wait for the wake, then flow.
	if !d.deployHeld(worker.ID) {
		t.Fatal("the worker's messages are not held until its wake")
	}
	d.wakeTyped(worker.ID)
	if d.deployHeld(worker.ID) {
		t.Fatal("still held after the wake was typed")
	}

	// A second startup finds nothing to lift and queues nothing.
	if _, err := d.clearWake(worker.ID, "test"); err != nil {
		t.Fatal(err)
	}
	d.liftAtStartup()
	if w := d.wake.get(worker.ID); w != nil {
		t.Fatalf("a second startup woke the worker again: %+v", w)
	}
}

func TestTheSameBuildComingBackSaysTheDeployDidNotTake(t *testing.T) {
	d, worker, _ := heldRoom(t)
	restartUnder(t, d, worker.ID)
	d.liftAtStartup()
	w := d.wake.get(worker.ID)
	if w == nil || !strings.Contains(w.Text, "room deploy did not take") {
		t.Fatalf("worker's wake = %+v", w)
	}
}

func TestACardWithItsOwnWakeHearsBothInOneLine(t *testing.T) {
	d, worker, _ := heldRoom(t)
	if _, _, err := d.queueWake(worker.ID, "pick up r-038", "worker"); err != nil {
		t.Fatal(err)
	}
	restartUnder(t, d, worker.ID)
	d.build = "newbuild"
	d.liftAtStartup()
	w := d.wake.get(worker.ID)
	if w == nil || !strings.HasPrefix(w.Text, "pick up r-038") || !strings.Contains(w.Text, "room deploy done") ||
		w.By != "worker" {
		t.Fatalf("worker's wake = %+v", w)
	}
}

func TestAHoldThatRunsOutIsLiftedWithItsSentence(t *testing.T) {
	d, worker, _ := heldRoom(t)
	d.holdTick(time.Now().Add(2 * time.Hour))
	if d.deployHold() != nil {
		t.Fatal("still held after its expiry")
	}
	msgs := pendingFrom(t, d, worker.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "hold ran out after 60 minutes") {
		t.Fatalf("worker has %+v", msgs)
	}
}

func TestWithNoHoldNothingChanges(t *testing.T) {
	d := testDaemon(t)
	worker := peerCard(t, d, "worker")
	if d.deployHeld(worker.ID) {
		t.Fatal("held with no hold")
	}
	if _, auto := ask(t, d, "worker", "Bash", "ls"); auto != nil {
		t.Fatalf("a call with no hold was answered %+v", auto)
	}
	if err := d.launchHeld(LaunchRequest{SpawnedBy: "worker"}); err != nil {
		t.Fatal(err)
	}
}

func TestBusyCardIsTheRoomsRule(t *testing.T) {
	cases := []struct {
		name       string
		supervised bool
		status     string
		idle       time.Duration
		activity   string
		want       bool
	}{
		{"working", true, store.StatusRunning, time.Second, ActivityTool, true},
		{"not supervised", false, store.StatusRunning, time.Second, ActivityTool, false},
		{"waiting on a human", true, store.StatusNeedsInput, time.Second, ActivityTool, false},
		{"stale", true, store.StatusRunning, BusyIdleAfter + time.Second, ActivityTool, false},
		{"thinking", true, store.StatusRunning, time.Second, ActivityThinking, false},
	}
	for _, c := range cases {
		if got := BusyCard(c.supervised, c.status, c.idle, c.activity); got != c.want {
			t.Errorf("%s: busy = %v, want %v", c.name, got, c.want)
		}
	}
}
