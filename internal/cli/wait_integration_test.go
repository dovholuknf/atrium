//go:build integration

package cli

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestWaitReadyPrintsEachAsItComesUp(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asked.Add(1) < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"rooms":2,"room_names":["a","b"],"rooms_quiet":["b"]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := waitReady(&out, srv.URL, []string{"a"}, 5*time.Second, 10*time.Millisecond); err != nil {
		t.Fatalf("waitReady: %v\n%s", err, out.String())
	}
	for _, want := range []string{"up: board", "up: room link", "up: room a"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %q", want, out.String())
		}
	}
}

func TestWaitReadyNamesWhatNeverCameUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"rooms":2,"room_names":["a","b"],"rooms_quiet":["b"]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := waitReady(&out, srv.URL, []string{"a", "b", "c"}, 50*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, errAlreadySaid) {
		t.Fatalf("err = %v, want errAlreadySaid", err)
	}
	if !strings.Contains(out.String(), "never came up: room b, room c") {
		t.Fatalf("output %q does not name b and c", out.String())
	}
}

func TestWaitFailsQuietlyAndNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"rooms":1,"room_names":["a"]}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	root := &cobra.Command{Use: "atrium"}
	root.AddCommand(newWait())
	root.SetOut(&out)
	root.SetErr(&out)
	args := []string{"wait", "--url", srv.URL, "--rooms", "c", "--timeout", "50ms", "--every", "10ms"}
	if code := runRoot(root, args); code == 0 {
		t.Fatalf("exit code 0 on a wait that never came up")
	}
	if strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "already reported") {
		t.Fatalf("failure printed usage or the sentinel: %q", out.String())
	}
	if !strings.Contains(out.String(), "never came up: room c") {
		t.Fatalf("output %q does not name room c", out.String())
	}
}
