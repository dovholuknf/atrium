//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func orchestratorCard(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	card := peerCard(t, d, "orch")
	if err := d.st.SetTags(card.ID, []string{OrchestratorTag}); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mayPark(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return d.mayIdlePark(got)
}

func TestOrchestratorParksWithNothingElseLive(t *testing.T) {
	d := testDaemon(t)
	orch := orchestratorCard(t, d)
	liveRunner(d, orch.ID)
	if !mayPark(t, d, orch.ID) {
		t.Fatal("the orchestrator may not park with nothing else running")
	}
}

func TestOrchestratorDoesNotParkWhileAnotherCardIsLive(t *testing.T) {
	d := testDaemon(t)
	orch := orchestratorCard(t, d)
	other := peerCard(t, d, "busy")
	liveRunner(d, orch.ID)
	liveRunner(d, other.ID)
	if mayPark(t, d, orch.ID) {
		t.Fatal("the orchestrator may park while another card has a runner")
	}
	// Any status counts, `done` at its prompt included.
	if err := d.st.SetStatus(other.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	if mayPark(t, d, orch.ID) {
		t.Fatal("a done card at its prompt did not count as live")
	}
}

func TestOrchestratorParksOnceTheLastOneParksOrEnds(t *testing.T) {
	d := testDaemon(t)
	orch := orchestratorCard(t, d)
	a := peerCard(t, d, "a")
	b := peerCard(t, d, "b")
	liveRunner(d, orch.ID)
	liveRunner(d, a.ID)
	liveRunner(d, b.ID)
	if mayPark(t, d, orch.ID) {
		t.Fatal("may park with two live")
	}
	// One parks: its runner is gone and it is marked.
	endRunner(d, a.ID)
	if err := d.parkCard(a.ID, store.StatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	if mayPark(t, d, orch.ID) {
		t.Fatal("may park with one still live")
	}
	// The other ends.
	endRunner(d, b.ID)
	if !mayPark(t, d, orch.ID) {
		t.Fatal("may not park once the last one parked or ended")
	}
}

func TestOrchestratorIgnoresFixturesAndShells(t *testing.T) {
	d := testDaemon(t)
	orch := orchestratorCard(t, d)
	fix := peerCard(t, d, "fixture-term")
	shellOwner := peerCard(t, d, "shelled")
	if _, err := d.st.SaveFixture(&store.Fixture{Label: "term", Harness: "claude", Cwd: "/tmp/x", TaskID: fix.ID}); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, orch.ID)
	liveRunner(d, fix.ID)
	// A shell beside a card is in the other map and is not work.
	d.sup.mu.Lock()
	d.sup.shells[shellOwner.ID] = &runner{}
	d.sup.mu.Unlock()
	if !mayPark(t, d, orch.ID) {
		t.Fatal("a fixture or a shell kept the orchestrator up")
	}
}

func TestOrchestratorRuleIsNotTheOptInTag(t *testing.T) {
	d := testDaemon(t)
	// The opt-in tag on an operator card has no orchestrator condition, and the
	// orchestrator tag alone makes a card subject. An untagged operator card is
	// neither.
	plain := peerCard(t, d, "plain")
	if mayPark(t, d, plain.ID) {
		t.Fatal("an untagged operator card is subject")
	}
	opt := peerCard(t, d, "optin")
	if err := d.st.SetTags(opt.ID, []string{ParkIdleTag}); err != nil {
		t.Fatal(err)
	}
	busy := peerCard(t, d, "busy2")
	liveRunner(d, busy.ID)
	if !mayPark(t, d, opt.ID) {
		t.Fatal("an opted-in card is held by the orchestrator's rule")
	}
	orch := orchestratorCard(t, d)
	liveRunner(d, orch.ID)
	if mayPark(t, d, orch.ID) {
		t.Fatal("the orchestrator ignored a live card")
	}
}

func TestIdleParkNeverSubjectsFixturesOrThrowaways(t *testing.T) {
	d := testDaemon(t)
	fix := peerCard(t, d, "fixed")
	if err := d.st.SetTags(fix.ID, []string{OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.SaveFixture(&store.Fixture{Label: "f", Harness: "claude", Cwd: "/tmp/x", TaskID: fix.ID}); err != nil {
		t.Fatal(err)
	}
	if mayPark(t, d, fix.ID) {
		t.Fatal("a fixture is subject")
	}
	agent := peerCard(t, d, "agentcard")
	if err := d.st.SetTags(agent.ID, []string{OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if !mayPark(t, d, agent.ID) {
		t.Fatal("an agent card is not subject")
	}
}
