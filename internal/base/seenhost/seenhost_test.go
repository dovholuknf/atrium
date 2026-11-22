package seenhost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyARealBrowserSetsTheHostAndTheChangeIsLoggedToAFile(t *testing.T) {
	Reset()
	t.Cleanup(func() { Reset(); LogFile("") })
	path := filepath.Join(t.TempDir(), "human-host.log")
	LogFile(path)
	try := func(ua, fetch string) {
		r := httptest.NewRequest("POST", "http://atrium.ziti/v1/settings", nil)
		r.Header.Set("User-Agent", ua)
		if fetch != "" {
			r.Header.Set("Sec-Fetch-Mode", fetch)
		}
		Note(r)
	}
	for _, ua := range []string{"Mozilla/5.0 (Windows NT 10.0; Microsoft Windows 10.0.26100; en-US) PowerShell/7.6.10",
		"Go-http-client/1.1", "curl/8.1", "node", "Mozilla/5.0 (compatible)"} {
		try(ua, "")
		if got := Base("none"); got != "none" {
			t.Fatalf("%q set the host to %q", ua, got)
		}
	}
	try("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36", "")
	if got := Base("none"); got != "http://atrium.ziti" {
		t.Fatalf("a browser left %q", got)
	}
	try("Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0", "")
	b, err := os.ReadFile(path)
	if err != nil || strings.Count(string(b), "the human's host is now http://atrium.ziti") != 1 {
		t.Fatalf("log file = %q, %v", b, err)
	}
}

func TestALinkWeChooseNeverSaysLocalhost(t *testing.T) {
	Reset()
	for in, want := range map[string]string{
		"http://localhost:7778": "http://127.0.0.1:7778",
		"http://localhost":      "http://127.0.0.1",
		"http://localhostx:1":   "http://localhostx:1",
		"http://atrium.ziti":    "http://atrium.ziti",
	} {
		if got := Base(in); got != want {
			t.Errorf("Base(%q) = %q, want %q", in, got, want)
		}
	}
	NoteBase("http://atrium.ziti")
	if got := Base("http://localhost:7778"); got != "http://atrium.ziti" {
		t.Errorf("a seen host lost to the fallback: %q", got)
	}
	Reset()
}

func TestALoopbackTabNeverDisplacesTheOperatorsOrigin(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	NoteBase("http://atrium.ziti")
	for _, host := range []string{"localhost:7778", "127.0.0.1:7778", "[::1]:7778"} {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		Note(r)
		NoteBase("http://" + host)
		if got := Base("x"); got != "http://atrium.ziti" {
			t.Fatalf("%s displaced the origin: %q", host, got)
		}
	}
	NoteBase("http://other.ziti")
	if got := Base("x"); got != "http://other.ziti" {
		t.Fatalf("a remote origin was refused: %q", got)
	}
	Reset()
	NoteBase("http://localhost:7778")
	if got := Base("x"); got != "http://localhost:7778" {
		t.Fatalf("a lone loopback origin was refused: %q", got)
	}
}
