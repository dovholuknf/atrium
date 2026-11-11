//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

func loginStatusOf(t *testing.T, d *Daemon) string {
	t.Helper()
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/v1/login-status", nil))
	var a map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("answer is not JSON: %v: %s", err, rec.Body.String())
	}
	return a["state"]
}

func TestLoginStatusIsInOutOrUnknown(t *testing.T) {
	cases := []struct {
		name string
		look func(string) (string, error)
		run  func(context.Context, string, []string, io.Writer) error
		each time.Duration
		want string
	}{
		{"in", nil, func(context.Context, string, []string, io.Writer) error { return nil }, 0, "in"},
		{"out", nil, func(context.Context, string, []string, io.Writer) error { return errors.New("exit status 1") }, 0, "out"},
		{"not found", func(string) (string, error) { return "", io.EOF }, nil, 0, "unknown"},
		{"timeout", nil, func(ctx context.Context, _ string, _ []string, _ io.Writer) error {
			<-ctx.Done()
			return ctx.Err()
		}, 50 * time.Millisecond, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := testDaemon(t)
			(&fakeExec{}).install(t, d)
			if c.look != nil {
				d.hk.preflightLook = c.look
			}
			if c.run != nil {
				d.hk.preflightRun = c.run
			}
			if c.each > 0 {
				d.hk.preflightEachFor = c.each
			}
			if got := loginStatusOf(t, d); got != c.want {
				t.Fatalf("login status was %q, want %q", got, c.want)
			}
		})
	}
}
