//go:build integration

package daemon

import "testing"

func TestAnUnwrittenBoardWideSwitchApproves(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemonUsing(t, nil, testNewAutoUnwritten)
	defer cancel()
	plainTask(t, d, "default-1")

	_, auto := ask(t, d, "default-1", "Bash", "go test ./...")
	if auto == nil || auto.Decision != "approve" {
		t.Fatalf("an unwritten board-wide switch left the request for a human: %+v", auto)
	}
}

func TestAnExplicitOffAsksAndAnExplicitOnApproves(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, _ := startDaemonUsing(t, nil, testNewAutoUnwritten)
	defer cancel()
	plainTask(t, d, "default-2")

	if err := d.st.SetGlobalAuto(false); err != nil {
		t.Fatal(err)
	}
	if _, auto := ask(t, d, "default-2", "Bash", "go build ./..."); auto != nil {
		t.Fatalf("an explicit off answered: %+v", auto)
	}
	if err := d.st.SetGlobalAuto(true); err != nil {
		t.Fatal(err)
	}
	if _, auto := ask(t, d, "default-2", "Bash", "go vet ./..."); auto == nil || auto.Decision != "approve" {
		t.Fatalf("an explicit on left the request for a human: %+v", auto)
	}
}
