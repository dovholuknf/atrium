//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// AN UNKNOWN CARD IS A 404, NEVER A 500, on every card route there is. A 500
// read as a broken room to every script that named a card the room did not
// have, and the hub's routing reads anything but a 200 as "not here", so the
// two are not interchangeable. The routes are read out of api.go, so a route
// added later is walked without anybody remembering to list it.
func TestUnknownCardIsNeverA5xx(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := regexp.MustCompile(`HandleFunc\("GET (/v1/tasks/\{id\}[^"]*)"`).FindAllStringSubmatch(string(src), -1)
	if len(routes) < 10 {
		t.Fatalf("found %d GET card routes in api.go, the pattern has stopped matching", len(routes))
	}
	srv, _, _ := fileServer(t)
	other := regexp.MustCompile(`\{[a-z_]+\.{0,3}\}`)
	for _, m := range routes {
		path := strings.Replace(m[1], "{id}", "01a0ffff-ffff-7fff-bfff-ffffffffffff", 1)
		path = other.ReplaceAllString(path, "x")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code >= 500 {
			t.Errorf("GET %s with an unknown card answered %d: %s", m[1], rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}
