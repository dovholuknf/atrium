//go:build integration && windows

package ptyhost

import (
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The pipe's DACL is asserted by reading it back off the live pipe, not by trying a second account: exactly one
// ACE, GENERIC_ALL, for the current user's SID, protected so nothing is inherited.
func TestPipeDACLGrantsTheCurrentUserOnly(t *testing.T) {
	_, addr := testHost(t, Options{})
	p, _ := windows.UTF16PtrFromString(addr)
	var h windows.Handle
	var err error
	for i := 0; i < 100; i++ {
		h, err = windows.CreateFile(p, windows.READ_CONTROL|windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, 0, 0)
		if err != windows.ERROR_PIPE_BUSY {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("open the pipe: %v", err)
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read the descriptor back: %v", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("no DACL: %v", err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("%d ACEs in %s, want exactly one", dacl.AceCount, sd.String())
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	me := userIdentity()
	if got := (*windows.SID)(unsafePointer(&ace.SidStart)).String(); got != me {
		t.Fatalf("the one ACE is for %s, want the current user %s", got, me)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != windows.GENERIC_ALL && ace.Mask != 0x1f01ff {
		t.Fatalf("ACE type %d mask %#x", ace.Header.AceType, ace.Mask)
	}
	ctl, _, _ := sd.Control()
	if ctl&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("the DACL is not protected: %s", sd.String())
	}
	if want := "D:P(A;;GA;;;" + me + ")"; PipeSDDL() != want {
		t.Fatalf("PipeSDDL %q, want %q", PipeSDDL(), want)
	}
}
