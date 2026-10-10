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
	name := "ptyhost-" + hash + ".sock"
	if addr := filepath.Join(abs, name); len(addr) <= maxSocketPath {
		return addr
	}
	return filepath.Join(shortSocketDir(), name)
}

// maxSocketPath is the longest unix socket path that binds on every platform here. sun_path is 104 bytes on macOS
// (108 on Linux) and includes a NUL, so 103 is the most that fits.
const maxSocketPath = 103

// shortSocketDir is where a socket goes when the state dir's own path is too long for sun_path, for example under
// a macOS TMPDIR (/var/folders/...) or a long home. /tmp exists on macOS and Linux and is short. The directory is
// per user, and listenChannel creates it at mode 0700.
func shortSocketDir() string {
	return filepath.Join("/tmp", "atrium-"+userIdentity())
}

// prepareSocketDir makes sure the directory of a fallback socket exists, is ours, and is private. A path inside the
// state dir is left alone. A path that is still too long gets an error that names the limit.
func prepareSocketDir(addr string) error {
	if len(addr) > maxSocketPath {
		return fmt.Errorf("pty host socket path %q is %d bytes, over the %d-byte unix socket limit (sun_path is 104 on macOS)",
			addr, len(addr), maxSocketPath)
	}
	dir := filepath.Dir(addr)
	if dir != shortSocketDir() {
		return nil
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is not a private directory owned by this user, refusing to put the pty host socket there", dir)
	}
	return nil
}

// listenChannel binds the unix socket at mode 0600. A socket file left by a crashed host refuses connections and
// is removed. One that answers is a live host, and this is refused rather than stolen.
func listenChannel(addr string) (net.Listener, error) {
	if err := prepareSocketDir(addr); err != nil {
		return nil, err
	}
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
