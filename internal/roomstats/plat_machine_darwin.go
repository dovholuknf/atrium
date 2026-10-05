//go:build darwin

package roomstats

import (
	"golang.org/x/sys/unix"
)

// ReadMachine is memory from sysctl and CPU from a streaming top (see
// cpu_darwin.go). HasCPU is false until top has given its first interval, and
// when top is not there.
//
// Used is total minus available, where available is free pages plus the
// file-backed (external) and purgeable pages the kernel gives back on demand.
// That is close to what Activity Monitor calls memory used.
func ReadMachine() (MachineReading, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return MachineReading{}, err
	}
	pageSize, err := unix.SysctlUint32("vm.pagesize")
	if err != nil {
		return MachineReading{}, err
	}
	var pages uint64
	for _, name := range []string{"vm.page_free_count", "vm.page_pageable_external_count", "vm.page_purgeable_count"} {
		n, err := unix.SysctlUint32(name)
		if err != nil {
			return MachineReading{}, err
		}
		pages += uint64(n)
	}
	avail := pages * uint64(pageSize)
	if avail > total {
		avail = total
	}
	mr := MachineReading{MemUsed: total - avail, MemTotal: total}
	if idle, tot, ok := topSampler.read(); ok {
		mr.HasCPU, mr.CPUIdle, mr.CPUTotal = true, idle, tot
	}
	return mr, nil
}
