package sshcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exeWin = `C:\Users\claude\.atrium\bin\atrium.exe`
const exeNix = "/Users/claude/.atrium/bin/atrium"

func home(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, h, text string) string {
	t.Helper()
	d, cfg, _ := Paths(h)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func backups(t *testing.T, h string) []string {
	t.Helper()
	d, _, _ := Paths(h)
	m, _ := filepath.Glob(filepath.Join(d, "config.atrium-backup-*"))
	return m
}

func TestBlockIsTheWindowsQuotedFormOnWindowsAndPlainElsewhere(t *testing.T) {
	win := Block("sg3", exeWin, "windows")
	want := "Host sg3\n  ProxyCommand \"" + exeWin + "\" ssh-proxy %h\n  HostKeyAlias sg3.atrium\n  ServerAliveInterval 30\n  CheckHostIP no\n"
	if win != want {
		t.Fatalf("windows block:\n%s", win)
	}
	for _, goos := range []string{"darwin", "linux"} {
		got := Block("sg3", exeNix, goos)
		if !strings.Contains(got, "  ProxyCommand "+exeNix+" ssh-proxy %h\n") || strings.Contains(got, `"`) {
			t.Fatalf("%s block:\n%s", goos, got)
		}
	}
	// A path with a space is quoted for the shell, never left to split.
	if got := ProxyCommand("/Users/a b/atrium", "darwin"); got != "'/Users/a b/atrium' ssh-proxy %h" {
		t.Fatalf("got %s", got)
	}
}

func TestAddCreatesAConfigWithJustTheIncludeLine(t *testing.T) {
	h := home(t)
	res, err := Add(h, exeNix, "linux", []string{"sg3"})
	if err != nil {
		t.Fatal(err)
	}
	_, cfg, own := Paths(h)
	if got := read(t, cfg); got != IncludeLine+"\n" {
		t.Fatalf("config is %q", got)
	}
	if !res.Created || !res.Include || res.Backup != "" {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(read(t, own), "Host sg3\n") {
		t.Fatalf("no block: %s", read(t, own))
	}
	if len(backups(t, h)) != 0 {
		t.Fatal("a backup was made of a file that did not exist")
	}
}

func TestAddToAnExistingConfigPutsTheIncludeFirstAndTouchesNothingElse(t *testing.T) {
	h := home(t)
	// Odd on purpose: tabs, CRLF, no trailing newline, a Match, comments.
	orig := "# mine\r\nHost github.com\r\n\tUser git\r\n\tIdentityFile ~/.ssh/id_ed25519\r\n\r\nMatch host *.corp\r\n  ForwardAgent no"
	cfg := write(t, h, orig)
	res, err := Add(h, exeNix, "linux", []string{"sg3", "m1mini"})
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, cfg)
	if got != IncludeLine+"\r\n"+orig {
		t.Fatalf("the rest of the file did not survive byte for byte:\n%q", got)
	}
	b := backups(t, h)
	if len(b) != 1 || read(t, b[0]) != orig || res.Backup != b[0] {
		t.Fatalf("backup %v %+v", b, res)
	}
}

func TestAddAgainIsIdempotentWithNoSecondIncludeOrBackup(t *testing.T) {
	h := home(t)
	cfg := write(t, h, "Host x\n  User y\n")
	if _, err := Add(h, exeNix, "linux", []string{"sg3"}); err != nil {
		t.Fatal(err)
	}
	after1, own1 := read(t, cfg), read(t, filepath.Join(h, ".ssh", File))
	res, err := Add(h, exeNix, "linux", []string{"sg3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Backup != "" {
		t.Fatalf("a second add changed things: %+v", res)
	}
	if read(t, cfg) != after1 || read(t, filepath.Join(h, ".ssh", File)) != own1 {
		t.Fatal("a second add rewrote a file")
	}
	if strings.Count(after1, IncludeLine) != 1 || len(backups(t, h)) != 1 {
		t.Fatalf("includes %d, backups %d", strings.Count(after1, IncludeLine), len(backups(t, h)))
	}
}

func TestAConfigThatAlreadyHasTheLineIsNotTouchedOrBackedUp(t *testing.T) {
	h := home(t)
	orig := "Host x\n  User y\n\ninclude  atrium_config\n"
	cfg := write(t, h, orig)
	res, err := Add(h, exeNix, "linux", []string{"sg3"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, cfg) != orig || len(backups(t, h)) != 0 || res.Include || res.Backup != "" {
		t.Fatalf("a config that had the line was changed: %+v", res)
	}
}

func TestAddReplacesAChangedBlockInPlace(t *testing.T) {
	h := home(t)
	if _, err := Add(h, exeNix, "linux", []string{"sg3", "m1mini"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(h, "/opt/atrium", "linux", []string{"sg3"}); err != nil {
		t.Fatal(err)
	}
	own := read(t, filepath.Join(h, ".ssh", File))
	if strings.Count(own, "Host sg3\n") != 1 || !strings.Contains(own, "/opt/atrium ssh-proxy") ||
		!strings.Contains(own, exeNix+" ssh-proxy") {
		t.Fatalf("%s", own)
	}
}

func TestRemoveDropsBlocksAndNeverTheInclude(t *testing.T) {
	h := home(t)
	cfg := write(t, h, "Host x\n")
	if _, err := Add(h, exeNix, "linux", []string{"sg3", "m1mini"}); err != nil {
		t.Fatal(err)
	}
	withInclude := read(t, cfg)
	removed, missing, err := Remove(h, []string{"sg3", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "sg3" || len(missing) != 1 || missing[0] != "nope" {
		t.Fatalf("%v %v", removed, missing)
	}
	own := read(t, filepath.Join(h, ".ssh", File))
	if strings.Contains(own, "Host sg3") || !strings.Contains(own, "Host m1mini") {
		t.Fatalf("%s", own)
	}
	if _, _, err := Remove(h, nil); err != nil {
		t.Fatal(err)
	}
	own = read(t, filepath.Join(h, ".ssh", File))
	if strings.Contains(own, "Host ") {
		t.Fatalf("all did not remove everything: %s", own)
	}
	if read(t, cfg) != withInclude {
		t.Fatal("remove touched ~/.ssh/config")
	}
}

func TestRemoveWithNothingConfiguredIsQuiet(t *testing.T) {
	h := home(t)
	removed, _, err := Remove(h, nil)
	if err != nil || len(removed) != 0 {
		t.Fatalf("%v %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(h, ".ssh", File)); err == nil {
		t.Fatal("remove created a file")
	}
}

func TestShowReadsAndChangesNothing(t *testing.T) {
	h := home(t)
	s, err := Show(h)
	if err != nil || len(s.Rooms) != 0 || s.ConfigExists || s.Included {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := os.Stat(filepath.Join(h, ".ssh")); err == nil {
		t.Fatal("show created the directory")
	}
	if _, err := Add(h, exeNix, "linux", []string{"sg3", "m1mini"}); err != nil {
		t.Fatal(err)
	}
	s, err = Show(h)
	if err != nil || len(s.Rooms) != 2 || !s.Included || !s.ConfigExists || !strings.Contains(s.Text, "Host m1mini") {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestABadRoomNameIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	h := home(t)
	for _, bad := range []string{"", "a b", "x\nProxyCommand evil", "-oProxyCommand=x"} {
		if _, err := Add(h, exeNix, "linux", []string{bad}); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(h, ".ssh")); err == nil {
		t.Fatal("a refused name still wrote something")
	}
}

func TestBlockHubWritesHubIntoTheProxyCommand(t *testing.T) {
	if got := ProxyCommandHub("/usr/bin/atrium", "linux", ""); got != "/usr/bin/atrium ssh-proxy %h" {
		t.Fatalf("no hub: %q", got)
	}
	if got := ProxyCommandHub("/usr/bin/atrium", "linux", "http://127.0.0.1:7790"); got != "/usr/bin/atrium ssh-proxy --hub http://127.0.0.1:7790 %h" {
		t.Fatalf("hub: %q", got)
	}
	if got := ProxyCommandHub(`C:\Program Files\atrium.exe`, "windows", "http://127.0.0.1:7790"); got != `"C:\Program Files\atrium.exe" ssh-proxy --hub http://127.0.0.1:7790 %h` {
		t.Fatalf("windows: %q", got)
	}
	if !strings.Contains(BlockHub("sg3", "atrium", "linux", "http://h:1"), "ProxyCommand atrium ssh-proxy --hub http://h:1 %h\n") {
		t.Fatal("block lacks --hub")
	}
}
