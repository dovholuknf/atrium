package roomspec

import (
	"errors"
	"fmt"
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

func packStep(spec *Spec, a Adapter, v View, host *Host, pk Pack, lk *Lock, add func(step, status, detail string)) {
	name := "agent-pack"
	if pk.Runner != "claude" {
		name += "-" + pk.Runner
	}
	layout, ok := packLayout(pk.Runner, v.FS.Home())
	dir := a.PackDir(pk.Runner, v.FS.Home())
	if !ok || dir == "" {
		add(name, StatusWarn, fmt.Sprintf("no pack adapter for %s", pk.Runner))
		return
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
