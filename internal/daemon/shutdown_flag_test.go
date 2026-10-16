//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stopAsk posts a shutdown with now=1, so no wrap-up is involved, and returns the answer.
func stopAsk(d *Daemon, runners bool) *httptest.ResponseRecorder {
	q := "?now=1"
	if runners {
		q += "&runners=1"
	}
	rec := httptest.NewRecorder()
	d.handleShutdown(rec, wrapShutdownReq(q))
	return rec
}

func TestFirstRunnersStopKeepsItsFlagAgainstAPlainOne(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	d.opts.ShutdownToken = "tok"
	if rec := stopAsk(d, true); rec.Code != http.StatusOK {
		t.Fatalf("first stop: %d %q", rec.Code, rec.Body.String())
	}
	rec := stopAsk(d, false)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "--runners") {
		t.Fatalf("plain after --runners: %d %q", rec.Code, rec.Body.String())
	}
	if !d.stopRunners.Load() {
		t.Fatal("the later plain stop cleared the flag stopkeep reads")
	}
}

func TestFirstPlainStopKeepsItsFlagAgainstARunnersOne(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	d.opts.ShutdownToken = "tok"
	if rec := stopAsk(d, false); rec.Code != http.StatusOK {
		t.Fatalf("first stop: %d %q", rec.Code, rec.Body.String())
	}
	rec := stopAsk(d, true)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plain") {
		t.Fatalf("--runners after plain: %d %q", rec.Code, rec.Body.String())
	}
	if d.stopRunners.Load() {
		t.Fatal("the later --runners stop set the flag stopkeep reads")
	}
}

func TestTwoAgreeingStopsBothAreAccepted(t *testing.T) {
	t.Parallel()
	for _, runners := range []bool{false, true} {
		d := testDaemon(t)
		d.opts.ShutdownToken = "tok"
		for i := 0; i < 2; i++ {
			if rec := stopAsk(d, runners); rec.Code != http.StatusOK {
				t.Fatalf("runners=%v request %d: %d %q", runners, i, rec.Code, rec.Body.String())
			}
		}
		if d.stopRunners.Load() != runners {
			t.Fatalf("runners=%v: flag = %v", runners, d.stopRunners.Load())
		}
	}
}
