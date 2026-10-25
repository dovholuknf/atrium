package rules

import "testing"

// The check refuses a limit out of range and returns an empty map as "".
func TestCheckContextLimits(t *testing.T) {
	t.Parallel()
	if v, err := CheckContextLimits(map[string]int{" claude ": 250}); err != nil || v != `{"claude":250}` {
		t.Fatalf("check gave %q %v", v, err)
	}
	for _, bad := range []map[string]int{{"claude": 9}, {"claude": 2001}, {" ": 200}} {
		if _, err := CheckContextLimits(bad); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}
	if v, err := CheckContextLimits(nil); err != nil || v != "" {
		t.Fatalf("empty gave %q %v", v, err)
	}
}
