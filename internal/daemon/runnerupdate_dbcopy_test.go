package daemon

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The update check driven against a COPY of a real database, which is where the
// cards this fixes actually live. ATRIUM_DBCOPY names the copied atrium.db, with
// its -wal beside it. Unset, this skips, so the normal suite never needs it.
// Never point it at the live file: each subtest copies again before opening.

func copyDB(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	for _, suffix := range []string{"", "-wal"} {
		in, err := os.Open(src + suffix)
		if err != nil {
			if suffix == "-wal" && os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dir, "atrium.db"+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			t.Fatal(err)
		}
		in.Close()
		out.Close()
	}
	return filepath.ToSlash(filepath.Join(dir, "atrium.db"))
}

func dbcopyDaemon(t *testing.T, src string) *Daemon {
	t.Helper()
	dir := t.TempDir()
	d, err := New(Options{
		AgentAddr: freePort(t), HumanAddr: freePort(t), DBPath: copyDB(t, src),
		LongPoll: time.Second, LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// cardByPrefix finds a card by the start of its id, archived or not.
func cardByPrefix(t *testing.T, d *Daemon, prefix string) *store.Task {
	t.Helper()
	live, _ := d.st.List()
	arch, _ := d.st.ListArchived(100000)
	for _, c := range append(live, arch...) {
		if strings.HasPrefix(c.ID, prefix) {
			return c
		}
	}
	t.Fatalf("no card %s in the copy", prefix)
	return nil
}

// registryAnswering serves one version for every package, through the same seam
// the real request goes through.
func registryAnswering(t *testing.T, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":%q}`, version)
	}))
	old := updateRegistry
	updateRegistry = srv.URL
	t.Cleanup(func() { updateRegistry = old; srv.Close() })
}

// forceAsk makes the cached answer stale so the check goes to the registry.
func forceAsk(t *testing.T, d *Daemon) {
	t.Helper()
	if err := d.st.SetSetting(updateSetting, "{}"); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateCardsInACopyOfTheLiveDatabase(t *testing.T) {
	src := os.Getenv("ATRIUM_DBCOPY")
	if src == "" {
		t.Skip("ATRIUM_DBCOPY is not set")
	}

	t.Run("codex 01a0d368, registry says the installed version", func(t *testing.T) {
		d := dbcopyDaemon(t, src)
		before := cardByPrefix(t, d, "01a0d368")
		t.Logf("before: %q status=%s archived=%v url=%q key=%q",
			before.Title, before.Status, before.ArchivedAt, before.URL, before.IntakeKey)
		h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
		registryAnswering(t, "0.156.1")
		forceAsk(t, d)
		d.checkRunnerUpdate(h, true)
		after := cardByPrefix(t, d, "01a0d368")
		if after.ArchivedAt == nil {
			t.Fatalf("not archived: %q", after.Title)
		}
		t.Logf("after: archived=%v", after.ArchivedAt)
	})

	t.Run("codex 01a0d368, registry says 0.158.0", func(t *testing.T) {
		d := dbcopyDaemon(t, src)
		h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
		registryAnswering(t, "0.158.0")
		forceAsk(t, d)
		d.checkRunnerUpdate(h, true)
		after := cardByPrefix(t, d, "01a0d368")
		t.Logf("after: %q archived=%v url=%q", after.Title, after.ArchivedAt, after.URL)
		if after.ArchivedAt == nil && after.Title != "codex: 0.156.1 to 0.158.0" {
			t.Fatalf("left saying %q", after.Title)
		}
		if after.ArchivedAt != nil {
			t.Fatal("expected a rewrite, got a withdrawal")
		}
	})

	t.Run("legacy claude cards, a newer claude installed", func(t *testing.T) {
		d := dbcopyDaemon(t, src)
		const pkg = "@anthropic-ai/claude-code"
		h, _ := fakeRunner(t, "claude", pkg, "2.1.300")
		registryAnswering(t, "2.1.300")
		for _, p := range []string{"01a0a201", "01a0a286"} {
			c := cardByPrefix(t, d, p)
			t.Logf("before %s: %q status=%s archived=%v ext=%q", p, c.Title, c.Status, c.ArchivedAt, c.ExternalID)
		}
		forceAsk(t, d)
		d.checkRunnerUpdate(h, true)
		for _, p := range []string{"01a0a201", "01a0a286"} {
			if c := cardByPrefix(t, d, p); c.ArchivedAt == nil {
				t.Fatalf("%s not archived: %q status=%s", p, c.Title, c.Status)
			}
		}
	})
}
