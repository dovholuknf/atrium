//go:build integration

package link

// Helpers shared by tests with and without the git tag. They live here, untagged, because a file under
// `integration && git` is not compiled in a local run and anything it defines would vanish with it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

const loopAddr = "127.0.0.1:5555"

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func obj(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("not an object: %v: %s", err, raw)
	}
	return m
}

func str(m map[string]any, path ...string) string {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return s
}

func loopbackReq(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Host = "127.0.0.1:7778"
	return r
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// a detached auto maintenance run would create and remove maintenance.lock while copyTree walks the template
	noGC := []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=maintenance.auto", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=gc.auto", "GIT_CONFIG_VALUE_1=0"}
	out, err := gitsync.Default.GitEnv(context.Background(), dir, noGC, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(out)
}
