package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The launch dialog's `repo` list, room side: the checkouts this room's own cards have used, and cloning one from the
// hub. docs/changes/launch-repo-picker.md.
//
// FAST BY CONSTRUCTION. The list is made from the directories of the room's cards, and each is resolved to its
// repository by READING .git, HEAD and config, never by running git: no process on the open path. The answer is kept
// for checkoutsFresh and a stale one is returned at once while a refresh runs behind it, so the dialog paints from
// memory on every open but the first of a room's life.
//
// CLONE IS THE HUB'S COPY ONLY. It goes through Server.CloneFromHub, which the daemon refuses when the room has no hub,
// so a room never reads a forge. It lands in `<git.scm_root>/<host>/<org>/<repo>` (`~/git` when unset), the folder
// every other scm clone uses.

const checkoutsFresh = 20 * time.Second

// checkoutHistory is how many archived cards are read besides the live ones.
const checkoutHistory = 500

// checkout is one directory a card has used, as the list shows it.
type checkout struct {
	// Repo is the repository's own folder name.
	Repo string `json:"repo"`
	// Slug is host/org/repo as the hub names it, read from origin, or "" when origin is not a forge URL.
	Slug string `json:"slug"`
	// Path is the checkout or worktree, forward slashes.
	Path   string `json:"path"`
	Branch string `json:"branch"`
	// Linked marks a worktree. Root is the repository's own checkout, which is Path for one that is not.
	Linked bool   `json:"linked"`
	Root   string `json:"root"`
	// Last is when a card last used it.
	Last string `json:"last"`
}

// dirUse is a directory a card used and when.
type dirUse struct {
	dir  string
	when time.Time
}

type checkoutCache struct {
	mu         sync.Mutex
	list       []checkout
	at         time.Time
	have       bool
	refreshing bool
}

// repoFacts is what a directory's .git says. top is the folder holding it.
type repoFacts struct {
	top, root, branch, slug string
	linked                  bool
}

// readRepoFacts finds the repository a directory is in by walking up to its .git and reading the files. ok is false
// for a directory that is gone or is in no repository.
func readRepoFacts(dir string) (f repoFacts, ok bool) {
	dir = filepath.Clean(filepath.FromSlash(strings.TrimSpace(dir)))
	if dir == "" || dir == "." {
		return f, false
	}
	for i := 0; i < 24; i++ {
		dotgit := filepath.Join(dir, ".git")
		if fi, err := os.Lstat(dotgit); err == nil {
			f.top = dir
			gitdir, common := dotgit, dotgit
			if !fi.IsDir() {
				raw, err := os.ReadFile(dotgit)
				if err != nil {
					return f, false
				}
				line := strings.TrimSpace(string(raw))
				if !strings.HasPrefix(line, "gitdir:") {
					return f, false
				}
				gitdir = filepath.FromSlash(strings.TrimSpace(strings.TrimPrefix(line, "gitdir:")))
				if !filepath.IsAbs(gitdir) {
					gitdir = filepath.Join(dir, gitdir)
				}
				common = gitdir
				if raw, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
					common = filepath.FromSlash(strings.TrimSpace(string(raw)))
					if !filepath.IsAbs(common) {
						common = filepath.Join(gitdir, common)
					}
					common = filepath.Clean(common)
				}
				f.linked = true
			}
			f.root = f.top
			if f.linked {
				f.root = filepath.Dir(common)
				if filepath.Base(common) != ".git" {
					f.root = f.top
				}
			}
			f.branch = headBranchOf(gitdir)
			f.slug = originSlug(filepath.Join(common, "config"))
			return f, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return f, false
}

func headBranchOf(gitdir string) string {
	raw, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "ref:") {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "ref:")), "refs/heads/")
}

func originSlug(config string) string {
	return originSlugWithReadFile(config, os.ReadFile)
}

func originSlugWithReadFile(config string, readFile func(string) ([]byte, error)) string {
	raw, err := readFile(config)
	if err != nil {
		return ""
	}
	in := false
	for _, ln := range strings.Split(string(raw), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "[") {
			in = strings.HasPrefix(ln, `[remote "origin"]`)
			continue
		}
		if !in {
			continue
		}
		k, v, found := strings.Cut(ln, "=")
		if found && strings.TrimSpace(k) == "url" {
			ref, err := gitsync.ParseURL(strings.TrimSpace(v))
			if err != nil {
				return ""
			}
			return ref.Name()
		}
	}
	return ""
}

// buildCheckouts resolves directories to checkouts, newest use first, one row per worktree or checkout.
func buildCheckouts(uses []dirUse) []checkout {
	byPath := map[string]*checkout{}
	var order []string
	key := func(p string) string {
		if filepath.Separator == '\\' {
			return strings.ToLower(p)
		}
		return p
	}
	for _, u := range uses {
		if strings.TrimSpace(u.dir) == "" {
			continue
		}
		f, ok := readRepoFacts(u.dir)
		if !ok {
			continue
		}
		k := key(f.top)
		c := byPath[k]
		if c == nil {
			c = &checkout{
				Repo: filepath.Base(f.root), Slug: f.slug, Path: filepath.ToSlash(f.top), Branch: f.branch,
				Linked: f.linked, Root: filepath.ToSlash(f.root),
			}
			byPath[k] = c
			order = append(order, k)
		}
		if w := u.when.UTC().Format(time.RFC3339); w > c.Last {
			c.Last = w
		}
	}
	out := make([]checkout, 0, len(order))
	for _, k := range order {
		out = append(out, *byPath[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

// cardDirs is every directory a live or archived card of this room was launched in.
func (s *Server) cardDirs() []dirUse {
	var uses []dirUse
	if s.st == nil {
		return nil
	}
	if tasks, err := s.st.List(); err == nil {
		for _, t := range tasks {
			uses = append(uses, dirUse{t.Worktree, t.LastActivityAt})
		}
	}
	if tasks, err := s.st.ListArchived(checkoutHistory); err == nil {
		for _, t := range tasks {
			uses = append(uses, dirUse{t.Worktree, t.LastActivityAt})
		}
	}
	return uses
}

// checkoutList answers from memory when it can. A stale answer is returned and refreshed behind the request.
func (s *Server) checkoutList() []checkout {
	c := &s.checkouts
	c.mu.Lock()
	if c.have {
		list, stale := c.list, time.Since(c.at) > checkoutsFresh
		if stale && !c.refreshing {
			c.refreshing = true
			go s.refreshCheckouts()
		}
		c.mu.Unlock()
		return list
	}
	c.mu.Unlock()
	return s.refreshCheckouts()
}

func (s *Server) refreshCheckouts() []checkout {
	list := buildCheckouts(s.cardDirs())
	c := &s.checkouts
	c.mu.Lock()
	c.list, c.at, c.have, c.refreshing = list, time.Now(), true, false
	c.mu.Unlock()
	return list
}

// GET /v1/checkouts: this room's checkouts, newest use first.
func (s *Server) listCheckouts(w http.ResponseWriter, r *http.Request) {
	list := s.checkoutList()
	if list == nil {
		list = []checkout{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkouts": list})
}

// POST /v1/checkouts/clone {repo: "host/org/repo"}: clone it from the hub, or answer where it already is.
func (s *Server) cloneCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ref, err := gitsync.ParseName(strings.Trim(strings.TrimSpace(in.Repo), "/"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if s.CloneFromHub == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("this room cannot clone"))
		return
	}
	host := ref.Host
	if host == gitsync.DefaultHost {
		host = "github.com"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	res, err := s.CloneFromHub(ctx, "https://"+host+"/"+ref.Owner+"/"+ref.Repo)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, gitsync.ErrAsked) || errors.Is(err, gitsync.ErrDenied) {
			code = http.StatusConflict
		}
		writeErr(w, code, err)
		return
	}
	// The next open of the list shows it.
	s.refreshCheckouts()
	writeJSON(w, http.StatusOK, res)
}
