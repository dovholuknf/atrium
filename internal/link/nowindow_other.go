//go:build !windows

package link

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
