//go:build !windows

package cli

import "os"

func runsAsAdmin() bool { return os.Geteuid() == 0 }
