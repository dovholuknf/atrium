package ptyhost

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/base/detach"
	"github.com/dovholuknf/atrium/internal/base/rolebin"
)

// StartOptions for Start. The zero value is what the daemon wants.
type StartOptions struct {
	// Exe is the installed binary the host is copied from. Default: rolebin.Installed().
	Exe string
	// CopyDir is where atrium-pty[.exe] is put. Default: next to Exe.
	CopyDir string
	// IdleExit is passed to the host. Default: DefaultIdleExit.
	IdleExit time.Duration
	// Build is the build string the host reports.
	Build string
	// Args replaces the subcommand and flags handed to the copy. A test uses it to re-exec its own binary. Default:
	// `ptyhost --state-dir <dir> ...`.
	Args []string
}

// Runs from a copy, `atrium-pty` beside the binary, so the rename-aside swap of a new atrium never touches the host's image.
func Start(stateDir string, o StartOptions) (*os.Process, error) {
	exe := o.Exe
	if exe == "" {
		if exe = rolebin.Installed(); exe == "" {
			return nil, errors.New("cannot find the atrium binary")
		}
	}
	dir := o.CopyDir
	if dir == "" {
		dir = filepath.Dir(exe)
	}
	copyPath := rolebin.EnsureIn("pty", exe, dir)
	rolebin.RemoveLegacy(dir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	logPath := filepath.Join(stateDir, "ptyhost.log")
	if o.Args != nil {
		return detach.Start(copyPath, o.Args, logPath)
	}
	// The host opens its own log, so a start through WMI needs no cmd.exe to redirect it and the host heads its tree.
	return detach.Start(copyPath, append(startArgs(stateDir, o), "--log", logPath), "")
}

func startArgs(stateDir string, o StartOptions) []string {
	if o.Args != nil {
		return o.Args
	}
	args := []string{"ptyhost", "--state-dir", stateDir}
	if o.IdleExit > 0 {
		args = append(args, "--idle-exit", o.IdleExit.String())
	}
	if o.Build != "" {
		args = append(args, "--build", o.Build)
	}
	return args
}
