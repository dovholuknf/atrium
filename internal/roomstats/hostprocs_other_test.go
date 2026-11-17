//go:build !windows && !linux

package roomstats

import (
	"testing"
	"time"
)

func TestParseHostPS(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	ps := parseHostPS("  412     1 root      2-00:00:00  47:10.25   5120 /usr/sbin/sshd -D\n", now)
	if len(ps) != 1 || ps[0].PID != 412 || ps[0].Owner != "root" || !ps[0].Started.Equal(now.Add(-48*time.Hour)) ||
		ps[0].CPU != 47*time.Minute+10250*time.Millisecond || ps[0].RSS != 5120<<10 || ps[0].Name != "/usr/sbin/sshd -D" {
		t.Fatalf("got %+v", ps)
	}
}
