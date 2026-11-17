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
