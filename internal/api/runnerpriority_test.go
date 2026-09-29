package api

import "testing"

// The runner priority defaults to above normal, reads back what was written, and refuses a class it
// does not know rather than storing something that would look like it took.
func TestRunnerPrioritySetting(t *testing.T) {
	srv, st, _ := fileServer(t)
	if got := settingsGet(t, srv)["runner_priority"]; got != "above_normal" {
		t.Fatalf("default = %v, want above_normal", got)
	}
	if rec := settingsPost(t, srv, `{"runner_priority":"normal"}`); rec.Code != 200 {
		t.Fatalf("setting normal answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["runner_priority"]; got != "normal" || st.RunnerPriorityRaised() {
		t.Fatalf("read back %v, raised=%v", got, st.RunnerPriorityRaised())
	}
	if rec := settingsPost(t, srv, `{"runner_priority":"high"}`); rec.Code != 400 {
		t.Fatalf("an unknown class answered %d, want 400", rec.Code)
	}
	if rec := settingsPost(t, srv, `{"runner_priority":"above_normal"}`); rec.Code != 200 {
		t.Fatalf("setting above_normal answered %d", rec.Code)
	}
	if !st.RunnerPriorityRaised() {
		t.Fatal("above_normal did not turn the raise back on")
	}
}
