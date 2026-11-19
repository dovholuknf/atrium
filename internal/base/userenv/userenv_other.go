//go:build !windows

package userenv

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

func current() map[string]string { return nil }

// freshPath is the PATH a login shell sets up now, where an installer's profile line lands.
func freshPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sh, "-lc", `printf %s "$PATH"`).Output()
	if err != nil {
		return ""
	}
	// A profile that prints leaves its lines ahead of the PATH.
	s := string(out)
	return s[strings.LastIndex(s, "\n")+1:]
}
