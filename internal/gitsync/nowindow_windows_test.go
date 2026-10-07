//go:build windows

package gitsync

import (
	"context"
	"testing"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

// The hub and the rooms have no console, so a git child without CREATE_NO_WINDOW opens one
// on the operator's desktop every time gitsync runs it.
func TestGitCommandHasNoWindow(t *testing.T) {
	cmd := gitCommand(context.Background(), t.TempDir(), nil, []string{"version"})
	a := cmd.SysProcAttr
	if a == nil || !a.HideWindow || a.CreationFlags&nowindow.CreateNoWindow == 0 {
		t.Fatalf("git child would open a console window: SysProcAttr = %+v", a)
	}
}
