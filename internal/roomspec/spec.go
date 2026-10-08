// Package roomspec is what a room is meant to be, as data: room.yaml (desired, written by a person or the hub) and room.lock
// (observed, written by the room after an apply). `atrium room setup` reads the one, converges the machine, and writes the
// other. Nothing here is per machine: the machine's choices (V:, the account, the packs) are values in the spec.
//
// The contract is version 1 and its field names are shared with the hub side. Do not rename one.
package roomspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"runtime"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Version is the one spec version this code reads and the lock it writes.
const Version = 1

// The operating systems a spec may name. A spec's os is checked against the machine, never guessed.
const (
	Windows = "windows"
	Linux   = "linux"
	Darwin  = "darwin"
)

// Spec is room.yaml.
type Spec struct {
	Version  int      `yaml:"version"`
	Name     string   `yaml:"name"`
	OS       string   `yaml:"os"`
	Account  string   `yaml:"account"`
	WorkRoot string   `yaml:"work_root"`
	Layout   Layout   `yaml:"layout"`
	Caches   []string `yaml:"caches"`
	Packs    []Pack   `yaml:"packs"`
	Runners  []string `yaml:"runners"`

	// Hash is the SHA-256 of the bytes Parse was given, which the lock records. Not part of the file.
	Hash string `yaml:"-"`
}

// Layout names the folders under the work root. Each is relative to it unless absolute. Empty means the default.
type Layout struct {
	Git     string `yaml:"git"`
	Reviews string `yaml:"reviews"`
	Handoff string `yaml:"handoff"`
	Cache   string `yaml:"cache"`
}

// Pack is one runner's agents and skills, taken from a repository the hub mirrors: pin the repo and branch, never vendor.
type Pack struct {
	Runner string `yaml:"runner"`
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	From   string `yaml:"from"`
}

// KnownRunners are the runner names a spec may list. Only claude has a pack adapter today.
var KnownRunners = []string{"claude", "codex", "gemini"}

// secretKey is a key a spec may not carry, at any depth. Atrium holds the name of a command that has a credential, never one.
var secretKey = regexp.MustCompile(`(?i)token|pass|secret|key|cred|auth|bearer`)

var (
	nameRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	repoRE   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	branchRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]*$`)
	// an account is a login: a name, optionally DOMAIN\name or name@domain. No quote, space-less shell syntax or path
	acctRE  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@\\-]{0,127}$`)
	segRE   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_. -]*$`)
	driveRE = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// Parse reads room.yaml. Unknown keys, a second document, and any key named like a credential are refused, and the result is
// validated for the machine it is running on.
func Parse(data []byte) (*Spec, error) { return ParseFor(data, runtime.GOOS) }

// ParseFor is Parse for a named operating system, so a test can read a spec for one it is not on.
func ParseFor(data []byte, goos string) (*Spec, error) {
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("room spec: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("room spec: more than one document")
	}
	if err := refuseSecrets(&root, ""); err != nil {
		return nil, err
	}
	var s Spec
	d2 := yaml.NewDecoder(bytes.NewReader(data))
	d2.KnownFields(true)
	if err := d2.Decode(&s); err != nil {
		return nil, fmt.Errorf("room spec: %w", err)
	}
	sum := sha256.Sum256(data)
	s.Hash = hex.EncodeToString(sum[:])
	if err := s.Validate(goos); err != nil {
		return nil, err
	}
	return &s, nil
}

func refuseSecrets(n *yaml.Node, path string) error {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if err := refuseSecrets(c, path); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			p := strings.TrimPrefix(path+"."+k.Value, ".")
			if secretKey.MatchString(k.Value) {
				return fmt.Errorf("room spec: %s: a spec holds no credential. name the command that has one, never the value", p)
			}
			if err := refuseSecrets(n.Content[i+1], p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate says why the spec cannot be used on goos, or nil. It changes nothing, so a spec that passes is the spec that was
// read; the defaults are applied by Resolve.
func (s *Spec) Validate(goos string) error {
	bad := func(field, format string, a ...any) error {
		return fmt.Errorf("room spec: %s %s", field, fmt.Sprintf(format, a...))
	}
	if s.Version != Version {
		return bad("version", "is %d, and this reads version %d", s.Version, Version)
	}
	if !nameRE.MatchString(s.Name) {
		return bad("name", "%q is not a room name (letters, digits, . _ -)", s.Name)
	}
	switch s.OS {
	case Windows, Linux, Darwin:
	default:
		return bad("os", "%q is not windows, linux or darwin", s.OS)
	}
	if s.OS != goos {
		return bad("os", "is %s and this machine is %s. a spec is checked against the machine, never guessed", s.OS, goos)
	}
	if !acctRE.MatchString(s.Account) {
		return bad("account", "%q is not a login name", s.Account)
	}
	if why := CheckWorkRoot(s.OS, s.WorkRoot); why != "" {
		return bad("work_root", "%q %s", s.WorkRoot, why)
	}
	if why := checkAnotherHome(s.WorkRoot, s.Account); why != "" {
		return bad("work_root", "%q %s", s.WorkRoot, why)
	}
	for name, v := range map[string]string{"layout.git": s.Layout.Git, "layout.reviews": s.Layout.Reviews, "layout.handoff": s.Layout.Handoff, "layout.cache": s.Layout.Cache} {
		if why := checkLayoutName(s.OS, v); why != "" {
			return bad(name, "%q %s", v, why)
		}
	}
	// Whatever the layout says, every folder this makes or sets resolves to a place under the work root.
	rp := s.Resolve()
	root := strings.ToLower(path.Clean(rp.Root))
	for name, v := range map[string]string{"layout.git": rp.Git, "layout.reviews": rp.Reviews, "layout.handoff": rp.Handoff, "layout.cache": rp.Cache} {
		if !strings.HasPrefix(strings.ToLower(path.Clean(v)), root+"/") {
			return bad(name, "resolves to %s, which is not under the work root %s", v, rp.Root)
		}
	}
	seen := map[string]bool{}
	for _, c := range s.Caches {
		if _, ok := CacheByName(c); !ok {
			return bad("caches", "%q is not a known cache (%s)", c, strings.Join(CacheNames(), ", "))
		}
		if seen[c] {
			return bad("caches", "names %q twice", c)
		}
		seen[c] = true
	}
	runners := map[string]bool{}
	for _, r := range s.Runners {
		if !isKnownRunner(r) {
			return bad("runners", "%q is not a known runner (%s)", r, strings.Join(KnownRunners, ", "))
		}
		runners[r] = true
	}
	packRunners := map[string]bool{}
	for i, p := range s.Packs {
		f := fmt.Sprintf("packs[%d]", i)
		if !isKnownRunner(p.Runner) {
			return bad(f+".runner", "%q is not a known runner (%s)", p.Runner, strings.Join(KnownRunners, ", "))
		}
		if packRunners[p.Runner] {
			return bad(f+".runner", "%q has two packs", p.Runner)
		}
		packRunners[p.Runner] = true
		if why := CheckPackArg(p.Repo, p.branchOrDefault()); why != "" {
			return bad(f, "%s", why)
		}
		if why := checkFrom(p.From); why != "" {
			return bad(f+".from", "%q %s", p.From, why)
		}
	}
	return nil
}

func (p Pack) branchOrDefault() string {
	if p.Branch == "" {
		return "main"
	}
	return p.Branch
}

func isKnownRunner(r string) bool {
	for _, k := range KnownRunners {
		if k == r {
			return true
		}
	}
	return false
}

// CheckPackArg is why a pack's repo and branch cannot be used, or "". The same rule as the provision script's
// Test-AgentPackArg: both end up as git arguments, so neither may look like an option or climb out of a path.
func CheckPackArg(repo, branch string) string {
	if !repoRE.MatchString(repo) {
		return fmt.Sprintf("repo %q is not owner/name", repo)
	}
	if !branchRE.MatchString(branch) || strings.Contains(branch, "..") {
		return fmt.Sprintf("branch %q is not a branch name (it may not start with - or hold ..)", branch)
	}
	return ""
}

func checkFrom(from string) string {
	if from == "" {
		return ""
	}
	if strings.HasPrefix(from, "/") || strings.HasPrefix(from, `\`) || driveRE.MatchString(from) {
		return "must be a path inside the repo, not an absolute one"
	}
	for _, seg := range strings.FieldsFunc(from, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || seg == "." {
			return "holds . or .."
		}
	}
	if strings.ContainsAny(from, ";\r\n\x00") {
		return "holds a character a path may not"
	}
	return ""
}

// CheckWorkRoot is why a work root cannot be used on goos, or "". Absolute only, never a filesystem root (an agent must never
// be able to create a folder there), no UNC, no "..", and no character that could end a shell word or a line.
func CheckWorkRoot(goos, dir string) string {
	if dir == "" {
		return "is empty"
	}
	if strings.ContainsAny(dir, ";\r\n\x00") {
		return "holds a semicolon or a newline"
	}
	if strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//") {
		return "is a network path, which this does not set up"
	}
	isDrive := driveRE.MatchString(dir)
	switch {
	case goos == Windows && !isDrive:
		return "is not a drive path on a Windows machine. name a folder on a drive, like V:/localai"
	case goos != Windows && isDrive:
		return "is a drive path and the machine is " + goos + ". name an absolute folder, like /srv/localai"
	case goos != Windows && !strings.HasPrefix(dir, "/"):
		return "is not an absolute path"
	}
	trim := strings.Trim(strings.ReplaceAll(dir, `\`, "/"), "/")
	if trim == "" || (isDrive && len(trim) == 2) {
		return "is a filesystem root, and an agent must never be able to create a folder there. name a folder on the drive"
	}
	for _, seg := range strings.Split(strings.ReplaceAll(dir, `\`, "/"), "/") {
		if seg == ".." {
			return "holds .., so the folder it means is not the one it says"
		}
	}
	// The root is made and handed to the account, so it must be a folder of its own: not a top-level one on Unix (/srv), and
	// not inside the operating system's.
	segs := splitSegs(trim)
	if isDrive {
		segs = segs[1:] // the drive itself
	}
	if goos != Windows && len(segs) < 2 {
		return "is a top-level folder. name one inside it, like /srv/localai, so the account is never given a folder others use"
	}
	if len(segs) == 1 && (strings.EqualFold(segs[0], "home") || strings.EqualFold(segs[0], "users")) {
		return "is the folder every user's home is in. name a folder of its own"
	}
	if len(segs) > 0 && systemFolder(goos, strings.ToLower(segs[0])) {
		return "is inside " + segs[0] + ", a system folder. name a folder of its own, like /srv/localai or V:/localai"
	}
	return ""
}

// checkAnotherHome is why a work root lies inside a home that is not the account's own, or "". Under /home, /Users or C:/Users
// the folder after it is a person's, and only the account's own may hold the root.
func checkAnotherHome(dir, account string) string {
	trim := strings.Trim(slash(dir), "/")
	segs := splitSegs(trim)
	if driveRE.MatchString(dir) && len(segs) > 0 {
		segs = segs[1:]
	}
	if len(segs) < 2 || !(strings.EqualFold(segs[0], "home") || strings.EqualFold(segs[0], "users")) {
		return ""
	}
	name := account
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	if !strings.EqualFold(segs[1], name) {
		return fmt.Sprintf("is inside %s's home, and the account is %s. a work root is not inside another user's folder", segs[1], account)
	}
	return ""
}

func systemFolder(goos, first string) bool {
	var l []string
	if goos == Windows {
		l = []string{"windows", "program files", "program files (x86)", "programdata", "system volume information", "$recycle.bin", "recovery"}
	} else {
		l = []string{"bin", "boot", "dev", "etc", "lib", "lib32", "lib64", "proc", "root", "run", "sbin", "sys", "usr", "system", "library", "applications", "cores"}
	}
	return contains(l, first)
}

// ContainsHome is whether the work root is the account's home or holds it, which would make the whole home the work root's.
func ContainsHome(root, home string) bool {
	r, h := strings.ToLower(path.Clean(slash(root))), strings.ToLower(path.Clean(slash(home)))
	return h == r || strings.HasPrefix(h, r+"/")
}

func checkLayoutName(goos, v string) string {
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, ";\r\n\x00") {
		return "holds a semicolon or a newline"
	}
	if strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) || driveRE.MatchString(v) {
		// Everything a spec makes or sets is under the work root, so a folder is named relative to it.
		return "is an absolute path. name a folder under the work root, like cache"
	}
	for _, seg := range strings.FieldsFunc(v, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "holds .."
		}
		if !segRE.MatchString(seg) {
			return "is not a folder name"
		}
	}
	return ""
}

// Paths are the folders a spec means, resolved: absolute, forward slashes, no trailing slash. This is how the room settings
// spell them too.
type Paths struct {
	Root, Git, Reviews, Handoff, Cache string
}

// Resolve applies the layout defaults. Call it on a validated spec.
func (s *Spec) Resolve() Paths {
	root := slash(s.WorkRoot)
	under := func(v, def string) string {
		if v == "" {
			v = def
		}
		v = slash(v)
		if strings.HasPrefix(v, "/") || driveRE.MatchString(v) {
			return v
		}
		return root + "/" + v
	}
	return Paths{Root: root, Git: under(s.Layout.Git, "git"), Reviews: under(s.Layout.Reviews, "reviews"),
		Handoff: under(s.Layout.Handoff, "handoff"), Cache: under(s.Layout.Cache, "cache")}
}

// CacheSet is the caches to set: the spec's, or every known one when it names none.
func (s *Spec) CacheSet() []string {
	if len(s.Caches) == 0 {
		return CacheNames()
	}
	return append([]string(nil), s.Caches...)
}

// PackFor is the pack for a runner, with the defaults filled in, or false.
func (s *Spec) PackFor(runner string) (Pack, bool) {
	for _, p := range s.Packs {
		if p.Runner == runner {
			if p.Branch == "" {
				p.Branch = "main"
			}
			if p.From == "" {
				p.From = runner
			}
			return p, true
		}
	}
	return Pack{}, false
}

func slash(p string) string { return strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/") }
