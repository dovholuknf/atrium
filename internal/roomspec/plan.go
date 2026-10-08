package roomspec

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Tools asks an installed tool its own answer ("npm config get cache"), to see something overriding the file that was written.
// It is a question, never a change.
type Tools interface {
	Query(argv []string) (out string, ok bool)
}

// ReadSettings reads the room's settings. A running room holds its store, so ErrRoomRunning says it cannot be read here.
type ReadSettings interface {
	Get(key string) (string, error)
}

// Settings is the room's settings, written the way `atrium room set` does.
type Settings interface {
	ReadSettings
	Set(key, value string) error
}

// ErrRoomRunning is a settings store that a running room holds: it can be neither read nor set until the room stops.
var ErrRoomRunning = errors.New("the room is running and holds its settings")

// View is everything a plan may use. There is no write method anywhere in it, so a plan has no write path: this is the
// structure, not a flag. Apply is given a Host, which is a View plus the means to change things.
type View struct {
	FS       ReadFS
	Env      ReadEnv
	Tools    Tools        // nil: no tool is asked
	Settings ReadSettings // nil: the settings are not read
	// Latest asks the hub's mirror for a branch's commit. nil: it is not asked.
	Latest func(repo, branch string) (string, error)
	// Need are the agents something relies on (the review panel's).
	Need []string
}

// settingsOrder is the room settings a spec sets, in order, with the step each reports as.
var settingKeys = []struct{ Step, Key string }{
	{"git-root", "git_root"}, {"scm-root", "scm_root"}, {"reviews-root", "reviews_root"}, {"handoff-dir", "context_handoff_dir"},
}

func settingWant(p Paths, key string) string {
	switch key {
	case "git_root", "scm_root":
		return p.Git
	case "reviews_root":
		return p.Reviews
	}
	return p.Handoff
}

// Plan is what an apply would do, step by step. It reads, and cannot write.
func Plan(spec *Spec, a Adapter, v View) Lock {
	lk := newLock(spec, v.FS)
	run(spec, a, &lk, v, nil)
	return lk
}

// checkRootHere judges the work root against this machine's disk, which a spec alone cannot: the root as the machine really
// reaches it (a link or junction followed, a short name written out) must not be, hold or lie in the account's home folder or
// another user's, and the account's own home is the real one, whatever the profile folder is called. It returns why not, or "".
func checkRootHere(spec *Spec, r ReadFS, root string) string {
	home := r.Home().Dir
	homeReal := home
	if home != "" {
		homeReal = r.Resolve(home)
	}
	real := r.Resolve(root)
	for _, c := range []string{root, real} {
		if home != "" && (ContainsHome(c, home) || ContainsHome(c, homeReal)) {
			return fmt.Sprintf("the work root %s is or holds the account's home folder %s. a work root is a folder of its own", c, home)
		}
	}
	if real == root {
		return ""
	}
	if why := CheckWorkRoot(spec.OS, real); why != "" {
		return fmt.Sprintf("the work root %s is reached through a link, and the folder it means is %s, which %s", root, real, why)
	}
	for _, own := range []string{home, homeReal} {
		if own != "" && within(spec.OS, real, own) {
			return ""
		}
	}
	if why := checkAnotherHome(spec.OS, real, spec.Account, ""); why != "" {
		return fmt.Sprintf("the work root %s is reached through a link, and the folder it means is %s, which %s", root, real, why)
	}
	return ""
}

func newLock(spec *Spec, r ReadFS) Lock {
	return Lock{Version: Version, SpecHash: spec.Hash, OS: spec.OS, Account: r.Login(), WorkRoot: spec.Resolve().Root,
		Steps: []Step{}, AdminLines: []string{}, Packs: []PackLock{}}
}

// run is the one path both a plan and an apply take. It reads through v. When host is nil that is all it does; when it is not,
// each step that is todo is acted on and read again, so the step reports what is true afterwards.
func run(spec *Spec, a Adapter, lk *Lock, v View, host *Host) {
	add := func(step, status, detail string) {
		lk.Steps = append(lk.Steps, Step{Step: step, Status: status, Detail: detail})
	}
	p := spec.Resolve()

	// The account first: caches and packs are written to the profile of whoever runs this, so the wrong account would
	// configure the wrong profile and say ok.
	if !sameAccount(spec.Account, v.FS.Login()) {
		add("account", StatusFail, fmt.Sprintf("this runs as %s and the spec's account is %s. run it as that account (ssh in as it)", v.FS.Login(), spec.Account))
		return
	}

	if spec.WorkRoot == "" {
		// a spec of packs alone: no work root, no folders, caches or settings
		for _, pk := range spec.Packs {
			packStep(spec, a, v, host, pk, lk, add)
		}
		return
	}

	if why := checkRootHere(spec, v.FS, p.Root); why != "" {
		add("work-root", StatusFail, why)
		return
	}

	// ── the work root ──
	st := examineRoot(spec, a, v)
	if host != nil && st.rootMissing && len(st.bad) == 0 && !st.driveMissing {
		if _, err := a.MakeDirs(host.FS, []string{p.Root}); err == nil {
			st = examineRoot(spec, a, v)
			st.madeNow = true
		} else {
			st.makeFailed = true
		}
	}
	step, lines := rootStep(spec, a, st, v.FS.Login())
	lk.Steps = append(lk.Steps, step)
	if step.Status == StatusHuman || step.Status == StatusFail {
		lk.AdminLines = append(lk.AdminLines, lines...)
		return // nothing else is touched until the root is sound
	}

	// ── the folders ──
	caches := spec.CacheSet()
	dirs := Dirs(p, caches)
	missing := missingDirs(v.FS, dirs, st.rootMissing && !st.madeNow)
	switch {
	case len(missing) == 0:
		add("work-dirs", StatusOK, "the folders under the work root are there")
	case host != nil:
		n, err := a.MakeDirs(host.FS, dirs)
		if err != nil {
			add("work-dirs", StatusFail, err.Error())
		} else {
			add("work-dirs", StatusDone, fmt.Sprintf("made %d under %s", n, p.Root))
		}
	default:
		add("work-dirs", StatusTodo, fmt.Sprintf("%d to make under %s. `atrium room setup --apply` makes them", len(missing), p.Root))
	}

	// ── the tool caches ──
	cacheStep(spec, a, v, host, add)

	// ── the room settings ──
	for _, k := range settingKeys {
		settingStep(spec, a, v, host, p, k.Step, k.Key, add)
	}

	// ── the packs ──
	for _, pk := range spec.Packs {
		packStep(spec, a, v, host, pk, lk, add)
	}
}

// sameAccount compares logins as the OS spells them. A bare name matches the same name in any domain; when both name a domain
// (DOMAIN\name) it must be the same one.
func sameAccount(want, got string) bool {
	split := func(s string) (domain, name string) {
		if i := strings.LastIndex(s, `\`); i >= 0 {
			domain, s = s[:i], s[i+1:]
		}
		if i := strings.Index(s, "@"); i >= 0 {
			s = s[:i]
		}
		return strings.ToLower(domain), strings.ToLower(s)
	}
	wd, wn := split(want)
	gd, gn := split(got)
	if wn != gn {
		return false
	}
	return wd == "" || gd == "" || wd == gd
}

// ── the work root ────────────────────────────────────────────────────────────

type rootState struct {
	parents, bad, missing []string
	driveMissing          bool
	rootExists, writable  bool
	rootMissing           bool
	madeNow, makeFailed   bool
}

func examineRoot(spec *Spec, a Adapter, v View) rootState {
	p := spec.Resolve()
	st := rootState{parents: a.Parents(p.Root)}
	for i, par := range st.parents {
		switch v.FS.Examine(par) {
		case ExamineOK:
		case ExamineMissing:
			st.bad = append(st.bad, par)
			st.missing = append(st.missing, par)
			if i == 0 && spec.OS == Windows {
				st.driveMissing = true
			}
		default:
			st.bad = append(st.bad, par)
		}
	}
	if fi, err := v.FS.Lstat(p.Root); err == nil && fi.IsDir() {
		st.rootExists = true
		st.writable = v.FS.Writable(p.Root)
	}
	st.rootMissing = !st.rootExists
	return st
}

// rootStep is the work-root row and, when an administrator is needed, the lines.
func rootStep(spec *Spec, a Adapter, st rootState, login string) (Step, []string) {
	p := spec.Resolve()
	row := func(status, detail string) Step { return Step{Step: "work-root", Status: status, Detail: detail} }
	if st.driveMissing {
		return row(StatusHuman, fmt.Sprintf("the drive %s is not there for %s (a mapped drive belongs to one logon session and is not there over ssh). pick a work_root on a drive that is", st.parents[0], login)), nil
	}
	notWritable := st.rootExists && !st.writable
	if len(st.bad) > 0 || notWritable || st.makeFailed {
		g := Grant{Account: login, Root: p.Root, Bad: st.bad, Missing: st.missing, RootMissing: st.rootMissing, RootNotWritable: notWritable}
		var why []string
		var closed []string
		for _, b := range st.bad {
			if !contains(st.missing, b) {
				closed = append(closed, b)
			}
		}
		if n := len(st.missing); n > 0 {
			verb := "is"
			if n > 1 {
				verb = "are"
			}
			why = append(why, fmt.Sprintf("%s %s not there, and %s may not make %s", strings.Join(natives(a, st.missing), ", "), verb, login, pronoun(n)))
		}
		if len(closed) > 0 {
			why = append(why, fmt.Sprintf("%s cannot examine %s. Claude Code examines every folder on the way to a path it writes and raises its own unanswerable prompt when it cannot", login, strings.Join(natives(a, closed), ", ")))
		} else if len(st.missing) > 0 {
			why = append(why, fmt.Sprintf("once made, each is given to %s as an examine-only entry, because Claude Code examines every folder on the way to a path it writes and raises its own unanswerable prompt when it cannot", login))
		}
		if st.rootMissing && len(st.bad) == 0 {
			why = append(why, fmt.Sprintf("%s is missing and %s may not make it", p.Root, login))
		}
		if notWritable {
			why = append(why, fmt.Sprintf("%s exists and %s cannot write to it", p.Root, login))
		}
		return row(StatusHuman, strings.Join(why, ". ")+". this never needs admin itself. an administrator runs the lines below, which give the attributes of the parent folders and nothing else (no listing, no creating, not inherited)"), a.GrantExamine(g)
	}
	parents := "it has no parent"
	if len(st.parents) > 0 {
		parents = fmt.Sprintf("every parent (%s) is examinable", strings.Join(natives(a, st.parents), ", "))
	}
	switch {
	case st.madeNow:
		return row(StatusDone, fmt.Sprintf("%s made, writable by %s, and %s", p.Root, login, parents)), nil
	case st.rootMissing:
		return row(StatusTodo, fmt.Sprintf("%s is missing on this machine, and %s. `atrium room setup --apply` makes it", p.Root, parents)), nil
	}
	return row(StatusOK, fmt.Sprintf("%s is there, writable by %s, and %s", p.Root, login, parents)), nil
}

func natives(a Adapter, l []string) []string {
	out := make([]string, len(l))
	for i, s := range l {
		out[i] = a.Native(s)
	}
	return out
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func missingDirs(r ReadFS, dirs []string, allMissing bool) []string {
	var out []string
	for _, d := range dirs {
		if allMissing {
			out = append(out, d)
			continue
		}
		if _, err := r.Lstat(d); err != nil && errors.Is(err, fs.ErrNotExist) {
			out = append(out, d)
		}
	}
	return out
}

// ── the tool caches ──────────────────────────────────────────────────────────

type cacheEdit struct {
	row      CacheRow
	format   Format
	file     string
	old, new []string
	value    string
	same     bool
	utf16    bool // the file is UTF-16, and is left alone
}

func cacheEdits(spec *Spec, a Adapter, v View) []cacheEdit {
	p := spec.Resolve()
	h := v.FS.Home()
	var out []cacheEdit
	for _, row := range RowsFor(spec.CacheSet()) {
		e := cacheEdit{row: row, format: row.FormatFor(spec.OS), value: row.Value(p)}
		if e.format == FormatUserEnv {
			cur, ok := v.Env.UserEnv(row.Key)
			e.same = ok && cur == e.value
			out = append(out, e)
			continue
		}
		e.file = a.CacheFile(row, h)
		if b, err := v.FS.ReadFile(e.file); err == nil {
			t, u16 := ParseText(b, a.Newline())
			if u16 {
				e.utf16, e.same = true, true // never edited: it is not UTF-8
				out = append(out, e)
				continue
			}
			e.old = t.Lines
		}
		switch e.format {
		case FormatKV:
			e.new = EditKV(e.old, row.Key, e.value)
		case FormatINI:
			e.new = EditINI(e.old, row.Section, row.Key, e.value)
		case FormatProfile:
			e.new = EditProfile(e.old, row.Key, e.value)
		}
		e.same = EqualLines(e.old, e.new)
		out = append(out, e)
	}
	return out
}

func cacheStep(spec *Spec, a Adapter, v View, host *Host, add func(step, status, detail string)) {
	edits := cacheEdits(spec, a, v)
	p := spec.Resolve()
	var todo, u16 []string
	for _, e := range edits {
		if e.utf16 {
			u16 = append(u16, a.Native(e.file))
		} else if !e.same {
			todo = append(todo, e.row.Tool)
		}
	}
	if len(u16) > 0 {
		defer add("work-cache-encoding", StatusWarn, fmt.Sprintf("%s is UTF-16, which PowerShell 5.1's Out-File writes, and is left alone because editing it as UTF-8 would corrupt it. re-save it as UTF-8 and run again", strings.Join(u16, ", ")))
	}
	set := fmt.Sprintf("caches under %s", p.Cache)
	switch {
	case len(todo) > 0 && host != nil:
		var done []string
		// Two rows may share a file (go's GOMODCACHE and GOCACHE): each edit is made on what the last one left.
		for _, e := range edits {
			if err := applyCache(host, a, e); err != nil {
				add("work-cache", StatusFail, fmt.Sprintf("%s: %v", e.row.Tool, err))
				return
			}
			if !e.same {
				done = append(done, e.row.Tool)
			}
		}
		add("work-cache", StatusDone, fmt.Sprintf("%s set. %s. a process that was already running reads them at its next start", strings.Join(done, ", "), set))
	case len(todo) > 0:
		add("work-cache", StatusTodo, fmt.Sprintf("%s not yet pointed at the work root. `atrium room setup --apply` does it", strings.Join(todo, ", ")))
	default:
		if diff := toolDifferences(edits, v); len(diff) > 0 {
			add("work-cache", StatusWarn, fmt.Sprintf("written, but %s and not the work root, so something overrides the file (GOENV, a project .npmrc). %s", strings.Join(diff, ". "), set))
			return
		}
		add("work-cache", StatusOK, "already pointed at the work root. "+set)
	}
}

func toolDifferences(edits []cacheEdit, v View) []string {
	if v.Tools == nil {
		return nil
	}
	var diff []string
	for _, e := range edits {
		if len(e.row.Query) == 0 {
			continue
		}
		out, ok := v.Tools.Query(e.row.Query)
		if !ok {
			continue
		}
		out = firstLine(out)
		if out != "" && !strings.EqualFold(slash(out), slash(e.value)) {
			diff = append(diff, fmt.Sprintf("%s says %s", e.row.Tool, out))
		}
	}
	return diff
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// ── the room settings ────────────────────────────────────────────────────────

func settingStep(spec *Spec, a Adapter, v View, host *Host, p Paths, step, key string, add func(step, status, detail string)) {
	want := settingWant(p, key)
	if v.Settings == nil {
		add(step, StatusWarn, fmt.Sprintf("the room's settings were not read here. %s should be %s", key, want))
		return
	}
	cur, err := v.Settings.Get(key)
	if errors.Is(err, ErrRoomRunning) && host == nil {
		add(step, StatusWarn, fmt.Sprintf("the room is running, so its %s cannot be read here. stop it to check", key))
		return
	}
	if err == nil && sameSetting(cur, want) {
		add(step, StatusOK, fmt.Sprintf("the room's %s is %s", key, want))
		return
	}
	if host == nil {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			add(step, StatusWarn, fmt.Sprintf("the room's %s could not be read: %v", key, err))
			return
		}
		add(step, StatusTodo, fmt.Sprintf("the room's %s is %q, and a run sets it to %s", key, cur, want))
		return
	}
	if serr := host.Settings.Set(key, want); serr != nil {
		stop := fmt.Sprintf("stop the room and run: atrium room set %s %s", key, want)
		if errors.Is(serr, ErrRoomRunning) {
			add(step, StatusWarn, fmt.Sprintf("the room is running, so %s cannot be set. %s", key, stop))
			return
		}
		add(step, StatusWarn, fmt.Sprintf("could not set the room's %s to %s (%v). %s", key, want, serr, stop))
		return
	}
	add(step, StatusDone, fmt.Sprintf("the room's %s is %s", key, want))
}

func sameSetting(a, b string) bool { return strings.EqualFold(slash(a), slash(b)) }
