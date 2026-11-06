package cli

import "testing"

func TestServiceRoomRestartsThroughTheSCMNeverARespawn(t *testing.T) {
	asked := false
	if err := scheduleRoomRestart(roomLaunch{dir: t.TempDir()}, func() { asked = true }); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !asked {
		t.Fatal("a service room did not mark its exit as a restart")
	}
}

func TestServiceExitCodeIsNonZeroOnlyForARestart(t *testing.T) {
	for _, c := range []struct {
		code    int
		restart bool
		want    int
	}{
		{0, false, 0},
		{0, true, serviceRestartExit},
		{7, false, 7},
		{7, true, 7},
	} {
		if got := serviceExitCode(c.code, c.restart); got != c.want {
			t.Errorf("serviceExitCode(%d, %v) = %d, want %d", c.code, c.restart, got, c.want)
		}
	}
	if serviceRestartExit == 0 {
		t.Fatal("a restart exit of 0 is a clean stop, and the recovery action never restarts it")
	}
}
