//go:build integration

package link

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnUploadForwardedByTheHubCarriesItsSteps(t *testing.T) {
	t.Parallel()
	room := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Server-Timing", "room-read;dur=1.0")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer room.Close()

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, r, done := timeUpload(w, r, time.Now())
		defer done("r1")
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, room.URL+r.URL.Path, r.Body)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		for k, v := range res.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	defer front.Close()

	res, err := http.Post(front.URL+"/v1/tasks/c1/files", "text/plain", strings.NewReader("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got := strings.Join(res.Header.Values("Server-Timing"), ",")
	for _, step := range []string{"room-read", "hub-route", "hub-body", "hub-sent", "hub-room"} {
		if !strings.Contains(got, step) {
			t.Fatalf("Server-Timing %q has no %s", got, step)
		}
	}
	if !isUpload(httptest.NewRequest("POST", "/v1/tasks/c1/files", nil)) || isUpload(httptest.NewRequest("GET", "/v1/tasks/c1/files", nil)) {
		t.Fatal("isUpload matched the wrong requests")
	}
}
