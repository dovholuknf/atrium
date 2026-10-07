package guard

import (
	"encoding/base64"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"
)

// The shell model every rule reads. Bash and PowerShell are parsed into the
// same thing, so a rule is written once and sees what the shell would run
// rather than the text that was typed: a word inside quotes is an argument,
// `2>&1` is a redirection and never a branch name, and `;` inside a sed
// expression is part of an argument.

// Word is one argument as the shell would pass it. Dyn means part of it is only
// known when the shell runs (a variable, a substitution), so Lit is a guess.
type Word struct {
	Lit string
	Dyn bool
}

// Redir is one redirection. Body is a here-document's text.
type Redir struct {
	Op     string
	Target Word
	Body   string
}

// Cmd is one simple command.
type Cmd struct {
	Words   []Word
	Assigns []string
	Redirs  []Redir
	// Dir is the directory this command runs in, as far as the guard can tell.
	// Empty means it cannot tell: a cd to a variable, a path that does not
	// exist, a cd whose effect depends on whether something failed.
	Dir string
	// Nested is a command inside another one: a substitution, a script block,
	// a compound command's body, or the script handed to `bash -c`.
	Nested bool
	// Shell is "bash" or "pwsh", which decides how a cd path is read.
	Shell string
}

// Name is the command name, lower case, with a directory and `.exe` stripped.
func (c *Cmd) Name() string {
	if len(c.Words) == 0 {
		return ""
	}
	return cmdName(c.Words[0].Lit)
}

// Args is every word after the name.
func (c *Cmd) Args() []Word {
	if len(c.Words) < 2 {
		return nil
	}
	return c.Words[1:]
}

func cmdName(w string) string {
	w = strings.ToLower(w)
	if i := strings.LastIndexAny(w, `/\`); i >= 0 {
		w = w[i+1:]
	}
	return strings.TrimSuffix(w, ".exe")
}

// Link is one top-level command and the operator that follows it. Cmd is nil
// for anything that is not a simple command (a loop, a subshell, a block).
type Link struct {
	Cmd *Cmd
	Sep string
}

// Script is a parsed tool call.
type Script struct {
	Shell string
	// Cmds is every simple command, nested ones included, in the order they
	// appear.
	Cmds []*Cmd
	// Chain is the top level, flattened: `a && b | c` is three links.
	Chain []Link
	// Semicolons counts `;` used to chain statements. Only bash has a rule on it.
	Semicolons int
	// Redirs is every redirection, including those on compound commands.
	Redirs []Redir
}

// dirResolver works out where a cd lands.
type dirResolver struct {
	env   Env
	shell string
}

// cdNames are the commands that change the shell's directory.
var cdNames = map[string]bool{
	"cd": true, "chdir": true, "pushd": true, "sl": true,
	"set-location": true, "push-location": true,
}

// unknownDirNames are commands that move the shell somewhere the guard does not
// follow.
var unknownDirNames = map[string]bool{"popd": true, "pop-location": true}

// safePath is the only shape of path the guard will follow: it means the same
// directory to the guard and to the shell. No `$` or backtick (expansion), no
// glob characters, no `~`, no provider prefix.
var safePath = regexp.MustCompile(`^[\w .\\/:-]+$`)

// msysDrive is the Bash tool's spelling of a drive path: /d/git/x is D:/git/x.
var msysDrive = regexp.MustCompile(`^/([A-Za-z])(/.*)?$`)

// cd is where a cd command leaves the shell, or "" when the guard cannot tell.
func (r dirResolver) cd(cur string, c *Cmd) string {
	var target *Word
	for _, w := range c.Args() {
		if r.shell == "pwsh" && strings.HasPrefix(w.Lit, "-") && !w.Dyn {
			switch strings.ToLower(w.Lit) {
			case "-literalpath", "-path", "-lp", "-pspath":
				continue
			}
			return ""
		}
		if target != nil {
			return ""
		}
		w := w
		target = &w
	}
	if target == nil || target.Dyn {
		return ""
	}
	return r.path(cur, target.Lit, true)
}

// path resolves a directory argument. fromCd says bash's CDPATH applies, so a
// bare relative name could land anywhere.
func (r dirResolver) path(cur, p string, fromCd bool) string {
	if m := msysDrive.FindStringSubmatch(p); m != nil {
		p = m[1] + ":" + m[2] + "/"
	}
	if p == "" || !safePath.MatchString(p) || strings.HasPrefix(p, "-") {
		return ""
	}
	if i := strings.LastIndex(p, ":"); i >= 0 && i != 1 {
		return ""
	}
	rooted := len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') || filepath.IsAbs(p)
	if strings.Contains(p, ":") && !rooted {
		// C:foo is relative to drive C's own current directory.
		return ""
	}
	if !rooted {
		dotted := p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") ||
			strings.HasPrefix(p, `.\`) || strings.HasPrefix(p, `..\`)
		if fromCd && r.shell == "bash" && !dotted {
			return ""
		}
		if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || cur == "" {
			return ""
		}
		p = filepath.Join(cur, p)
	}
	p = filepath.Clean(p)
	if isDir, ok := r.env.Stat(p); !ok || !isDir {
		return ""
	}
	return p
}

// Wrappers: commands that run the rest of their arguments as a command.
var plainWrappers = map[string]bool{
	"command": true, "exec": true, "nohup": true, "builtin": true, "sudo": true, "time": true,
}

var shellNames = map[string]string{
	"bash": "bash", "sh": "bash", "zsh": "bash", "dash": "bash", "ksh": "bash",
	"pwsh": "pwsh", "powershell": "pwsh",
}

// unwrap turns `env FOO=1 git push`, `timeout 5 git push`, `xargs git push`
// and the like into the command they run. ok is false when nothing changed.
func unwrap(c *Cmd) (inner *Cmd, ok bool) {
	name := c.Name()
	args := c.Args()
	skip := -1
	switch {
	case plainWrappers[name]:
		skip = 0
		for skip < len(args) && strings.HasPrefix(args[skip].Lit, "-") {
			skip++
		}
	case name == "env":
		skip = 0
		for skip < len(args) && (strings.HasPrefix(args[skip].Lit, "-") || strings.Contains(args[skip].Lit, "=")) {
			skip++
		}
	case name == "nice":
		skip = 0
		for skip < len(args) && strings.HasPrefix(args[skip].Lit, "-") {
			if args[skip].Lit == "-n" {
				skip++
			}
			skip++
		}
	case name == "timeout":
		skip = 0
		for skip < len(args) && strings.HasPrefix(args[skip].Lit, "-") {
			if args[skip].Lit == "-s" || args[skip].Lit == "-k" {
				skip++
			}
			skip++
		}
		skip++ // the duration
	case name == "xargs":
		skip = 0
		for skip < len(args) && strings.HasPrefix(args[skip].Lit, "-") {
			switch args[skip].Lit {
			case "-I", "-n", "-P", "-L", "-s", "-d", "-E", "-a":
				skip++
			}
			skip++
		}
	}
	if skip < 0 || skip >= len(args) {
		return nil, false
	}
	n := *c
	n.Words = append([]Word(nil), args[skip:]...)
	n.Assigns = nil
	return &n, true
}

// innerScript is the script a shell command runs from an argument: `bash -c`,
// `pwsh -Command`, `cmd /c`, `eval`, `Invoke-Expression`, and pwsh's
// -EncodedCommand.
func innerScript(c *Cmd) (src, shell string, ok bool) {
	name := c.Name()
	args := c.Args()
	join := func(ws []Word) string {
		parts := make([]string, len(ws))
		for i, w := range ws {
			parts[i] = w.Lit
		}
		return strings.Join(parts, " ")
	}
	switch name {
	case "eval":
		return join(args), "bash", len(args) > 0
	case "invoke-expression", "iex":
		for i, w := range args {
			if strings.EqualFold(w.Lit, "-command") && i+1 < len(args) {
				return args[i+1].Lit, "pwsh", true
			}
		}
		return join(args), "pwsh", len(args) > 0
	case "cmd":
		for i, w := range args {
			if l := strings.ToLower(w.Lit); l == "/c" || l == "/k" {
				return join(args[i+1:]), "pwsh", true
			}
		}
		return "", "", false
	}
	sh, isShell := shellNames[name]
	if !isShell {
		return "", "", false
	}
	for i, w := range args {
		l := strings.ToLower(w.Lit)
		if sh == "bash" && strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "--") && strings.Contains(l, "c") && i+1 < len(args) {
			return args[i+1].Lit, "bash", true
		}
		if sh == "pwsh" {
			switch l {
			case "-c", "-command", "-com", "-comm", "-comma", "-comman":
				return join(args[i+1:]), "pwsh", i+1 < len(args)
			case "-encodedcommand", "-enc", "-e", "-ec":
				if i+1 < len(args) {
					if s, ok := decodeUTF16(args[i+1].Lit); ok {
						return s, "pwsh", true
					}
				}
			}
		}
	}
	return "", "", false
}

func decodeUTF16(b64 string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw)%2 != 0 {
		return "", false
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(u)), true
}

// Parse reads a tool call's command as the named shell would. dir is where it
// starts.
func Parse(src, shell, dir string, env Env) *Script {
	s := &Script{Shell: shell}
	r := dirResolver{env: env, shell: shell}
	if shell == "pwsh" {
		parsePwsh(s, r, src, dir)
	} else {
		parseBash(s, r, src, dir)
	}
	expandInner(s, env, 0)
	return s
}

// expandInner adds what wrappers and `bash -c` run to the command list, so a
// rule cannot be stepped around by putting the command one level down.
func expandInner(s *Script, env Env, depth int) {
	if depth > 4 {
		return
	}
	var add []*Cmd
	for _, c := range s.Cmds {
		cur := c
		for i := 0; i < 4; i++ {
			inner, ok := unwrap(cur)
			if !ok {
				break
			}
			inner.Nested = true
			add = append(add, inner)
			cur = inner
		}
		if src, sh, ok := innerScript(cur); ok {
			sub := Parse(src, sh, cur.Dir, env)
			for _, ic := range sub.Cmds {
				ic.Nested = true
				add = append(add, ic)
			}
		}
		// A shell reading its script from a here-document.
		if _, isShell := shellNames[cur.Name()]; isShell {
			for _, rd := range cur.Redirs {
				if rd.Body != "" {
					sub := Parse(rd.Body, shellNames[cur.Name()], cur.Dir, env)
					for _, ic := range sub.Cmds {
						ic.Nested = true
						add = append(add, ic)
					}
				}
			}
		}
	}
	s.Cmds = append(s.Cmds, add...)
}

// tracker follows the directory through a chain of commands.
type tracker struct {
	r   dirResolver
	dir string
}

// after updates the directory once a command has run, given the operator that
// follows it.
func (t *tracker) after(c *Cmd, sep string) {
	if c == nil {
		return
	}
	name := c.Name()
	if unknownDirNames[name] {
		t.dir = ""
		return
	}
	if !cdNames[name] {
		return
	}
	switch sep {
	case "&":
		// Backgrounded: it moves a job, not this shell.
	case "|", "||":
		// A pipe runs it apart from this shell in bash and not in pwsh, and
		// after `||` it depends on whether it failed. Either way: unknown.
		t.dir = ""
	default:
		t.dir = t.r.cd(t.dir, c)
	}
}
