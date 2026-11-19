package cli

import "golang.org/x/sys/windows"

func runsAsAdmin() bool {
	if windows.GetCurrentProcessToken().IsElevated() {
		return true
	}
	sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	// Token 0 is the thread's own: CheckTokenMembership refuses a primary token.
	member, err := windows.Token(0).IsMember(sid)
	return err == nil && member
}
