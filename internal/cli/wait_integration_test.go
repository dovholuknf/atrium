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
