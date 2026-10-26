//go:build windows

package cardproc

// InDir is nothing on Windows: a process there does not keep a working directory that StopTree misses.
func InDir(string) map[int]string { return map[int]string{} }

// StopInDir is nothing on Windows.
func StopInDir(string) map[int]string { return map[int]string{} }
