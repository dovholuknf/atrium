package roomstats

import (
	"os"
	"path/filepath"
	"runtime"
)

func memStats() (heap uint64, goroutines int) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc, runtime.NumGoroutine()
}

// DBBytes is the database file plus its WAL and shared-memory files.
func DBBytes(dbPath string) (int64, error) {
	fi, err := os.Stat(dbPath)
	if err != nil {
		return 0, err
	}
	n := fi.Size()
	for _, suf := range []string{"-wal", "-shm"} {
		if f, err := os.Stat(dbPath + suf); err == nil {
			n += f.Size()
		}
	}
	return n, nil
}

// CountWorktrees is the number of distinct paths that exist as directories,
// one os.Stat each. Never a walk.
func CountWorktrees(paths []string) int {
	seen := map[string]bool{}
	n := 0
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			n++
		}
	}
	return n
}
