package ptyhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// intoScope moves pid into a systemd user scope named after label, so `ps -o cgroup` and `systemctl --user` say
// whose it is, and answers the unit. Empty where there is no user manager to ask: the process group stands in.
func intoScope(pid int, label, runID string) string {
	if label == "" {
		return ""
	}
	busctl, err := exec.LookPath("busctl")
	if err != nil {
		return ""
	}
	unit := scopeName(label, runID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, busctl, "--user", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "StartTransientUnit", "ssa(sv)a(sa(sv))", unit, "fail",
		"2", "PIDs", "au", "1", strconv.Itoa(pid), "Description", "s", label, "0").Run()
	if err != nil {
		return ""
	}
	return unit
}

// scopeName is label as a unit name: what systemd refuses in one becomes a dash, and the run id keeps it unique.
func scopeName(label, runID string) string {
	var b strings.Builder
	for _, r := range label {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-:", r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := b.String()
	if len(name) > 180 {
		name = name[:180]
	}
	return name + "-" + runID + ".scope"
}

// treeMem is the scope's memory.current while pid is still in it, else the resident memory of pid's process group.
func treeMem(pid int, unit string) int64 {
	if unit != "" {
		if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup"); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				p, ok := strings.CutPrefix(line, "0::")
				if !ok || filepath.Base(p) != unit {
					continue
				}
				if v, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", p, "memory.current")); err == nil {
					if n, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64); err == nil {
						return n
					}
				}
			}
		}
	}
	return groupMem(pid)
}

// groupMem sums the resident pages of every process whose group is pgid.
func groupMem(pgid int) int64 {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var pages int64
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			continue
		}
		// After the name: state, ppid, pgrp, ... rss is the 22nd field from here.
		f := strings.Fields(string(b[i+1:]))
		if len(f) < 22 || f[2] != strconv.Itoa(pgid) {
			continue
		}
		n, _ := strconv.ParseInt(f[21], 10, 64)
		pages += n
	}
	return pages * int64(os.Getpagesize())
}
