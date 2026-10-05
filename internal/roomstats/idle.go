package roomstats

import "sync"

// IdleMeter is the machine's idle CPU as a percent, over the short window between one ask and the next. It is asked
// once per room heartbeat, so the window is a few seconds, and it needs no timer of its own.
//
// Where the platform has no tick counters (darwin without cgo) it falls back to the one minute load average over the
// CPU count, which is an estimate and is only good for ranking one room against another.
type IdleMeter struct {
	read func() (MachineReading, error)
	load func() (float64, bool)

	mu        sync.Mutex
	prevIdle  uint64
	prevTotal uint64
	havePrev  bool
	last      float64
	haveLast  bool
}

// NewIdleMeter reads this machine.
func NewIdleMeter() *IdleMeter { return &IdleMeter{read: ReadMachine, load: loadIdle} }

// Idle is the idle percent, 0 to 100. ok is false until there is a figure, and when this machine cannot give one.
func (m *IdleMeter) Idle() (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mr, err := m.read()
	if err != nil {
		return m.last, m.haveLast
	}
	if !mr.HasCPU {
		if v, ok := m.load(); ok {
			m.last, m.haveLast = v, true
		}
		return m.last, m.haveLast
	}
	if m.havePrev {
		if busy, ok := cpuBusyPct(m.prevIdle, m.prevTotal, mr.CPUIdle, mr.CPUTotal); ok {
			m.last, m.haveLast = 100-busy, true
		}
	}
	m.prevIdle, m.prevTotal, m.havePrev = mr.CPUIdle, mr.CPUTotal, true
	return m.last, m.haveLast
}
