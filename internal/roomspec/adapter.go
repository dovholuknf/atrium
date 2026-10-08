package roomspec

import (
	"fmt"
	"regexp"
	"strings"
)

// Adapter is everything that differs between operating systems, and nothing else. What does not differ (the layout, the
// settings, the pack contents, the plan, the exit codes) is code that does not branch on the OS.
//
// Every path an Adapter takes or returns is forward-slash ("V:/a", "/srv/a"). Native spells one the way the OS does, and is used
// only for text a person runs. An Adapter never runs a command and never changes the machine itself: GrantExamine RETURNS the
// lines for an administrator, and the room never runs escalation.
type Adapter interface {
	OS() string
	// Newline is the line ending of the files this OS keeps (the tool config files).
	Newline() string
	// Native spells a forward-slash path as the OS does.
	Native(path string) string
	// Parents are the folders ABOVE the work root, top first. The root itself is the account's own. A parent is one the
	// account needs to examine and is never given more than that.
	Parents(root string) []string
	// GrantExamine is the lines an administrator runs, one each: make the parents that are missing (top down), open each parent
	// to the account for examining only (never listing, never creating, never to every user), then make and give the work
	// root. It never has a line for the filesystem root. The lines are printed, never run.
	GrantExamine(g Grant) []string
	// MakeDirs makes the folders, and says how many were missing. fs is the machine, so a fake can stand in.
	MakeDirs(fs FS, dirs []string) (made int, err error)
	// CacheFile is the config file a cache row writes on this OS, forward-slash, or "" when the row is not a file.
	CacheFile(row CacheRow, h Home) string
	// PackDir is the folder a runner keeps its agents and skills in, or "" when this runner has no pack adapter.
	PackDir(runner string, h Home) string
	// InstallPack puts the pack's files in dir (see InstallPack).
	InstallPack(fs FS, dir string, src *PackSource, prev *PackRecord, apply bool) (PackResult, error)
}

// Grant is what GrantExamine is told.
type Grant struct {
	Account string   // the login, as the OS spells it (SG3\localai)
	Root    string   // the work root
	Bad     []string // parents the account cannot examine, including the missing ones
	Missing []string // the parents that are not there (a subset of Bad)
	// RootMissing is a root that is not there, and RootNotWritable one that is and cannot be written.
	RootMissing, RootNotWritable bool
}

// ForOS is the adapter for an OS name.
func ForOS(goos string) (Adapter, error) {
	switch goos {
	case Windows:
		return windowsAdapter{}, nil
	case Linux:
		return linuxAdapter{}, nil
	case Darwin:
		return darwinAdapter{}, nil
	}
	return nil, fmt.Errorf("no room adapter for %q", goos)
}

// parentsOf is every folder above root on a Unix path: / then /srv then /srv/work for /srv/work/localai.
func unixParents(root string) []string {
	segs := splitSegs(root)
	out := []string{"/"}
	acc := ""
	for i := 0; i < len(segs)-1; i++ {
		acc += "/" + segs[i]
		out = append(out, acc)
	}
	return out
}

func splitSegs(p string) []string {
	var out []string
	for _, s := range strings.Split(slash(p), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

var (
	psBare = regexp.MustCompile(`^[A-Za-z0-9_.:\\/-]+$`)
	shBare = regexp.MustCompile(`^[A-Za-z0-9_.:/=+-]+$`)
)

// psWord is a word of a PowerShell line: bare when it is safe, else single-quoted with a quote doubled.
func psWord(s string) string {
	if psBare.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// shWord is a word of an sh line.
func shWord(s string) string {
	if shBare.MatchString(s) {
		return s
	}
	return ShellQuote(s)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ── Windows ──────────────────────────────────────────────────────────────────

type windowsAdapter struct{ base }

func (windowsAdapter) OS() string      { return Windows }
func (windowsAdapter) Newline() string { return "\r\n" }
func (windowsAdapter) Native(p string) string {
	return strings.ReplaceAll(p, "/", `\`)
}

// Parents: V:/ then V:/a for V:/a/localai. The drive root is the first, and is where the grant matters most.
func (windowsAdapter) Parents(root string) []string {
	p := slash(root)
	if len(p) < 2 || p[1] != ':' {
		return nil
	}
	out := []string{p[:2] + "/"}
	acc := p[:2]
	segs := splitSegs(p[2:])
	for i := 0; i < len(segs)-1; i++ {
		acc += "/" + segs[i]
		out = append(out, acc)
	}
	return out
}

// GrantExamine for Windows: attributes only, `(RA,REA)`, and NOT inherited ((OI)(CI) is the root's alone), so the account can
// neither list the drive nor create at its root. The root gets full control, inherited. PowerShell lines.
func (a windowsAdapter) GrantExamine(g Grant) []string {
	var l []string
	for _, p := range g.Bad {
		if contains(g.Missing, p) {
			l = append(l, "New-Item -ItemType Directory -Force "+psWord(a.Native(p)))
		}
	}
	for _, p := range g.Bad {
		l = append(l, "icacls "+psWord(a.Native(p))+" /grant "+psWord(g.Account+":(RA,REA)"))
	}
	root := a.Native(slash(g.Root))
	if g.RootMissing {
		l = append(l, "New-Item -ItemType Directory -Force "+psWord(root))
	}
	if g.RootMissing || g.RootNotWritable {
		// Modify, not full control: the account needs to make and change files in its own folder, not to take it over.
		l = append(l, "icacls "+psWord(root)+" /grant "+psWord(g.Account+":(OI)(CI)M"))
	}
	return l
}

func (windowsAdapter) CacheFile(r CacheRow, h Home) string  { return r.File(Windows, h) }
func (windowsAdapter) PackDir(runner string, h Home) string { return packDir(runner, h) }

// ── Linux ────────────────────────────────────────────────────────────────────

type linuxAdapter struct{ base }

func (linuxAdapter) OS() string                   { return Linux }
func (linuxAdapter) Newline() string              { return "\n" }
func (linuxAdapter) Native(p string) string       { return p }
func (linuxAdapter) Parents(root string) []string { return unixParents(root) }

// GrantExamine for Linux: `setfacl -m u:<acct>:x` opens a parent to the account alone, never `chmod o+x` (every user), and
// never `/`. A missing parent is made first, top down.
func (a linuxAdapter) GrantExamine(g Grant) []string {
	return unixGrant(g, func(account, p string) string {
		return "sudo setfacl -m " + shWord("u:"+account+":x") + " " + shWord(p)
	})
}

func (linuxAdapter) CacheFile(r CacheRow, h Home) string  { return r.File(Linux, h) }
func (linuxAdapter) PackDir(runner string, h Home) string { return packDir(runner, h) }

// ── macOS ────────────────────────────────────────────────────────────────────

type darwinAdapter struct{ base }

func (darwinAdapter) OS() string                   { return Darwin }
func (darwinAdapter) Newline() string              { return "\n" }
func (darwinAdapter) Native(p string) string       { return p }
func (darwinAdapter) Parents(root string) []string { return unixParents(root) }

// GrantExamine for a Mac: `chmod +a` with a search entry, the ACL form macOS has instead of setfacl.
func (a darwinAdapter) GrantExamine(g Grant) []string {
	return unixGrant(g, func(account, p string) string {
		return "sudo chmod +a " + shWord("user:"+account+" allow search") + " " + shWord(p)
	})
}

func (darwinAdapter) CacheFile(r CacheRow, h Home) string  { return r.File(Darwin, h) }
func (darwinAdapter) PackDir(runner string, h Home) string { return packDir(runner, h) }

func unixGrant(g Grant, acl func(account, parent string) string) []string {
	var l []string
	for _, p := range g.Bad {
		if p != "/" && contains(g.Missing, p) {
			l = append(l, "sudo install -d -m 755 "+shWord(p))
		}
	}
	for _, p := range g.Bad {
		if p == "/" {
			continue // the root of the filesystem is never touched
		}
		l = append(l, acl(g.Account, p))
	}
	root := slash(g.Root)
	switch {
	case g.RootMissing:
		l = append(l, "sudo install -d -o "+shWord(g.Account)+" -m 755 "+shWord(root))
	case g.RootNotWritable:
		// the folder alone, never -R: what is inside it is not this command's to hand over
		l = append(l, "sudo chown "+shWord(g.Account)+": "+shWord(root))
	}
	return l
}

// packDir is where a runner's pack goes. Only claude has an adapter so far: codex and gemini keep agents and skills
// elsewhere (internal/runnersetup), which is a later phase.
func packDir(runner string, h Home) string {
	if runner == "claude" {
		return h.Dir + "/.claude"
	}
	return ""
}
