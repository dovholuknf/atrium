package guard

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The git policy: claude works only on its own claude/* branches and never
// reaches a remote, except this room's hub remote. Every git command in a
// script is read once into a gitCall, with the directory it runs in and its
// aliases resolved there, and the git checks read that.

type gitCall struct {
	cmd *Cmd
	dir string
	// bad is a global option that runs configuration, like `-c`.
	bad string
	// onlyC says every global option was `-C`.
	onlyC bool
	// typed is the subcommand as written; sub and args are after aliases.
	typed string
	sub   string
	args  []Word
	// aliased is true when sub came from an alias.
	aliased bool
	// shellAlias or loop: an alias that is a shell command, or does not end.
	shellAlias string
	loop       bool
	// unknown: not a git command and not an alias the guard could resolve.
	unknown bool
}

// gitBuiltins are git's own commands, so the common case asks git nothing.
// An alias cannot shadow one of them: git ignores it.
var gitBuiltins = toSet(strings.Fields(`add am annotate apply archive bisect blame branch bundle cat-file
check-attr check-ignore check-mailmap check-ref-format checkout checkout-index cherry cherry-pick citool
clean clone column commit commit-graph commit-tree config count-objects credential describe diff
diff-files diff-index diff-tree difftool fast-export fast-import fetch fetch-pack filter-branch
for-each-ref for-each-repo format-patch fsck gc get-tar-commit-id grep hash-object help index-pack init
interpret-trailers log ls-files ls-remote ls-tree mailinfo mailsplit maintenance merge merge-base
merge-file merge-index merge-tree mktag mktree multi-pack-index mv name-rev notes pack-objects
pack-refs patch-id prune prune-packed pull push range-diff read-tree rebase reflog remote repack
replace rerere reset restore rev-list rev-parse revert rm send-pack shortlog show show-branch show-index
show-ref sparse-checkout stash status stripspace submodule switch symbolic-ref tag unpack-file
unpack-objects update-index update-ref update-server-info var verify-commit verify-pack verify-tag
version whatchanged worktree write-tree gui instaweb request-pull`))

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Gits is every git command in the script, analysed.
func (c *evalCtx) Gits() []*gitCall {
	if c.gitsOK {
		return c.gits
	}
	c.gitsOK = true
	s := c.Script()
	r := dirResolver{env: c.env, shell: s.Shell}
	var listed map[string]bool
	for _, cmd := range s.Cmds {
		if cmd.Name() != "git" {
			continue
		}
		g := &gitCall{cmd: cmd, dir: cmd.Dir, onlyC: true}
		args := cmd.Args()
		i := 0
		for ; i < len(args) && strings.HasPrefix(args[i].Lit, "-"); i++ {
			a := args[i].Lit
			name, _, hasVal := strings.Cut(a, "=")
			switch name {
			case "-C":
				i++
				if i < len(args) && !args[i].Dyn && g.dir != "" {
					g.dir = r.path(g.dir, args[i].Lit, false)
				} else {
					g.dir = ""
				}
				continue
			case "-c", "--config-env", "--exec-path":
				if name == "--exec-path" && !hasVal {
					break
				}
				g.bad = name
				if !hasVal && name != "--exec-path" {
					i++
				}
			case "--git-dir", "--work-tree", "--namespace", "--super-prefix":
				g.dir = ""
				if !hasVal {
					i++
				}
			}
			g.onlyC = false
		}
		if i >= len(args) {
			c.gits = append(c.gits, g)
			continue
		}
		if args[i].Dyn {
			g.unknown = true
			g.typed = args[i].Lit
			g.sub = g.typed
			c.gits = append(c.gits, g)
			continue
		}
		g.typed = args[i].Lit
		g.sub = g.typed
		g.args = append([]Word(nil), args[i+1:]...)
		for n := 0; !gitBuiltins[strings.ToLower(g.sub)]; n++ {
			if n >= 10 {
				g.loop = true
				break
			}
			if g.dir == "" {
				// An alias in a repository the guard cannot see.
				g.unknown = true
				break
			}
			out, err := c.env.Git(g.dir, "config", "--get", "alias."+g.sub)
			exp := strings.TrimSpace(out)
			if err != nil || exp == "" {
				if listed == nil {
					listed = map[string]bool{}
					lo, _ := c.env.Git(g.dir, "--list-cmds=builtins,main")
					for _, l := range strings.Fields(lo) {
						listed[l] = true
					}
				}
				if !listed[g.sub] {
					g.unknown = true
				}
				break
			}
			if strings.HasPrefix(exp, "!") {
				g.shellAlias = g.sub
				break
			}
			f := strings.Fields(exp)
			var words []Word
			for _, x := range f[1:] {
				words = append(words, Word{Lit: x})
			}
			g.sub = f[0]
			g.args = append(words, g.args...)
			g.aliased = true
		}
		c.gits = append(c.gits, g)
	}
	return c.gits
}

// positional is the args that are not options.
func positional(ws []Word) []Word {
	var out []Word
	for _, w := range ws {
		if !strings.HasPrefix(w.Lit, "-") || w.Dyn {
			out = append(out, w)
		}
	}
	return out
}

func checkGitAliasConfig(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if g.sub != "config" {
			continue
		}
		touches, reads := false, false
		for _, a := range g.args {
			l := strings.ToLower(a.Lit)
			if strings.Contains(l, "alias.") {
				touches = true
			}
			switch l {
			case "--get", "--get-all", "--get-regexp", "--list", "-l", "get", "list":
				reads = true
			}
		}
		if touches && !reads {
			return true, "config"
		}
	}
	return false, ""
}

func checkGitGlobalOption(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if g.bad != "" {
			for _, o := range r.Params.Options {
				if g.bad == o {
					return true, g.bad
				}
			}
		}
	}
	return false, ""
}

func checkGitAlias(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if g.shellAlias != "" {
			return true, g.shellAlias
		}
		if g.loop {
			return true, g.typed
		}
	}
	return false, ""
}

func checkGitUnknown(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if !g.unknown {
			continue
		}
		if strings.EqualFold(g.sub, "lfs") {
			if p := positional(g.args); len(p) > 0 && contains(r.Params.Verbs, p[0].Lit) {
				return true, "lfs " + p[0].Lit
			}
			continue
		}
		return true, g.sub
	}
	return false, ""
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if strings.EqualFold(x, y) {
			return true
		}
	}
	return false
}

// hubBranch is a branch a hub push may name: no `+`, no `:`, no leading dash.
var hubBranch = regexp.MustCompile(`^[A-Za-z0-9_][\w./-]*$`)

// agentAddr is the only kind of room address the hub rule trusts.
var agentAddr = regexp.MustCompile(`^http://127\.0\.0\.1:\d+$`)

func checkGitRemote(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if !contains(r.Params.Verbs, g.sub) {
			continue
		}
		if !c.hubOK(g, r.Params.Remotes) {
			return true, g.sub
		}
	}
	return false, ""
}

// hubOK says whether g is the one remote op allowed: exactly `git push [-u]
// hub <branch>` or `git fetch hub`, as typed (not through an alias), alone in
// the command or after one directory change, and only when every url of that
// remote in the directory it runs in is this room's forwarder.
func (c *evalCtx) hubOK(g *gitCall, remotes []string) bool {
	if g.aliased || g.bad != "" || !g.onlyC || g.dir == "" {
		return false
	}
	var remote string
	a := g.args
	for _, w := range a {
		if w.Dyn {
			return false
		}
	}
	switch g.sub {
	case "push":
		if len(a) == 3 && a[0].Lit == "-u" {
			a = a[1:]
		}
		if len(a) != 2 || !hubBranch.MatchString(a[1].Lit) {
			return false
		}
		remote = a[0].Lit
	case "fetch":
		if len(a) != 1 {
			return false
		}
		remote = a[0].Lit
	default:
		return false
	}
	if !contains(remotes, remote) {
		return false
	}
	if !c.hubShape(g.cmd) {
		return false
	}
	agent := strings.TrimRight(strings.TrimSpace(c.env.Agent()), "/")
	if !agentAddr.MatchString(agent) {
		return false
	}
	var urls []string
	for _, args := range [][]string{
		{"remote", "get-url", "--all", remote},
		{"remote", "get-url", "--push", "--all", remote},
	} {
		out, err := c.env.Git(g.dir, args...)
		if err != nil {
			return false
		}
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				urls = append(urls, l)
			}
		}
	}
	if len(urls) == 0 {
		return false
	}
	for _, u := range urls {
		if !strings.HasPrefix(u, agent+"/git/") {
			return false
		}
	}
	return true
}

// hubShape is the whole script being the hub op, or one directory change then
// the hub op, joined by a newline or &&.
func (c *evalCtx) hubShape(git *Cmd) bool {
	s := c.Script()
	switch len(s.Chain) {
	case 1:
		return len(s.Cmds) == 1 && s.Chain[0].Cmd == git
	case 2:
		cd := s.Chain[0]
		if len(s.Cmds) != 2 || cd.Cmd == nil || s.Chain[1].Cmd != git {
			return false
		}
		switch cd.Cmd.Name() {
		case "cd", "sl", "set-location", "pushd":
		default:
			return false
		}
		return cd.Sep == "\n" || cd.Sep == "&&"
	}
	return false
}

// scriptHubOK says the script is an allowed hub op, which is the one place a
// `cd x && git ...` is allowed.
func (c *evalCtx) scriptHubOK() bool {
	if c.hub == nil {
		ok := false
		for _, g := range c.Gits() {
			if g.sub == "push" || g.sub == "fetch" {
				ok = c.hubOK(g, []string{"hub", "atrium-hub"})
				break
			}
		}
		c.hub = &ok
	}
	return *c.hub
}

func checkGitPlainCheckout(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if g.sub == "checkout" && !hasCreate(g.args, "-b", "-B") {
			return true, ""
		}
	}
	return false, ""
}

func hasCreate(args []Word, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a.Lit == f {
				return true
			}
		}
	}
	return false
}

// valueOf is the word after the first of flags.
func valueOf(args []Word, flags ...string) (Word, bool) {
	for i, a := range args {
		for _, f := range flags {
			if a.Lit == f && i+1 < len(args) {
				return args[i+1], true
			}
		}
	}
	return Word{}, false
}

var branchCreateFlag = regexp.MustCompile(`^(?:-f|--force|-t|--track(?:=\S+)?|--no-track|-q|--quiet)$`)
var refChars = regexp.MustCompile(`^[\w./@^~:{}+-]+$`)

func checkGitBranchName(c *evalCtx, r *Rule) (bool, string) {
	pre := r.Params.Prefix
	bad := func(w Word) bool { return w.Dyn || !strings.HasPrefix(w.Lit, pre) }
	for _, g := range c.Gits() {
		switch g.sub {
		case "checkout":
			if n, ok := valueOf(g.args, "-b", "-B"); ok && bad(n) {
				return true, n.Lit
			}
		case "switch":
			n, ok := valueOf(g.args, "-c", "-C", "--create", "--force-create", "--orphan")
			if !ok {
				p := positional(g.args)
				if len(p) == 0 {
					return true, ""
				}
				n = p[0]
			}
			if bad(n) {
				return true, n.Lit
			}
		case "branch":
			var flags, names []Word
			for _, a := range g.args {
				if strings.HasPrefix(a.Lit, "-") && !a.Dyn {
					flags = append(flags, a)
				} else {
					names = append(names, a)
				}
			}
			create := len(names) == 2 && !names[1].Dyn && refChars.MatchString(names[1].Lit)
			for _, f := range flags {
				if !branchCreateFlag.MatchString(f.Lit) {
					create = false
				}
			}
			if create {
				names = names[:1]
			}
			for _, n := range names {
				if bad(n) {
					return true, n.Lit
				}
			}
		}
	}
	return false, ""
}

func checkGitCurrentBranch(c *evalCtx, r *Rule) (bool, string) {
	for _, g := range c.Gits() {
		if !contains(r.Params.Verbs, g.sub) {
			continue
		}
		if g.dir == "" {
			return true, "the guard cannot tell which repository this runs in"
		}
		b := c.branch(g.dir)
		if !strings.HasPrefix(b, r.Params.Prefix) {
			if b == "" {
				return true, "no claude/* branch is checked out"
			}
			return true, "current branch is '" + b + "'"
		}
	}
	return false, ""
}

// branch is the branch checked out in dir. Mid-rebase HEAD is detached, and the
// branch being rebased is in head-name.
func (c *evalCtx) branch(dir string) string {
	out, err := c.env.Git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	b := strings.TrimSpace(out)
	if b != "HEAD" {
		return b
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		hn, err := c.env.Git(dir, "rev-parse", "--git-path", d+"/head-name")
		hn = strings.TrimSpace(hn)
		if err != nil || hn == "" {
			continue
		}
		if !filepath.IsAbs(hn) {
			hn = filepath.Join(dir, hn)
		}
		if raw, err := c.env.ReadFile(hn); err == nil {
			return strings.TrimPrefix(strings.TrimSpace(string(raw)), "refs/heads/")
		}
	}
	return "HEAD"
}
