//go:build !darwin

package roomstats

// loadIdle has nothing to offer here: the platforms with tick counters use those.
func loadIdle() (float64, bool) { return 0, false }
