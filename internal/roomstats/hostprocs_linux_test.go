//go:build linux

package roomstats

import (
	"os"
	"testing"
	"time"
)

func TestParseProcPIDStat(t *testing.T) {
	boot := time.Unix(1_700_000_000, 0)
	text := "6688 (sshd: a (b)) R 5008 6688 6688 0 -1 4194560 100 0 0 0 1500 500 0 0 20 0 1 0 12345 1000000 250 0 0"
	p, ok := parseProcPIDStat(text, boot)
	if !ok || p.Name != "sshd: a (b)" || p.PPID != 5008 || p.CPU != 20*time.Second ||
		p.RSS != 250*uint64(os.Getpagesize()) || !p.Started.Equal(boot.Add(12345*time.Second/clkTck)) {
		t.Fatalf("got %+v %v", p, ok)
	}
}

func TestTCPInodesSkipsListeners(t *testing.T) {
	text := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 111 1\n" +
		"   1: 0100007F:0016 0100007F:D431 01 00000000:00000000 00:00000000 00000000     0        0 222 1\n"
	got := tcpInodes(text)
	if got["111"] || !got["222"] || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}
