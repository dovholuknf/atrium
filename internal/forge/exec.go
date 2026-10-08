package forge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Exec is a Runner for a process that has none of its own: the hub. The room's PR runner keeps its own, which starts
// from the room's child environment. It bounds the time and the output, and git never stops to ask for a credential.
// Prepare, when set, is given the command before it starts, so a caller can hide a console on Windows.
func Exec(prepare func(*exec.Cmd)) Runner {
	return func(ctx context.Context, c Cmd) ([]byte, error) {
		if c.Timeout <= 0 {
			c.Timeout = 2 * time.Minute
		}
		if c.Limit <= 0 {
			c.Limit = 4 << 20
		}
		runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
		defer cancel()
		cmd := exec.CommandContext(runCtx, c.Name, c.Args...)
		cmd.Dir = c.Dir
		if prepare != nil {
			prepare(cmd)
		}
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		var out, errOut bytes.Buffer
		var tail *TailWriter
		if c.Tail > 0 {
			tail = &TailWriter{Lines: c.Tail, Bytes: c.Limit}
			cmd.Stdout = tail
		} else {
			cmd.Stdout = &capped{w: &out, left: c.Limit + 1}
		}
		cmd.Stderr = &capped{w: &errOut, left: 8 << 10}
		err := cmd.Run()
		label := c.Name
		if len(c.Args) > 1 {
			label += " " + c.Args[0] + " " + c.Args[1]
		}
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case runCtx.Err() == context.DeadlineExceeded:
			return nil, fmt.Errorf("%s took longer than %s and was stopped", label, c.Timeout)
		case out.Len() > c.Limit:
			return nil, fmt.Errorf("%s printed more than %d bytes and was stopped", label, c.Limit)
		case err != nil:
			if s := strings.TrimSpace(errOut.String()); s != "" {
				if i := strings.IndexByte(s, '\n'); i >= 0 {
					s = s[:i]
				}
				return nil, fmt.Errorf("%s: %s", label, s)
			}
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if tail != nil {
			text, _, _ := tail.Result()
			return []byte(text), nil
		}
		return out.Bytes(), nil
	}
}

// capped keeps at most left bytes and says it took them all, so the command is not stopped by a broken pipe.
type capped struct {
	w    io.Writer
	left int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.left > 0 {
		n := len(p)
		if n > c.left {
			n = c.left
		}
		_, _ = c.w.Write(p[:n])
		c.left -= n
	}
	return len(p), nil
}
