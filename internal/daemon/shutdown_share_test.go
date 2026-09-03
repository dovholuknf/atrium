package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// probe sends a shutdown request from an address and reports what came back.
func probeShutdown(t *testing.T, d *Daemon, from, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/shutdown", nil)
	req.RemoteAddr = from
	if token != "" {
		req.Header.Set("X-Atrium-Token", token)
	}
	d.handleShutdown(rec, req)
	return rec
}

// startSharing puts an overlay into the state the shutdown rule cares about,
// without needing a zrok environment to do it.
func startSharing(d *Daemon) {
	n := d.nat(OverlayZrok)
	n.mu.Lock()
	n.srv = &http.Server{}
	n.mu.Unlock()
}

// The rule being protected: at this keyboard, no share, no token, allowed.
func TestShutdownFromLoopbackWithNoShare(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if rec := probeShutdown(t, d, "127.0.0.1:5555", ""); rec.Code != http.StatusOK {
		t.Fatalf("a loopback shutdown with no share answered %d, wanted 200", rec.Code)
	}
}

// A share makes the loopback rule a lie. The tunneler runs on this machine and
// terminates the connection here, so a request from anywhere in the world
// presents as 127.0.0.1. Trusting the source address would turn "only someone
// at this keyboard" into "anyone the overlay admits", and a kill switch is the
// worst thing to hand out by accident.
func TestShutdownIsRefusedWhileAShareIsUp(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	startSharing(d)

	rec := probeShutdown(t, d, "127.0.0.1:5555", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("shutdown answered %d while sharing, wanted 403", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("the refusal says nothing about why, so it reads as a bug")
	}
}

// A token was always how remote shutdown was allowed on purpose, and a share
// must not take that away.
func TestShutdownWithATokenStillWorksWhileSharing(t *testing.T) {
	d, _, cancel, _ := startDaemonWith(t, func(o *Options) { o.ShutdownToken = "s3cret" })
	defer cancel()
	startSharing(d)

	rec := probeShutdown(t, d, "203.0.113.7:5555", "s3cret")
	if rec.Code != http.StatusOK {
		t.Fatalf("a tokened shutdown answered %d while sharing, wanted 200", rec.Code)
	}
}
