package link

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// viewHash stands in for api.BoardID: the board's own bytes, so two trees with
// different files have different ids.
func viewHash(fsys fs.FS) string {
	raw, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return ""
	}
	js, _ := fs.ReadFile(fsys, "js/x.js")
	return "id:" + string(raw) + "|" + string(js)
}

// worktree makes a directory holding a board whose page says `page`.
func worktree(t *testing.T, dir, page string) string {
	t.Helper()
	web := filepath.Join(dir, "internal", "api", "web")
	if err := os.MkdirAll(filepath.Join(web, "js"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "js", "x.js"), []byte("// "+page), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func viewCard(id, status, alias, worktree, host string) CardState {
	raw, _ := json.Marshal(map[string]any{
		"id": id, "status": status, "wire_name": "atrium-" + id, "alias": alias,
		"worktree": filepath.ToSlash(worktree), "hostname": host,
	})
	return CardState{ID: id, Status: status, Payload: raw}
}

// An offered card has a worktree and no name yet, and is still a view.
func TestBoardViewsUnnamedCard(t *testing.T) {
	root := t.TempDir()
	dir := worktree(t, filepath.Join(root, "offered"), "offered")
	raw, _ := json.Marshal(map[string]any{"id": "c1", "status": "backlog", "worktree": filepath.ToSlash(dir)})
	hub := NewHub(Timings{})
	hub.IndexEverywhere("r1", []CardState{{ID: "c1", Status: "backlog", Payload: raw}})
	p := NewProxy(hub, fstest.MapFS{"index.html": {Data: []byte("root")}}, "r", nil)
	p.SetBoardViews([]string{root}, viewHash)
	req := httptest.NewRequest(http.MethodGet, "http://offered.localhost:7778/", nil)
	req.Host = "offered.localhost:7778"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "offered" {
		t.Errorf("an unnamed card's view: %d %q", rec.Code, rec.Body.String())
	}
}

type viewRig struct {
	p    *Proxy
	hub  *Hub
	root string
	a, b string
}

func newViewRig(t *testing.T) viewRig {
	t.Helper()
	root := t.TempDir()
	outside := t.TempDir()
	me, _ := os.Hostname()
	a := worktree(t, filepath.Join(root, "atrium", "u-alpha"), "alpha")
	b := worktree(t, filepath.Join(root, "atrium", "u-beta"), "beta")
	far := worktree(t, filepath.Join(outside, "u-outside"), "outside")
	other := worktree(t, filepath.Join(root, "atrium", "u-mac"), "mac")
	ended := worktree(t, filepath.Join(root, "atrium", "u-ended"), "ended")
	plain := filepath.Join(root, "atrium", "no-board")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{})
	hub.IndexEverywhere("r1", []CardState{
		viewCard("c1", "running", "alfie", a, me),
		viewCard("c2", "needs-input", "", b, me),
		// A second card in a worktree is the same view.
		viewCard("c3", "running", "", a, me),
		viewCard("c4", "running", "", far, me),
		viewCard("c5", "running", "", plain, me),
		viewCard("c6", "done", "", ended, me),
	})
	hub.IndexEverywhere("m1mini", []CardState{viewCard("c7", "running", "", other, "m1mini-not-"+me)})
	board := fstest.MapFS{"index.html": {Data: []byte("root")}, "js/x.js": {Data: []byte("// root")}}
	p := NewProxy(hub, board, viewHash(board), nil)
	if bad := p.SetBoardViews([]string{root, filepath.Join(root, "missing")}, viewHash); len(bad) != 1 {
		t.Fatalf("a missing root should be named, got %v", bad)
	}
	return viewRig{p: p, hub: hub, root: root, a: a, b: b}
}

func (rig viewRig) get(t *testing.T, host, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	rig.p.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

// Each view serves its own worktree's files, side by side with the root board,
// and the host is the only thing that picks.
func TestBoardViewsRouteByHost(t *testing.T) {
	rig := newViewRig(t)
	cases := []struct {
		host, path string
		code       int
		body       string
	}{
		{"127.0.0.1:7778", "/", 200, "root"},
		{"localhost:7778", "/js/x.js", 200, "// root"},
		{"u-alpha.localhost:7778", "/", 200, "alpha"},
		{"u-alpha.localhost:7778", "/js/x.js", 200, "// alpha"},
		{"U-Beta.localhost:7778", "/", 200, "beta"},
		{"u-beta.localhost:7778", "/js/x.js", 200, "// beta"},
		// The alias answers too.
		{"alfie.localhost:7778", "/", 200, "alpha"},
		// A card address is the view's own page, not the root's.
		{"u-beta.localhost:7778", "/alias/someone", 200, "beta"},
	}
	for _, c := range cases {
		code, body := rig.get(t, c.host, c.path)
		if code != c.code || body != c.body {
			t.Errorf("%s%s: %d %q, want %d %q", c.host, c.path, code, body, c.code, c.body)
		}
	}
}

// Only a live card's worktree, on this machine, under a root, holding a board.
// Anything else is a 404 and never the root board.
func TestBoardViewsContainment(t *testing.T) {
	rig := newViewRig(t)
	for _, host := range []string{
		"u-outside.localhost", // not under a root
		"no-board.localhost",  // no internal/api/web/index.html
		"u-mac.localhost",     // another machine's card
		"u-ended.localhost",   // the card is done
		"nothing.localhost",   // no such view
	} {
		code, body := rig.get(t, host+":7778", "/")
		if code != http.StatusNotFound || strings.Contains(body, "root") {
			t.Errorf("%s: %d %q, want a 404 that is not the root board", host, code, body)
		}
	}
	// A path never leaves the view's board directory.
	if err := os.WriteFile(filepath.Join(rig.a, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/../../../secret.txt", "/..%2f..%2f..%2fsecret.txt", "/%2e%2e/%2e%2e/%2e%2e/secret.txt"} {
		if _, body := rig.get(t, "u-alpha.localhost:7778", p); strings.Contains(body, "secret") {
			t.Errorf("%s reached a file outside the board", p)
		}
	}
	// A link inside the tree that points out of it is refused.
	link := filepath.Join(rig.a, "internal", "api", "web", "out.txt")
	if err := os.Symlink(filepath.Join(rig.a, "secret.txt"), link); err != nil {
		t.Logf("no symlink on this machine (%v), skipping that part", err)
		return
	}
	if code, body := rig.get(t, "u-alpha.localhost:7778", "/out.txt"); strings.Contains(body, "secret") {
		t.Errorf("a link out of the tree was followed: %d %q", code, body)
	}
}

// A view's tab compares against its own tree's build id, everywhere `build` is
// answered, and the id follows the view's files without touching the root's.
func TestBoardViewsBuildIDPerView(t *testing.T) {
	rig := newViewRig(t)
	build := func(host string) string {
		_, body := rig.get(t, host, "/_hub/health")
		var h struct{ Build string }
		if err := json.Unmarshal([]byte(body), &h); err != nil {
			t.Fatalf("%s: %v in %q", host, err, body)
		}
		return h.Build
	}
	root, alpha, beta := build("127.0.0.1:7778"), build("u-alpha.localhost:7778"), build("u-beta.localhost:7778")
	if root != "id:root|// root" || alpha != "id:alpha|// alpha" || beta != "id:beta|// beta" {
		t.Fatalf("builds: root %q alpha %q beta %q", root, alpha, beta)
	}
	// The merged health of the ALL view answers the same way.
	_, body := rig.get(t, "u-beta.localhost:7778", "/v1/health")
	if !strings.Contains(body, `"build":"id:beta|// beta"`) {
		t.Errorf("/v1/health in the beta view: %s", body)
	}
	// A save in alpha is a new build for alpha's tab once the id is rehashed, and
	// nothing for the root's or beta's.
	if err := os.WriteFile(filepath.Join(rig.a, "internal", "api", "web", "js", "x.js"), []byte("// alpha 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, v := range rig.p.views.all() {
		v.id.mu.Lock()
		v.id.at = time.Time{}
		v.id.mu.Unlock()
	}
	if got := build("u-alpha.localhost:7778"); got != "id:alpha|// alpha 2" {
		t.Errorf("alpha after a save: %q", got)
	}
	if build("127.0.0.1:7778") != root || build("u-beta.localhost:7778") != beta {
		t.Error("a save in one view moved another's build")
	}
}

// The list the board reads, and a view going when its card does.
func TestBoardViewsListAndGo(t *testing.T) {
	rig := newViewRig(t)
	_, body := rig.get(t, "127.0.0.1:7778", "/_hub/views")
	var got struct {
		On    bool
		Views []struct{ Name, Alias, URL, Dir, Card, Room string }
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if !got.On || len(got.Views) != 2 {
		t.Fatalf("views: %s", body)
	}
	if v := got.Views[0]; v.Name != "u-alpha" || v.Alias != "alfie" || v.URL != "http://u-alpha.localhost:7778/" || v.Room != "r1" {
		t.Errorf("first view: %+v", v)
	}
	if v := got.Views[1]; v.Name != "u-beta" || v.Card != "c2" {
		t.Errorf("second view: %+v", v)
	}
	// The card ends, and its view goes at the next look.
	me, _ := os.Hostname()
	rig.hub.IndexEverywhere("r1", []CardState{viewCard("c1", "running", "alfie", rig.a, me), viewCard("c2", "done", "", rig.b, me)})
	rig.p.views.at = time.Time{}
	if code, _ := rig.get(t, "u-beta.localhost:7778", "/"); code != http.StatusNotFound {
		t.Errorf("a done card's view still answers: %d", code)
	}
	// The worktree goes, and so does its view.
	if err := os.RemoveAll(rig.a); err != nil {
		t.Fatal(err)
	}
	rig.p.views.at = time.Time{}
	if code, _ := rig.get(t, "u-alpha.localhost:7778", "/"); code != http.StatusNotFound {
		t.Errorf("a removed worktree's view still answers: %d", code)
	}
}

// Off unless the hub names a root, and a view name is always a host label.
func TestBoardViewsOffAndNames(t *testing.T) {
	board := fstest.MapFS{"index.html": {Data: []byte("root")}}
	p := NewProxy(NewHub(Timings{}), board, "r", nil)
	req := httptest.NewRequest(http.MethodGet, "http://x.localhost:7778/", nil)
	req.Host = "x.localhost:7778"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "--board-views") {
		t.Errorf("views off: %d %s", rec.Code, rec.Body.String())
	}
	for in, want := range map[string]string{
		"f-board-views": "f-board-views", "U_New.Thing": "u-new-thing", "@sa89": "sa89",
		"--x--": "x", "a  b": "a-b", strings.Repeat("a", 70): strings.Repeat("a", 63),
	} {
		if got := viewName(in); got != want {
			t.Errorf("viewName(%q) = %q, want %q", in, got, want)
		}
	}
	for host, want := range map[string]string{
		"127.0.0.1:7778": "", "localhost:7778": "", "a.localhost:7778": "a", "A.LOCALHOST.": "a", "evillocalhost": "",
	} {
		if got := viewLabel(host); got != want {
			t.Errorf("viewLabel(%q) = %q, want %q", host, got, want)
		}
	}
}

// Two worktrees with one folder name are two views, the second numbered.
func TestBoardViewsSameFolderName(t *testing.T) {
	root := t.TempDir()
	me, _ := os.Hostname()
	x := worktree(t, filepath.Join(root, "one", "same"), "one")
	y := worktree(t, filepath.Join(root, "two", "same"), "two")
	hub := NewHub(Timings{})
	hub.IndexEverywhere("r1", []CardState{viewCard("c1", "running", "", y, me), viewCard("c2", "running", "", x, me)})
	p := NewProxy(hub, fstest.MapFS{"index.html": {Data: []byte("root")}}, "r", nil)
	p.SetBoardViews([]string{root}, viewHash)
	var names []string
	for _, v := range p.views.all() {
		names = append(names, v.Name+"="+filepath.Base(filepath.Dir(v.Dir)))
	}
	if strings.Join(names, " ") != "same=one same-2=two" {
		t.Errorf("names: %v", names)
	}
}
