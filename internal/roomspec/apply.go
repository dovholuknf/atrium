package roomspec

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/runnersetup"
)

// Host is a machine an apply may change: the plan's View, and the means to write.
type Host struct {
	FS       FS
	Env      Env
	Tools    Tools
	Settings Settings
	Fetch    Fetcher // nil: no pack source
	Now      func() time.Time
	Need     []string
}

// View is the read-only face of a Host, which is what a step reads through.
func (h Host) View() View {
	v := View{FS: h.FS, Env: h.Env, Tools: h.Tools, Need: h.Need}
	if h.Settings != nil {
		v.Settings = h.Settings
	}
	if h.Fetch != nil {
		v.Latest = h.Fetch.Latest
	}
	return v
}

// Apply converges the machine to the spec and writes the lock. It is the same path as Plan, acting on each step that is todo.
func Apply(spec *Spec, a Adapter, host Host) Lock {
	v := host.View()
	lk := newLock(spec, host.FS)
	run(spec, a, &lk, v, &host)
	now := time.Now
	if host.Now != nil {
		now = host.Now
	}
	lk.AppliedAt = now().UTC().Format(time.RFC3339)
	if err := WriteLock(host.FS, a, lk); err != nil {
		lk.Steps = append(lk.Steps, Step{Step: "lock", Status: StatusFail, Detail: err.Error()})
	}
	return lk
}

// LockPath is where the room keeps its lock.
func LockPath(h Home) string { return h.Dir + "/.atrium/room.lock" }

// WriteLock writes the lock into the account's ~/.atrium.
func WriteLock(f FS, a Adapter, lk Lock) error {
	b, err := MarshalLock(lk)
	if err != nil {
		return err
	}
	p := LockPath(f.Home())
	// A rerun that found everything as it was says the same thing again. The time alone is not a change worth a write.
	if old, err := f.ReadFile(p); err == nil {
		if prev, perr := ParseLock(old); perr == nil {
			a, b := prev, lk
			a.AppliedAt, b.AppliedAt = "", ""
			if ab, _ := MarshalLock(a); string(ab) != "" {
				if bb, _ := MarshalLock(b); string(ab) == string(bb) {
					return nil
				}
			}
		}
	}
	if err := f.MkdirAll(path.Dir(p), 0o755); err != nil {
		return fmt.Errorf("make %s: %w", a.Native(path.Dir(p)), err)
	}
	if err := f.WriteFile(p, b, 0o644); err != nil {
		return fmt.Errorf("write the lock %s: %w", a.Native(p), err)
	}
	return nil
}

// applyCache makes one cache edit. A file is read again here, because two rows may share one (go's GOMODCACHE and GOCACHE) and
// each edit is made on what the last one left. An edit that is already so writes nothing.
func applyCache(host *Host, a Adapter, e cacheEdit) error {
	if e.same {
		return nil
	}
	if e.format == FormatUserEnv {
		return host.Env.SetUserEnv(e.row.Key, e.value)
	}
	if e.utf16 {
		return nil
	}
	txt := Text{NL: a.Newline()}
	if b, err := host.FS.ReadFile(e.file); err == nil {
		var u16 bool
		if txt, u16 = ParseText(b, a.Newline()); u16 {
			return nil
		}
	}
	var nw []string
	switch e.format {
	case FormatKV:
		nw = EditKV(txt.Lines, e.row.Key, e.value)
	case FormatINI:
		nw = EditINI(txt.Lines, e.row.Section, e.row.Key, e.value)
	case FormatProfile:
		nw = EditProfile(txt.Lines, e.row.Key, e.value)
	}
	if EqualLines(txt.Lines, nw) {
		return nil
	}
	if err := host.FS.MkdirAll(path.Dir(e.file), 0o755); err != nil {
		return err
	}
	txt.Lines = nw
	return host.FS.WriteFile(e.file, txt.Bytes(), 0o644) // its own line ending and BOM, the OS's only for a new file
}

// accountEnv reads a variable of the account's environment: its user environment where the OS keeps one (the Windows registry),
// else the environment this process was started with. A variable set only in a login profile is not seen by an ssh command, so
// a room that keeps CODEX_HOME there is not followed: set it where the account's processes get it.
func accountEnv(e ReadEnv) func(string) string {
	return func(k string) string {
		if e != nil {
			if s, ok := e.UserEnv(k); ok && s != "" {
				return s
			}
		}
		return os.Getenv(k)
	}
}

// packMoveVar is the variable that moves each runner's pack folder.
var packMoveVar = map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "gemini": "GEMINI_CLI_HOME"}

// expandedEnv is env with %NAME% expanded in the values of packMoveVar on Windows, where HKCU\Environment keeps a REG_EXPAND_SZ
// value as written. USERPROFILE and HOME are the account's home as the room reads it, any other name comes from env. A name
// that is not known stays as written, and checkMovedPackDir refuses the % left over.
func expandedEnv(goos string, h Home, env func(string) string) func(string) string {
	if goos != Windows {
		return env
	}
	moves := map[string]bool{}
	for _, k := range packMoveVar {
		moves[k] = true
	}
	return func(k string) string {
		v := env(k)
		if !moves[k] || !strings.Contains(v, "%") {
			return v
		}
		var b strings.Builder
		for i := 0; i < len(v); {
			if v[i] == '%' {
				if j := strings.IndexByte(v[i+1:], '%'); j > 0 {
					name := v[i+1 : i+1+j]
					val := ""
					switch strings.ToUpper(name) {
					case "USERPROFILE", "HOME":
						val = h.Dir
					default:
						val = env(name)
					}
					if val != "" {
						b.WriteString(val)
						i += j + 2
						continue
					}
				}
			}
			b.WriteByte(v[i])
			i++
		}
		return b.String()
	}
}

// printable drops a control character from a value a row quotes.
func printable(r rune) rune {
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

// otherPackDirs are the folders the other runners keep their packs in.
func otherPackDirs(runner string, h Home, env func(string) string) []string {
	var out []string
	for r := range packMoveVar {
		if r == runner {
			continue
		}
		if l, ok := packLayout(r, h, env); ok {
			out = append(out, l.Dir)
		}
	}
	return out
}

// checkMovedPackDir is why a folder a variable moved a runner's pack to cannot be used, or "". It must be absolute for the OS,
// hold no .. and no % or $ left unexpanded, be neither inside nor around another runner's folder, and lie under the account's
// home or the work root.
func checkMovedPackDir(goos, dir string, h Home, workRoot string, others []string) string {
	d := slash(dir)
	fold := func(s string) string {
		if goos == Windows {
			return strings.ToLower(s)
		}
		return s
	}
	abs := strings.HasPrefix(d, "/")
	if goos == Windows {
		abs = len(d) >= 3 && d[1] == ':' && d[2] == '/' && (d[0]|0x20 >= 'a' && d[0]|0x20 <= 'z')
	}
	if !abs {
		return "it is not an absolute path on " + goos
	}
	if strings.ContainsAny(d, "%$") {
		return "it holds a % or $ that was not expanded"
	}
	if goos == Windows {
		if strings.Contains(d[2:], ":") {
			return "it holds a : after the drive letter"
		}
		for i := 0; i+1 < len(d); i++ {
			if d[i] == '~' && d[i+1] >= '0' && d[i+1] <= '9' {
				return "it holds a ~ and a digit, a short 8.3 name that could be any folder"
			}
		}
	}
	for _, seg := range strings.Split(d, "/") {
		if seg == ".." {
			return "it holds .."
		}
	}
	d = path.Clean(d) // /./ and // are the same folder, so the comparisons below see it as it is
	under := func(p, root string) bool {
		root = fold(slash(root))
		return root != "" && strings.HasPrefix(fold(p)+"/", root+"/") && fold(p) != root
	}
	for _, o := range others {
		o = slash(o)
		if fold(d) == fold(o) || under(d, o) || under(o, d) {
			return "it is the folder of another runner's pack, or inside or around it: " + o
		}
	}
	if !under(d, h.Dir) && !under(d, workRoot) {
		return "it is not under the account's home or the work root"
	}
	return ""
}

func packStep(spec *Spec, a Adapter, v View, host *Host, pk Pack, lk *Lock, add func(step, status, detail string)) {
	name := "agent-pack"
	if pk.Runner != "claude" {
		name += "-" + pk.Runner
	}
	home := v.FS.Home()
	raw := accountEnv(v.Env)
	env := expandedEnv(spec.OS, home, raw)
	layout, ok := packLayout(pk.Runner, home, env)
	dir := a.PackDir(pk.Runner, home, env)
	if !ok || dir == "" {
		add(name, StatusWarn, fmt.Sprintf("no pack adapter for %s", pk.Runner))
		return
	}
	// a variable that moves the folder is followed only to a place this account owns
	if k := packMoveVar[pk.Runner]; k != "" && env(k) != "" {
		if why := checkMovedPackDir(spec.OS, layout.Dir, home, spec.WorkRoot, otherPackDirs(pk.Runner, home, env)); why != "" {
			add(name, StatusFail, fmt.Sprintf("%s=\"%s\" is not used, nothing was installed: %s", k, strings.Map(printable, raw(k)), why))
			return
		}
		layout.Dir = path.Clean(slash(layout.Dir))
		dir = layout.Dir
	}
	full, _ := spec.PackFor(pk.Runner)
	rec := ReadRecord(v.FS, dir)

	if host == nil {
		latest := ""
		if v.Latest != nil {
			latest, _ = v.Latest(full.Repo, full.Branch)
		}
		var have []string
		need := v.Need
		if !layout.Agents {
			need = nil // a runner that reads no agents is not missing the panel's
		}
		for _, n := range need {
			if fi, err := v.FS.Lstat(dir + "/agents/" + n + ".md"); err == nil && !fi.IsDir() {
				have = append(have, n)
			}
		}
		st, d := PackVerdict(rec, have, latest, need)
		add(name, st, d)
		return
	}

	if host.Fetch == nil {
		add(name, StatusWarn, "no pack source (pass --hub-addr or --pack-dir)")
		return
	}
	src, err := host.Fetch.Fetch(full.Repo, full.Branch, full.From)
	if err != nil {
		add(name, StatusFail, fmt.Sprintf("could not get the pack %s: %v", full.Repo, err))
		return
	}
	if src, err = forRunner(src, layout); err != nil {
		add(name, StatusFail, fmt.Sprintf("the pack %s has nothing for %s: %v", full.Repo, pk.Runner, err))
		return
	}
	res, err := a.InstallPack(host.FS, dir, src, rec, true)
	if err != nil {
		add(name, StatusFail, err.Error())
		return
	}
	short := src.Commit
	if len(short) > 9 {
		short = short[:9]
	}
	var notes []string
	if len(res.Edited) > 0 {
		notes = append(notes, fmt.Sprintf("replaced %d edited here: %s", len(res.Edited), strings.Join(res.Edited, ", ")))
	}
	if len(src.Skipped) > 0 {
		notes = append(notes, "not given, a link in the repo: "+strings.Join(src.Skipped, ", "))
	}
	extra := ""
	if len(notes) > 0 {
		extra = ". " + strings.Join(notes, ". ")
	}
	switch {
	case len(res.Refused) > 0:
		add(name, StatusWarn, fmt.Sprintf("%d files not written, a folder on their way is a link: %s. the record was not written, so a rerun tries again%s", len(res.Refused), strings.Join(res.Refused, ", "), extra))
	case res.Changed == 0 && res.Was == src.Commit:
		add(name, StatusOK, fmt.Sprintf("the agent pack is already at %s, %d files%s", short, res.Files, extra))
	default:
		add(name, StatusDone, fmt.Sprintf("the agent pack is at %s: %d files, %d written%s", short, res.Files, res.Changed, extra))
	}
	if len(res.Refused) == 0 {
		lk.Packs = append(lk.Packs, PackLock{Runner: pk.Runner, Repo: full.Repo, Commit: src.Commit, Files: res.Files})
	}
}

// forRunner is the part of a pack a runner reads: agents only where it has them, skills only where it has them. A pack with
// none of what the runner reads is an error, so a repository with no folder for it is said and not installed as nothing.
func forRunner(src *PackSource, l runnersetup.PackLayout) (*PackSource, error) {
	out := *src
	out.Files = map[string][]byte{}
	for rel, b := range src.Files {
		switch {
		case strings.HasPrefix(rel, "agents/") && l.Agents, strings.HasPrefix(rel, "skills/") && l.Skills:
			out.Files[rel] = b
		}
	}
	if len(out.Files) == 0 {
		return nil, errors.New("no agents or skills it reads")
	}
	return &out, nil
}
