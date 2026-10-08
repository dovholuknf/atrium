package roomspec

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// MemFS is a machine in memory: the fake every adapter is tested against. Paths are forward-slash. It counts writes, so a test
// can say a plan made none.
type MemFS struct {
	nodes    map[string]*memNode
	Denied   map[string]bool   // parents the account cannot examine
	ReadOnly map[string]bool   // folders the account cannot write in
	Aliases  map[string]string // a folder that is really another
	H        Home
	User     string
	Writes   int
	Env      map[string]string
}

type memNode struct {
	dir, link bool
	data      []byte
	mode      fs.FileMode
}

func NewMemFS(home Home, user string) *MemFS {
	m := &MemFS{nodes: map[string]*memNode{}, Denied: map[string]bool{}, ReadOnly: map[string]bool{}, H: home, User: user, Env: map[string]string{}}
	m.mk(home.Dir)
	return m
}

func (m *MemFS) mk(p string) {
	if len(p) == 3 && p[1] == ':' {
		m.nodes[p] = &memNode{dir: true, mode: 0o755}
		return
	}
	p = strings.TrimRight(p, "/")
	for p != "" && p != "." {
		if _, ok := m.nodes[p]; ok {
			return
		}
		m.nodes[p] = &memNode{dir: true, mode: 0o755}
		parent := path.Dir(p)
		if parent == p || strings.HasSuffix(parent, ":") {
			m.nodes[parent+"/"] = &memNode{dir: true, mode: 0o755}
			return
		}
		p = parent
	}
}

func key(p string) string {
	if len(p) > 3 && strings.HasSuffix(p, "/") {
		return strings.TrimRight(p, "/")
	}
	return p
}

// Put makes a file (and its folders) without counting it as a write: it is the machine's starting state.
func (m *MemFS) Put(p, content string) {
	m.mk(path.Dir(p))
	m.nodes[p] = &memNode{data: []byte(content), mode: 0o644}
}

// Link makes p a symlink or junction.
func (m *MemFS) Link(p string) {
	m.mk(path.Dir(p))
	m.nodes[p] = &memNode{link: true, dir: true}
}

func (m *MemFS) Dir(p string) { m.mk(p) }

func (m *MemFS) Has(p string) bool { _, ok := m.nodes[key(p)]; return ok }

func (m *MemFS) Text(p string) string {
	if n, ok := m.nodes[p]; ok {
		return string(n.data)
	}
	return ""
}

func (m *MemFS) Files() []string {
	var out []string
	for p, n := range m.nodes {
		if !n.dir {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

type memInfo struct {
	name string
	n    *memNode
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return int64(len(i.n.data)) }
func (i memInfo) Mode() fs.FileMode {
	switch {
	case i.n.link:
		return fs.ModeSymlink
	case i.n.dir:
		return fs.ModeDir | i.n.mode
	}
	return i.n.mode
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.n.dir && !i.n.link }
func (i memInfo) Sys() any           { return nil }

func (m *MemFS) Lstat(p string) (fs.FileInfo, error) {
	if n, ok := m.nodes[key(p)]; ok {
		return memInfo{path.Base(p), n}, nil
	}
	return nil, &fs.PathError{Op: "lstat", Path: p, Err: fs.ErrNotExist}
}

func (m *MemFS) ReadFile(p string) ([]byte, error) {
	n, ok := m.nodes[p]
	if !ok || n.dir {
		return nil, &fs.PathError{Op: "read", Path: p, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), n.data...), nil
}

func (m *MemFS) Examine(p string) Examine {
	if !m.Has(p) {
		return ExamineMissing
	}
	if m.Denied[key(p)] {
		return ExamineDenied
	}
	return ExamineOK
}

func (m *MemFS) Writable(dir string) bool { return !m.ReadOnly[key(dir)] }
func (m *MemFS) Home() Home               { return m.H }

// Resolve follows Aliases, a folder that is really somewhere else (a link, a junction, a short name).
func (m *MemFS) Resolve(p string) string {
	for from, to := range m.Aliases {
		if p == from || strings.HasPrefix(p, from+"/") {
			return to + strings.TrimPrefix(p, from)
		}
	}
	return p
}
func (m *MemFS) Login() string { return m.User }

func (m *MemFS) WriteFile(p string, d []byte, mode fs.FileMode) error {
	if _, ok := m.nodes[path.Dir(p)]; !ok {
		return &fs.PathError{Op: "write", Path: p, Err: fs.ErrNotExist}
	}
	m.Writes++
	m.nodes[p] = &memNode{data: append([]byte(nil), d...), mode: mode}
	return nil
}

func (m *MemFS) MkdirAll(p string, mode fs.FileMode) error {
	m.Writes++
	m.mk(p)
	return nil
}

func (m *MemFS) RemoveAll(p string) error {
	m.Writes++
	for k := range m.nodes {
		if k == p || strings.HasPrefix(k, p+"/") {
			delete(m.nodes, k)
		}
	}
	return nil
}

// UserEnv and SetUserEnv make MemFS its own environment.
func (m *MemFS) UserEnv(name string) (string, bool) { v, ok := m.Env[name]; return v, ok }
func (m *MemFS) SetUserEnv(name, value string) error {
	m.Writes++
	m.Env[name] = value
	return nil
}

// fakeFetcher serves one pack.
type fakeFetcher struct {
	src    *PackSource
	latest string
	calls  int
}

func (f *fakeFetcher) Fetch(repo, branch, from string) (*PackSource, error) {
	f.calls++
	s := *f.src
	s.Repo, s.Branch = repo, branch
	return &s, nil
}
func (f *fakeFetcher) Latest(repo, branch string) (string, error) { return f.latest, nil }

type fakeSettings struct {
	vals    map[string]string
	running bool
}

func (s *fakeSettings) Get(k string) (string, error) {
	if s.running {
		return "", ErrRoomRunning
	}
	return s.vals[k], nil
}
func (s *fakeSettings) Set(k, v string) error {
	if s.running {
		return ErrRoomRunning
	}
	s.vals[k] = v
	return nil
}
