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
	"path/filepath"
	"strings"
	"sync"
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

// hookEvents is every githooks(5) event, each switched off by name because core.hooksPath does not stop a hook.<name>.command.
var hookEvents = strings.Fields(`applypatch-msg pre-applypatch post-applypatch pre-commit pre-merge-commit
prepare-commit-msg commit-msg post-commit pre-rebase post-checkout post-merge pre-push pre-receive update
proc-receive post-receive post-update reference-transaction push-to-checkout pre-auto-gc post-rewrite
sendemail-validate fsmonitor-watchman p4-changelist p4-prepare-changelist p4-post-changelist p4-pre-submit
post-index-change`)

// Isolation is the config Command puts first, so a worktree its card wrote runs no fsmonitor and no hook as atrium.
func Isolation(o Opts) ([]string, error) {
	kv := []string{"core.fsmonitor=false", "core.hooksPath=/dev/null"}
	for _, e := range hookEvents {
		kv = append(kv, "hook."+e+".enabled=false")
	}
	names, err := hookNames(o)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		kv = append(kv, "hook."+n+".enabled=false")
	}
	return kv, nil
}

// hookNames is every hook.<name> the config o reads defines, switched off by name because git 2.54 has no
// hook.<event>.enabled. A repeat for the same repository and environment costs a stat of each config file git read.
func hookNames(o Opts) ([]string, error) {
	dir := o.Dir
	if dir == "" {
		dir = "."
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	env := o.environ()
	if env == nil {
		env = os.Environ()
	}
	key := dir + "\x00" + o.GitDir
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k = strings.ToUpper(k); strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "USERPROFILE" || k == "XDG_CONFIG_HOME" {
			key += "\x00" + kv
		}
	}
	hookMu.Lock()
	r, ok := hookSeen[key]
	hookMu.Unlock()
	if ok && r.fresh() {
		return r.names, nil
	}
	r, err = readHookNames(o, dir, env)
	if err != nil {
		return nil, err
	}
	if r.files != nil {
		hookMu.Lock()
		hookSeen[key] = r
		hookMu.Unlock()
	}
	return r.names, nil
}

var (
	hookMu   sync.Mutex
	hookSeen = map[string]hookRead{}
)

// hookRead is one read of the hook names, with the stamp of every file that could change them. Nil files is a
// read that must not be kept.
type hookRead struct {
	names []string
	files map[string]stamp
}

type stamp struct {
	mod  int64
	size int64
	ok   bool
}

func stampOf(p string) stamp {
	if !filepath.IsAbs(p) || strings.Contains(p, "..") {
		return stamp{}
	}
	fi, err := os.Stat(p)
	if err != nil {
		return stamp{}
	}
	return stamp{fi.ModTime().UnixNano(), fi.Size(), true}
}

func (r hookRead) fresh() bool {
	for p, s := range r.files {
		if stampOf(p) != s {
			return false
		}
	}
	return true
}

func envGet(env []string, name string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, name) {
			v = val
		}
	}
	return v
}

// readHookNames runs git for the hook names and the files they came from. The read is kept only when the
// repository was found, no includeIf follows the branch, and every file is older than the read by two seconds, so
// a write during the read cannot hide behind its stamp.
func readHookNames(o Opts, dir string, env []string) (hookRead, error) {
	start := time.Now()
	out, err := hookGit(o, "config", "-z", "--show-scope", "--show-origin", "--list")
	if err != nil {
		return hookRead{}, fmt.Errorf("git config: reading the hooks to switch off: %w", err)
	}
	home := envGet(env, "HOME")
	if home == "" {
		home = envGet(env, "USERPROFILE")
	}
	paths := []string{filepath.Join(dir, ".git")}
	if g := envGet(env, "GIT_CONFIG_GLOBAL"); g != "" {
		paths = append(paths, g)
	} else {
		xdg := envGet(env, "XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		paths = append(paths, filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig"))
	}
	if s := envGet(env, "GIT_CONFIG_SYSTEM"); s != "" {
		paths = append(paths, s)
	}
	var r hookRead
	repo, keep := false, true
	f := strings.Split(string(out), "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		scope, origin := f[i], f[i+1]
		k, v, _ := strings.Cut(f[i+2], "\n")
		repo = repo || scope == "local" || scope == "worktree"
		keep = keep && !strings.HasPrefix(k, "includeif.onbranch:")
		if p, ok := strings.CutPrefix(origin, "file:"); ok {
			if !filepath.IsAbs(p) {
				p = filepath.Join(dir, p)
			}
			paths = append(paths, p)
			if k == "include.path" || strings.HasPrefix(k, "includeif.") && strings.HasSuffix(k, ".path") {
				if rest, ok := strings.CutPrefix(v, "~/"); ok {
					v = filepath.Join(home, rest)
				} else if !filepath.IsAbs(v) {
					v = filepath.Join(filepath.Dir(p), v)
				}
				paths = append(paths, v)
			}
		}
		if k == "extensions.worktreeconfig" {
			wt, err := hookGit(o, "rev-parse", "--path-format=absolute", "--git-path", "config.worktree")
			keep = keep && err == nil
			paths = append(paths, strings.TrimSpace(string(wt)))
		}
		if !strings.HasPrefix(k, "hook.") || !strings.HasSuffix(k, ".command") && !strings.HasSuffix(k, ".event") {
			continue
		}
		n := k[len("hook."):strings.LastIndexByte(k, '.')]
		if n == "" {
			continue
		}
		if strings.ContainsAny(n, "=\n") {
			return hookRead{}, fmt.Errorf("git config: hook %q cannot be switched off by name", n)
		}
		r.names = append(r.names, n)
	}
	if !repo || !keep {
		return r, nil
	}
	r.files = map[string]stamp{}
	for _, p := range paths {
		s := stampOf(p)
		if s.ok && s.mod > start.Add(-2*time.Second).UnixNano() {
			return hookRead{names: r.names}, nil
		}
		r.files[p] = s
	}
	return r, nil
}

func hookGit(o Opts, args ...string) ([]byte, error) {
	if o.GitDir != "" {
		args = append([]string{"--git-dir=" + o.GitDir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = o.Dir
	cmd.Env = o.environ()
	nowindow.Hide(cmd)
	return cmd.Output()
}

// environ is the environment git runs with, nil for the process's own.
func (o Opts) environ() []string {
	switch {
	case o.CleanEnv:
		return append([]string{}, o.Env...)
	case o.Env != nil:
		return append(os.Environ(), o.Env...)
	}
	return nil
}

// Command is the git child for op, not started, for a caller that sets its own streams or cancel.
func Command(ctx context.Context, o Opts, op Op) (*exec.Cmd, error) {
	iso, err := Isolation(o)
	if err != nil {
		return nil, err
	}
	o.Config = append(iso, o.Config...)
	args, err := Argv(o, op)
	if err != nil {
		return nil, err
	}
	// codeql[go/command-injection] the program is the literal git, and args are built by Argv, which refuses a value that reads as an option
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
