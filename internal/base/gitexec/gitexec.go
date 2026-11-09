// Package gitexec is the one place atrium starts git. Each git operation atrium uses is a struct, and a value
// that names a ref, branch, remote or revision is refused before git runs when it is empty, holds a NUL or begins
// with '-', so no value can be read as an option.
package gitexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// Op is one git operation. Only the structs in this package are Ops.
type Op interface {
	build(b *builder)
}

// Opts is how git runs: where, with what environment and input, and what output comes back.
type Opts struct {
	// Dir is the directory git runs in. Empty is the process's own.
	Dir string
	// GitDir is passed as --git-dir.
	GitDir string
	// Config is each "key=value" passed as -c for this command alone.
	Config []string
	// Env is added to os.Environ(). Nil leaves the environment as it is.
	Env []string
	// CleanEnv replaces the environment with Env alone.
	CleanEnv bool
	// Stdin is fed to git. Nil is no input.
	Stdin []byte
	// Combined answers stdout and stderr together.
	Combined bool
	// Stderr receives stderr when Combined is false.
	Stderr io.Writer
	// Visible leaves the console window alone, for the operator's own CLI.
	Visible bool
	// WaitDelay is exec.Cmd.WaitDelay.
	WaitDelay time.Duration
}

// ArgError is a value refused before git ran.
type ArgError struct {
	Op     string
	Field  string
	Value  string
	Reason string
}

func (e *ArgError) Error() string {
	return fmt.Sprintf("git %s: %s %q refused: %s", e.Op, e.Field, e.Value, e.Reason)
}

// Argv is the argv after "git" that op and o make, or the *ArgError that refused it.
func Argv(o Opts, op Op) ([]string, error) {
	b := &builder{}
	for _, kv := range o.Config {
		k, _, _ := strings.Cut(kv, "=")
		b.ref("config key", k)
		b.text("config", kv)
		b.add("-c", kv)
	}
	if o.GitDir != "" {
		b.text("git-dir", o.GitDir)
		b.add("--git-dir=" + o.GitDir)
	}
	op.build(b)
	if b.err != nil {
		return nil, b.err
	}
	return b.args, nil
}

// Command is the git child for op, not started, for a caller that sets its own streams or cancel.
func Command(ctx context.Context, o Opts, op Op) (*exec.Cmd, error) {
	args, err := Argv(o, op)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = o.Dir
	switch {
	case o.CleanEnv:
		cmd.Env = append([]string{}, o.Env...)
	case o.Env != nil:
		cmd.Env = append(os.Environ(), o.Env...)
	}
	if o.Stdin != nil {
		cmd.Stdin = bytes.NewReader(o.Stdin)
	}
	cmd.WaitDelay = o.WaitDelay
	if !o.Visible {
		nowindow.Hide(cmd)
	}
	return cmd, nil
}

// Run runs op and answers its stdout, or stdout and stderr together when o.Combined. A failure of git is the
// *exec.ExitError exec gives, a refused value an *ArgError.
func Run(ctx context.Context, o Opts, op Op) ([]byte, error) {
	cmd, err := Command(ctx, o, op)
	if err != nil {
		return nil, err
	}
	if o.Combined {
		return cmd.CombinedOutput()
	}
	cmd.Stderr = o.Stderr
	return cmd.Output()
}

type builder struct {
	op   string
	args []string
	err  error
}

func (b *builder) add(a ...string) { b.args = append(b.args, a...) }

func (b *builder) sub(a ...string) {
	if b.op == "" {
		b.op = strings.Join(a, " ")
	}
	b.add(a...)
}

func (b *builder) refuse(field, v, why string) {
	if b.err == nil {
		op := b.op
		if op == "" {
			op = field
		}
		b.err = &ArgError{Op: op, Field: field, Value: v, Reason: why}
	}
}

func (b *builder) text(field, v string) {
	if strings.IndexByte(v, 0) >= 0 {
		b.refuse(field, v, "it holds a NUL")
	}
}

// ref checks a ref, branch, remote, revision or key: present, no NUL, no leading '-'.
func (b *builder) ref(field, v string) string {
	switch {
	case v == "":
		b.refuse(field, v, "it is empty")
	case strings.HasPrefix(v, "-"):
		b.refuse(field, v, "it begins with '-'")
	}
	b.text(field, v)
	return v
}

func (b *builder) refs(field string, vs []string) []string {
	for _, v := range vs {
		b.ref(field, v)
	}
	return vs
}

// path checks a path, which always follows "--" or --end-of-options: present and no NUL.
func (b *builder) path(field, v string) string {
	if v == "" {
		b.refuse(field, v, "it is empty")
	}
	b.text(field, v)
	return v
}

func (b *builder) paths(field string, vs []string) []string {
	for _, v := range vs {
		b.path(field, v)
	}
	return vs
}

// value checks the value of an option given as --opt=value: no NUL.
func (b *builder) value(field, v string) string {
	b.text(field, v)
	return v
}

func (b *builder) oneOf(field, v string, ok ...string) string {
	for _, o := range ok {
		if v == o {
			return v
		}
	}
	b.refuse(field, v, "it is not one of "+strings.Join(ok, ", "))
	return v
}

func (b *builder) flag(on bool, f string) {
	if on {
		b.add(f)
	}
}

const eoo = "--end-of-options"

// Name is op's git subcommand, such as "rev-parse", for a message.
func Name(op Op) string {
	b := &builder{}
	op.build(b)
	name, _, _ := strings.Cut(b.op, " ")
	return name
}
