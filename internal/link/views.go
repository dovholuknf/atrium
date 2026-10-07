package link

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/webasset"
)

// BOARD VIEWS: one board tree per worker worktree, at `<name>.localhost`.
//
// The API is shared and only the static files differ, so two workers editing the
// board never collide and the operator opens the view of the change being tested.
// A view is derived from the cards the rooms announce and never stored, so it goes
// when its card or its directory does. See docs/rnd/board-views-design.md.

// viewBoardDir is where a worktree keeps its board.
const viewBoardDir = "internal/api/web"

// viewTTL is how long the list of views and a view's build id are reused. Short,
// so a save shows up as a new build within a health poll or two.
const viewTTL = 2 * time.Second

// boardView is one worktree's board.
type boardView struct {
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
	Dir   string `json:"dir"`
	Card  string `json:"card"`
	Room  string `json:"room"`
	// web is the board directory itself, `<Dir>/internal/api/web`.
	web    string
	fsys   fs.FS
	assets *webasset.Server
	id     *viewID
}

// viewID is a view's build id, rehashed at most once per viewTTL.
type viewID struct {
	mu   sync.Mutex
	at   time.Time
	id   string
	hash func(fs.FS) string
}

// views is the hub's list of board views.
type views struct {
	roots []string
	host  string
	now   func() time.Time
	// cards is every live card the rooms announced. hash is the board build id of
	// a tree, the root board's own function so the same files hash the same.
	cards func() []everyCard
	hash  func(fs.FS) string

	mu    sync.Mutex
	at    time.Time
	list  []*boardView
	byDir map[string]*boardView
}

type viewKey struct{}

// SetBoardViews turns the board views on, for worktrees under these roots, with
// hash as the build id of a tree (`api.BoardID`). No roots, the default, leaves
// them off and every `*.localhost` name a 404. It returns the roots it could not
// use, which do not exist or are not directories.
func (p *Proxy) SetBoardViews(roots []string, hash func(fs.FS) string) (bad []string) {
	var clean []string
	for _, r := range roots {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		real, err := realDir(r)
		if err != nil {
			bad = append(bad, r)
			continue
		}
		clean = append(clean, real)
	}
	if len(clean) == 0 {
		p.views = nil
		return bad
	}
	host, _ := os.Hostname()
	p.views = &views{roots: clean, host: host, now: time.Now, hash: hash,
		cards: func() []everyCard { return p.hub.every.allTrees() },
		byDir: map[string]*boardView{}}
	return bad
}

// viewLabel is the view a Host header names: the label before `.localhost`, or
// "" for any other host, the root board's `localhost` among them.
func viewLabel(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	label, ok := strings.CutSuffix(host, ".localhost")
	if !ok || label == "" {
		return ""
	}
	return label
}

// viewOf resolves a request's Host to its view. ok is false for a `*.localhost`
// name that is not a view, which is answered with a 404 rather than the root
// board, so a typo never looks like a view of nothing.
func (p *Proxy) viewOf(r *http.Request) (v *boardView, ok bool) {
	label := viewLabel(r.Host)
	if label == "" {
		return nil, true
	}
	if p.views == nil {
		return nil, false
	}
	for _, v := range p.views.all() {
		if v.Name == label || (v.Alias != "" && v.Alias == label) {
			return v, true
		}
	}
	return nil, false
}

// viewFrom is the view a request was resolved to, nil on the root board.
func viewFrom(ctx context.Context) *boardView {
	v, _ := ctx.Value(viewKey{}).(*boardView)
	return v
}

// buildFor is the board build id a request's page compares against: its view's,
// or the root board's. Every place that answers `build` asks this, so a view tab
// never sees the root's id and reloads forever.
func (p *Proxy) buildFor(ctx context.Context) string {
	if v := viewFrom(ctx); v != nil {
		return v.build()
	}
	return p.boardID
}

func (v *boardView) build() string {
	v.id.mu.Lock()
	defer v.id.mu.Unlock()
	if v.id.id == "" || time.Since(v.id.at) > viewTTL {
		v.id.id, v.id.at = v.id.hash(v.fsys), time.Now()
	}
	return v.id.id
}

// boardFor is the tree and file server a request is answered from.
func (p *Proxy) boardFor(ctx context.Context) (fs.FS, *webasset.Server) {
	if v := viewFrom(ctx); v != nil {
		return v.fsys, v.assets
	}
	return p.board, p.assets
}

// all is the views now, rebuilt from the cards the rooms announced at most once
// per viewTTL.
func (vs *views) all() []*boardView {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if vs.list != nil && vs.now().Sub(vs.at) < viewTTL {
		return vs.list
	}
	var cards []everyCard
	if vs.cards != nil {
		cards = vs.cards()
	}
	vs.list = vs.refresh(cards)
	if vs.list == nil {
		vs.list = []*boardView{}
	}
	vs.at = vs.now()
	return vs.list
}

// refresh rebuilds the list from these cards. Called with vs.mu held.
func (vs *views) refresh(cards []everyCard) []*boardView {
	if len(vs.roots) == 0 {
		return nil
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Worktree != cards[j].Worktree {
			return cards[i].Worktree < cards[j].Worktree
		}
		if cards[i].Room != cards[j].Room {
			return cards[i].Room < cards[j].Room
		}
		return cards[i].ID < cards[j].ID
	})
	var out []*boardView
	seen := map[string]*boardView{}
	taken := map[string]bool{}
	for _, c := range cards {
		dir, ok := vs.boardDir(c)
		if !ok {
			continue
		}
		key := strings.ToLower(dir)
		if v := seen[key]; v != nil {
			// Two cards in one worktree are one view. An alias the first lacked
			// is still worth answering to.
			if v.Alias == "" && c.Alias != "" {
				v.Alias = c.Alias
			}
			continue
		}
		name := viewName(filepath.Base(dir))
		if name == "" {
			continue
		}
		for n, base := 2, name; taken[name]; n++ {
			name = base + "-" + strconv.Itoa(n)
		}
		taken[name] = true
		v := vs.byDir[key]
		if v == nil {
			web := filepath.Join(dir, filepath.FromSlash(viewBoardDir))
			fsys := rootFS(web)
			v = &boardView{Dir: dir, web: web, fsys: fsys, assets: webasset.New(fsys), id: &viewID{hash: vs.hash}}
			vs.byDir[key] = v
		}
		// A copy, so a request holding the last list never sees a name change under it.
		cp := *v
		cp.Name, cp.Alias, cp.Card, cp.Room = name, viewName(c.Alias), c.ID, c.Room
		if cp.Alias == name {
			cp.Alias = ""
		}
		seen[key] = &cp
		out = append(out, &cp)
	}
	// An alias answers only when no view has it as its name.
	for _, v := range out {
		if v.Alias != "" && taken[v.Alias] {
			v.Alias = ""
		}
	}
	for key := range vs.byDir {
		if seen[key] == nil {
			delete(vs.byDir, key)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// boardDir is the card's worktree when it may be a view: on this machine, under a
// root, and holding a board. The real path, so a link out of a root is refused.
func (vs *views) boardDir(c everyCard) (string, bool) {
	wt := strings.TrimSpace(c.Worktree)
	if wt == "" || !filepath.IsAbs(filepath.FromSlash(wt)) {
		return "", false
	}
	if c.Host != "" && vs.host != "" && !sameHost(c.Host, vs.host) {
		return "", false
	}
	dir, err := realDir(wt)
	if err != nil {
		return "", false
	}
	under := false
	for _, root := range vs.roots {
		if within(root, dir) {
			under = true
			break
		}
	}
	if !under {
		return "", false
	}
	st, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(viewBoardDir), "index.html"))
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return dir, true
}

// realDir is a directory made absolute with its links resolved.
func realDir(p string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(real), nil
}

// within is whether dir is root or under it. Case-folded, which is right on the
// two machines that run a hub today and only too strict elsewhere.
func within(root, dir string) bool {
	rel, err := filepath.Rel(strings.ToLower(root), strings.ToLower(dir))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// sameHost compares two machine names by their first label, case-folded.
func sameHost(a, b string) bool {
	first := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.IndexByte(s, '.'); i >= 0 {
			s = s[:i]
		}
		return strings.ToLower(s)
	}
	return first(a) == first(b)
}

// viewName is a folder name or alias as a host label: lower case, letters,
// digits and `-`, nothing leading or trailing, at most 63.
func viewName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@")) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

// rootFS serves a directory through os.OpenInRoot, so a link inside the tree
// that points out of it is refused, which os.DirFS would follow. Nothing is held
// open between requests, so the worktree can still be deleted on Windows.
type rootFS string

func (d rootFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f, err := os.OpenInRoot(string(d), filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	return f, nil
}

// noView is the answer for a `*.localhost` name that is not a view.
func (p *Proxy) noView(w http.ResponseWriter, r *http.Request) {
	label := viewLabel(r.Host)
	var names []string
	if p.views != nil {
		for _, v := range p.views.all() {
			names = append(names, v.Name)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	body := `<!doctype html><meta charset="utf-8"><title>no such board view</title>` +
		`<body style="font-family:system-ui;margin:2em"><p>There is no board view called <code>` +
		html.EscapeString(label) + `</code>.`
	if p.views == nil {
		body += ` This hub serves no views: it was started without <code>--board-views</code>.</p>`
	} else if len(names) == 0 {
		body += ` No live card has a worktree with a board in it.</p>`
	} else {
		_, port, _ := net.SplitHostPort(r.Host)
		body += ` These are:</p><ul>`
		for _, n := range names {
			u := viewURL(n, port)
			body += `<li><a href="` + html.EscapeString(u) + `">` + html.EscapeString(n) + `</a></li>`
		}
		body += `</ul>`
	}
	_, _ = w.Write([]byte(body + `</body>`))
}

func viewURL(name, port string) string {
	u := "http://" + name + ".localhost"
	if port != "" {
		u += ":" + port
	}
	return u + "/"
}

// serveViews answers `GET /_hub/views`: every view, with the address to open it at.
func (p *Proxy) serveViews(w http.ResponseWriter, r *http.Request) {
	type row struct {
		*boardView
		URL   string `json:"url"`
		Build string `json:"build"`
	}
	_, port, _ := net.SplitHostPort(r.Host)
	out := []row{}
	if p.views != nil {
		for _, v := range p.views.all() {
			out = append(out, row{v, viewURL(v.Name, port), v.build()})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"on": p.views != nil, "views": out})
}
