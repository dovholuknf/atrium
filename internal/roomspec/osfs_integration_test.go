//go:build integration

package roomspec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempHost is the real filesystem of THIS machine with the account's home moved into a temp folder, so the real ~/.npmrc,
// ~/.claude and registry are never touched. It runs on whatever OS runs the tests: on Windows it is the Windows adapter for
// real, and in a container it is the Linux one.
type tempHome struct {
	OSFS
	home Home
}

func (t tempHome) Home() Home { return t.home }

type mapEnv map[string]string

func (m mapEnv) UserEnv(n string) (string, bool) { v, ok := m[n]; return v, ok }

func (m mapEnv) SetUserEnv(n, v string) error { m[n] = v; return nil }

func realSpec(t *testing.T, root string) *Spec {
	t.Helper()
	acct := OSFS{}.Login()
	text := "version: 1\nname: real\nos: " + runtime.GOOS + "\naccount: '" + acct + "'\nwork_root: '" + root + "'\n" +
		"packs:\n  - runner: claude\n    repo: o/agents\n"
	return mustSpec(t, runtime.GOOS, text)
}

func TestRealFSApplyConverges(t *testing.T) {
	if runtime.GOOS != Windows && runtime.GOOS != Linux && runtime.GOOS != Darwin {
		t.Skip("no adapter")
	}
	tmp := filepath.ToSlash(t.TempDir())
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = filepath.ToSlash(resolved)
	}
	h := Home{Dir: tmp + "/home", Config: tmp + "/home/cfg", AppSup: tmp + "/home/appsup"}
	// the parent exists: a missing parent is an administrator's to make, and is tested above with the fake
	if err := os.MkdirAll(filepath.FromSlash(tmp+"/work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := tmp + "/work/localai"
	spec := realSpec(t, root)
	a := adapterFor(t, runtime.GOOS)
	fs := tempHome{home: h}
	env := mapEnv{}
	settings := &fakeSettings{vals: map[string]string{}}
	ff := &fakeFetcher{src: packFiles()}

	plan := Plan(spec, a, View{FS: fs, Env: env, Settings: settings, Need: []string{"c-systems-reviewer"}})
	if _, err := os.Stat(filepath.FromSlash(root)); err == nil {
		t.Fatal("a plan made the work root")
	}
	if _, err := os.Stat(filepath.FromSlash(h.Dir + "/.atrium")); err == nil {
		t.Fatal("a plan wrote the lock")
	}
	wantStatus(t, plan, "work-root", StatusTodo)

	lk := Apply(spec, a, Host{FS: fs, Env: env, Settings: settings, Fetch: ff})
	for _, s := range lk.Steps {
		if s.Status == StatusFail || s.Status == StatusHuman || s.Status == StatusTodo {
			t.Fatalf("apply left %+v", s)
		}
	}
	for _, d := range []string{root, root + "/git", root + "/reviews", root + "/handoff", root + "/cache/npm", root + "/cache/go-mod"} {
		if fi, err := os.Stat(filepath.FromSlash(d)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", d, err)
		}
	}
	npmrc, _ := os.ReadFile(filepath.FromSlash(h.Dir + "/.npmrc"))
	if !strings.Contains(string(npmrc), "cache="+root+"/cache/npm") {
		t.Errorf(".npmrc %q", npmrc)
	}
	if runtime.GOOS == Windows {
		if !strings.Contains(string(npmrc), "\r\n") {
			t.Error("a Windows file has CRLF lines")
		}
		if env["CARGO_HOME"] != root+"/cache/cargo" {
			t.Errorf("CARGO_HOME %q", env["CARGO_HOME"])
		}
	} else {
		profName := "/.profile"
		if runtime.GOOS == Darwin {
			profName = "/.zprofile" // zsh is a Mac's login shell
		}
		prof, _ := os.ReadFile(filepath.FromSlash(h.Dir + profName))
		if !strings.Contains(string(prof), "export CARGO_HOME='"+root+"/cache/cargo'") {
			t.Errorf("%s %q", profName, prof)
		}
		if strings.Contains(string(npmrc), "\r") {
			t.Error("CR in a Unix file")
		}
	}
	for _, f := range []string{"/.claude/agents/c-systems-reviewer.md", "/.claude/skills/s1/SKILL.md", "/.claude/atrium-agent-pack.json", "/.atrium/room.lock"} {
		if _, err := os.Stat(filepath.FromSlash(h.Dir + f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if runtime.GOOS != Windows {
		fi, err := os.Stat(filepath.FromSlash(h.Dir + "/.claude/skills/s1/tool.sh"))
		if err != nil || fi.Mode()&0o100 == 0 {
			t.Errorf("a script must stay executable: %v %v", fi, err)
		}
	}
	b, _ := os.ReadFile(filepath.FromSlash(h.Dir + "/.atrium/room.lock"))
	if l, err := ParseLock(b); err != nil || l.SpecHash != spec.Hash || l.WorkRoot != root {
		t.Errorf("lock %v %+v", err, l)
	}

	// converged: a rerun is all ok, and a plan agrees
	again := Apply(spec, a, Host{FS: fs, Env: env, Settings: settings, Fetch: ff})
	for _, s := range again.Steps {
		if s.Status != StatusOK {
			t.Errorf("rerun: %+v", s)
		}
	}
	after := Plan(spec, a, View{FS: fs, Env: env, Settings: settings, Need: []string{"c-systems-reviewer"}})
	for _, s := range after.Steps {
		if s.Status != StatusOK {
			t.Errorf("plan after: %+v", s)
		}
	}
}

func TestRealFSLinkedPackFolderIsRefused(t *testing.T) {
	tmp := filepath.ToSlash(t.TempDir())
	h := Home{Dir: tmp + "/home", Config: tmp + "/home/cfg", AppSup: tmp + "/home/appsup"}
	outside := filepath.FromSlash(tmp + "/outside")
	claude := filepath.FromSlash(h.Dir + "/.claude")
	for _, d := range []string{outside, claude} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(claude, "skills")); err != nil {
		t.Skipf("cannot make a symlink here: %v", err)
	}
	a := adapterFor(t, runtime.GOOS)
	res, err := a.InstallPack(tempHome{home: h}, h.Dir+"/.claude", packFiles(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Refused) != 2 || res.RecordWritten {
		t.Fatalf("%+v", res)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("wrote through the link: %v", ents)
	}
}

func TestRealFSExamineAndWritable(t *testing.T) {
	dir := t.TempDir()
	f := OSFS{}
	if f.Examine(dir) != ExamineOK || !f.Writable(dir) {
		t.Error("a temp folder is examinable and writable")
	}
	if f.Examine(filepath.Join(dir, "nope")) != ExamineMissing {
		t.Error("missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "nope")); err == nil {
		t.Error("Examine made something")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("Writable left %v in the folder: it must read the permission, not make a file", entries)
	}
}
