package link

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A repository fixture costs a dozen git processes and git is slow to start on a machine with a virus scanner. So each
// one is built once per test binary and copied for every test that wants it. A copy is private: a test may change it.
// The folders are removed with the rig snapshots, by removeSnapshots.

// templates is the one set of built fixtures. Its fields are touched only through its methods.
var templates = &templateSet{done: map[string]string{}, failed: map[string]bool{}}

type templateSet struct {
	mu     sync.Mutex
	root   string
	done   map[string]string
	failed map[string]bool
}

// fromTemplate copies the folder built by build for key into dst, building it the first time. build gets an empty
// folder and must leave the whole fixture inside it.
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
		snaps.add(root)
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
