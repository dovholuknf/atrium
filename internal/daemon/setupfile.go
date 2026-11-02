package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

const (
	setupFile     = ".atrium/setup.toml"
	setupMaxBytes = 16 << 10
	setupLine     = "Read .atrium/setup.toml if the repo has one."
	versionBound  = 3 * time.Second
)

const (
	needOK      = "ok"
	needOld     = "old"
	needMissing = "missing"
	needUnknown = "unknown"
	needNA      = "na"
)

type setupDetect struct {
	Bin  string `toml:"bin"`
	File string `toml:"file"`
	Env  string `toml:"env"`

	Docker bool   `toml:"docker"`
	Wsl    string `toml:"wsl"`
}

// setupNeed is one table of the file. Detect is a closed set of probes, so the repo never supplies a command.
type setupNeed struct {
	Name     string
	Detect   setupDetect       `toml:"detect"`
	Version  string            `toml:"version"`
	Install  map[string]string `toml:"install"`
	OS       []string          `toml:"os"`
	Required bool              `toml:"required"`
	Why      string            `toml:"why"`
}

type needResult struct {
	setupNeed
	State   string
	Found   string
	Message string
}

// parseSetup reads the file's tables in name order. Unknown fields are ignored so an older room reads a newer file.
func parseSetup(data []byte) ([]setupNeed, error) {
	if len(data) > setupMaxBytes {
		return nil, fmt.Errorf("%s is over %d bytes", setupFile, setupMaxBytes)
	}
	var raw map[string]setupNeed
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", setupFile, err)
	}
	out := make([]setupNeed, 0, len(raw))
	for name, n := range raw {
		n.Name = name
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var dottedNumber = regexp.MustCompile(`\d+(\.\d+)*`)

// parseConstraint accepts only ">=" and a dotted number.
func parseConstraint(c string) ([]int, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(c), ">=")
	if !ok {
		return nil, false
	}
	return parseDotted(strings.TrimSpace(rest))
}

func parseDotted(s string) ([]int, bool) {
	if s == "" || dottedNumber.FindString(s) != s {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// atLeast compares dotted numbers, a missing part counting as 0.
func atLeast(have, want []int) bool {
	for i := 0; i < len(have) || i < len(want); i++ {
		var h, w int
		if i < len(have) {
			h = have[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if h != w {
			return h > w
		}
	}
	return true
}

// toolVersion runs `path --version` with no shell and a minimal environment, stdout first then stderr.
func toolVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionBound)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	nowindow.Hide(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
	_ = cmd.Run()
	if v := dottedNumber.FindString(so.String()); v != "" {
		return v
	}
	return dottedNumber.FindString(se.String())
}

func expandSetupPath(p string) string {
	if rest, ok := strings.CutPrefix(p, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == '\\') {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + rest
		}
	}
	return filepath.FromSlash(os.ExpandEnv(p))
}

func (n setupNeed) applies() bool {
	if len(n.OS) == 0 {
		return true
	}
	for _, o := range n.OS {
		if strings.EqualFold(o, runtime.GOOS) || (o == "macos" && runtime.GOOS == "darwin") {
			return true
		}
	}
	return false
}

// probe is one need checked against this room. Only bin, file and env exist, anything else is unknown and warns.
func (n setupNeed) probe(path string) needResult {
	r := needResult{setupNeed: n, State: needMissing}
	if !n.applies() {
		r.State = needNA
		return r
	}
	d := n.Detect
	switch {
	case d.File != "":
		if _, err := os.Stat(expandSetupPath(d.File)); err == nil {
			r.State = needOK
		}
	case d.Env != "":
		if os.Getenv(d.Env) != "" {
			r.State = needOK
		}
	case d.Bin != "" || d == (setupDetect{}):
		name := d.Bin
		if name == "" {
			name = n.Name
		}
		p := findTool(name, path)
		if p == "" {
			return r
		}
		r.State = needOK
		if n.Version == "" {
			return r
		}
		want, ok := parseConstraint(n.Version)
		r.Found = toolVersion(p)
		have, found := parseDotted(r.Found)
		if !ok || !found {
			r.State, r.Message = needUnknown, "could not compare the version "+n.Version
		} else if !atLeast(have, want) {
			r.State = needOld
		}
	default:
		r.State, r.Message = needUnknown, "this room has no probe for it"
	}
	return r
}

// checkSetup reads the file in dir and probes every need. A repo without the file is nil, nil.
func checkSetup(dir, path string) ([]needResult, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(setupFile)))
	if err != nil {
		return nil, nil
	}
	needs, err := parseSetup(data)
	if err != nil {
		return nil, err
	}
	out := make([]needResult, 0, len(needs))
	for _, n := range needs {
		out = append(out, n.probe(path))
	}
	return out, nil
}

func (r needResult) line() string {
	s := "- " + r.Name + ": " + r.State
	if r.State == needOld {
		s += " (found " + r.Found + ", needs " + r.Version + ")"
	}
	if r.Message != "" {
		s += " (" + r.Message + ")"
	}
	if r.Why != "" {
		s += ". why: " + r.Why
	}
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	if i := r.Install[osName]; i != "" {
		s += ". install." + osName + ": " + i
	}
	return s
}

func (r needResult) lacking() bool {
	return r.State == needMissing || r.State == needOld
}

// setupRefusal is the launch error for a required need this room lacks, "" when none.
func setupRefusal(results []needResult) string {
	var lines []string
	for _, r := range results {
		if r.Required && r.lacking() {
			lines = append(lines, r.line())
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "this room lacks what " + setupFile + " requires:\n" + strings.Join(lines, "\n")
}

// setupBlock is the framing line and, when a need is not ok, the warning block. "" when the repo has no file.
func setupBlock(results []needResult, hasFile bool) string {
	if !hasFile {
		return ""
	}
	var lines []string
	for _, r := range results {
		if r.State != needOK && r.State != needNA {
			lines = append(lines, r.line())
		}
	}
	if len(lines) == 0 {
		return setupLine
	}
	return setupLine + "\n\nSetup: this repo's " + setupFile + " needs things this room lacks.\n" +
		strings.Join(lines, "\n") + "\nInstall what you can in your card directory or tell your launcher. Do not " +
		"hand-build a toolchain for an hour before saying so."
}

// setupFor checks the repo in dir for a fresh launch: the text for the brief, or an error naming what a required
// need lacks.
func setupFor(dir, path string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(setupFile))); err != nil {
		return "", nil
	}
	results, err := checkSetup(dir, path)
	if err != nil {
		return setupLine + "\n\nSetup: " + err.Error(), nil
	}
	if why := setupRefusal(results); why != "" {
		return "", fmt.Errorf("%s", why)
	}
	return setupBlock(results, true), nil
}
