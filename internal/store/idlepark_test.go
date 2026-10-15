package store

import (
	"testing"
	"time"
)

func TestIdleParkIsOffUnlessSet(t *testing.T) {
	for _, v := range []string{"", "off", " OFF ", "0", "-5", "soon"} {
		if d, on := ParseIdleParkAfter(v); on {
			t.Fatalf("%q parsed as on at %v, want off", v, d)
		}
	}
	if d, on := ParseIdleParkAfter("60"); !on || d != MinIdleParkAfter {
		t.Fatalf("a number under the floor is %v %v, want the floor", d, on)
	}
	if d, on := ParseIdleParkAfter("7200"); !on || d != 2*time.Hour {
		t.Fatalf("7200 is %v %v, want 2h on", d, on)
	}
}
