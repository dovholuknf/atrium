//go:build integration

package roomspec

import (
	"strings"
	"testing"
)

func TestValidateWorkRootForAnotherOperatingSystem(t *testing.T) {
	for _, c := range []struct{ goos, root, account, want string }{
		{Linux, "/home/AL/work", "al", "AL's home"}, // case-sensitive: another user's folder
		{Linux, "/home/bob/work", "al", "bob's home"},
		{Linux, "/var/root/work", "al", "root's home"},
		{Darwin, "/private/var/root/work", "al", "root's home"},
		{Darwin, "/var/root/work", "al", "root's home"},
		{Darwin, "/Users/Shared/localai", "al", "every user shares"},
		{Windows, "C:/Users/Public/localai", "al", "every user shares"},
		{Windows, "C:/Users/BOB/x", `SG3\al`, "BOB's home"},
		{Windows, "C:/Users/al.smith/x", "al", "al.smith's home"},
		{Windows, "C:/Users/al.smith/x", `SG3\al`, "al.smith's home"},
		{Windows, "C:/Users/al.OTHER/x", `SG3\al`, "al.OTHER's home"},
		{Windows, "C:/Users/al.SG3/x", "al", "al.SG3's home"}, // a bare login cannot say its domain: the room judges its real home
		{Windows, "relative/dir", "al", "not a drive path"},
		{Windows, "/srv/localai", "al", "not a drive path"},
		{Linux, "V:/localai", "al", "drive path"},
		{Linux, "/srv", "al", "top-level"},
		{Linux, "/srv/../etc/x", "al", ".."},
		{Linux, "//host/share/x", "al", "network"},
		{"plan9", "/x/y", "al", "not windows, linux or darwin"},
	} {
		err := ValidateWorkRoot(c.goos, c.root, c.account)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: %v, want %q", c.goos, c.root, err, c.want)
		}
	}
	for _, c := range []struct{ goos, root, account string }{
		{Linux, "/home/al/work", "al"},
		{Darwin, "/Users/AL/work", "al"},
		{Windows, `C:\Users\al\x`, `SG3\al`},
		{Windows, "C:/Users/al.SG3/x", `SG3\al`},
		{Windows, "C:/Users/LOCALA~1/x", "localai"},
		{Linux, "/srv/localai", "al"},
		{Linux, "/srv/localai", ""},
	} {
		if err := ValidateWorkRoot(c.goos, c.root, c.account); err != nil {
			t.Errorf("%s %s: %v", c.goos, c.root, err)
		}
	}
}

func TestASpecOfPacksAloneNeedsNoWorkRoot(t *testing.T) {
	head := "version: 1\nname: r1\nos: windows\naccount: localai\n"
	spec := mustSpec(t, Windows, head+"packs:\n  - runner: claude\n    repo: o/agents\n")
	m := NewMemFS(winHome, `SG3\localai`)
	m.Dir("C:/Users/localai/.claude")
	ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
	lk := Apply(spec, adapterFor(t, Windows), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: ff, Need: []string{"c-systems-reviewer"}})
	wantStatus(t, lk, "agent-pack", StatusDone)
	for _, s := range lk.Steps {
		if s.Step != "agent-pack" {
			t.Errorf("a pack-only spec ran %+v", s)
		}
	}
	for _, bad := range []string{head, head + "packs:\n  - runner: claude\n    repo: o/a\ncaches: [npm]\n"} {
		if _, err := ParseFor([]byte(bad), Windows); err == nil || !strings.Contains(err.Error(), "work_root") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestWorkRootIsJudgedWhereTheMachineReallyReachesIt(t *testing.T) {
	for _, c := range []struct {
		name, goos, spec, root string
		home                   Home
		user                   string
		alias                  map[string]string
		want                   string // "" is a root that stands
	}{
		{"link into another user's home", Linux, linSpec, "/srv/localai", linHome, "localai",
			map[string]string{"/srv": "/home/bob"}, "bob's home"},
		{"link to a system folder", Linux, linSpec, "/srv/localai", linHome, "localai",
			map[string]string{"/srv/localai": "/etc/x"}, "system folder"},
		{"link to the account's home", Linux, linSpec, "/srv/localai", linHome, "localai",
			map[string]string{"/srv/localai": "/home/localai"}, "home folder"},
		{"link to a folder of its own", Linux, linSpec, "/srv/localai", linHome, "localai",
			map[string]string{"/srv": "/data"}, ""},
		{"link into the account's own home", Linux, linSpec, "/srv/localai", linHome, "localai",
			map[string]string{"/srv": "/home/localai/srv"}, ""},
		{"junction on a drive", Windows, winSpec, "V:/localai", winHome, `SG3\localai`,
			map[string]string{"V:/localai": "C:/Users/bob/x"}, "bob's home"},
		{"the profile folder is not the login", Windows, strings.Replace(winSpec, "V:/localai", "C:/Users/LOCALA~1/work", 1), "C:/Users/LOCALA~1/work",
			Home{Dir: "C:/Users/localai.SG3"}, `SG3\localai`, map[string]string{"C:/Users/LOCALA~1": "C:/Users/localai.SG3"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			spec := mustSpec(t, c.goos, c.spec)
			m := NewMemFS(c.home, c.user)
			m.Aliases = c.alias
			got := checkRootHere(spec, m, c.root)
			if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("%q, want %q", got, c.want)
			}
		})
	}
}
