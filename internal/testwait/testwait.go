package testwait

import (
	"testing"
	"time"
)

const (
	Local = 10 * time.Millisecond
	HTTP  = 50 * time.Millisecond
)

func Eventually(t testing.TB, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	EventuallyEvery(t, Local, limit, what, cond)
}

func EventuallyHTTP(t testing.TB, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	EventuallyEvery(t, HTTP, limit, what, cond)
}

func EventuallyEvery(t testing.TB, step, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(step)
	}
}
