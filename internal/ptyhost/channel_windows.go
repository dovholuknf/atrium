//go:build windows

package ptyhost

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// userIdentity is the current user's SID. It goes into the address hash, so two users never share a name, and
// into the pipe's DACL, so only this one can connect.
func userIdentity() string {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "unknown"
	}
	return u.User.Sid.String()
}

func channelAddress(_ string, hash string) string { return `\\.\pipe\atrium-ptyhost-` + hash }

// PipeSDDL is the security descriptor the host's pipe is created with: a protected DACL with one entry, GENERIC_ALL
// to the current user's SID. Nobody else, not Administrators, not SYSTEM, and no inheritance. go-winio's own
// default would grant SYSTEM and the built-in administrators and is never used (design G2, G6).
func PipeSDDL() string { return fmt.Sprintf("D:P(A;;GA;;;%s)", userIdentity()) }

// listenChannel makes the pipe. go-winio creates it with FILE_FLAG_FIRST_PIPE_INSTANCE, so nothing can already be
// squatting the name, with PIPE_REJECT_REMOTE_CLIENTS, and it keeps one instance waiting at all times, so a
// reconnect never finds none. Overlapped I/O is what makes a full duplex pipe work: a handle without it serialises
// a blocked ReadFile against a WriteFile (the spike's finding).
func listenChannel(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, &winio.PipeConfig{
		SecurityDescriptor: PipeSDDL(),
		MessageMode:        false,
		InputBufferSize:    1 << 16,
		OutputBufferSize:   1 << 16,
	})
}

func dialChannel(addr string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(addr, &timeout)
}

// errNoHost says whether a dial failed because nobody is listening yet, which is worth retrying.
func errNoHost(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PIPE_BUSY)
}
