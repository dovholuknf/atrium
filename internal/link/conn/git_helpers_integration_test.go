//go:build integration && git

package conn

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

func TestMain(m *testing.M) {
	code := m.Run()
	removeSnapshots()
	os.Exit(code)
}

// gitSlots bounds how many git-heavy rigs run at once, so a loaded machine does not starve the hub's heartbeats.
var gitSlots = make(chan struct{}, 4)

// gitSlot lets the calling test run beside the other git rigs, once, and holds one of the slots until it ends.
func gitSlot(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow: builds a git fixture; runs in the full set, not under -short")
	}
	if _, again := slotted.LoadOrStore(t, true); again {
		return
	}
	t.Cleanup(func() { slotted.Delete(t) })
	parallel(t)
	gitSlots <- struct{}{}
	t.Cleanup(func() { <-gitSlots })
}

var slotted sync.Map

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

// templates is the one set of built fixtures. Its fields are touched only through its methods.
var templates = &templateSet{done: map[string]string{}, failed: map[string]bool{}}

type templateSet struct {
	mu     sync.Mutex
	root   string
	done   map[string]string
	failed map[string]bool
}

// fromTemplate copies the folder built by build for key into dst, building it the first time.
func fromTemplate(t *testing.T, key, dst string, build func(t *testing.T, dir string)) {
	t.Helper()
	copyTree(t, templates.dir(t, key, build), dst)
}

func (s *templateSet) dir(t *testing.T, key string, build func(t *testing.T, dir string)) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock() // a Fatal in build still unlocks
	if src, ok := s.done[key]; ok {
		return src
	}
	if s.failed[key] {
		t.Fatalf("the %s fixture failed to build", key)
	}
	if s.root == "" {
		root, err := os.MkdirTemp("", "link-tmpl-")
		if err != nil {
			t.Fatal(err)
		}
		s.root = root
	}
	src := filepath.Join(s.root, key)
	s.failed[key] = true // stays set when build fails the test
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	build(t, src)
	s.failed[key] = false
	s.done[key] = src
	return src
}

func removeSnapshots() {
	templates.mu.Lock()
	defer templates.mu.Unlock()
	if templates.root != "" {
		_ = os.RemoveAll(templates.root)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// a lock is transient, and one that vanishes between the listing and the stat fails the copy
		if strings.HasSuffix(p, ".lock") {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()|0o200)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}
