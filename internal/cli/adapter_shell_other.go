//go:build !windows

package cli

import "os/exec"

func rawShellLine(*exec.Cmd, []string) {}
