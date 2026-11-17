//go:build !windows

package cli

import "path/filepath"

func swapRealPath(p string) (string, error) { return filepath.EvalSymlinks(p) }
