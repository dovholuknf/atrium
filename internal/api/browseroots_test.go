package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The settings answer shows the resolved browse roots, and reuses the last
// resolve while the configured list is unchanged. A change to the list shows
// on the next read, not after the window.
func TestSettingsBrowseRootsFollowTheListAtOnce(t *testing.T) {
	srv, st, work := fileServer(t)
	a, b := filepath.Join(work, "a"), filepath.Join(work, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	shown := func() []any {
		v, _ := settingsGet(t, srv)["browse_roots_now"].([]any)
		return v
	}
	if err := st.SetSetting(SettingBrowseRoots, filepath.ToSlash(a)); err != nil {
		t.Fatal(err)
	}
	if got := shown(); len(got) != 1 {
		t.Fatalf("one configured root showed as %v", got)
	}
	if err := st.SetSetting(SettingBrowseRoots, filepath.ToSlash(a)+";"+filepath.ToSlash(b)); err != nil {
		t.Fatal(err)
	}
	if got := shown(); len(got) != 2 {
		t.Fatalf("a second root saved did not show on the next read: %v", got)
	}
	// The permission side resolves on its own and agrees.
	if got := srv.browseRootsFor(); len(got) != 2 {
		t.Fatalf("browse resolved %v", got)
	}
}

// A settings read on a machine with a worktree per card. Timed, so the log says
// what one read costs with the resolve reused.
func TestSettingsReadWithManyWorktrees(t *testing.T) {
	srv, st, work := fileServer(t)
	for i := 0; i < 160; i++ {
		d := filepath.Join(work, fmt.Sprintf("wt%03d", i))
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.Register(store.Observed{
			WireName: fmt.Sprintf("wt%03d", i), Worktree: filepath.ToSlash(d), Runner: "claude",
		}); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	first := settingsGet(t, srv)
	cold := time.Since(start)
	start = time.Now()
	for i := 0; i < 10; i++ {
		settingsGet(t, srv)
	}
	warm := time.Since(start) / 10
	t.Logf("settings read with 160 worktrees: first %v, then %v each", cold, warm)
	roots, _ := first["browse_roots_now"].([]any)
	if len(roots) < 160 {
		t.Fatalf("expected the 160 worktrees among the roots, got %d", len(roots))
	}
}
