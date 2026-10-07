package guard

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// checkFunc reports whether a rule matches a call, and what it matched, which
// replaces `{name}` in the rule's reason.
type checkFunc func(c *evalCtx, r *Rule) (bool, string)

// checks is every check a rule may name. Initialised in init because a check
// reads the registry's neighbours through evalCtx.
var checks map[string]checkFunc

func init() {
	checks = map[string]checkFunc{
		"drive-root":             checkDriveRoot,
		"subagent":               checkSubagent,
		"phrase":                 checkPhrase,
		"go-build":               checkGoBuild,
		"git-alias-config":       checkGitAliasConfig,
		"git-global-option":      checkGitGlobalOption,
		"git-alias":              checkGitAlias,
		"git-unknown-subcommand": checkGitUnknown,
		"git-remote":             checkGitRemote,
		"git-plain-checkout":     checkGitPlainCheckout,
		"git-branch-name":        checkGitBranchName,
		"git-current-branch":     checkGitCurrentBranch,
		"compound-cd":            checkCompoundCd,
		"command":                checkCommand,
		"env-prefix":             checkEnvPrefix,
		"semicolon":              checkSemicolon,
		"redirect-to-file":       checkRedirect,
		"cmake-preset":           checkCmakePreset,
		"gh-api":                 checkGhAPI,
		"path":                   checkPath,
		"content":                checkContent,
		"file-name":              checkFileName,
	}
}

// isFileTool is a tool whose input is a file path rather than a command.
func (c *evalCtx) isFileTool() bool {
	return c.in.ToolInput.Command == "" && c.in.ToolInput.FilePath != ""
}

// --- drive roots ---

var winRoot = regexp.MustCompile(`([A-Za-z]):[\\/]([^\\/:*?"<>|'` + "`" + `\s,;)]+)`)
var bashRoot = regexp.MustCompile(`/([a-zA-Z])/([^/\\:*?"<>|'` + "`" + `\s,;)]+)`)

// newRoot is the first top-level folder of a drive named in text that does not
// exist yet, or "".
func newRoot(env Env, text string) string {
	for _, m := range winRoot.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 {
			if p := text[m[0]-1]; p == '.' || p == '_' || isAlnum(p) {
				continue
			}
		}
		drive, seg := text[m[2]:m[3]], text[m[4]:m[5]]
		top := drive + `:\` + seg
		if _, ok := env.Stat(top); ok {
			continue
		}
		// An unquoted path with a space in it arrives cut at the space, so
		// anything the root already holds by that prefix means the real path
		// is longer than what matched.
		if env.HasPrefix(drive+`:\`, seg) {
			continue
		}
		return top
	}
	for _, m := range bashRoot.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > 0 {
			if p := text[m[0]-1]; p == '.' || p == '_' || p == '/' || isAlnum(p) {
				continue
			}
		}
		top := text[m[2]:m[3]] + `:\` + text[m[4]:m[5]]
		if _, ok := env.Stat(top); !ok {
			return top
		}
	}
	return ""
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func checkDriveRoot(c *evalCtx, r *Rule) (bool, string) {
	if c.in.ToolInput.Command == "" {
		if c.isFileTool() {
			if p := newRoot(c.env, c.in.ToolInput.FilePath); p != "" {
				return true, p
			}
		}
		return false, ""
	}
	for _, cmd := range c.Script().Cmds {
		if isSearch(cmd, searchCommands) {
			continue
		}
		for _, w := range cmd.Words {
			if p := newRoot(c.env, w.Lit); p != "" {
				return true, p
			}
		}
		for _, rd := range cmd.Redirs {
			if p := newRoot(c.env, rd.Target.Lit); p != "" {
				return true, p
			}
		}
	}
	return false, ""
}

// searchCommands read text and never write it, so what they are asked to look
// for is not something being done.
var searchCommands = []string{"grep", "egrep", "fgrep", "rg", "ag", "ack", "select-string", "sls", "findstr",
	"git grep", "git log"}

// isSearch says whether cmd is one of names. A two-word name is a command and
// its first argument, like `git grep`.
func isSearch(cmd *Cmd, names []string) bool {
	n := cmd.Name()
	for _, s := range names {
		first, second, two := strings.Cut(strings.ToLower(s), " ")
		if n != first {
			continue
		}
		if !two {
			return true
		}
		for _, a := range cmd.Args() {
			if strings.HasPrefix(a.Lit, "-") {
				continue
			}
			if strings.EqualFold(a.Lit, second) {
				return true
			}
			break
		}
	}
	return false
}

// --- subagents ---

func checkSubagent(c *evalCtx, r *Rule) (bool, string) {
	t := strings.ToLower(strings.TrimSpace(c.in.ToolInput.SubagentType))
	for _, a := range r.Params.Allow {
		if t == strings.ToLower(a) {
			return false, ""
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.env.Getenv(r.Params.Env))) {
	case "0", "off", "false", "no":
		return false, ""
	}
	return true, t
}

// --- commands ---

func checkPhrase(c *evalCtx, r *Rule) (bool, string) {
	phrase := strings.ToLower(r.Params.Phrase)
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), phrase) }
	for _, cmd := range c.Script().Cmds {
		if isSearch(cmd, r.Params.Except) {
			continue
		}
		for _, w := range cmd.Words {
			if has(w.Lit) {
				return true, r.Params.Phrase
			}
		}
		for _, a := range cmd.Assigns {
			if has(a) {
				return true, r.Params.Phrase
			}
		}
		for _, rd := range cmd.Redirs {
			if has(rd.Body) || has(rd.Target.Lit) {
				return true, r.Params.Phrase
			}
		}
	}
	return false, ""
}

func checkGoBuild(c *evalCtx, r *Rule) (bool, string) {
	for _, cmd := range c.Script().Cmds {
		args := cmd.Args()
		if cmd.Name() != "go" || len(args) == 0 || args[0].Lit != "build" {
			continue
		}
		ok := false
		for i, a := range args {
			if a.Lit == "-o" && i+1 < len(args) && strings.Contains(args[i+1].Lit, r.Params.Dir) {
				ok = true
			}
			if strings.HasPrefix(a.Lit, "-o=") && strings.Contains(a.Lit, r.Params.Dir) {
				ok = true
			}
		}
		if !ok {
			return true, ""
		}
	}
	return false, ""
}

func checkCompoundCd(c *evalCtx, r *Rule) (bool, string) {
	for _, l := range c.Script().Chain {
		if l.Cmd != nil && l.Cmd.Name() == "cd" && l.Sep == "&&" {
			if c.scriptHubOK() {
				return false, ""
			}
			return true, ""
		}
	}
	return false, ""
}

func checkCommand(c *evalCtx, r *Rule) (bool, string) {
	for _, cmd := range c.Script().Cmds {
		if contains(r.Params.Names, cmd.Name()) {
			return true, cmd.Name()
		}
	}
	return false, ""
}

func checkEnvPrefix(c *evalCtx, r *Rule) (bool, string) {
	for _, cmd := range c.Script().Cmds {
		if len(cmd.Assigns) > 0 && contains(r.Params.Names, cmd.Name()) {
			return true, cmd.Name()
		}
	}
	return false, ""
}

func checkSemicolon(c *evalCtx, r *Rule) (bool, string) {
	return c.Script().Semicolons > 0, ""
}

// checkRedirect matches output sent to a file. `2>&1`, `>&2` and `/dev/null`
// are not files.
func checkRedirect(c *evalCtx, r *Rule) (bool, string) {
	for _, rd := range c.Script().Redirs {
		op := strings.TrimLeft(rd.Op, "0123456789*")
		switch op {
		case ">", ">>", "&>", "&>>", ">|":
		default:
			continue
		}
		t := strings.ToLower(rd.Target.Lit)
		if t == "/dev/null" || t == "nul" || t == "$null" {
			continue
		}
		return true, rd.Target.Lit
	}
	return false, ""
}

func checkCmakePreset(c *evalCtx, r *Rule) (bool, string) {
	for _, cmd := range c.Script().Cmds {
		args := cmd.Args()
		if cmd.Name() != "cmake" || len(args) == 0 {
			continue
		}
		build := false
		preset := false
		for _, a := range args {
			if a.Lit == "--build" {
				build = true
			}
			if a.Lit == "--preset" || strings.HasPrefix(a.Lit, "--preset=") {
				preset = true
			}
		}
		if preset {
			continue
		}
		a0 := args[0].Lit
		configure := a0 == "-S" || a0 == "-B" || a0 == "-G" || strings.HasPrefix(a0, "-D") ||
			a0 == "." || strings.HasPrefix(a0, "..") || strings.HasPrefix(a0, "/") ||
			(len(a0) > 2 && a0[1] == ':' && (a0[2] == '\\' || a0[2] == '/'))
		switch a0 {
		case "-E", "--version", "--help", "--list-presets":
			continue
		}
		if !build && !configure || cmd.Dir == "" {
			continue
		}
		for _, f := range []string{"CMakePresets.json", "CMakeUserPresets.json"} {
			if _, ok := c.env.Stat(filepath.Join(cmd.Dir, f)); ok {
				return true, ""
			}
		}
	}
	return false, ""
}

// checkGhAPI as a deny matches any `gh api` not starting `-X GET`. As an allow
// it matches a command that is only `gh api -X GET <endpoint>` with an endpoint
// the rule names.
func checkGhAPI(c *evalCtx, r *Rule) (bool, string) {
	s := c.Script()
	for _, cmd := range s.Cmds {
		a := cmd.Args()
		if cmd.Name() != "gh" || len(a) == 0 || a[0].Lit != "api" {
			continue
		}
		get := len(a) >= 3 && a[1].Lit == "-X" && a[2].Lit == "GET"
		if r.Decision == "allow" {
			if len(s.Cmds) != 1 || !get || len(a) < 4 {
				return false, ""
			}
			for _, re := range r.patterns {
				if re.MatchString(a[3].Lit) {
					return true, ""
				}
			}
			return false, ""
		}
		if !get {
			return true, ""
		}
	}
	return false, ""
}

// --- files ---

func (c *evalCtx) slashPath() string {
	return strings.ReplaceAll(c.in.ToolInput.FilePath, `\`, "/")
}

func checkPath(c *evalCtx, r *Rule) (bool, string) {
	p := c.slashPath()
	for _, re := range r.patterns {
		if re.MatchString(p) {
			return true, p
		}
	}
	return false, ""
}

func checkContent(c *evalCtx, r *Rule) (bool, string) {
	if len(r.patterns) > 0 {
		match := false
		for _, re := range r.patterns {
			if re.MatchString(c.slashPath()) {
				match = true
			}
		}
		if !match {
			return false, ""
		}
	}
	text := c.in.ToolInput.Content + c.in.ToolInput.NewString
	return strings.Contains(text, r.Params.Contains), ""
}

func checkFileName(c *evalCtx, r *Rule) (bool, string) {
	p := c.slashPath()
	if p == "" {
		return false, ""
	}
	leaf := p[strings.LastIndex(p, "/")+1:]
	for _, n := range r.Params.Names {
		if ok, _ := path.Match(n, leaf); ok {
			return true, leaf
		}
	}
	return false, ""
}
