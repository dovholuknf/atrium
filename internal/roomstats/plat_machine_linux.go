//go:build linux

package roomstats

import "os"

// ReadMachine is /proc/stat's aggregate cpu line and /proc/meminfo.
func ReadMachine() (MachineReading, error) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return MachineReading{}, err
	}
	idle, total, err := parseProcStat(string(b))
	if err != nil {
		return MachineReading{}, err
	}
	b, err = os.ReadFile("/proc/meminfo")
	if err != nil {
		return MachineReading{}, err
	}
	mt, avail, err := parseMeminfo(string(b))
	if err != nil {
		return MachineReading{}, err
	}
	return MachineReading{HasCPU: true, CPUIdle: idle, CPUTotal: total, MemUsed: mt - avail, MemTotal: mt}, nil
}
