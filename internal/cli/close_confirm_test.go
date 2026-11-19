package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloseRerunIsWhatWasTypedWithYes(t *testing.T) {
	got := closeRerun([]string{"close", "01a1", "--keep", "all", "--url", "127.0.0.1:27778"})
	if want := "atrium close 01a1 --keep all --url 127.0.0.1:27778 --yes"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := closeRerun([]string{"close", "--cwd", `C:\my work`}); got != `atrium close --cwd "C:\my work" --yes` {
		t.Fatalf("a path with a space was not quoted: %q", got)
	}
}

// A yes at the prompt closes the card as --yes would, and a no changes nothing.
func TestCloseAsksAndAYesCloses(t *testing.T) {
	var posted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted++
			_, _ = w.Write([]byte(`{"card":"c1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"card":"c1"}`))
	}))
	defer srv.Close()

	if err := closeCardCmd("c1", closeOpts{boardURL: srv.URL, confirm: func(string) bool { return false }}); err != nil || posted != 0 {
		t.Fatalf("a no closed it: %v posted=%d", err, posted)
	}
	if err := closeCardCmd("c1", closeOpts{boardURL: srv.URL, confirm: func(string) bool { return true }}); err != nil || posted != 1 {
		t.Fatalf("a yes did not close it: %v posted=%d", err, posted)
	}
}
