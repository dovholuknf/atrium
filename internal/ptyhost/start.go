package ptyhost

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/detach"
)

// StartOptions for Start. The zero value is what the daemon wants.
type StartOptions struct {
	// Exe is the installed binary the host is copied from. Default: this process's own.
	Exe string
	// CopyDir is where atrium.ptyhost[.exe] is put. Default: next to Exe.
	CopyDir string
	// IdleExit is passed to the host. Default: DefaultIdleExit.
	IdleExit time.Duration
	// Build is the build string the host reports.
	Build string
	// Args replaces the subcommand and flags handed to the copy. A test uses it to re-exec its own binary. Default:
	// `ptyhost --state-dir <dir> ...`.
	Args []string
}

// Start launches a host for stateDir, detached, and returns without waiting for it to listen: Dial retries.
//
// Detached with internal/detach: DETACHED_PROCESS, CREATE_NEW_PROCESS_GROUP and CREATE_BREAKAWAY_FROM_JOB with the
// retry without breakaway on Windows, and Setsid elsewhere (design 3.3). It runs from a COPY, `atrium.ptyhost` next
// to the installed binary, so the rename-aside swap of a new atrium never touches the image the host runs from.
// Callers should Probe first: Start on a state dir that already has a host makes a second process that exits at
// once, refused by the listener.
//
// The host's output goes to `ptyhost.log` in stateDir.
func Start(stateDir string, o StartOptions) (*os.Process, error) {
	exe := o.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	dir := o.CopyDir
	if dir == "" {
		dir = filepath.Dir(exe)
	}
	copyPath, err := copyForHost(exe, dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(filepath.Join(stateDir, "ptyhost.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer logf.Close()
	args := o.Args
	if args == nil {
		args = []string{"ptyhost", "--state-dir", stateDir}
		if o.IdleExit > 0 {
			args = append(args, "--idle-exit", o.IdleExit.String())
		}
		if o.Build != "" {
			args = append(args, "--build", o.Build)
		}
	}
	return detach.Start(copyPath, args, logf)
}

// copyForHost puts a copy of exe in dir as atrium.ptyhost (with exe's extension) and returns its path. A copy that
// is already there and current is left alone, which also covers a copy a running host holds open on Windows. A copy
// that cannot be replaced is used as it is when one exists, since a stale copy still speaks a protocol the host
// answers `probe` for.
func copyForHost(exe, dir string) (string, error) {
	dst := filepath.Join(dir, "atrium.ptyhost"+filepath.Ext(exe))
	si, err := os.Stat(exe)
	if err != nil {
		return "", err
	}
	if di, err := os.Stat(dst); err == nil && di.Size() == si.Size() && !di.ModTime().Before(si.ModTime()) {
		return dst, nil
	}
	tmp := dst + ".new"
	if err := copyFile(exe, tmp); err != nil {
		_ = os.Remove(tmp)
		if _, statErr := os.Stat(dst); statErr == nil {
			return dst, nil
		}
		return "", fmt.Errorf("copy %s for the pty host: %w", exe, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		if _, statErr := os.Stat(dst); statErr == nil {
			return dst, nil
		}
		return "", fmt.Errorf("install the pty host copy: %w", err)
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
