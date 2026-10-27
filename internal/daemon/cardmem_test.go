package daemon

import "testing"

func TestMemMovedIgnoresNoiseUnderATenthOr16MB(t *testing.T) {
	const mb = 1 << 20
	for _, c := range []struct {
		was, now int64
		want     bool
	}{
		{0, 10 * mb, false},
		{0, 17 * mb, true},
		{1000 * mb, 1090 * mb, false},
		{1000 * mb, 1110 * mb, true},
		{1000 * mb, 890 * mb, true},
		{100 * mb, 0, true},
	} {
		if got := memMoved(c.was, c.now); got != c.want {
			t.Errorf("memMoved(%d MB, %d MB) = %v, want %v", c.was/mb, c.now/mb, got, c.want)
		}
	}
}
