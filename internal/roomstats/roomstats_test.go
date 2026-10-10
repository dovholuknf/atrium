package roomstats

import (
	"errors"
	"testing"
)

// machineSeq feeds successive readings, or an error while fail is set.
type machineSeq struct {
	n    uint64
	fail bool
}

func (m *machineSeq) read() (MachineReading, error) {
	if m.fail {
		return MachineReading{}, errors.New("boom")
	}
	m.n++
	// each reading: 100 more ticks, 25 of them idle, so 75% busy
	return MachineReading{HasCPU: true, CPUIdle: m.n * 25, CPUTotal: m.n * 100, MemUsed: 4 << 30, MemTotal: 16 << 30}, nil
}

// THE IDLE METER needs two readings, then reports the idle share of the window between them.
func TestIdleMeterNeedsTwoReadings(t *testing.T) {
	var idle, total uint64
	m := &IdleMeter{
		read: func() (MachineReading, error) {
			return MachineReading{HasCPU: true, CPUIdle: idle, CPUTotal: total}, nil
		},
		load: func() (float64, bool) { return 0, false },
	}
	idle, total = 100, 400
	if _, ok := m.Idle(); ok {
		t.Fatal("one reading gave a figure")
	}
	idle, total = 130, 500
	if v, ok := m.Idle(); !ok || v != 30 {
		t.Fatalf("idle = %v %v, want 30", v, ok)
	}
	// Counters that did not advance keep the last figure rather than inventing one.
	if v, ok := m.Idle(); !ok || v != 30 {
		t.Fatalf("idle = %v %v, want 30 held", v, ok)
	}
}

// A PLATFORM WITH NO TICK COUNTERS falls back to the load estimate, and with neither says nothing.
func TestIdleMeterFallsBackToLoad(t *testing.T) {
	m := &IdleMeter{
		read: func() (MachineReading, error) { return MachineReading{}, nil },
		load: func() (float64, bool) { return 62.5, true },
	}
	if v, ok := m.Idle(); !ok || v != 62.5 {
		t.Fatalf("idle = %v %v", v, ok)
	}
	m.load = func() (float64, bool) { return 0, false }
	m2 := &IdleMeter{read: m.read, load: m.load}
	if _, ok := m2.Idle(); ok {
		t.Fatal("no source gave a figure")
	}
}
