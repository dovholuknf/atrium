//go:build windows

package api

import "os/exec"

// shellName is the shell to offer as a bare-terminal runner.
//
// pwsh when it is installed, since a machine that has it wants it, falling back
// to the one every Windows install has.
func shellName() string {
	if _, err := exec.LookPath("pwsh.exe"); err == nil {
		return "pwsh.exe"
	}
	return "powershell.exe"
}
