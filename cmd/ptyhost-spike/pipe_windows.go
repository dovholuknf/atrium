//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A named pipe by hand on x/sys/windows, so the spike adds no dependency. Two things the design does not say
// and this found: a pipe handle opened WITHOUT FILE_FLAG_OVERLAPPED serialises every call on it, so a blocked
// ReadFile stops a WriteFile on the same handle, which is fatal for "one connection, a control stream and
// pty streams multiplexed". Full duplex needs overlapped I/O (go-winio does this, and stage 1 should use it).
// Second, the pipe name is a flat global namespace with no directory, so "derived from the state dir" has to
// mean a hash of it.

func pipePath(name string) string { return `\\.\pipe\atrium-spike-` + name }

type pipeConn struct {
	h    windows.Handle
	once sync.Once
}

func (c *pipeConn) op(b []byte, read bool) (int, error) {
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(ev)
	ov := &windows.Overlapped{HEvent: ev}
	var n uint32
	if read {
		if len(b) == 0 {
			return 0, nil
		}
		err = windows.ReadFile(c.h, b, &n, ov)
	} else {
		err = windows.WriteFile(c.h, b, &n, ov)
	}
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		err = windows.GetOverlappedResult(c.h, ov, &n, true)
	}
	if err != nil {
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) ||
			errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
			return int(n), io.EOF
		}
		return int(n), err
	}
	return int(n), nil
}

func (c *pipeConn) Read(b []byte) (int, error) { return c.op(b, true) }
func (c *pipeConn) Write(b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		n, err := c.op(b, false)
		total += n
		if err != nil {
			return total, err
		}
		b = b[n:]
	}
	return total, nil
}
// Close is idempotent, and that is not optional. The first version closed twice (the serve loop's defer and the
// "a new daemon closes the older connection" path), and the second CloseHandle hit a handle value the runtime had
// already reused: "GetQueuedCompletionStatusEx failed (errno=735)", fatal error: netpoll failed, the whole host
// gone with every runner. Any host code that closes a handle from two paths has this hazard.
func (c *pipeConn) Close() error {
	var err error
	c.once.Do(func() {
		_ = windows.CancelIoEx(c.h, nil)
		windows.DisconnectNamedPipe(c.h)
		err = windows.CloseHandle(c.h)
	})
	return err
}

type pipeListener struct {
	path  string
	sa    *windows.SecurityAttributes
	first bool
}

// listen creates the pipe with a DACL granting GENERIC_ALL to the current user's SID and to nobody else, and
// FIRST_PIPE_INSTANCE so nothing can already be squatting the name.
func listen(name string) (*pipeListener, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)", u.User.Sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd, InheritHandle: 0}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	l := &pipeListener{path: pipePath(name), sa: sa, first: true}
	logf("pipe %s dacl %s", l.path, sddl)
	return l, nil
}

func (l *pipeListener) Accept() (io.ReadWriteCloser, error) {
	p, _ := windows.UTF16PtrFromString(l.path)
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if l.first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	h, err := windows.CreateNamedPipe(p, flags,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES, 1<<16, 1<<16, 0, l.sa)
	if err != nil {
		return nil, err
	}
	l.first = false
	ev, _ := windows.CreateEvent(nil, 1, 0, nil)
	defer windows.CloseHandle(ev)
	ov := &windows.Overlapped{HEvent: ev}
	err = windows.ConnectNamedPipe(h, ov)
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		var n uint32
		err = windows.GetOverlappedResult(h, ov, &n, true)
	}
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		windows.CloseHandle(h)
		return nil, err
	}
	return &pipeConn{h: h}, nil
}

func dial(name string) (io.ReadWriteCloser, error) {
	p, _ := windows.UTF16PtrFromString(pipePath(name))
	var last error
	for i := 0; i < 50; i++ {
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_ANONYMOUS, 0)
		if err == nil {
			return &pipeConn{h: h}, nil
		}
		last = err
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			break
		}
		time.Sleep(100 * time.Millisecond) // the window between two Accepts
	}
	return nil, last
}
