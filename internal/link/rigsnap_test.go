package link

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A rigSnap is the folders of the first git rig a test run builds, kept so the next rigs start from a copy of them.
// Building one costs a few dozen git processes, which on Windows is seconds, and about a hundred tests build one.
type rigSnap struct {
	mu  sync.Mutex
	ok  bool
	dir string
	sha []string
}

var (
	passSnap, urlSnap rigSnap

	snaps snapRoots
)

// snapRoots is the folders the snapshots were copied into, kept to remove them when the run ends.
type snapRoots struct {
	mu   sync.Mutex
	dirs []string
}

func (s *snapRoots) add(dir string) {
	s.mu.Lock()
	s.dirs = append(s.dirs, dir)
	s.mu.Unlock()
}

// start is the snapshot when there is one. When there is not, the caller is the one to build it, holds every other
// caller off until publish or abandon, and gets building true.
func (s *rigSnap) start() (snap *rigSnap, building bool) {
	s.mu.Lock()
	if s.ok {
		s.mu.Unlock()
		return s, false
	}
	return nil, true
}

// abandon lets the others go when the builder ended without publishing, so the next one builds. After a publish it
// does nothing, so it can be deferred.
func (s *rigSnap) abandon() {
	if s.published() {
		return
	}
	s.mu.Unlock()
}

func (s *rigSnap) published() bool { return s.dir != "" && s.ok }

// publish copies `names` under root into a folder of its own and lets the others go.
func (s *rigSnap) publish(t *testing.T, root string, names, sha []string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "link-rigsnap-")
	if err != nil {
		t.Fatal(err)
	}
	snaps.add(dir)
	for _, n := range names {
		copyTree(t, filepath.Join(root, n), filepath.Join(dir, n))
	}
	s.dir, s.sha, s.ok = dir, sha, true
	s.mu.Unlock()
}

func removeSnapshots() {
	snaps.mu.Lock()
	defer snaps.mu.Unlock()
	for _, d := range snaps.dirs {
		_ = os.RemoveAll(d)
	}
}

// copyTree copies a folder of files, keeping their modes.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
