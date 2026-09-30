// Package requirements parses atrium.requirements.yaml.
//
// The binary is the ONE parser of that file, so the schema has one set of rules
// and one set of error messages. scripts/room-check.ps1 reads the JSON this
// prints and never the YAML. See docs/fabric/room-requirements-design.md, section 2.
//
// Three refusals do most of the work, because the file is read on machines that
// are not the one it was written on:
//
//   - an unknown key, because a misspelt key that is silently ignored is a
//     requirement that looks like it is being checked and is not
//   - an absolute path, because the file describes a PROJECT, never a room
//   - anything that looks like a secret, because a file committed with a
//     project is read by everyone who can read the project
//
// Values are read as text from the node, never decoded into a Go type, so an
// unquoted `min: 2.30` stays "2.30" rather than becoming the float 2.3, and a
// sha like 1234567 stays a sha.
package requirements

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FileName is what the file is called at a project's root.
const FileName = "atrium.requirements.yaml"

// MaxSize bounds what is read. The file is a short list of names.
const MaxSize = 256 << 10

// File is the normalized result. Sections that were absent are absent (nil) or
// empty, never invented, except the defaults the design names for git.
type File struct {
	Version   int                 `json:"version"`
	Atrium    *Atrium             `json:"atrium,omitempty"`
	Git       *Git                `json:"git,omitempty"`
	Toolchain map[string]Tool     `json:"toolchain"`
	Runners   map[string]Runner   `json:"runners"`
	Room      Room                `json:"room"`
	Env       map[string]EnvEntry `json:"env"`
	Services  []string            `json:"services"`
}

type Atrium struct {
	Min string `json:"min"`
}

type Git struct {
	Base      string `json:"base"`
	Mirror    string `json:"mirror"`
	Clone     string `json:"clone"`
	Worktrees string `json:"worktrees"`
	Fresh     bool   `json:"fresh"`
}

type Tool struct {
	From    string   `json:"from,omitempty"`
	Min     string   `json:"min,omitempty"`
	Windows string   `json:"windows,omitempty"`
	OS      []string `json:"os,omitempty"`
}

type Runner struct {
	Hooks   string   `json:"hooks,omitempty"`
	Gate    string   `json:"gate,omitempty"`
	MCP     []string `json:"mcp,omitempty"`
	Helpers []string `json:"helpers,omitempty"`
	Smoke   bool     `json:"smoke"`
}

type Room struct {
	Survives   string   `json:"survives"`
	RunnerAuth []string `json:"runner_auth"`
}

// EnvEntry is a name the runner must see. Required means presence only, checked
// in the room process. Value is set only for a plain, non-secret value.
type EnvEntry struct {
	Required bool   `json:"required"`
	Value    string `json:"value,omitempty"`
}

// Error is every problem found, each naming the key and the line.
type Error struct{ Problems []string }

func (e *Error) Error() string { return strings.Join(e.Problems, "\n") }

var (
	nameRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	toolRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	envRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	shaRE     = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	versionRE = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
	branchRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	braceRE   = regexp.MustCompile(`\{[^{}]*\}`)
	driveRE   = regexp.MustCompile(`^[A-Za-z]:`)

	secretKeyRE = regexp.MustCompile(`(?i)(secret|token|passw|credential|api_?key|private_?key|auth)`)
	// Shapes of credentials that have a recognisable prefix.
	secretValueRE = regexp.MustCompile(`(?i)(ghp_|gho_|ghs_|ghu_|github_pat_|glpat-|sk-[a-z0-9]|xox[abpr]-|` +
		`AKIA[0-9A-Z]{16}|-----BEGIN|eyJ[A-Za-z0-9_-]{10,}\.|://[^/\s:@]+:[^/\s@]+@)`)
	// A long unbroken run of mixed letters and digits is a token far more often
	// than it is a requirement.
	tokenishRE = regexp.MustCompile(`^[A-Za-z0-9+/_=-]{32,}$`)
)

var placeholders = map[string]bool{"{home}": true, "{owner}": true, "{repo}": true, "{clone}": true}

type problem struct {
	line int
	msg  string
}

type parser struct {
	problems []problem
}

func (p *parser) fail(n *yaml.Node, key, format string, args ...any) {
	line := 0
	if n != nil {
		line = n.Line
	}
	if key == "" {
		key = "file"
	}
	p.problems = append(p.problems, problem{line, fmt.Sprintf("%s:%d: %s: %s", FileName, line, key, fmt.Sprintf(format, args...))})
}

// Parse reads a file's bytes.
func Parse(data []byte) (*File, error) {
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%s: larger than %d bytes, which is not a requirements file", FileName, MaxSize)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: empty. it needs at least `version: 1`", FileName)
		}
		return nil, fmt.Errorf("%s: %v", FileName, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s:%d: document: more than one YAML document in the file", FileName, extra.Line)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("%s: not a YAML mapping", FileName)
	}

	p := &parser{}
	f := p.file(doc.Content[0])
	if len(p.problems) > 0 {
		// Maps are walked in no particular order, so report in file order.
		sort.SliceStable(p.problems, func(i, j int) bool { return p.problems[i].line < p.problems[j].line })
		msgs := make([]string, len(p.problems))
		for i, pr := range p.problems {
			msgs[i] = pr.msg
		}
		return nil, &Error{Problems: msgs}
	}
	return f, nil
}

// mapping returns a mapping's entries by key, reporting a key that is not in
// allowed and a key given twice.
func (p *parser) mapping(n *yaml.Node, where string, allowed ...string) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		p.fail(n, where, "anchors and aliases are not allowed")
		return out
	}
	if n.Kind != yaml.MappingNode {
		p.fail(n, where, "must be a mapping")
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			p.fail(k, where, "a key must be a plain name")
			continue
		}
		if len(allowed) > 0 {
			ok := false
			for _, a := range allowed {
				ok = ok || a == k.Value
			}
			if !ok {
				p.fail(k, join(where, k.Value), "unknown key. known keys here: %s", strings.Join(allowed, ", "))
				continue
			}
		}
		if _, dup := out[k.Value]; dup {
			p.fail(k, join(where, k.Value), "given twice")
			continue
		}
		out[k.Value] = v
	}
	return out
}

func join(where, key string) string {
	if where == "" {
		return key
	}
	return where + "." + key
}

// scalar reads a value as text and applies the refusals every value gets.
func (p *parser) scalar(n *yaml.Node, where string) (string, bool) {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		p.fail(n, where, "anchors and aliases are not allowed")
		return "", false
	}
	if n.Kind != yaml.ScalarNode {
		p.fail(n, where, "must be a single value")
		return "", false
	}
	v := n.Value
	if isAbsolute(v) {
		p.fail(n, where, "%q is an absolute path. the file describes a project and is read on machines that are not "+
			"this one, so use {home}, {owner}, {repo} or {clone}", v)
		return "", false
	}
	if looksSecret(v) {
		p.fail(n, where, "looks like a secret. the file names what a room needs and never carries a credential. "+
			"name the variable under env and set it on the room")
		return "", false
	}
	return v, true
}

func isAbsolute(v string) bool {
	return strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) || strings.HasPrefix(v, "~") || driveRE.MatchString(v)
}

func looksSecret(v string) bool {
	if shaRE.MatchString(v) {
		return false
	}
	if secretValueRE.MatchString(v) {
		return true
	}
	if tokenishRE.MatchString(v) {
		hasDigit, hasLetter := false, false
		for _, r := range v {
			hasDigit = hasDigit || (r >= '0' && r <= '9')
			hasLetter = hasLetter || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		}
		return hasDigit && hasLetter
	}
	return false
}

func (p *parser) boolean(n *yaml.Node, where string) bool {
	if n.Kind != yaml.ScalarNode || (n.Value != "true" && n.Value != "false") {
		p.fail(n, where, "must be true or false")
		return false
	}
	return n.Value == "true"
}

// enum reads one of a fixed set of words.
func (p *parser) enum(n *yaml.Node, where string, words ...string) string {
	v, ok := p.scalar(n, where)
	if !ok {
		return ""
	}
	for _, w := range words {
		if v == w {
			return v
		}
	}
	p.fail(n, where, "%q is not one of: %s", v, strings.Join(words, ", "))
	return ""
}

// names reads a list of plain names, which is how a path is refused as a name.
func (p *parser) names(n *yaml.Node, where string, re *regexp.Regexp) []string {
	out := []string{}
	if n.Kind != yaml.SequenceNode {
		p.fail(n, where, "must be a list")
		return out
	}
	for i, item := range n.Content {
		at := fmt.Sprintf("%s[%d]", where, i)
		v, ok := p.scalar(item, at)
		if !ok {
			continue
		}
		if !re.MatchString(v) {
			p.fail(item, at, "%q is not a plain name", v)
			continue
		}
		out = append(out, v)
	}
	return out
}

// template checks a path template: only the placeholders the design names, no
// parent directory, and {clone} only where it means something.
func (p *parser) template(n *yaml.Node, where string, allowClone bool) string {
	v, ok := p.scalar(n, where)
	if !ok {
		return ""
	}
	if v == "" {
		p.fail(n, where, "must not be empty")
		return ""
	}
	for _, b := range braceRE.FindAllString(v, -1) {
		if !placeholders[b] {
			p.fail(n, where, "unknown template %s. known: {home}, {owner}, {repo}, {clone}", b)
			return ""
		}
		if b == "{clone}" && !allowClone {
			p.fail(n, where, "{clone} is only meaningful in worktrees")
			return ""
		}
	}
	if strings.Contains(strings.ReplaceAll(v, `\`, "/"), "..") {
		p.fail(n, where, "%q has a parent directory in it", v)
		return ""
	}
	return v
}

func (p *parser) file(root *yaml.Node) *File {
	f := &File{
		Toolchain: map[string]Tool{},
		Runners:   map[string]Runner{},
		Room:      Room{Survives: "none", RunnerAuth: []string{}},
		Env:       map[string]EnvEntry{},
		Services:  []string{},
	}
	top := p.mapping(root, "", "version", "atrium", "git", "toolchain", "runners", "room", "env", "services")

	v, ok := top["version"]
	if !ok {
		p.fail(root, "version", "required. this parser reads version 1")
	} else if s, ok := p.scalar(v, "version"); ok {
		if s != "1" {
			p.fail(v, "version", "%q is not a version this atrium reads. it reads 1", s)
		}
		f.Version = 1
	}

	if n, ok := top["atrium"]; ok {
		m := p.mapping(n, "atrium", "min")
		if mn, ok := m["min"]; ok {
			if s, ok := p.scalar(mn, "atrium.min"); ok {
				if !shaRE.MatchString(s) {
					p.fail(mn, "atrium.min", "%q is not a commit sha (7 to 40 lowercase hex). there are no release tags yet", s)
				} else {
					f.Atrium = &Atrium{Min: s}
				}
			}
		} else {
			p.fail(n, "atrium", "needs min")
		}
	}

	if n, ok := top["git"]; ok {
		f.Git = p.git(n)
	}

	if n, ok := top["toolchain"]; ok {
		for name, tn := range p.mapping(n, "toolchain") {
			at := join("toolchain", name)
			if !toolRE.MatchString(name) {
				p.fail(tn, at, "%q is not a tool name", name)
				continue
			}
			f.Toolchain[name] = p.tool(tn, at)
		}
	}

	if n, ok := top["runners"]; ok {
		for name, rn := range p.mapping(n, "runners") {
			at := join("runners", name)
			if !toolRE.MatchString(name) {
				p.fail(rn, at, "%q is not a runner name", name)
				continue
			}
			f.Runners[name] = p.runner(rn, at)
		}
	}

	if n, ok := top["room"]; ok {
		m := p.mapping(n, "room", "survives", "runner_auth")
		if s, ok := m["survives"]; ok {
			f.Room.Survives = p.enum(s, "room.survives", "none", "logoff", "reboot")
		}
		if r, ok := m["runner_auth"]; ok {
			f.Room.RunnerAuth = p.names(r, "room.runner_auth", toolRE)
		}
	}

	if n, ok := top["env"]; ok {
		for name, en := range p.mapping(n, "env") {
			f.Env[name] = p.env(en, join("env", name), name)
		}
	}

	if n, ok := top["services"]; ok {
		// Names from the inventory, never an address, so a URL fails as not a name.
		f.Services = p.names(n, "services", nameRE)
	}
	sort.Strings(f.Services)
	return f
}

func (p *parser) git(n *yaml.Node) *Git {
	m := p.mapping(n, "git", "base", "mirror", "clone", "worktrees", "fresh")
	g := &Git{Mirror: "hub-main", Clone: "{home}/git/github/{owner}/{repo}", Worktrees: "{clone}-worktrees"}
	branch := func(key string, dst *string) {
		bn, ok := m[key]
		if !ok {
			return
		}
		at := join("git", key)
		if s, ok := p.scalar(bn, at); ok {
			if !branchRE.MatchString(s) || strings.Contains(s, "..") {
				p.fail(bn, at, "%q is not a branch name", s)
				return
			}
			*dst = s
		}
	}
	branch("base", &g.Base)
	branch("mirror", &g.Mirror)
	if _, ok := m["base"]; !ok {
		p.fail(n, "git.base", "required when git is given. it is what the mirror follows")
	}
	if c, ok := m["clone"]; ok {
		if s := p.template(c, "git.clone", false); s != "" {
			g.Clone = s
		}
	}
	if w, ok := m["worktrees"]; ok {
		if s := p.template(w, "git.worktrees", true); s != "" {
			g.Worktrees = s
		}
	}
	if fr, ok := m["fresh"]; ok {
		g.Fresh = p.boolean(fr, "git.fresh")
	}
	return g
}

func (p *parser) tool(n *yaml.Node, at string) Tool {
	var t Tool
	m := p.mapping(n, at, "from", "min", "windows", "os")
	if fn, ok := m["from"]; ok {
		if s, ok := p.scalar(fn, at+".from"); ok {
			if s == "" || strings.Contains(strings.ReplaceAll(s, `\`, "/"), "..") {
				p.fail(fn, at+".from", "%q must name a file inside the project", s)
			} else {
				t.From = s
			}
		}
	}
	if mn, ok := m["min"]; ok {
		if s, ok := p.scalar(mn, at+".min"); ok {
			if !versionRE.MatchString(s) {
				p.fail(mn, at+".min", "%q is not a version like 24 or 2.39", s)
			} else {
				t.Min = s
			}
		}
	}
	if w, ok := m["windows"]; ok {
		t.Windows = p.enum(w, at+".windows", "git-for-windows")
	}
	if o, ok := m["os"]; ok {
		if o.Kind != yaml.SequenceNode {
			p.fail(o, at+".os", "must be a list")
		} else {
			for i, item := range o.Content {
				t.OS = append(t.OS, p.enum(item, fmt.Sprintf("%s.os[%d]", at, i), "windows", "linux", "darwin"))
			}
		}
	}
	return t
}

func (p *parser) runner(n *yaml.Node, at string) Runner {
	var r Runner
	m := p.mapping(n, at, "hooks", "gate", "mcp", "helpers", "smoke")
	// `hooks` takes one value and never names a command, so a project cannot
	// bring a hook of its own onto a room.
	if h, ok := m["hooks"]; ok {
		r.Hooks = p.enum(h, at+".hooks", "atrium")
	}
	if g, ok := m["gate"]; ok {
		r.Gate = p.enum(g, at+".gate", "required")
	}
	if mc, ok := m["mcp"]; ok {
		r.MCP = p.names(mc, at+".mcp", nameRE)
	}
	if h, ok := m["helpers"]; ok {
		r.Helpers = p.names(h, at+".helpers", nameRE)
	}
	if s, ok := m["smoke"]; ok {
		r.Smoke = p.boolean(s, at+".smoke")
	}
	return r
}

func (p *parser) env(n *yaml.Node, at, name string) EnvEntry {
	var e EnvEntry
	if !envRE.MatchString(name) {
		p.fail(n, at, "%q is not a variable name", name)
		return e
	}
	if n.Kind == yaml.MappingNode {
		m := p.mapping(n, at, "required")
		if r, ok := m["required"]; ok {
			e.Required = p.boolean(r, at+".required")
		}
		return e
	}
	// A plain value. A variable whose name says it is a secret may only be
	// required, never given a value here.
	if secretKeyRE.MatchString(name) {
		p.fail(n, at, "%s reads as a secret, so it cannot have a value in this file. write `%s: { required: true }` "+
			"and set it on the room", name, name)
		return e
	}
	if s, ok := p.scalar(n, at); ok {
		e.Value = s
	}
	return e
}
