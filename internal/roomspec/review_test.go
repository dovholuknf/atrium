package roomspec

import (
	"strings"
	"testing"
)

func TestSpecRefusesWhatLeavesTheWorkRoot(t *testing.T) {
	cases := []struct{ name, goos, spec, want string }{
		{"absolute git", Linux, linSpec + "layout:\n  git: /etc/foo\n", "absolute"},
		{"absolute cache windows", Windows, winSpec + "layout:\n  cache: 'C:/Windows/x'\n", "absolute"},
		{"backslash root", Windows, winSpec + "layout:\n  git: '\\x'\n", "absolute"},
		{"dotdot layout", Linux, linSpec + "layout:\n  git: ../x\n", ".."},
		{"top-level folder", Linux, strings.Replace(linSpec, "/srv/localai", "/srv", 1), "top-level"},
		{"usr", Linux, strings.Replace(linSpec, "/srv/localai", "/usr/local", 1), "system folder"},
		{"etc", Linux, strings.Replace(linSpec, "/srv/localai", "/etc/localai", 1), "system folder"},
		{"windows folder", Windows, strings.Replace(winSpec, "V:/localai", "C:/Windows/localai", 1), "system folder"},
		{"program files", Windows, strings.Replace(winSpec, "V:/localai", "C:/Program Files/x", 1), "system folder"},
		{"api_key", Linux, linSpec + "api_key: x\n", "credential"},
		{"credentials", Linux, linSpec + "credentials: x\n", "credential"},
		{"auth", Linux, linSpec + "auth: x\n", "credential"},
		{"bearer", Linux, linSpec + "bearer: x\n", "credential"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseFor([]byte(c.spec), c.goos)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%v, want %q", err, c.want)
			}
		})
	}
	if _, err := ParseFor([]byte(linSpec+"layout:\n  git: repos/all\n"), Linux); err != nil {
		t.Errorf("a nested relative folder is fine: %v", err)
	}
}

func TestWorkRootThatIsOrHoldsTheHomeFailsBeforeAnythingIsWritten(t *testing.T) {
	spec := mustSpec(t, Linux, strings.Replace(linSpec, "/srv/localai", "/home/localai", 1))
	m := NewMemFS(linHome, "localai")
	lk := Apply(spec, adapterFor(t, Linux), Host{FS: m, Env: m})
	wantStatus(t, lk, "work-root", StatusFail)
	if m.Has("/home/localai/git") || Code(lk.Steps) != 3 {
		t.Fatal("went on")
	}
	if !ContainsHome("/home", "/home/localai") || ContainsHome("/srv/x", "/home/localai") || ContainsHome("/home/localai/w", "/home/localai") {
		t.Error("ContainsHome")
	}
}

func TestRootGrantsAreNotWiderThanNeeded(t *testing.T) {
	l := adapterFor(t, Linux).GrantExamine(Grant{Account: "localai", Root: "/srv/localai", RootNotWritable: true})
	if len(l) != 1 || l[0] != "sudo chown localai: /srv/localai" {
		t.Errorf("%q", l)
	}
	w := adapterFor(t, Windows).GrantExamine(Grant{Account: `SG3\localai`, Root: "V:/localai", RootNotWritable: true})
	if len(w) != 1 || !strings.HasSuffix(w[0], `'SG3\localai:(OI)(CI)M'`) || strings.Contains(w[0], ")F") {
		t.Errorf("%q", w)
	}
	w = adapterFor(t, Windows).GrantExamine(Grant{Account: `SG3\localai`, Root: "V:/a/localai", Bad: []string{"V:/a"}, Missing: []string{"V:/a"}})
	if !strings.Contains(w[0], "-Force") {
		t.Errorf("%q", w)
	}
}

func TestSameAccountChecksTheDomainWhenBothHaveOne(t *testing.T) {
	if sameAccount(`OTHER\localai`, `SG3\localai`) {
		t.Error("another domain")
	}
	if !sameAccount(`SG3\localai`, "localai") || !sameAccount("localai", `SG3\localai`) || !sameAccount(`sg3\LocalAI`, `SG3\localai`) {
		t.Error("a bare name and a case difference match")
	}
}

func TestEditsKeepTheFilesOwnLineEndingAndBOM(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec+"caches: [npm]\n")
	a := adapterFor(t, Windows)
	for _, c := range []struct{ name, in, want string }{
		{"lf stays lf", "a=1\nb=2\n", "a=1\nb=2\ncache=V:/localai/cache/npm\n"},
		{"bom stays", "\xef\xbb\xbfa=1\r\n", "\xef\xbb\xbfa=1\r\ncache=V:/localai/cache/npm\r\n"},
		{"new file takes the OS's", "", "cache=V:/localai/cache/npm\r\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := NewMemFS(winHome, `SG3\localai`)
			m.Dir("V:/")
			m.Dir("V:/localai")
			if c.in != "" {
				m.Put("C:/Users/localai/.npmrc", c.in)
			}
			Apply(spec, a, Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}})
			if got := m.Text("C:/Users/localai/.npmrc"); got != c.want {
				t.Errorf("%q, want %q", got, c.want)
			}
		})
	}
	// a CRLF profile on Linux stays CRLF
	ls := mustSpec(t, Linux, strings.Replace(linSpec, "caches: [npm, go]", "caches: [cargo]", 1))
	m := NewMemFS(linHome, "localai")
	m.Dir("/srv")
	m.Put("/home/localai/.profile", "x=1\r\n")
	Apply(ls, adapterFor(t, Linux), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}})
	if got := m.Text("/home/localai/.profile"); got != "x=1\r\nexport CARGO_HOME='/srv/localai/cache/cargo'  "+ProfileMarker+"\r\n" {
		t.Errorf("%q", got)
	}
}

func TestUTF16FileIsRefusedWithAWarnAndNotTouched(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec+"caches: [npm, pip]\n")
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/")
	m.Dir("V:/localai")
	utf16 := "\xff\xfec\x00a\x00c\x00h\x00e\x00=\x00x\x00"
	m.Put("C:/Users/localai/.npmrc", utf16)
	lk := Apply(spec, adapterFor(t, Windows), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}})
	if m.Text("C:/Users/localai/.npmrc") != utf16 {
		t.Error("a UTF-16 file was edited")
	}
	if s := step(lk, "work-cache-encoding"); s.Status != StatusWarn || !strings.Contains(s.Detail, "UTF-16") {
		t.Errorf("%+v", s)
	}
	if !strings.Contains(m.Text("C:/Users/localai/AppData/Roaming/pip/pip.ini"), "cache-dir") {
		t.Error("the other cache was not set")
	}
	if Code(lk.Steps) != 0 {
		t.Error("a warn is not a failure")
	}
}

func TestIniSectionWithAComment(t *testing.T) {
	got := EditINI([]string{"[global] ; mine", "x = 1"}, "global", "cache-dir", "/c")
	if strings.Join(got, "|") != "[global] ; mine|cache-dir = /c|x = 1" {
		t.Errorf("%v", got)
	}
}

func TestRerunDoesNotRewriteTheLockForTheTimeAlone(t *testing.T) {
	spec := mustSpec(t, Windows, winSpec)
	a := adapterFor(t, Windows)
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("V:/")
	m.Dir("C:/Users/localai/.claude")
	host := Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}}
	Apply(spec, a, host) // done
	Apply(spec, a, host) // ok: the lock says so now
	m.Writes = 0
	Apply(spec, a, host)
	if m.Writes != 0 {
		t.Errorf("a third run wrote %d times", m.Writes)
	}
}

func TestInstallRefusesPathsOutsideThePack(t *testing.T) {
	m := NewMemFS(winHome, `SG3\localai`)
	src := &PackSource{Commit: "c", Files: map[string][]byte{
		"agents/ok.md": []byte("x"), "../evil": []byte("x"), "/abs": []byte("x"), "a/../../b": []byte("x"), `agents\x.md`: []byte("x"), "C:/x": []byte("x"),
	}}
	res, err := adapterFor(t, Windows).InstallPack(m, "C:/Users/localai/.claude", src, nil, true)
	if err != nil || len(res.Refused) != 5 || res.RecordWritten {
		t.Fatalf("%+v %v", res, err)
	}
	if !m.Has("C:/Users/localai/.claude/agents/ok.md") || m.Has("C:/Users/localai/evil") {
		t.Error("only the safe file is written")
	}
}

func TestMacCargoGoesToZprofile(t *testing.T) {
	row, _ := CacheByName("cargo")
	if f := adapterFor(t, Darwin).CacheFile(row[0], macHome); f != "/Users/localai/.zprofile" {
		t.Error(f)
	}
}
