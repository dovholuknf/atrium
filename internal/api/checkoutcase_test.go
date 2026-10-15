package api

import (
	"os"
	"path/filepath"
	"testing"
)

type fakeEnt struct{ name string }

func (f fakeEnt) Name() string               { return f.name }
func (f fakeEnt) IsDir() bool                { return true }
func (f fakeEnt) Type() os.FileMode          { return os.ModeDir }
func (f fakeEnt) Info() (os.FileInfo, error) { return nil, nil }

func TestFindProviderCheckoutIn(t *testing.T) {
	disk := map[string][]string{"/r": {"O", "other"}, "/r/O": {"R", "x"}, "/r/other": {"R"}}
	read := func(d string) ([]os.DirEntry, error) {
		var out []os.DirEntry
		for _, n := range disk[filepath.ToSlash(d)] {
			out = append(out, fakeEnt{n})
		}
		return out, nil
	}
	repos := map[string]bool{"/r/O/R": true, "/r/other/R": true}
	is := func(p string) bool { return repos[filepath.ToSlash(p)] }
	for _, c := range []struct{ org, repo, want string }{
		{"O", "R", "/r/O/R"}, {"o", "r", "/r/O/R"}, {"O", "r", "/r/O/R"}, {"zz", "R", ""}, {"O", "none", ""},
	} {
		if got := findProviderCheckoutIn("/r", c.org, c.repo, read, is); got != c.want {
			t.Errorf("%s/%s = %q, want %q", c.org, c.repo, got, c.want)
		}
	}
}
