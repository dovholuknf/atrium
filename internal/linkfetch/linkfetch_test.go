package linkfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A slug-only Discourse link redirects to the numbered one, and the pattern reads the number off where it landed.
// The server refuses HEAD, so the GET fallback is the path that lands.
func TestFollowReadsTheNumberOffTheRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/t/mfa-posture-check" {
			http.Redirect(w, r, "/t/mfa-posture-check/6158", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := Follow(context.Background(), srv.URL+"/t/mfa-posture-check", []string{`/t/(?P<slug>[^/]+)/(?P<num>\d+)`})
	if err != nil {
		t.Fatal(err)
	}
	if got["num"] != "6158" || got["slug"] != "mfa-posture-check" {
		t.Fatalf("facts: %v", got)
	}
}

func TestFollowRefusesWhatItCannotFollow(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	for name, c := range map[string]struct {
		link string
		args []string
	}{
		"no pattern":   {srv.URL, nil},
		"bad pattern":  {srv.URL, []string{"("}},
		"not http":     {"file:///etc/passwd", []string{"x"}},
		"answered 404": {srv.URL + "/t/gone", []string{"x"}},
	} {
		if _, err := Follow(context.Background(), c.link, c.args); err == nil {
			t.Fatalf("%s: wanted an error", name)
		}
	}
}
