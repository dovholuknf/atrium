//go:build !windows

package cli

import "os"

func setStderrHandle(*os.File) {}
