package roomspec

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var winHome = Home{Dir: "C:/Users/localai", Config: "C:/Users/localai/AppData/Roaming", AppSup: "C:/Users/localai/AppData/Roaming"}
var linHome = Home{Dir: "/home/localai", Config: "/home/localai/.config", AppSup: "/home/localai/.config"}
var macHome = Home{Dir: "/Users/localai", Config: "/Users/localai/.config", AppSup: "/Users/localai/Library/Application Support"}

func mustSpec(t *testing.T, goos, text string) *Spec {
	t.Helper()
	s, err := ParseFor([]byte(text), goos)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func adapterFor(t *testing.T, goos string) Adapter {
	t.Helper()
	a, err := ForOS(goos)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func step(lk Lock, name string) Step {
	for _, s := range lk.Steps {
		if s.Step == name {
			return s
		}
	}
	return Step{Step: name, Status: "(absent)"}
}

func wantStatus(t *testing.T, lk Lock, name, status string) {
	t.Helper()
	if s := step(lk, name); s.Status != status {
		t.Fatalf("%s is %s (%s), want %s\nall: %+v", name, s.Status, s.Detail, status, lk.Steps)
	}
}

func packFiles() *PackSource {
	return &PackSource{Commit: "abcdef1234567890", Files: map[string][]byte{
		"agents/c-systems-reviewer.md": []byte("agent c"),
		"skills/s1/SKILL.md":           []byte("skill"),
		"skills/s1/tool.sh":            []byte("#!/bin/sh\n"),
	}}
}

const winPackSpec = winSpec + "packs:\n  - runner: claude\n    repo: o/agents\n"

// The structure of the claim: a plan is given a View, and a View has no way to write.
func TestViewAndReadFSHaveNoWriteMethods(t *testing.T) {
	for _, ty := range []reflect_t{rt((*ReadFS)(nil)), rt((*ReadEnv)(nil)), rt((*ReadSettings)(nil)), rt((*Tools)(nil))} {
		for i := 0; i < ty.NumMethod(); i++ {
			n := ty.Method(i).Name
			for _, w := range []string{"Write", "Mkdir", "Remove", "Set", "Fetch", "Install", "Grant"} {
				if strings.HasPrefix(n, w) {
					t.Errorf("%s has method %s, and a plan could write with it", ty, n)
				}
			}
		}
	}
	vt := reflect.TypeOf(View{})
	for i := 0; i < vt.NumField(); i++ {
		switch vt.Field(i).Type.Kind() {
		case reflect.Interface, reflect.Func, reflect.Slice:
		default:
			t.Errorf("View field %s", vt.Field(i).Name)
		}
	}
	if _, ok := reflect.TypeOf(View{}).FieldByName("FS"); !ok || reflect.TypeOf(View{}).Field(0).Type != rt((*ReadFS)(nil)) {
		t.Error("View.FS must be the read-only ReadFS")
	}
}

type reflect_t = reflect.Type

func rt(p any) reflect.Type { return reflect.TypeOf(p).Elem() }

func TestWindowsPlanWritesNothingAndApplyConverges(t *testing.T) {
	spec := mustSpec(t, Windows, winPackSpec)
	a := adapterFor(t, Windows)
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/") // the drive is there; the root is not
	m.Dir("C:/Users/localai/.claude")
	settings := &fakeSettings{vals: map[string]string{}}
	ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
	need := []string{"c-systems-reviewer"}

	plan := Plan(spec, a, View{FS: m, Env: m, Settings: settings, Latest: ff.Latest, Need: need})
	if m.Writes != 0 || len(settings.vals) != 0 || ff.calls != 0 {
		t.Fatalf("a plan wrote: fs %d settings %v fetches %d", m.Writes, settings.vals, ff.calls)
	}
	wantStatus(t, plan, "work-root", StatusTodo)
	wantStatus(t, plan, "work-dirs", StatusTodo)
	wantStatus(t, plan, "work-cache", StatusTodo)
	wantStatus(t, plan, "git-root", StatusTodo)
	wantStatus(t, plan, "agent-pack", StatusTodo)
	if plan.AppliedAt != "" {
		t.Error("a plan has no applied_at")
	}
	if Code(plan.Steps) != 0 {
		t.Errorf("a plan with work to do is exit 0, got %d", Code(plan.Steps))
	}

	host := Host{FS: m, Env: m, Settings: settings, Fetch: ff, Need: need, Now: func() time.Time { return time.Date(2026, 10, 8, 1, 2, 3, 0, time.UTC) }}
	lk := Apply(spec, a, host)
	for _, s := range lk.Steps {
		if s.Status == StatusFail || s.Status == StatusHuman || s.Status == StatusTodo {
			t.Fatalf("apply left %+v", s)
		}
	}
	if lk.AppliedAt != "2026-10-08T01:02:03Z" {
		t.Errorf("applied_at %q", lk.AppliedAt)
	}
	if len(lk.Packs) != 1 || lk.Packs[0].Commit != "abcdef1234567890" || lk.Packs[0].Files != 3 {
		t.Errorf("packs %+v", lk.Packs)
	}
	for _, d := range []string{"V:/localai", "V:/localai/git", "V:/localai/reviews", "V:/localai/handoff", "V:/localai/cache/npm", "V:/localai/cache/go-mod", "V:/localai/cache/cargo"} {
		if !m.Has(d) {
			t.Errorf("%s not made", d)
		}
	}
	if got := m.Text("C:/Users/localai/.npmrc"); got != "cache=V:/localai/cache/npm\r\n" {
		t.Errorf(".npmrc %q", got)
	}
	if got := m.Text("C:/Users/localai/AppData/Roaming/go/env"); got != "GOMODCACHE=V:/localai/cache/go-mod\r\nGOCACHE=V:/localai/cache/go-build\r\n" {
		t.Errorf("go env %q", got)
	}
	if got := m.Text("C:/Users/localai/AppData/Roaming/pip/pip.ini"); got != "[global]\r\ncache-dir = V:/localai/cache/pip\r\n" {
		t.Errorf("pip.ini %q", got)
	}
	if m.Env["CARGO_HOME"] != "V:/localai/cache/cargo" {
		t.Errorf("CARGO_HOME %q", m.Env["CARGO_HOME"])
	}
	if settings.vals["git_root"] != "V:/localai/git" || settings.vals["context_handoff_dir"] != "V:/localai/handoff" || settings.vals["reviews_root"] != "V:/localai/reviews" {
		t.Errorf("settings %v", settings.vals)
	}
	if rec := ReadRecord(m, "C:/Users/localai/.claude"); rec == nil || rec.Commit != "abcdef1234567890" || len(rec.Files) != 3 {
		t.Errorf("record %+v", rec)
	}
	if !strings.Contains(m.Text("C:/Users/localai/.atrium/room.lock"), `"spec_hash"`) {
		t.Error("no lock written")
	}

	// a rerun is a no-op that writes nothing but the lock, and a plan after it has nothing to do
	m.Writes = 0
	again := Apply(spec, a, host)
	for _, s := range again.Steps {
		if s.Status != StatusOK {
			t.Errorf("rerun: %+v", s)
		}
	}
	if m.Writes != 2 { // the lock's folder and the lock: nothing else
		t.Errorf("a rerun wrote %d times, want 2 (the lock)", m.Writes)
	}
	m.Writes = 0
	after := Plan(spec, a, View{FS: m, Env: m, Settings: settings, Latest: ff.Latest, Need: need})
	for _, s := range after.Steps {
		if s.Status != StatusOK {
			t.Errorf("plan after apply: %+v", s)
		}
	}
	if m.Writes != 0 {
		t.Error("the plan wrote")
	}
}

func TestPlanOnABareHomeNeedsAnAdminWhenTheDriveIsClosed(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec)
	a := adapterFor(t, Windows)
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/")
	m.Denied["V:/"] = true
	lk := Plan(spec, a, View{FS: m, Env: m})
	wantStatus(t, lk, "work-root", StatusHuman)
	if Code(lk.Steps) != 13 {
		t.Fatalf("code %d", Code(lk.Steps))
	}
	if len(lk.AdminLines) == 0 || !strings.Contains(strings.Join(lk.AdminLines, "\n"), `icacls V:\ /grant 'SG3\localai:(RA,REA)'`) {
		t.Fatalf("lines %q", lk.AdminLines)
	}
	if len(lk.Steps) != 1 { // nothing else is touched until the root is sound
		t.Fatalf("steps after a human step: %+v", lk.Steps)
	}
	// apply does the same and writes nothing but the lock
	m.Writes = 0
	ap := Apply(spec, a, Host{FS: m, Env: m})
	if Code(ap.Steps) != 13 || m.Has("V:/localai") {
		t.Fatalf("apply went past an admin step: %+v", ap.Steps)
	}
}

func TestMissingDriveIsNotAnAdminLine(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec)
	m := NewMemFS(winHome, `SG3\localai`)
	lk := Plan(spec, adapterFor(t, Windows), View{FS: m, Env: m})
	s := step(lk, "work-root")
	if s.Status != StatusHuman || !strings.Contains(s.Detail, "mapped drive") || len(lk.AdminLines) != 0 {
		t.Fatalf("%+v %v", s, lk.AdminLines)
	}
}

func TestWrongAccountFailsBeforeAnythingIsWritten(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec)
	m := NewMemFS(winHome, `SG3\someoneelse`)
	m.Dir("V:/")
	lk := Apply(spec, adapterFor(t, Windows), Host{FS: m, Env: m})
	wantStatus(t, lk, "account", StatusFail)
	if m.Has("V:/localai") || Code(lk.Steps) != 3 {
		t.Fatal("a wrong account configured something")
	}
	// a bare name and a domain-qualified one are the same account
	if !sameAccount(`SG3\localai`, "localai") || !sameAccount("LocalAI@sg3", `SG3\localai`) {
		t.Error("account match")
	}
}

func TestLinuxMissingParentsAreMadeFirstAndSetfaclNeverChmod(t *testing.T) {
	spec := mustSpec(t, Linux, strings.Replace(linSpec, "/srv/localai", "/srv/work/localai", 1))
	a := adapterFor(t, Linux)
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv")
	m.Denied["/srv"] = true
	m.Denied["/"] = true // root is never given a line
	lk := Plan(spec, a, View{FS: m, Env: m})
	wantStatus(t, lk, "work-root", StatusHuman)
	text := strings.Join(lk.AdminLines, "\n")
	if strings.Contains(text, "chmod o") || strings.Contains(text, "chmod 7") {
		t.Errorf("opens to every user:\n%s", text)
	}
	if !strings.Contains(text, "sudo setfacl -m u:localai:x /srv\n") && !strings.HasSuffix(text, "/srv") {
		if !strings.Contains(text, "setfacl -m u:localai:x /srv") {
			t.Errorf("no setfacl for /srv:\n%s", text)
		}
	}
	for _, l := range lk.AdminLines {
		if strings.HasSuffix(l, " /") {
			t.Errorf("a line for /: %s", l)
		}
	}
	mk := strings.Index(text, "install -d -m 755 /srv/work")
	grant := strings.Index(text, "setfacl -m u:localai:x /srv/work")
	if mk < 0 || grant < 0 || mk > grant {
		t.Errorf("a missing parent is made before it is granted:\n%s", text)
	}
	if !strings.Contains(text, "install -d -o localai -m 755 /srv/work/localai") {
		t.Errorf("the root is made for the account:\n%s", text)
	}
}

func TestDarwinUsesChmodACLAndLinuxSpellingOfCaches(t *testing.T) {
	a := adapterFor(t, Darwin)
	l := a.GrantExamine(Grant{Account: "localai", Root: "/Volumes/w/localai", Bad: []string{"/Volumes/w"}})
	if len(l) != 1 || l[0] != "sudo chmod +a 'user:localai allow search' /Volumes/w" {
		t.Fatalf("%q", l)
	}
	row, _ := CacheByName("go")
	if f := a.CacheFile(row[0], macHome); f != "/Users/localai/Library/Application Support/go/env" {
		t.Errorf("mac go env %s", f)
	}
	if f := adapterFor(t, Linux).CacheFile(row[0], linHome); f != "/home/localai/.config/go/env" {
		t.Errorf("linux go env %s", f)
	}
}

func TestQuotingAWordWithAQuote(t *testing.T) {
	if got := psWord(`C:\it's here`); got != `'C:\it''s here'` {
		t.Errorf("ps %s", got)
	}
	if got := shWord("/srv/it's"); got != `'/srv/it'\''s'` {
		t.Errorf("sh %s", got)
	}
	l := adapterFor(t, Linux).GrantExamine(Grant{Account: "localai", Root: "/srv/it's/x", Bad: []string{"/srv/it's"}, Missing: []string{"/srv/it's"}})
	if !strings.Contains(strings.Join(l, "\n"), `'/srv/it'\''s'`) {
		t.Errorf("%q", l)
	}
}

func TestLinuxApplyWritesProfileForCargoAndSharesTheGoFile(t *testing.T) {
	spec := mustSpec(t, Linux, strings.Replace(linSpec, "caches: [npm, go]", "caches: [go, cargo]", 1))
	a := adapterFor(t, Linux)
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv")
	settings := &fakeSettings{vals: map[string]string{}}
	m.Put("/home/localai/.config/go/env", "GOFLAGS=-mod=mod\nGOCACHE=/old\n")
	lk := Apply(spec, a, Host{FS: m, Env: m, Settings: settings})
	wantStatus(t, lk, "work-cache", StatusDone)
	if got := m.Text("/home/localai/.config/go/env"); got != "GOFLAGS=-mod=mod\nGOMODCACHE=/srv/localai/cache/go-mod\nGOCACHE=/srv/localai/cache/go-build\n" && got != "GOFLAGS=-mod=mod\nGOCACHE=/srv/localai/cache/go-build\nGOMODCACHE=/srv/localai/cache/go-mod\n" {
		t.Errorf("go env %q", got)
	}
	if got := m.Text("/home/localai/.profile"); got != "export CARGO_HOME='/srv/localai/cache/cargo'  "+ProfileMarker+"\n" {
		t.Errorf("profile %q", got)
	}
	if len(m.Env) != 0 {
		t.Error("the registry is Windows only")
	}
	m.Writes = 0
	Apply(spec, a, Host{FS: m, Env: m, Settings: settings})
	if m.Writes != 2 {
		t.Errorf("rerun wrote %d", m.Writes)
	}
}

func TestSettingsRunningRoom(t *testing.T) {
	spec := mustSpec(t, Linux, linSpec)
	a := adapterFor(t, Linux)
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv")
	s := &fakeSettings{vals: map[string]string{}, running: true}
	pl := Plan(spec, a, View{FS: m, Env: m, Settings: s})
	wantStatus(t, pl, "git-root", StatusWarn)
	ap := Apply(spec, a, Host{FS: m, Env: m, Settings: s})
	wantStatus(t, ap, "git-root", StatusWarn)
	if !strings.Contains(step(ap, "git-root").Detail, "stop the room") {
		t.Error(step(ap, "git-root").Detail)
	}
	if Code(ap.Steps) != 0 {
		t.Error("a running room is a warn, not a failure")
	}
}

func TestPackRules(t *testing.T) {
	spec := mustSpec(t, Windows, winPackSpec)
	a := adapterFor(t, Windows)
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/")
	m.Dir("V:/localai")
	m.Dir("C:/Users/localai/.claude")
	settings := &fakeSettings{vals: map[string]string{}}
	ff := &fakeFetcher{src: packFiles()}
	host := Host{FS: m, Env: m, Settings: settings, Fetch: ff}

	// a linked folder below the pack dir is refused, the record is not written, and the lock does not claim the pack
	m.Link("C:/Users/localai/.claude/skills")
	lk := Apply(spec, a, host)
	wantStatus(t, lk, "agent-pack", StatusWarn)
	if !strings.Contains(step(lk, "agent-pack").Detail, "is a link") || len(lk.Packs) != 0 {
		t.Fatalf("%+v %+v", step(lk, "agent-pack"), lk.Packs)
	}
	if m.Has("C:/Users/localai/.claude/atrium-agent-pack.json") {
		t.Error("record written with a file refused")
	}
	if !m.Has("C:/Users/localai/.claude/agents/c-systems-reviewer.md") {
		t.Error("the files that could be written were not")
	}
	if m.Has("C:/Users/localai/.claude/skills/s1/SKILL.md") {
		t.Error("wrote through a link")
	}

	// the link goes; a clean install, then someone edits an agent, then the pack is updated
	m.RemoveAll("C:/Users/localai/.claude/skills")
	lk = Apply(spec, a, host)
	wantStatus(t, lk, "agent-pack", StatusDone)
	m.Put("C:/Users/localai/.claude/agents/c-systems-reviewer.md", "edited by hand")
	ff.src = packFiles()
	ff.src.Commit = "2222222222222222"
	ff.src.Files["agents/c-systems-reviewer.md"] = []byte("agent c v2")
	lk = Apply(spec, a, host)
	d := step(lk, "agent-pack")
	if d.Status != StatusDone || !strings.Contains(d.Detail, "edited here: agents/c-systems-reviewer.md") {
		t.Fatalf("%+v", d)
	}
	if m.Text("C:/Users/localai/.claude/agents/c-systems-reviewer.md") != "agent c v2" {
		t.Error("the edited file was not replaced")
	}
	// the same commit again changes nothing
	m.Writes = 0
	lk = Apply(spec, a, host)
	wantStatus(t, lk, "agent-pack", StatusOK)
	if m.Writes != 2 {
		t.Errorf("rerun wrote %d", m.Writes)
	}
	// a file that is a link is replaced, and a stale pack is called stale
	rec := ReadRecord(m, "C:/Users/localai/.claude")
	if st, d := PackVerdict(rec, []string{"c-systems-reviewer"}, "3333333333333333", []string{"c-systems-reviewer"}); st != StatusWarn || !strings.Contains(d, "stale") {
		t.Errorf("%s %s", st, d)
	}
	if st, _ := PackVerdict(rec, nil, "", []string{"x"}); st != StatusWarn {
		t.Errorf("missing agent: %s", st)
	}
	if st, _ := PackVerdict(nil, nil, "", nil); st != StatusTodo {
		t.Errorf("no record: %s", st)
	}
}

const linHead = "version: 1\nname: pi\nos: linux\naccount: localai\nwork_root: /srv/localai\ncaches: [npm, go]\n"

// Each runner's pack goes where that runner reads it, and only the kinds it reads: codex has skills and no markdown agents.
func TestEachRunnersPackGoesWhereItReadsIt(t *testing.T) {
	spec := mustSpec(t, Linux, linHead+"packs:\n  - runner: claude\n    repo: o/a\n  - runner: gemini\n    repo: o/a\n  - runner: codex\n    repo: o/a\n")
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv/localai")
	ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
	need := []string{"c-systems-reviewer"}
	lk := Apply(spec, adapterFor(t, Linux), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: ff, Need: need})
	for _, n := range []string{"agent-pack", "agent-pack-gemini", "agent-pack-codex"} {
		wantStatus(t, lk, n, StatusDone)
	}
	for _, f := range []string{"/.claude/agents/c-systems-reviewer.md", "/.claude/skills/s1/SKILL.md", "/.gemini/agents/c-systems-reviewer.md",
		"/.gemini/skills/s1/tool.sh", "/.codex/skills/s1/SKILL.md", "/.codex/atrium-agent-pack.json"} {
		if !m.Has("/home/localai" + f) {
			t.Errorf("%s is missing", f)
		}
	}
	if m.Has("/home/localai/.codex/agents/c-systems-reviewer.md") {
		t.Error("codex reads no markdown agents")
	}
	if len(lk.Packs) != 3 {
		t.Errorf("packs %+v", lk.Packs)
	}
	// a plan: codex is not missing the panel's agents, and every pack is current
	pl := Plan(spec, adapterFor(t, Linux), View{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Latest: ff.Latest, Need: need})
	for _, n := range []string{"agent-pack", "agent-pack-gemini", "agent-pack-codex"} {
		wantStatus(t, pl, n, StatusOK)
	}
	// a repository with nothing a runner reads is said, not installed as nothing
	only := &fakeFetcher{src: &PackSource{Commit: "abc", Files: map[string][]byte{"agents/x.md": []byte("a")}}}
	cs := mustSpec(t, Linux, linHead+"packs:\n  - runner: codex\n    repo: o/a\n")
	m2 := NewMemFS(linHome, "localai")
	m2.Dir("/srv/localai")
	l2 := Apply(cs, adapterFor(t, Linux), Host{FS: m2, Env: m2, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: only})
	if s := step(l2, "agent-pack-codex"); s.Status != StatusFail || !strings.Contains(s.Detail, "nothing for codex") {
		t.Errorf("%+v", s)
	}
}

// CODEX_HOME and GEMINI_CLI_HOME in the account's environment move where the pack goes, as they move where the runner looks.
func TestPackFollowsTheVariablesThatMoveARunnersFolder(t *testing.T) {
	spec := mustSpec(t, Linux, linHead+"packs:\n  - runner: gemini\n    repo: o/a\n  - runner: codex\n    repo: o/a\n")
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv/localai")
	m.Env = map[string]string{"GEMINI_CLI_HOME": "/home/localai/g", "CODEX_HOME": `/srv/localai/c`}
	ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
	Apply(spec, adapterFor(t, Linux), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: ff})
	for _, f := range []string{"/home/localai/g/.gemini/agents/c-systems-reviewer.md", "/home/localai/g/.gemini/atrium-agent-pack.json", "/srv/localai/c/skills/s1/SKILL.md"} {
		if !m.Has(f) {
			t.Errorf("%s is missing", f)
		}
	}
	if m.Has("/home/localai/.gemini/agents/c-systems-reviewer.md") || m.Has("/home/localai/.codex/skills/s1/SKILL.md") {
		t.Error("the default folders were used")
	}
	pl := Plan(spec, adapterFor(t, Linux), View{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Latest: ff.Latest})
	wantStatus(t, pl, "agent-pack-gemini", StatusOK)
	wantStatus(t, pl, "agent-pack-codex", StatusOK)
}

// A variable that moves a runner's folder is followed only to an absolute place under the account's home or the work root, with
// a %NAME% of the Windows registry expanded first. Anything else is a fail row naming the variable, and nothing is installed.
func TestMovedPackFolderIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	cases := []struct {
		goos, runner, key, val string
		want                   string // the folder the pack lands in, or "" for a fail row
	}{
		{Linux, "codex", "CODEX_HOME", "/home/localai/.codex-x", "/home/localai/.codex-x"},
		{Linux, "codex", "CODEX_HOME", "/srv/localai/codex", "/srv/localai/codex"},
		{Linux, "codex", "CODEX_HOME", "/home/x/.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/.claude/sub", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/./.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai//.claude", ""},
		{Linux, "codex", "CODEX_HOME", "//.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/./.codex-x/", "/home/localai/.codex-x"},
		{Linux, "codex", "CODEX_HOME", "/etc", ""},
		{Linux, "codex", "CODEX_HOME", "../../x", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/../x", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/$X", ""},
		{Linux, "codex", "CODEX_HOME", "%USERPROFILE%/.codex-x", ""},
		{Linux, "claude", "CLAUDE_CONFIG_DIR", "/home/localai/.codex", ""},
		{Linux, "gemini", "GEMINI_CLI_HOME", "/data/g", ""},
		{Windows, "codex", "CODEX_HOME", `%USERPROFILE%\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `V:\localai\codex`, "V:/localai/codex"},
		{Windows, "codex", "CODEX_HOME", `%NOPE%\x`, ""},
		{Windows, "codex", "CODEX_HOME", `D:\other`, ""},
		{Windows, "codex", "CODEX_HOME", `.codex`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.codex-x:stream`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\CODEX~1`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `\Users\localai\.codex-x`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\..\other`, ""},
		{Windows, "codex", "CODEX_HOME", `%USERPROFILE%\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `c:\users\LOCALAI\.CLAUDE`, ""},
	}
	for _, c := range cases {
		var spec *Spec
		var m *MemFS
		if c.goos == Linux {
			spec = mustSpec(t, Linux, linHead+"packs:\n  - runner: "+c.runner+"\n    repo: o/a\n")
			m = NewMemFS(linHome, "localai")
			m.Dir("/srv/localai")
		} else {
			spec = mustSpec(t, Windows, winSpec+"packs:\n  - runner: "+c.runner+"\n    repo: o/a\n")
			m = NewMemFS(winHome, `SG3\localai`)
			m.Dir("V:/")
			m.Dir("V:/localai")
		}
		m.Env = map[string]string{c.key: c.val}
		name := "agent-pack"
		if c.runner != "claude" {
			name += "-" + c.runner
		}
		ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
		before := m.Writes
		lk := Apply(spec, adapterFor(t, c.goos), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: ff})
		s := step(lk, name)
		if c.want == "" {
			if s.Status != StatusFail || !strings.Contains(s.Detail, c.key) || !strings.Contains(s.Detail, c.val) {
				t.Errorf("%s=%s: %+v", c.key, c.val, s)
			}
			if m.Has("/home/localai/.claude/atrium-agent-pack.json") || m.Has("C:/Users/localai/.claude/atrium-agent-pack.json") {
				t.Errorf("%s=%s: another runner's record was written", c.key, c.val)
			}
			continue
		}
		if s.Status != StatusDone {
			t.Errorf("%s=%s: %+v", c.key, c.val, s)
			continue
		}
		if !m.Has(c.want+"/atrium-agent-pack.json") && !m.Has(c.want+"/.gemini/atrium-agent-pack.json") {
			t.Errorf("%s=%s: nothing in %s (writes %d, was %d)", c.key, c.val, c.want, m.Writes, before)
		}
	}
}

func TestPackWithoutASourceIsAWarn(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec+"packs:\n  - runner: claude\n    repo: o/a\n  - runner: codex\n    repo: o/a\n")
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/")
	m.Dir("V:/localai")
	lk := Apply(spec, adapterFor(t, Windows), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}})
	if s := step(lk, "agent-pack"); s.Status != StatusWarn || !strings.Contains(s.Detail, "no pack source") {
		t.Errorf("%+v", s)
	}
	if s := step(lk, "agent-pack-codex"); s.Status != StatusWarn || !strings.Contains(s.Detail, "no pack source") {
		t.Errorf("%+v", s)
	}
}

func TestInstallRefusesNothingOutsideDirAndKeepsOtherFiles(t *testing.T) {
	m := NewMemFS(winHome, `SG3\localai`)
	m.Put("C:/Users/localai/.claude/settings.json", "mine")
	src := packFiles()
	res, err := adapterFor(t, Windows).InstallPack(m, "C:/Users/localai/.claude", src, nil, true)
	if err != nil || res.Changed != 3 || !res.RecordWritten {
		t.Fatalf("%+v %v", res, err)
	}
	if m.Text("C:/Users/localai/.claude/settings.json") != "mine" {
		t.Error("an unrelated file was touched")
	}
	// plan mode of InstallPack writes nothing
	m2 := NewMemFS(winHome, `SG3\localai`)
	if _, err := adapterFor(t, Windows).InstallPack(m2, "C:/Users/localai/.claude", src, nil, false); err != nil || m2.Writes != 0 {
		t.Errorf("apply=false wrote %d", m2.Writes)
	}
}
