//go:build !windows

package ptyhost

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// userIdentity is the uid. It goes into the address hash so two users never share a name.
func userIdentity() string { return strconv.Itoa(os.Getuid()) }

func channelAddress(stateDir string, hash string) string {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		abs = stateDir
	}
	return filepath.Join(abs, "ptyhost-"+hash+".sock")
}

// listenChannel binds the unix socket at mode 0600. A socket file left by a crashed host refuses connections and
// is removed. One that answers is a live host, and this is refused rather than stolen.
func listenChannel(addr string) (net.Listener, error) {
	if _, err := os.Stat(addr); err == nil {
		if c, err := net.DialTimeout("unix", addr, time.Second); err == nil {
			_ = c.Close()
			return nil, fmt.Errorf("a pty host is already listening on %s", addr)
		}
		_ = os.Remove(addr)
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

func dialChannel(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", addr, timeout)
}

func errNoHost(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}
