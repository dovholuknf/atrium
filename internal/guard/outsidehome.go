package guard

import (
	"path/filepath"
	"runtime"
	"strings"
)

// writerAll are commands every positional argument of which is written. writerLast write only the last one.
var writerAll = []string{"mkdir", "md", "touch", "tee", "new-item", "ni", "set-content", "add-content", "out-file",
	"sc", "ac"}
var writerLast = []string{"cp", "mv", "ln", "install", "copy-item", "move-item", "cpi", "mi", "copy", "move"}

// placed is one place a command writes: the word as typed, and where it resolves to. An empty place means the guard
// cannot tell.
type placed struct{ written, place string }

// checkOutsideHome matches a write or a new folder inside the card root Params.Env names that is not inside the
// card's own home, the folder Params.Dir names. A session with no root or no home, and a path the guard cannot
// resolve, is not matched.
func checkOutsideHome(c *evalCtx, r *Rule) (bool, string) {
	root := strings.TrimSpace(c.env.Getenv(r.Params.Env))
	home := strings.TrimSpace(c.env.Getenv(r.Params.Dir))
	if root == "" || home == "" {
		return false, ""
	}
	strays := func(w placed) bool {
		if w.place == "" || sameFolder(root, w.place) {
			return false
		}
		return inside(foldPath(root), foldPath(w.place)) && !inside(foldPath(home), foldPath(w.place))
	}
	if c.in.ToolInput.Command == "" {
		if !c.isFileTool() {
			return false, ""
		}
		p := c.in.ToolInput.FilePath
		return strays(placed{p, dirResolver{env: c.env}.place(c.in.CWD, p, false)}), p
	}
	for _, cmd := range c.Script().Cmds {
		res := dirResolver{env: c.env, shell: cmd.Shell}
		for _, w := range writtenPlaces(c, cmd, res) {
			if strays(w) {
				return true, w.written
			}
		}
		for _, rd := range cmd.Redirs {
			op := strings.TrimLeft(rd.Op, "0123456789*")
			if op != ">" && op != ">>" && op != "&>" && op != "&>>" && op != ">|" || rd.Target.Dyn {
				continue
			}
			if w := (placed{rd.Target.Lit, res.place(cmd.Dir, rd.Target.Lit, false)}); strays(w) {
				return true, w.written
			}
		}
	}
	return false, ""
}

// writtenPlaces are the places cmd writes, as far as the guard reads its arguments.
func writtenPlaces(c *evalCtx, cmd *Cmd, res dirResolver) []placed {
	at := func(w Word) []placed {
		if w.Dyn {
			return nil
		}
		return []placed{{w.Lit, res.place(cmd.Dir, w.Lit, false)}}
	}
	here := func(what string) []placed {
		if cmd.Dir == "" {
			return nil
		}
		return []placed{{what, filepath.Clean(cmd.Dir)}}
	}
	var out []placed
	name := cmd.Name()
	ops := positional(cmd.Args())
	switch {
	case contains(writerAll, name):
		for _, w := range ops {
			out = append(out, at(w)...)
		}
	case contains(writerLast, name) && len(ops) > 0:
		out = at(ops[len(ops)-1])
	case name == "git":
		for _, g := range c.Gits() {
			if g.cmd != cmd {
				continue
			}
			if written, place, ok := c.checkoutTarget(g); ok {
				out = append(out, placed{written, place})
			}
		}
	case name == "tar" || name == "bsdtar":
		out = tarPlaces(cmd, at, here)
	case name == "unzip":
		out = unzipPlaces(cmd, at, here)
	case name == "curl" || name == "invoke-webrequest" || name == "iwr" || name == "invoke-restmethod" || name == "irm":
		out = fetchPlaces(cmd, at, here)
	case name == "expand-archive":
		out = namedValues(cmd, []string{"destinationpath"}, 4, at)
	}
	return out
}

// tarPlaces: the -C folder an archive is extracted into (or the cwd without one), and the archive a create writes.
func tarPlaces(cmd *Cmd, at func(Word) []placed, here func(string) []placed) []placed {
	var dir, file *Word
	create, extract := false, false
	args := cmd.Args()
	for i := 0; i < len(args); i++ {
		l := args[i].Lit
		next := func() *Word {
			if i+1 < len(args) {
				i++
				return &args[i]
			}
			return nil
		}
		if args[i].Dyn {
			continue
		}
		switch {
		case strings.HasPrefix(l, "--directory="):
			dir = &Word{Lit: l[len("--directory="):]}
		case l == "--directory":
			dir = next()
		case strings.HasPrefix(l, "--file="):
			file = &Word{Lit: l[len("--file="):]}
		case l == "--file":
			file = next()
		case l == "--create":
			create = true
		case l == "--extract" || l == "--get":
			extract = true
		case strings.HasPrefix(l, "--"):
		case strings.HasPrefix(l, "-") || i == 0:
			letters := strings.TrimPrefix(l, "-")
			for j := 0; j < len(letters); j++ {
				c := letters[j]
				switch c {
				case 'c':
					create = true
				case 'x':
					extract = true
				case 'C', 'f':
					v := next()
					if j < len(letters)-1 {
						v, j = &Word{Lit: letters[j+1:]}, len(letters)
					}
					if c == 'C' {
						dir = v
					} else {
						file = v
					}
				}
			}
		}
	}
	var out []placed
	if create && file != nil {
		out = append(out, at(*file)...)
	}
	if extract {
		if dir != nil {
			out = append(out, at(*dir)...)
		} else {
			out = append(out, here("tar (extracts here)")...)
		}
	}
	return out
}

// unzipPlaces: the -d folder, or the cwd without one. -l, -t, -p and -z write nothing.
func unzipPlaces(cmd *Cmd, at func(Word) []placed, here func(string) []placed) []placed {
	args := cmd.Args()
	var dir *Word
	for i := 0; i < len(args); i++ {
		l := args[i].Lit
		switch {
		case args[i].Dyn || !strings.HasPrefix(l, "-"):
		case l == "-d" && i+1 < len(args):
			i++
			dir = &args[i]
		case strings.HasPrefix(l, "-d") && !strings.HasPrefix(l, "--"):
			dir = &Word{Lit: l[2:]}
		case l == "-l" || l == "-t" || l == "-p" || l == "-z" || l == "-v":
			return nil
		}
	}
	if dir != nil {
		return at(*dir)
	}
	return here("unzip (extracts here)")
}

// fetchPlaces: where curl and Invoke-WebRequest put what they download: -o, --output, -OutFile, or the cwd (or
// --output-dir) for -O.
func fetchPlaces(cmd *Cmd, at func(Word) []placed, here func(string) []placed) []placed {
	args := cmd.Args()
	var file, dir *Word
	remote := false
	for i := 0; i < len(args); i++ {
		if args[i].Dyn {
			continue
		}
		l, low := args[i].Lit, strings.ToLower(args[i].Lit)
		next := func() *Word {
			if i+1 < len(args) {
				i++
				return &args[i]
			}
			return nil
		}
		switch {
		case strings.HasPrefix(low, "--output-dir="):
			dir = &Word{Lit: l[len("--output-dir="):]}
		case low == "--output-dir":
			dir = next()
		case strings.HasPrefix(low, "--output="):
			file = &Word{Lit: l[len("--output="):]}
		case low == "--output":
			file = next()
		case low == "--remote-name" || low == "--remote-name-all":
			remote = true
		case outFileParam(low):
			if i2 := strings.Index(l, ":"); i2 >= 0 {
				file = &Word{Lit: l[i2+1:]}
			} else {
				file = next()
			}
		case strings.HasPrefix(l, "--"):
		case strings.HasPrefix(l, "-") && len(l) > 1 && cmd.Shell != "pwsh" || l == "-o" || l == "-O":
			letters := l[1:]
			for j := 0; j < len(letters); j++ {
				switch letters[j] {
				case 'O':
					remote = true
				case 'o':
					if j < len(letters)-1 {
						file = &Word{Lit: letters[j+1:]}
					} else {
						file = next()
					}
					j = len(letters)
				}
			}
		}
	}
	switch {
	case file != nil:
		return at(*file)
	case remote && dir != nil:
		return at(*dir)
	case remote:
		return here("curl (saves here)")
	}
	return nil
}

// outFileParam says whether a lower-cased word is Invoke-WebRequest's -OutFile, or an abbreviation that is only it.
func outFileParam(low string) bool {
	if i := strings.Index(low, ":"); i >= 0 {
		low = low[:i]
	}
	return len(low) >= 5 && strings.HasPrefix("-outfile", low)
}

// namedValues are the values of a PowerShell parameter written by name, in full or shortened to atLeast letters after the
// dash, as `-Name value` or `-Name:value`.
func namedValues(cmd *Cmd, names []string, atLeast int, at func(Word) []placed) []placed {
	args := cmd.Args()
	var out []placed
	for i := 0; i < len(args); i++ {
		if args[i].Dyn || !strings.HasPrefix(args[i].Lit, "-") {
			continue
		}
		l := strings.ToLower(args[i].Lit[1:])
		val := ""
		if k := strings.Index(l, ":"); k >= 0 {
			l, val = l[:k], args[i].Lit[1+k+1:]
		}
		for _, n := range names {
			if len(l) < atLeast || !strings.HasPrefix(n, l) {
				continue
			}
			if val != "" {
				out = append(out, at(Word{Lit: val})...)
			} else if i+1 < len(args) {
				i++
				out = append(out, at(args[i])...)
			}
			break
		}
	}
	if len(out) == 0 {
		if p := positional(args); len(p) >= 2 && !strings.HasPrefix(args[0].Lit, "-") {
			out = at(p[1])
		}
	}
	return out
}

// foldPath is p cleaned for comparing. A Windows path, a drive letter or a Windows or macOS host, is case-blind, and a
// drive path's backslashes are separators on any host.
func foldPath(p string) string {
	drive := len(p) >= 2 && p[1] == ':'
	if drive {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	p = filepath.Clean(filepath.FromSlash(p))
	if drive || runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

func sameFolder(a, b string) bool { return foldPath(a) == foldPath(b) }
