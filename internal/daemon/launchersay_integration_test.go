//go:build integration

package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestALauncherOnAnotherRoomIsMatchedBySpawnedBy(t *testing.T) {
	d := testDaemon(t)
	worker := &store.Task{SpawnedBy: "orchestrator-sg4-control@sg4-control"}
	if !d.isLauncherOf("orchestrator-sg4-control@SG4-control", worker) {
		t.Fatal("a say from the recorded remote launcher is not recognised")
	}
	if d.isLauncherOf("someone-else@sg4-control", worker) {
		t.Fatal("another room's session is taken for the launcher")
	}
	if d.isLauncherOf("orchestrator-sg4-control@sg4-control", &store.Task{SpawnedBy: store.HumanLauncher}) {
		t.Fatal("a human-launched card has a launcher")
	}
}
