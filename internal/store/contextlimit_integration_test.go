//go:build integration

package store

import "testing"

// The runner's context limit is saved, returned, and validated like the board's threshold.
func TestARunnerContextLimitIsSavedAndChecked(t *testing.T) {
	s := open(t)
	save := func(k int) error {
		_, err := s.SaveHarness(Harness{ID: "probe", Cmd: "probe", LaunchMode: LaunchPTY, ContextLimitK: k})
		return err
	}
	if err := save(300); err != nil {
		t.Fatal(err)
	}
	if h, _ := s.Harness("probe"); h == nil || h.ContextLimitK != 300 {
		t.Fatalf("limit came back as %+v, want 300", h)
	}
	if err := save(0); err != nil {
		t.Fatalf("empty must be accepted: %v", err)
	}
	if h, _ := s.Harness("probe"); h.ContextLimitK != 0 {
		t.Fatalf("cleared limit came back as %d", h.ContextLimitK)
	}
	for _, bad := range []int{-5, 9, 2001} {
		if err := save(bad); err == nil {
			t.Fatalf("limit %d was accepted", bad)
		}
	}
}
