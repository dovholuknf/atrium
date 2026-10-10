//go:build integration

package cli

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

func writeBacklog(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBacklogFilesReadAsItems(t *testing.T) {
	root := t.TempDir()
	writeBacklog(t, root, map[string]string{
		"fabric/f-003.md":        "# f-003. An inventory\r\n\r\nStatus: HELD (pause). Owned by @fabric.\r\n",
		"fabric/F-new-x.md":      "# Something\n\nStatus: BUILT 2026-09-29 on `claude/x`.\n",
		"fabric/QUEUE.md":        "# Queue\n",
		"ui/NIGHT-2026-10-04.md": "# Night\n",
		"ui/HANDOFF-a.md":        "# Handoff\n",
		"ui/u-001.md":            "no heading, no status line\n",
		"ui/img/shot.md":         "# not a dept folder\n",
		"runtime/r-9.md":         "# r-9\nStatus: DONE, in claude/main\n",
		"README.md":              "# not under a dept\n",
		"release/m-2.md":         "# m-2 Dropped\nStatus: dropped\n",
		"fabric/notes.txt":       "not markdown",
	})
	items, skipped, err := readBacklogDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]backlogFile{}
	for _, it := range items {
		got[it.ID] = it
	}
	want := map[string][3]string{ // dept, status, title
		"f-003": {"fabric", "held", "An inventory"}, "f-new-x": {"fabric", "built", "Something"},
		"u-001": {"ui", "open", "u-001"}, "r-9": {"runtime", "done", "r-9"}, "m-2": {"release", "dropped", "m-2 Dropped"},
	}
	if len(got) != len(want) || len(skipped) != 3 {
		t.Fatalf("items %v, skipped %v", got, skipped)
	}
	for id, w := range want {
		g := got[id]
		if g.Dept != w[0] || g.Status != w[1] || g.Title != w[2] {
			t.Errorf("%s: %+v, want %v", id, g, w)
		}
	}
	if strings.Contains(got["f-003"].Body, "\r") || !strings.Contains(got["f-003"].Body, "Owned by @fabric") {
		t.Errorf("body: %q", got["f-003"].Body)
	}
}

// The import runs against a real hub store in a temp dir and never a live hub.
func TestBacklogImportRefreshesATempHubAndIsIdempotent(t *testing.T) {
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := link.NewProxy(link.NewHub(link.Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1}), nil, "", nil)
	p.SetBacklog(st)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	root := t.TempDir()
	writeBacklog(t, root, map[string]string{
		"fabric/f-003.md": "# f-003. An inventory\n\nStatus: held\n",
		"ui/u-001.md":     "# One\n\nStatus: open\n",
		"fabric/QUEUE.md": "# Queue\n",
	})
	run := func() string {
		cmd := newRoot()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SetArgs([]string{"backlog", "import", root, "--board-addr", strings.TrimPrefix(srv.URL, "http://")})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %s", err, buf.String())
		}
		return buf.String()
	}
	if out := run(); !strings.Contains(out, "2 added, 0 updated, 0 unchanged, 0 failed, 1 skipped") {
		t.Fatalf("first: %s", out)
	}
	if out := run(); !strings.Contains(out, "0 added, 0 updated, 2 unchanged") {
		t.Fatalf("second: %s", out)
	}
	writeBacklog(t, root, map[string]string{"ui/u-001.md": "# One\n\nStatus: done\n"})
	if out := run(); !strings.Contains(out, "0 added, 1 updated, 1 unchanged") {
		t.Fatalf("third: %s", out)
	}
	b, err := st.ItemGet("u-001")
	if err != nil || b.Status != "done" || b.Dept != "ui" || b.Title != "One" {
		t.Fatalf("row: %+v %v", b, err)
	}
	// The hub gives the next id above what the import brought.
	if n, err := st.ItemFile(hubstore.ItemNew{Dept: "fabric", Title: "new"}); err != nil || n.ID != "f-004" {
		t.Fatalf("next id: %+v %v", n, err)
	}
}
