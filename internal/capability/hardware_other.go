//go:build !windows && !linux && !darwin

package capability

import "context"

func cpuModel(context.Context) string         { return "" }
func memory(context.Context) (uint64, uint64) { return 0, 0 }
func diskFree(string) (uint64, bool)          { return 0, false }
func osGPUs(context.Context, Hardware) []GPU  { return nil }
