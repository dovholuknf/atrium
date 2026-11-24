package gitsync

import (
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ON AN IDLE HUB NO GIT RUNS. A process spawn is 50-100ms on Windows, and the timers that asked git whether a ref
// moved ran a dozen a minute. They now look at the ref files with stat, and ask git only when one changed.

// mirrorRetry is how soon a repository whose mirror pass failed is passed again with no ref file changed.
const mirrorRetry = 5 * time.Minute

type printed struct {
	print string
	retry time.Time
}

// statPrint writes what a file's stat says to w, and a small file's text, or that it is missing.
func statPrint(w io.Writer, path string) {
	fi, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(w, "%s -\n", path)
		return
	}
	fmt.Fprintf(w, "%s %d %d\n", path, fi.Size(), fi.ModTime().UnixNano())
	if fi.Size() <= 4096 {
		b, _ := os.ReadFile(path)
		_, _ = w.Write(b)
	}
}

// commonDir is a checkout's git directory shared by its worktrees, read from the disk with no git. "" when it is
// not found.
func commonDir(checkout string) string {
	g := filepath.Join(checkout, ".git")
	fi, err := os.Stat(g)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return g
	}
	b, err := os.ReadFile(g)
	if err != nil {
		return ""
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(checkout, dir)
	}
	c, err := os.ReadFile(filepath.Join(dir, "commondir"))
	if err != nil {
		return dir
	}
	common := strings.TrimSpace(string(c))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	return common
}

// mirrorPrint is every ref file a mirror pass of r reads: the checkout's branch and origin/main, and the bare's
// branch, loose, packed or in a reftable. "" when the checkout's git directory is not found, which never matches.
func (h *Hub) mirrorPrint(r Repo) string {
	ck := commonDir(r.Checkout)
	if ck == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n%s\n", r.Name, r.Checkout, r.Branch)
	bare := h.Bare(r.Name)
	for _, p := range []string{
		filepath.Join(ck, "refs", "heads", filepath.FromSlash(r.Branch)),
		filepath.Join(ck, "refs", "remotes", "origin", "main"),
		filepath.Join(ck, "packed-refs"),
		filepath.Join(ck, "reftable", "tables.list"),
		filepath.Join(bare, "HEAD"),
		filepath.Join(bare, "refs", "heads", filepath.FromSlash(r.Branch)),
		filepath.Join(bare, "packed-refs"),
		filepath.Join(bare, "reftable", "tables.list"),
	} {
		statPrint(&b, p)
	}
	return b.String()
}

// mirrorIdle is whether name's ref files are as its last pass saw them, and that pass did not fail lately.
func (h *Hub) mirrorIdle(name, fp string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.printed[name]
	return ok && fp != "" && p.print == fp && (p.retry.IsZero() || time.Now().Before(p.retry))
}

func (h *Hub) mirrorDone(name, fp string, err error) {
	p := printed{print: fp}
	if err != nil {
		p.retry = time.Now().Add(mirrorRetry)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.printed == nil {
		h.printed = map[string]printed{}
	}
	h.printed[name] = p
}

// refsPrint is a hash of every ref file in the store: each repository's HEAD, packed-refs, reftable stack and the
// files under refs.
func (s *Store) refsPrint() uint64 {
	f := fnv.New64a()
	ents, _ := s.List()
	for _, e := range ents {
		statPrint(f, filepath.Join(e.Dir, "HEAD"))
		statPrint(f, filepath.Join(e.Dir, "packed-refs"))
		statPrint(f, filepath.Join(e.Dir, "reftable", "tables.list"))
		_ = filepath.WalkDir(filepath.Join(e.Dir, "refs"), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				statPrint(f, p)
			}
			return nil
		})
	}
	return f.Sum64()
}
