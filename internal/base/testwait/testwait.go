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
	if !PollEvery(step, limit, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

func Poll(limit time.Duration, cond func() bool) bool {
	return PollEvery(Local, limit, cond)
}

func PollEvery(step, limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(step)
	}
}
