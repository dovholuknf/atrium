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
	win := Block("room-d", exeWin, "windows")
	want := "Host room-d.atrium\n  ProxyCommand \"" + exeWin + "\" ssh-proxy %n\n  HostKeyAlias room-d.atrium\n  ServerAliveInterval 30\n  ServerAliveCountMax 3\n  CheckHostIP no\n"
	if win != want {
		t.Fatalf("windows block:\n%s", win)
	}
	for _, goos := range []string{"darwin", "linux"} {
		got := Block("room-d", exeNix, goos)
		if !strings.Contains(got, "  ProxyCommand "+exeNix+" ssh-proxy %n\n") || strings.Contains(got, `"`) {
			t.Fatalf("%s block:\n%s", goos, got)
		}
	}
	// A path with a space is quoted for the shell, never left to split.
	if got := ProxyCommand("/Users/a b/atrium", "darwin"); got != "'/Users/a b/atrium' ssh-proxy %n" {
		t.Fatalf("got %s", got)
	}
}

func TestAddCreatesAConfigWithJustTheIncludeLine(t *testing.T) {
	h := home(t)
	res, err := Add(h, exeNix, "linux", []string{"room-d"})
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
	if !strings.Contains(read(t, own), "Host room-d.atrium\n") {
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
	res, err := Add(h, exeNix, "linux", []string{"room-d", "room-a"})
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
	if _, err := Add(h, exeNix, "linux", []string{"room-d"}); err != nil {
		t.Fatal(err)
	}
	after1, own1 := read(t, cfg), read(t, filepath.Join(h, ".ssh", File))
	res, err := Add(h, exeNix, "linux", []string{"room-d"})
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
	res, err := Add(h, exeNix, "linux", []string{"room-d"})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, cfg) != orig || len(backups(t, h)) != 0 || res.Include || res.Backup != "" {
		t.Fatalf("a config that had the line was changed: %+v", res)
	}
}

func TestAddReplacesAChangedBlockInPlace(t *testing.T) {
	h := home(t)
	if _, err := Add(h, exeNix, "linux", []string{"room-d", "room-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(h, "/opt/atrium", "linux", []string{"room-d"}); err != nil {
		t.Fatal(err)
	}
	own := read(t, filepath.Join(h, ".ssh", File))
	if strings.Count(own, "Host room-d.atrium\n") != 1 || !strings.Contains(own, "/opt/atrium ssh-proxy") ||
		!strings.Contains(own, exeNix+" ssh-proxy") {
		t.Fatalf("%s", own)
	}
}

func TestRemoveDropsBlocksAndNeverTheInclude(t *testing.T) {
	h := home(t)
	cfg := write(t, h, "Host x\n")
	if _, err := Add(h, exeNix, "linux", []string{"room-d", "room-a"}); err != nil {
		t.Fatal(err)
	}
	withInclude := read(t, cfg)
	removed, missing, err := Remove(h, []string{"room-d", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "room-d" || len(missing) != 1 || missing[0] != "nope" {
		t.Fatalf("%v %v", removed, missing)
	}
	own := read(t, filepath.Join(h, ".ssh", File))
	if strings.Contains(own, "Host room-d.atrium") || !strings.Contains(own, "Host room-a.atrium") {
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
	if _, err := Add(h, exeNix, "linux", []string{"room-d", "room-a"}); err != nil {
		t.Fatal(err)
	}
	s, err = Show(h)
	if err != nil || len(s.Rooms) != 2 || !s.Included || !s.ConfigExists || !strings.Contains(s.Text, "Host room-a.atrium") {
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
	if got := ProxyCommandHub("/usr/bin/atrium", "linux", ""); got != "/usr/bin/atrium ssh-proxy %n" {
		t.Fatalf("no hub: %q", got)
	}
	if got := ProxyCommandHub("/usr/bin/atrium", "linux", "http://127.0.0.1:7790"); got != "/usr/bin/atrium ssh-proxy --hub http://127.0.0.1:7790 %n" {
		t.Fatalf("hub: %q", got)
	}
	if got := ProxyCommandHub(`C:\Program Files\atrium.exe`, "windows", "http://127.0.0.1:7790"); got != `"C:\Program Files\atrium.exe" ssh-proxy --hub http://127.0.0.1:7790 %n` {
		t.Fatalf("windows: %q", got)
	}
	if !strings.Contains(BlockHub("room-d", "atrium", "linux", "http://h:1"), "ProxyCommand atrium ssh-proxy --hub http://h:1 %n\n") {
		t.Fatal("block lacks --hub")
	}
}

// THE LOGIN THE ROOM PUBLISHED GOES INTO ITS BLOCK, so `ssh room-d` needs no user, and a room with none gets no line.
func TestAddHubUsersWritesTheUserIntoTheBlock(t *testing.T) {
	h := home(t)
	_, err := AddHubUsers(h, exeNix, "linux", "", []string{"room-d", "room-g", "room-e"}, map[string]string{"room-d": "claude", "room-g": "localai"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, own := Paths(h)
	text := read(t, own)
	for _, want := range []string{"Host room-d.atrium\n  User claude\n", "Host room-g.atrium\n  User localai\n", "Host room-e.atrium\n  ProxyCommand"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	// Again with the same input changes nothing, and a user change rewrites the one block.
	if res, _ := AddHubUsers(h, exeNix, "linux", "", []string{"room-d"}, map[string]string{"room-d": "claude"}); res.Changed {
		t.Fatal("the same input changed the file")
	}
	AddHubUsers(h, exeNix, "linux", "", []string{"room-d"}, map[string]string{"room-d": "other"})
	if text = read(t, own); !strings.Contains(text, "Host room-d.atrium\n  User other\n") || strings.Contains(text, "User claude") {
		t.Fatalf("not replaced:\n%s", text)
	}
	// A user that would add a line to the config is refused.
	if _, err := AddHubUsers(h, exeNix, "linux", "", []string{"room-d"}, map[string]string{"room-d": "x\nProxyCommand evil"}); err == nil {
		t.Fatal("an injected user was accepted")
	}
}

func TestAddMigratesAnOldHostBlockToTheNameAsTyped(t *testing.T) {
	home := t.TempDir()
	dir, cfg, own := Paths(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A LAN entry for the same Host name sets HostName, which is what %h would expand to.
	if err := os.WriteFile(cfg, []byte("Host room-g\n  HostName room-g.parkplace-via-dhcp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := "# Written by `atrium ssh config`. Edit with that command, not by hand.\n\n# BEGIN atrium other\nHost other\n  ProxyCommand atrium ssh-proxy %h\n  HostKeyAlias other.atrium\n# END atrium other\n"
	if err := os.WriteFile(own, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(home, "atrium", "linux", []string{"room-g"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(own)
	got := string(b)
	if strings.Contains(got, "%h") || strings.Count(got, "ssh-proxy %n\n") != 2 {
		t.Fatalf("every block must use %%n:\n%s", got)
	}
}

// A BLOCK WRITTEN BEFORE THE SUFFIX IS RENAMED when the file is rewritten, and one already renamed is left alone.
func TestBareHostLineGainsTheSuffix(t *testing.T) {
	b := block{room: "room-d", text: "Host room-d\n  HostKeyAlias room-d.atrium\n"}
	want := "Host room-d.atrium\n  HostKeyAlias room-d.atrium\n"
	if got := migrateHostName(b); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	b.text = want
	if got := migrateHostName(b); got != want {
		t.Fatalf("second pass changed it: %q", got)
	}
}

func TestTheBlockCarriesBothKeepaliveLines(t *testing.T) {
	b := Block("room-d", "/usr/bin/atrium", "linux")
	for _, l := range []string{"  ServerAliveInterval 30\n", "  ServerAliveCountMax 3\n"} {
		if !strings.Contains(b, l) {
			t.Fatalf("block lacks %q:\n%s", l, b)
		}
	}
}
