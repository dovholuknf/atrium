//go:build integration

package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func countingRegistry(t *testing.T, d *Daemon, status int) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		fmt.Fprint(w, `{"version":"0.200.0"}`)
	}))
	d.hk.updateRegistry = srv.URL
	t.Cleanup(srv.Close)
	return &hits
}

func TestRunnerUpdateCheckOffAsksNothing(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	hits := countingRegistry(t, d, http.StatusOK)
	if err := d.st.SetSetting(SettingRunnerUpdateCheck, "off"); err != nil {
		t.Fatal(err)
	}
	d.checkRunnerUpdate(h, true)
	if n := hits.Load(); n != 0 {
		t.Fatalf("the registry was asked %d time(s) with the check off", n)
	}
}

func TestRunnerUpdateCheckDefaultAsks(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	hits := countingRegistry(t, d, http.StatusOK)
	d.checkRunnerUpdate(h, true)
	if n := hits.Load(); n != 1 {
		t.Fatalf("the registry was asked %d time(s), want 1", n)
	}
	if got, fresh := d.cachedLatest(codexPkg); got != "0.200.0" || !fresh {
		t.Fatalf("cached %q fresh=%v", got, fresh)
	}
}

func TestAFailedLookupIsCachedForTheWindow(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	hits := countingRegistry(t, d, http.StatusServiceUnavailable)
	d.checkRunnerUpdate(h, true)
	d.checkRunnerUpdate(h, true)
	if n := hits.Load(); n != 1 {
		t.Fatalf("the registry was asked %d time(s) after a failure, want 1", n)
	}
	if got, fresh := d.cachedLatest(codexPkg); got != "" || !fresh {
		t.Fatalf("cached %q fresh=%v, want an empty fresh failure", got, fresh)
	}
}

func TestAFailedLookupKeepsTheLastAnswer(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	d.rememberCheck(codexPkg, func(c *updateCheck) { c.Latest, c.At = "0.190.0", "2000-01-01T00:00:00Z" })
	hits := countingRegistry(t, d, http.StatusServiceUnavailable)
	d.checkRunnerUpdate(h, true)
	d.checkRunnerUpdate(h, true)
	if n := hits.Load(); n != 1 {
		t.Fatalf("the registry was asked %d time(s) after a failure, want 1", n)
	}
	if got, fresh := d.cachedLatest(codexPkg); got != "0.190.0" || !fresh {
		t.Fatalf("cached %q fresh=%v, want the stale answer held fresh", got, fresh)
	}
}
