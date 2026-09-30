//go:build darwin

package roomstats

import (
	"golang.org/x/sys/unix"
)

// ReadMachine is memory only. Per-CPU tick counters come from host_statistics,
// which is cgo, and macOS has no kern.cp_time, so HasCPU stays false and the
// board shows no CPU there. Load average is not a percent and is not passed off
// as one.
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
	return MachineReading{MemUsed: total - avail, MemTotal: total}, nil
}
