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

var (
	tmplMu   sync.Mutex
	tmplRoot string
	tmplDone = map[string]string{}
	tmplErr  = map[string]bool{}
)

// fromTemplate copies the folder built by build for key into dst, building it the first time. build gets an empty
// folder and must leave the whole fixture inside it.
func fromTemplate(t *testing.T, key, dst string, build func(t *testing.T, dir string)) {
	t.Helper()
	copyTree(t, templateDir(t, key, build), dst)
}

func templateDir(t *testing.T, key string, build func(t *testing.T, dir string)) string {
	t.Helper()
	tmplMu.Lock()
	defer tmplMu.Unlock() // a Fatal in build still unlocks
	if src, ok := tmplDone[key]; ok {
		return src
	}
	if tmplErr[key] {
		t.Fatalf("the %s fixture failed to build", key)
	}
	if tmplRoot == "" {
		root, err := os.MkdirTemp("", "link-tmpl-")
		if err != nil {
			t.Fatal(err)
		}
		tmplRoot = root
		snaps.add(root)
	}
	src := filepath.Join(tmplRoot, key)
	tmplErr[key] = true // stays set when build fails the test
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	build(t, src)
	tmplErr[key] = false
	tmplDone[key] = src
	return src
}
