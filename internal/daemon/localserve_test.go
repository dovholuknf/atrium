package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestLocalKeeperStartsWhatIsDownOncePerGrace(t *testing.T) {
	up := map[string]bool{"http://up/": true}
	var started []string
	k := &localKeeper{
		started: map[string]time.Time{},
		up:      func(_ context.Context, url string) bool { return up[url] },
		start: func(s store.LocalServer, _ string) error {
			started = append(started, s.Name)
			return nil
		},
	}
	servers := []store.LocalServer{{Name: "a", Cmd: "a", Health: "http://up/"}, {Name: "b", Cmd: "b", Health: "http://down/"}}
	t0 := time.Now()
	if got := k.tick(context.Background(), t0, servers, t.TempDir()); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("first tick started %v, want [b]", got)
	}
	if got := k.tick(context.Background(), t0.Add(time.Minute), servers, t.TempDir()); len(got) != 0 {
		t.Fatalf("a server still loading was started again: %v", got)
	}
	if got := k.tick(context.Background(), t0.Add(localServeGrace), servers, t.TempDir()); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("after the grace started %v, want [b]", got)
	}
	if !slices.Equal(started, []string{"b", "b"}) {
		t.Fatalf("start calls %v", started)
	}
}

func TestHealthAnswersCountsAnyHTTPAnswerAsUp(t *testing.T) {
	loading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	url := loading.URL
	if !healthAnswers(context.Background(), url) {
		t.Fatal("a 503 while the model loads must read as up")
	}
	loading.Close()
	if healthAnswers(context.Background(), url) {
		t.Fatal("a closed port must read as down")
	}
}
