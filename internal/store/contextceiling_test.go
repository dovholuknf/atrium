package store

import "testing"

func TestContextCeilingSettingReadsAndChecks(t *testing.T) {
	s := openTestStore(t)
	if got := s.ContextCeilingK(); got != 150 {
		t.Fatalf("unset reads %d, want 150", got)
	}
	for v, want := range map[string]int{"200": 200, " 400 ": 400, "49": 150, "2001": 150, "x": 150} {
		if err := s.SetSetting(SettingContextCeilingK, v); err != nil {
			t.Fatal(err)
		}
		if got := s.ContextCeilingK(); got != want {
			t.Fatalf("ceiling %q reads %d, want %d", v, got, want)
		}
	}
	for v, ok := range map[string]bool{"": true, "50": true, "2000": true, "49": false, "2001": false, "abc": false} {
		if _, err := CheckContextCeilingK(v); (err == nil) != ok {
			t.Fatalf("ceiling %q: err %v, want ok=%v", v, err, ok)
		}
	}
}
