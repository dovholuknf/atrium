//go:build integration && windows

package nowindow

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideKeepsExistingFlags(t *testing.T) {
	cmd := exec.Command("git", "version")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	Hide(cmd)
	a := cmd.SysProcAttr
	if !a.HideWindow || a.CreationFlags&CreateNoWindow == 0 || a.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("SysProcAttr = %+v", a)
	}
}
