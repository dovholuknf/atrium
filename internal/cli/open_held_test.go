package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func heldStdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	was := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	ferr := f()
	w.Close()
	os.Stdout = was
	return <-done, ferr
}

func heldBoard(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
}

// A link another card holds answers that card's address instead of failing.
func TestOpenOfAHeldLinkSaysWhereTheCardIs(t *testing.T) {
	srv := heldBoard(`{"code":"held","step":"claim","held_by":"beta","card":"beta~c7",` +
		`"error":"that link is held by beta. open it there"}`)
	defer srv.Close()
	out, err := heldStdout(t, func() error {
		return openLink("https://github.com/o/r/issues/5", openOpts{boardURL: srv.URL})
	})
	if err != nil {
		t.Fatalf("a held link with a card is an answer, not an error: %v", err)
	}
	for _, want := range []string{"held by beta", "beta~c7", srv.URL + "/#term=beta~c7", "--attach"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestOpenOfAHeldLinkWithNoCardStillFails(t *testing.T) {
	srv := heldBoard(`{"code":"held","step":"claim","held_by":"beta","error":"that link is held by beta. open it there"}`)
	defer srv.Close()
	_, err := heldStdout(t, func() error {
		return openLink("https://github.com/o/r/issues/5", openOpts{boardURL: srv.URL})
	})
	if err == nil || !strings.Contains(err.Error(), "held by beta") {
		t.Errorf("a held link with no card: %v", err)
	}
}
