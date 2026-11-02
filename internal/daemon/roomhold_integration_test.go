//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// heldRoom is a room with a worker and a deployer, held for a deploy by the
// deployer.
func heldRoom(t *testing.T) (d *Daemon, worker, deployer *store.Task) {
	t.Helper()
	d = testDaemon(t)
	worker = peerCard(t, d, "worker")
	deployer = peerCard(t, d, "merge")
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	h, err := d.StartDeployHold(cycle.HoldStart{By: "merge", ByCard: deployer.ID, Whys: []string{"r-hold-notices"}})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Holds(worker.ID) || h.Holds(deployer.ID) {
		t.Fatalf("hold %+v: want the worker held and the deployer not", h)
	}
	return d, worker, deployer
}

func TestADeployHoldRefusesAHeldCallAndNotTheDeployers(t *testing.T) {
	t.Parallel()
	d, _, _ := heldRoom(t)

	id, auto := ask(t, d, "worker", "Bash", "go test ./...")
	if auto == nil || auto.Decision != "block" || !strings.Contains(auto.Reason, "is being redeployed by merge") ||
		!strings.Contains(auto.Reason, "r-hold-notices") {
		t.Fatalf("held call answered %+v", auto)
	}
	if len(auto.Reason) > cycle.MaxHoldLine {
		t.Fatalf("refusal is %d bytes, over %d", len(auto.Reason), cycle.MaxHoldLine)
	}
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != cycle.DecidedByDeployHold {
		t.Fatalf("recorded as decided by %q", p.DecidedBy)
	}
	if _, auto := ask(t, d, "merge", "Bash", "go build ./..."); auto != nil && strings.Contains(auto.Reason, "redeployed") {
		t.Fatalf("the deployer was held: %+v", auto)
	}
}

// A held card removed before the lift (a throwaway that ended) must not keep the room held: on 2026-10-12 one did, and
// every launch on claude-sg4 was refused until the hold ran out.
func TestAHoldLiftsWhenAHeldCardWasRemoved(t *testing.T) {
	t.Parallel()
	d, worker, _ := heldRoom(t)
	if err := d.st.Forget(worker.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LiftDeployHold(cycle.HoldOutcomeCancelled); err != nil {
		t.Fatalf("lift: %v", err)
	}
	if hs, _ := d.st.RoomHolds(); len(hs) != 0 {
		t.Fatalf("still held: %+v", hs)
	}
}

func TestAnExemptCardIsNotHeld(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	peerCard(t, d, "worker")
	watcher := peerCard(t, d, "watcher")
	h, err := d.StartDeployHold(cycle.HoldStart{By: "merge", Exempt: []string{"watcher"}})
	if err != nil {
		t.Fatal(err)
	}
	if h.Holds(watcher.ID) {
		t.Fatal("an exempt card is held")
	}
	if _, err := d.StartDeployHold(cycle.HoldStart{By: "merge", Exempt: []string{"a", "b", "c", "d"}}); err == nil {
		t.Fatal("four exempt cards were taken")
	}
}

func TestASecondHoldIsRefused(t *testing.T) {
	t.Parallel()
	d, _, _ := heldRoom(t)
	r := httptest.NewRequest(http.MethodPost, "/v1/hold", strings.NewReader(`{"action":"start","by":"merge"}`))
	w := httptest.NewRecorder()
	d.HandleHold(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("second start answered %d: %s", w.Code, w.Body)
	}
}

func TestAnAgentsSayIsHeldAndTheOperatorsIsNot(t *testing.T) {
	t.Parallel()
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

	ask(t, d, "worker", "Bash", "ls")
	if _, err := d.LiftDeployHold(cycle.HoldOutcomeCancelled); err != nil {
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
	if got != "from alice" {
		t.Fatalf("after the lift the hooks carried %q, want the agent's say only", got)
	}
}

func TestALaunchOntoAHeldRoomIsRefusedAndTheDeployersIsNot(t *testing.T) {
	t.Parallel()
	d, _, _ := heldRoom(t)
	if err := d.LaunchHeld(shared.LaunchRequest{SpawnedBy: "worker"}); err == nil ||
		!strings.Contains(err.Error(), "launch when the hold lifts") {
		t.Fatalf("a held card's launch answered %v", err)
	}
	if err := d.LaunchHeld(shared.LaunchRequest{SpawnedBy: "merge"}); err != nil {
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

func noMessageTo(t *testing.T, d *Daemon, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if msgs := pendingFrom(t, d, id); len(msgs) != 0 {
			t.Fatalf("card %s was sent %+v by the lift", id, msgs)
		}
		if w := d.Wake.Get(id); w != nil {
			t.Fatalf("card %s has a wake %+v from the lift", id, w)
		}
		if n := resumeEvents(t, d, id); n != 0 {
			t.Fatalf("card %s has %d resume-continue events", id, n)
		}
	}
}

func resumeEvents(t *testing.T, d *Daemon, id string) int {
	t.Helper()
	evs, err := d.st.Events(id, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if strings.Contains(string(e.Payload), "resume-continue") {
			n++
		}
	}
	return n
}

func TestTheStartupLiftSendsNothingToAnyCard(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	busy := peerCard(t, d, "busy")
	idle := peerCard(t, d, "idle")
	refused := peerCard(t, d, "refused")
	for id, st := range map[string]string{busy.ID: store.StatusRunning, idle.ID: store.StatusNeedsInput,
		refused.ID: store.StatusNeedsInput} {
		if err := d.st.SetStatus(id, st); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.StartDeployHold(cycle.HoldStart{By: "merge"}); err != nil {
		t.Fatal(err)
	}
	ask(t, d, "refused", "Bash", "ls")
	restartUnder(t, d, busy.ID, idle.ID, refused.ID)
	d.Build = "newbuild"

	d.LiftAtStartup()
	if d.DeployHold() != nil {
		t.Fatal("still held after the startup lift")
	}
	noMessageTo(t, d, busy.ID, idle.ID, refused.ID)
	for _, id := range []string{busy.ID, idle.ID, refused.ID} {
		if d.DeployHeld(id) {
			t.Fatalf("card %s is still held for messages", id)
		}
	}
	d.LiftAtStartup()
	noMessageTo(t, d, busy.ID, idle.ID, refused.ID)
}

func TestTheSameBuildComingBackSendsNothing(t *testing.T) {
	t.Parallel()
	d, worker, _ := heldRoom(t)
	restartUnder(t, d, worker.ID)
	d.LiftAtStartup()
	if d.DeployHold() != nil {
		t.Fatal("still held")
	}
	noMessageTo(t, d, worker.ID)
}

func TestALiftLeavesACardsOwnWakeAlone(t *testing.T) {
	t.Parallel()
	d, worker, _ := heldRoom(t)
	if _, _, err := d.QueueWake(worker.ID, "pick up r-038", "worker"); err != nil {
		t.Fatal(err)
	}
	restartUnder(t, d, worker.ID)
	d.Build = "newbuild"
	d.LiftAtStartup()
	w := d.Wake.Get(worker.ID)
	if w == nil || w.Text != "pick up r-038" || w.By != "worker" {
		t.Fatalf("worker's wake = %+v", w)
	}
}

func TestAHoldThatRunsOutSendsNothing(t *testing.T) {
	t.Parallel()
	d, worker, _ := heldRoom(t)
	ask(t, d, "worker", "Bash", "ls")
	d.HoldTick(time.Now().Add(2 * time.Hour))
	if d.DeployHold() != nil {
		t.Fatal("still held after its expiry")
	}
	noMessageTo(t, d, worker.ID)
}

func TestACalledOffHoldSendsNothingToWorkingOrIdleCards(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	var cards []*store.Task
	for n, st := range map[string]string{"idle1": store.StatusNeedsInput, "idle2": store.StatusNeedsInput,
		"working": store.StatusRunning, "refused": store.StatusNeedsInput} {
		c := peerCard(t, d, n)
		if err := d.st.SetStatus(c.ID, st); err != nil {
			t.Fatal(err)
		}
		cards = append(cards, c)
	}
	if _, err := d.StartDeployHold(cycle.HoldStart{By: "merge"}); err != nil {
		t.Fatal(err)
	}
	ask(t, d, "refused", "Bash", "ls")
	ask(t, d, "working", "Bash", "ls")

	if _, err := d.LiftDeployHold(cycle.HoldOutcomeCancelled); err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		noMessageTo(t, d, c.ID)
		if d.DeployHeld(c.ID) {
			t.Fatalf("%s is still held", c.DisplayTitle())
		}
	}
}

func TestWithNoHoldNothingChanges(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	worker := peerCard(t, d, "worker")
	if d.DeployHeld(worker.ID) {
		t.Fatal("held with no hold")
	}
	if _, auto := ask(t, d, "worker", "Bash", "ls"); auto != nil {
		t.Fatalf("a call with no hold was answered %+v", auto)
	}
	if err := d.LaunchHeld(shared.LaunchRequest{SpawnedBy: "worker"}); err != nil {
		t.Fatal(err)
	}
}

func TestBusyCardIsTheRoomsRule(t *testing.T) {
	t.Parallel()
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
		{"stale", true, store.StatusRunning, cycle.BusyIdleAfter + time.Second, ActivityTool, false},
		{"thinking", true, store.StatusRunning, time.Second, ActivityThinking, false},
	}
	for _, c := range cases {
		if got := cycle.BusyCard(c.supervised, c.status, c.idle, c.activity); got != c.want {
			t.Errorf("%s: busy = %v, want %v", c.name, got, c.want)
		}
	}
}
