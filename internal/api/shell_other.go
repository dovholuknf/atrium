//go:build !windows

package api

import "os"

// shellName is the shell to offer as a bare-terminal runner: whatever the
// operator's own shell is, falling back to one every system has.
func shellName() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}
