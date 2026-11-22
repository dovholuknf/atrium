// Package setupcheck is the one list of what a correct atrium setup is, read only. `atrium doctor` prints it and the
// room answers it at GET /v1/setup, so a later bring-up can ask the same questions and show the same answers.
//
// It never writes, installs or restarts anything. A check says what it found and, when it is not ok, who fixes it and
// how: atrium applies it, a person runs a command, or an administrator does.
package setupcheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// State is what one check found.
type State string

const (
	OK   State = "ok"
	Fail State = "fail"
	// Warn is "cannot tell", or "works and breaks a rule atrium keeps".
	Warn State = "warn"
	NA   State = "n/a"
)

// How a check that is not ok gets fixed.
const (
	FixApply   = "apply"
	FixExplain = "explain"
	// FixAdmin is a line only an administrator can run: the agent account cannot elevate.
	FixAdmin = "admin"
)

// Result is one check's answer, shaped like runnersetup's so the two read as one list.
type Result struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  State  `json:"state"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
	// Command is what a human runs, for a fix that has one.
	Command string `json:"command,omitempty"`
}

// Ran is one command's answer. TimedOut and a start failure both leave Code at -1.
type Ran struct {
	Out      string
	Code     int
	TimedOut bool
	Err      error
}

// Env is everything a check may look at, so a test points it at fixtures.
type Env struct {
	GOOS     string
	Getenv   func(string) string
	Run      func(timeout time.Duration, name string, args ...string) Ran
	LookPath func(string) (string, error)
	// Exe is the running binary's path.
	Exe func() (string, error)
}

func (e Env) goos() string {
	if e.GOOS == "" {
		return runtime.GOOS
	}
	return e.GOOS
}

func (e Env) getenv(k string) string {
	if e.Getenv == nil {
		return os.Getenv(k)
	}
	return e.Getenv(k)
}

func (e Env) run(timeout time.Duration, name string, args ...string) Ran {
	if e.Run != nil {
		return e.Run(timeout, name, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	r := Ran{Out: string(out)}
	switch {
	case ctx.Err() != nil:
		r.Code, r.TimedOut, r.Err = -1, true, ctx.Err()
	case err != nil:
		r.Code, r.Err = -1, err
		if ee, ok := err.(*exec.ExitError); ok {
			r.Code = ee.ExitCode()
		}
	}
	return r
}

func (e Env) lookPath(n string) (string, error) {
	if e.LookPath != nil {
		return e.LookPath(n)
	}
	return exec.LookPath(n)
}

func (e Env) exe() (string, error) {
	if e.Exe != nil {
		return e.Exe()
	}
	return os.Executable()
}

// Report is every row, and how many failed or warned.
type Report struct {
	OS      string   `json:"os"`
	Checks  []Result `json:"checks"`
	Failing int      `json:"failing"`
	Warning int      `json:"warning"`
}

// Add appends rows and counts them.
func (r *Report) Add(rows ...Result) {
	for _, c := range rows {
		switch c.State {
		case Fail:
			r.Failing++
		case Warn:
			r.Warning++
		}
		r.Checks = append(r.Checks, c)
	}
}

// Inspect runs the checks that need nothing but this machine. A runner's rows come from the caller, which knows the runners.
func Inspect(env Env) Report {
	rep := Report{OS: env.goos()}
	std, ded := account(env)
	rep.Add(std, ded, binaryRow(env))
	rep.Add(toolRows(env)...)
	return rep
}

func account(env Env) (Result, Result) {
	var lines []string
	kind := env.goos()
	var failed string
	if kind == "windows" {
		who := env.run(probeSudo, "whoami")
		g := env.run(probeSudo, "whoami", "/groups", "/fo", "csv", "/nh")
		sids, docker := WhoamiGroups(g.Out)
		switch {
		case g.Err != nil && len(sids) == 0:
			failed = "whoami /groups failed: " + g.Err.Error()
		case len(sids) == 0:
			failed = "whoami /groups listed no groups"
		default:
			lines = []string{"user=" + strings.TrimSpace(who.Out), "host=" + env.getenv("COMPUTERNAME"),
				"elevated=" + map[bool]string{true: "True", false: "False"}[Elevated(sids)], "sids=" + strings.Join(sids, ";"),
				"docker=" + itoa(docker)}
		}
	} else {
		if kind == "darwin" {
			kind = "mac"
		} else {
			kind = "linux"
		}
		one := func(args ...string) string { return strings.TrimSpace(env.run(probeSudo, args[0], args[1:]...).Out) }
		lines = []string{"user=" + one("id", "-un"), "uid=" + one("id", "-u"),
			"groups=" + strings.ReplaceAll(one("id", "-Gn"), " ", ","), "host=" + one("uname", "-n")}
		if _, err := env.lookPath("sudo"); err != nil {
			lines = append(lines, "sudo.rc=none")
		} else {
			s := env.run(probeSudo, "sudo", "-n", "-l")
			rc := itoa(s.Code)
			if s.TimedOut {
				rc = "timeout"
			}
			lines = append(lines, "sudo.rc="+rc,
				"sudo.nopasswd="+itoa(countLines(s.Out, "NOPASSWD:", "ALL")), "sudo.all="+itoa(countLines(s.Out, ")", "ALL")))
		}
	}
	f, bad := ParseFacts(lines)
	if bad != "" {
		failed = bad
	}
	return AccountRows(f, failed, kind, OperatorNames(env.getenv("ATRIUM_OPERATOR_ACCOUNT")))
}

// countLines counts sudo -n -l lines holding ALL as the command part: after NOPASSWD: for the first form, after a runas
// group for the second.
func countLines(out, after, word string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, " \r\t")
		if i := strings.LastIndex(l, after); i >= 0 && strings.TrimSpace(l[i+len(after):]) == word {
			n++
		}
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }

// binaryRow fails a binary that runs from a folder the next build or cleanup empties, since hooks and the self restart name it.
func binaryRow(env Env) Result {
	r := Result{ID: "binary.installed", Label: "atrium runs from an installed path"}
	exe, err := env.exe()
	if err != nil {
		r.State, r.Detail = Warn, "could not tell where atrium runs from: "+err.Error()
		return r
	}
	// backslashes are folded by hand: filepath.ToSlash leaves them alone off Windows.
	p := strings.ToLower(strings.ReplaceAll(exe, `\`, "/"))
	tmp := strings.ToLower(strings.ReplaceAll(firstSet(env.getenv("TEMP"), env.getenv("TMP"), env.getenv("TMPDIR")), `\`, "/"))
	for _, bad := range []string{"/build.claude/", "/build/", "/downloads/"} {
		if strings.Contains(p, bad) {
			r.State, r.Fix = Fail, FixExplain
			r.Detail = filepath.ToSlash(exe) + " is a build or download folder, which the next build or cleanup removes. install atrium to a fixed folder and run it from there"
			return r
		}
	}
	if tmp != "" && strings.HasPrefix(p, strings.TrimRight(tmp, "/")+"/") {
		r.State, r.Fix = Fail, FixExplain
		r.Detail = filepath.ToSlash(exe) + " is in a temp folder. install atrium to a fixed folder and run it from there"
		return r
	}
	r.State, r.Detail = OK, filepath.ToSlash(exe)
	return r
}

func firstSet(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// tools are the programs an agent's first build reaches for. Each row names the alternatives that count as the tool.
var tools = [][]string{{"git"}, {"go"}, {"node"}, {"npm"}, {"cc", "gcc", "clang"}, {"make"}, {"python", "python3"}, {"gh"}}

// toolRows is the tool survey: where each tool is on the PATH this account has, one row each. Only git is a warn when
// missing, since every card needs it. The rest are n/a, a fact the bring-up reads to offer an install.
func toolRows(env Env) []Result {
	var rows []Result
	for _, alts := range tools {
		r := Result{ID: "tool." + alts[0], Label: alts[0] + " is on the PATH"}
		found := ""
		for _, n := range alts {
			if p, err := env.lookPath(n); err == nil {
				found = filepath.ToSlash(p)
				break
			}
		}
		switch {
		case found != "":
			r.State, r.Detail = OK, found
		case alts[0] == "git":
			r.State, r.Fix = Warn, FixExplain
			r.Detail = "git is not on this account's PATH. install it and restart the room so cards find it"
		default:
			r.State, r.Detail = NA, strings.Join(alts, " or ")+" is not on this account's PATH"
		}
		rows = append(rows, r)
	}
	return rows
}
