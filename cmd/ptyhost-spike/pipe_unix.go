//go:build !windows

package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func sockPath(name string) string {
	if strings.ContainsRune(name, '/') {
		return name
	}
	return filepath.Join(os.TempDir(), "atrium-spike-"+name+".sock")
}

type sockListener struct{ l net.Listener }

// listen: a unix socket at mode 0600. The umask is tightened around the bind so there is no window at the
// default mode, which chmod-after-bind would leave.
func listen(name string) (*sockListener, error) {
	p := sockPath(name)
	_ = os.Remove(p)
	old := umaskSet(0o177)
	l, err := net.Listen("unix", p)
	umaskSet(old)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(p, 0o600)
	logf("socket %s mode 0600", p)
	return &sockListener{l}, nil
}

func (s *sockListener) Accept() (io.ReadWriteCloser, error) { return s.l.Accept() }

func dial(name string) (io.ReadWriteCloser, error) { return net.Dial("unix", sockPath(name)) }
