package api

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/safepath"
)

// pageSandbox makes a card's page run in an opaque origin, so its script cannot read or drive the board.
const pageSandbox = "sandbox allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals " +
	"allow-downloads"

// PagePrefixHeader is the path the hub served the page under, `/room/<room>/<card>/files/`, so a root-relative link in
// the page can be put back under it.
const PagePrefixHeader = "X-Atrium-Page-Prefix"

// pageBoardDir is where under the prefix a page finds the board's own css, js and vendor files.
const pageBoardDir = "_board/"

var (
	pageAttr = regexp.MustCompile(`(?i)(\s(?:href|src|action|poster)\s*=\s*["']?)/([^/"'][^"'\s>]*)`)
	pageURL  = regexp.MustCompile(`(?i)(url\(\s*["']?)/([^/"')][^"')\s]*)`)
	pageRoot = regexp.MustCompile(`^(css|js|vendor|m)/`)
)

// pageRewrite puts a page's root-relative links under the prefix: the board's own css, js and vendor under _board/,
// anything else beside the page, as a folder served at a site root would find it. Script is not rewritten.
func pageRewrite(body []byte, prefix, dir string) []byte {
	fix := func(re *regexp.Regexp) {
		body = re.ReplaceAllFunc(body, func(m []byte) []byte {
			g := re.FindSubmatch(m)
			rest := string(g[2])
			if pageRoot.MatchString(rest) {
				return []byte(string(g[1]) + prefix + pageBoardDir + rest)
			}
			return []byte(string(g[1]) + prefix + dir + rest)
		})
	}
	fix(pageAttr)
	fix(pageURL)
	return body
}

// pageDirOf is the folder of the file being served, relative to the card, as a prefix: "" or "mock/".
func pageDirOf(worktree, real string) string {
	rel, ok := realRelative(filepath.ToSlash(filepath.FromSlash(worktree)), filepath.Dir(real))
	if !ok || rel == "." {
		return ""
	}
	return rel + "/"
}

// pageDefault is the file a folder opens as: index.html, else its one .html file.
func pageDefault(dir string) (string, bool) {
	if fi, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !fi.IsDir() {
		return filepath.Join(dir, "index.html"), true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var only string
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".html") {
			if only != "" {
				return "", false
			}
			only = e.Name()
		}
	}
	return filepath.Join(dir, only), only != ""
}

// serveRoomPage is servePage for the address atrium_link gives, reached on the room's own port rather than the hub's.
func (s *Server) serveRoomPage(w http.ResponseWriter, r *http.Request) {
	r.Header.Set(PagePrefixHeader, "/room/"+r.PathValue("room")+"/"+r.PathValue("id")+"/files/")
	s.servePage(w, r)
}

// withoutCheckoutDir drops a leading folder name equal to the checkout's own ("work/"), which a link written from the
// card's directory carries, when the file is not there as written.
func withoutCheckoutDir(worktree, want string) string {
	first, rest, ok := strings.Cut(want, "/")
	if !ok || first != filepath.Base(filepath.FromSlash(worktree)) {
		return want
	}
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(worktree), filepath.FromSlash(want))); err == nil {
		return want
	}
	return rest
}

// servePage opens a file of a card inline as a sandboxed page. Relative assets resolve under the same prefix. The path
// is held to the card's own directory: no open_paths, and a link out of it answers like a missing file.
func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if task.Worktree == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", pageSandbox)
	w.Header().Set("Cache-Control", "no-store")
	want := r.PathValue("path")
	prefix := r.Header.Get(PagePrefixHeader)
	if rel, ok := strings.CutPrefix(want, pageBoardDir); ok {
		s.pageBoardFile(w, r, rel)
		return
	}
	if filepath.IsAbs(want) || strings.HasPrefix(want, "/") {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	want = withoutCheckoutDir(task.Worktree, want)
	if want == "" {
		want = "."
	}
	real, err := safepath.Contained(filepath.FromSlash(task.Worktree), want)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	fi, err := os.Stat(real)
	if err == nil && fi.IsDir() {
		if prefix != "" && want != "." && !strings.HasSuffix(want, "/") {
			q := ""
			if r.URL.RawQuery != "" {
				q = "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, prefix+url.PathEscape(strings.TrimSuffix(want, "/"))+"/"+q, http.StatusMovedPermanently)
			return
		}
		def, ok := pageDefault(real)
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("no index.html, or more than one .html file, in that folder"))
			return
		}
		if real, err = safepath.Contained(filepath.FromSlash(task.Worktree), def); err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		fi, err = os.Stat(real)
	}
	if err != nil || fi.IsDir() {
		writeErr(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	f, err := os.Open(real)
	if err != nil {
		writeErr(w, http.StatusForbidden, errors.New("cannot read that"))
		return
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(real))
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if prefix != "" && ext == ".md" && r.URL.Query().Get("raw") != "1" && fi.Size() <= 16<<20 {
		b, err := io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusForbidden, errors.New("cannot read that"))
			return
		}
		pageMarkdown(w, b, prefix, filepath.Base(real))
		return
	}
	if prefix != "" && (ext == ".html" || ext == ".htm" || ext == ".css") && fi.Size() <= 16<<20 {
		b, err := io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusForbidden, errors.New("cannot read that"))
			return
		}
		b = pageRewrite(b, prefix, pageDirOf(task.Worktree, real))
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
		return
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

// pageBoardFile serves one of the board's own files, so a mock can link the real css.
func (s *Server) pageBoardFile(w http.ResponseWriter, r *http.Request, rel string) {
	rel = path.Clean("/" + rel)[1:]
	if !pageRoot.MatchString(rel) {
		writeErr(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	b, err := fs.ReadFile(board(s.BoardDir), rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	if ct := mime.TypeByExtension(strings.ToLower(path.Ext(rel))); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Write(b)
}
