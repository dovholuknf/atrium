package cli

import (
	"strings"
	"testing"
)

func TestBoardOpensOnlyOnAFirstStartWithSomewhereToShowIt(t *testing.T) {
	cases := []struct {
		name          string
		fresh, noOpen bool
		goos          string
		env           map[string]string
		want          bool
	}{
		{"first start on windows", true, false, "windows", nil, true},
		{"first start on macos", true, false, "darwin", nil, true},
		{"first start on linux with x", true, false, "linux", map[string]string{"DISPLAY": ":0"}, true},
		{"first start on linux with wayland", true, false, "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"later start", false, false, "windows", nil, false},
		{"--no-open", true, true, "windows", nil, false},
		{"over ssh", true, false, "darwin", map[string]string{"SSH_CONNECTION": "10.0.0.2 50000 10.0.0.1 22"}, false},
		{"over ssh with a forwarded display", true, false, "linux", map[string]string{"DISPLAY": "localhost:10.0", "SSH_CONNECTION": "a b c d"}, false},
		{"linux with no display", true, false, "linux", nil, false},
	}
	for _, c := range cases {
		got := boardOpens(c.fresh, c.noOpen, c.goos, func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%s: boardOpens is %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFirstStartURLCarriesTheFolder(t *testing.T) {
	if got := firstStartURL("127.0.0.1:7778", ""); got != "http://127.0.0.1:7778/" {
		t.Errorf("no folder: %q", got)
	}
	if got := firstStartURL("127.0.0.1:7778", "C:/My Work/repo"); got != "http://127.0.0.1:7778/#start=C:%2FMy%20Work%2Frepo" {
		t.Errorf("folder: %q", got)
	}
}

func TestBannerHintsForAnAdministratorAndOverSSH(t *testing.T) {
	ssh := func(k string) string {
		if k == "SSH_CONNECTION" {
			return "10.0.0.2 50000 10.0.0.1 22"
		}
		return ""
	}
	none := func(string) string { return "" }
	cases := []struct {
		name  string
		admin bool
		env   func(string) string
		host  string
		want  []string
	}{
		{"neither", false, none, "box", nil},
		{"administrator", true, none, "box", []string{"agents run as you, an administrator"}},
		{"over ssh", false, ssh, "box", []string{"forward the board: ssh -L 7790:127.0.0.1:7790 box"}},
		{"over ssh, no hostname", false, ssh, "", []string{"forward the board: ssh -L 7790:127.0.0.1:7790 <host>"}},
		{"both", true, ssh, "box", []string{"agents run as you, an administrator",
			"forward the board: ssh -L 7790:127.0.0.1:7790 box"}},
	}
	for _, c := range cases {
		got := bannerHints("127.0.0.1:7790", c.admin, c.env, c.host)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
