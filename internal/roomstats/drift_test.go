package roomstats

import (
	"testing"
	"time"
)

func TestDrainIsAP99AndEmpties(t *testing.T) {
	d := &Drift{}
	if _, _, ok := d.Drain(); ok {
		t.Fatal("an empty probe gave a reading")
	}
	for i := 1; i <= 100; i++ {
		d.record(time.Duration(i) * time.Millisecond)
	}
	p, n, ok := d.Drain()
	if !ok || n != 100 || p != 99*time.Millisecond {
		t.Fatalf("p99 %v n %d ok %v", p, n, ok)
	}
	if _, _, ok := d.Drain(); ok {
		t.Fatal("drain did not empty the probe")
	}
}
