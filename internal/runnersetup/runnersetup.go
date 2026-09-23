// Package runnersetup finds and fixes what stops a runner working in the
// directories atrium launches it in: a folder it does not trust, a sign-in that
// was never done, hooks that were never wired.
//
// One adapter per runner, in one package, for the reason `claudeconf/target.go`
// gives for hook targets: everything below "which file, which checks" is shared,
// and two packages would be two places for the backup and parse rules to drift.
// See docs/runner-setup-design.md, which also has the checklist for adding one.
package runnersetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// State is what one check found.
type State string

const (
	OK   State = "ok"
	Fail State = "fail"
	// Warn is "atrium cannot tell", or "this works and breaks a rule atrium
	// keeps", such as a credential sitting in a harness row.
	Warn State = "warn"
	NA   State = "n/a"
)

// How a failing check is fixed. Every failing check carries exactly one.
const (
	// FixApply means atrium edits the runner's config itself, with a backup.
	FixApply = "apply"
	// FixExplain means atrium prints the command and a human runs it. Always
	// the answer for a credential and for a "no" somebody wrote down.
	FixExplain = "explain"
)

// Env is everything a check may look at. Checks read files under Home and
// never call os.UserHomeDir, so a test can point them at a temp directory.
type Env struct {
	// Home is the home directory of the account the runner runs as.
	Home string
	// Getenv is the environment the runner inherits. Nil means os.Getenv.
	Getenv func(string) string
	// RowEnv is the harness row's own env, which the runner also gets and
	// which wins over Getenv. Kept apart because a credential here is a
	// credential atrium is holding.
	RowEnv map[string]string
	// Prepare says the row runs a prepare command, whose environment atrium
	// cannot see without running it.
	Prepare bool
	// Roots are the workspace roots on this room: see WorkspaceRoots.
	Roots []string
	// Exe is the runner binary as launching resolves it, "" when not found.
	Exe string
	// AtriumExe is the path hooks are written with.
	AtriumExe string
	// GOOS is runtime.GOOS unless a test says otherwise.
	GOOS string
}

// lookup reads a variable the runner will see, row env first.
func (e Env) lookup(key string) string {
	if v, ok := e.RowEnv[key]; ok {
		return v
	}
	return e.inherited(key)
}

// inherited reads a variable from the environment alone, ignoring the row.
func (e Env) inherited(key string) string {
	if e.Getenv == nil {
		return os.Getenv(key)
	}
	return e.Getenv(key)
}

func (e Env) goos() string {
	if e.GOOS == "" {
		return runtime.GOOS
	}
	return e.GOOS
}

// Result is one check's answer, shaped for the board.
type Result struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  State  `json:"state"`
	Detail string `json:"detail"`
	// Fix is FixApply or FixExplain on a check that is not ok, empty otherwise.
	Fix string `json:"fix,omitempty"`
	// FixLabel is what the fix button says.
	FixLabel string `json:"fix_label,omitempty"`
	// Command is what a human runs, for an explained fix.
	Command string `json:"command,omitempty"`
	// Path is the config file this check reads, and an applied fix edits.
	Path string `json:"path,omitempty"`
	// Targets are what an applied fix can be pointed at, one button each.
	// Empty means the fix takes no target.
	Targets []string `json:"targets,omitempty"`
}

// Applied is what a fix did.
type Applied struct {
	Changed bool   `json:"changed"`
	Path    string `json:"path,omitempty"`
	Backup  string `json:"backup,omitempty"`
}

// Check is one named question about a runner's setup.
type Check struct {
	ID    string
	Label string
	Run   func(Env) Result
	// Apply fixes it. Nil means this check is only ever explained.
	Apply func(env Env, target string) (Applied, error)
}

// Adapter is one runner.
type Adapter struct {
	ID    string
	Label string
	// Cmds are the command leaf names that are this runner.
	Cmds []string
	// Package is the npm package the version is read from, or empty.
	Package string
	Checks  []Check
	// Launch runs before the runner starts in cwd. Nil means nothing to do.
	// Its error is logged by the caller and never fails the launch.
	Launch func(env Env, cwd string) (note string, err error)
}

// Adapters are the runners atrium knows how to set up.
var Adapters = []*Adapter{Gemini, Claude}

// For finds the adapter for a harness row, by its command's leaf name, or nil.
//
// By command rather than row id, since the id is whatever was typed when the
// row was made. The board's `hookTargetFor` uses the same rule.
func For(h *store.Harness) *Adapter {
	if h == nil {
		return nil
	}
	for _, cmd := range []string{h.Cmd, h.BinPath} {
		leaf := cmdLeaf(cmd)
		if leaf == "" {
			continue
		}
		for _, a := range Adapters {
			for _, c := range a.Cmds {
				if leaf == c {
					return a
				}
			}
		}
	}
	return nil
}

func cmdLeaf(cmd string) string {
	cmd = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(cmd, `\`, "/")))
	if i := strings.LastIndex(cmd, "/"); i >= 0 {
		cmd = cmd[i+1:]
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		cmd = strings.TrimSuffix(cmd, ext)
	}
	return cmd
}

// Report is everything the board shows for one row.
type Report struct {
	Adapter   string   `json:"adapter"`
	Label     string   `json:"label"`
	Installed bool     `json:"installed"`
	Exe       string   `json:"exe,omitempty"`
	Version   string   `json:"version,omitempty"`
	Checks    []Result `json:"checks"`
	// Failing counts the checks that are fail, which is the number on the chip.
	Failing int `json:"failing"`
}

// Inspect runs every check. Read only.
func Inspect(a *Adapter, env Env) Report {
	rep := Report{Adapter: a.ID, Label: a.Label, Installed: env.Exe != "", Exe: env.Exe,
		Version: InstalledVersion(env.Exe, a.Package)}
	for _, c := range a.Checks {
		r := c.Run(env)
		r.ID, r.Label = c.ID, c.Label
		if r.State == OK || r.State == NA {
			r.Fix, r.Command, r.Targets = "", "", nil
		}
		if r.State == Fail {
			rep.Failing++
		}
		rep.Checks = append(rep.Checks, r)
	}
	return rep
}

// ErrExplainOnly is a fix request for a check atrium only explains.
var ErrExplainOnly = errors.New("atrium does not apply this fix itself. run the command it shows")

// ErrNothingToFix is a fix request for a check that is not failing, or a
// target that is not one the check offered.
var ErrNothingToFix = errors.New("that is not something this check offers to fix")

// Fix applies one check's fix. The check is run first and the target must be
// one it offered, so a request cannot point an apply at an arbitrary path.
func Fix(a *Adapter, env Env, checkID, target string) (Applied, error) {
	for _, c := range a.Checks {
		if c.ID != checkID {
			continue
		}
		if c.Apply == nil {
			return Applied{}, ErrExplainOnly
		}
		r := c.Run(env)
		if r.Fix != FixApply {
			return Applied{}, ErrNothingToFix
		}
		if len(r.Targets) > 0 && !containsPath(r.Targets, target) {
			return Applied{}, ErrNothingToFix
		}
		return c.Apply(env, target)
	}
	return Applied{}, ErrNothingToFix
}

func containsPath(list []string, p string) bool {
	for _, x := range list {
		if strings.EqualFold(filepath.ToSlash(x), filepath.ToSlash(p)) {
			return true
		}
	}
	return false
}

// WorkspaceRoots are the directories the operator has said their work lives
// in: every enabled provider's root, and its worktree root when worktrees are
// on.
//
// Not the directory picker's roots. Those default to the home directory, and
// trusting home would trust everything the operator owns.
func WorkspaceRoots(ps []*store.Provider) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	for _, p := range ps {
		if p == nil || !p.Enabled {
			continue
		}
		add(p.Root)
		if p.Worktrees {
			add(p.WorktreeRoot)
		}
	}
	return out
}

// InstalledVersion reads the version of an installed npm package WITHOUT
// RUNNING IT.
//
// npm puts a launcher on PATH and the package beside it, in one of two shapes
// depending on the platform:
//
//	<prefix>/claude.cmd          <prefix>/node_modules/<pkg>/package.json
//	<prefix>/bin/claude          <prefix>/lib/node_modules/<pkg>/package.json
//
// Windows uses the first, unix prefixes and every node version manager use the
// second. Both are tried from the resolved launcher rather than by asking npm
// where its root is, because `npm root -g` is another process and the thing
// this function exists to avoid is starting processes.
//
// Empty when it cannot be worked out, which is a normal answer: a runner
// installed some other way has a version atrium has no business guessing at.
func InstalledVersion(exePath, pkg string) string {
	if exePath == "" || pkg == "" {
		return ""
	}
	dir := filepath.Dir(exePath)
	parts := strings.Split(pkg, "/")
	candidates := []string{
		filepath.Join(append([]string{dir, "node_modules"}, parts...)...),
		filepath.Join(append([]string{dir, "..", "lib", "node_modules"}, parts...)...),
		filepath.Join(append([]string{dir, "..", "node_modules"}, parts...)...),
	}
	for _, c := range candidates {
		b, err := os.ReadFile(filepath.Join(c, "package.json"))
		if err != nil {
			continue
		}
		var meta struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			continue
		}
		if v := strings.TrimSpace(meta.Version); v != "" {
			return v
		}
	}
	return ""
}

// exists reports whether a file is there.
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
