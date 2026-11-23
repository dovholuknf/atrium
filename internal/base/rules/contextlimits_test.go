package rules

import "testing"

// The check refuses a limit out of range and returns an empty map as "".
func TestCheckContextLimits(t *testing.T) {
	t.Parallel()
	if v, err := CheckContextLimits(map[string]int{" claude ": 250}); err != nil || v != `{"claude":250}` {
		t.Fatalf("check gave %q %v", v, err)
	}
	for _, bad := range []map[string]int{{"claude": 49}, {"claude": 2001}, {" ": 200}} {
		if _, err := CheckContextLimits(bad); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}
	if v, err := CheckContextLimits(nil); err != nil || v != "" {
		t.Fatalf("empty gave %q %v", v, err)
	}
}

// A limit saved under the minimum before it rose still cycles, at the minimum.
func TestReadContextLimitK(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]int{30: 50, 50: 50, 200: 200, 0: 0, -5: 0, 2001: 0} {
		if got := ReadContextLimitK(in); got != want {
			t.Fatalf("%d reads %d, want %d", in, got, want)
		}
	}
	if got := ParseContextLimits(`{"claude":20}`); got["claude"] != 50 {
		t.Fatalf("a stored 20 reads %v, want claude 50", got)
	}
}
