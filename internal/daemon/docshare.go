package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	zroksdk "github.com/openziti/zrok/v2/sdk/golang/sdk"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/docshare"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// One document from a card, shared on its own zrok public share.
//
// A SNAPSHOT, not a window on the worktree: the share serves a folder atrium wrote under the room's data directory,
// and its handler is a fixed list. Nothing else of the card, and nothing of the board, is reachable through it.
// `docs/changes/f-share-doc-zrok-stage1.md` has the design.

const (
	docShareMaxDoc     = 2 << 20
	docShareMaxTotal   = 32 << 20
	docShareMaxImages  = 200
	docShareMaxLive    = 20
	docShareDefaultTTL = 7
	docShareMaxTTL     = 30
)

const docShareSpeedBump = "this looks like it holds a secret, so it was not shared. That check is a speed bump, " +
	"not a guarantee: it cannot see a secret in a picture or a password written as a sentence"

// docShareBinder puts a snapshot on the network. The zrok one is the default and a test replaces it.
type docShareBinder interface {
	bind(rec *store.DocShare, h http.Handler) (ln net.Listener, token, address string, err error)
	release(token string)
	reserve(namespace, name string) error
	unreserve(namespace, name string)
}

type docShareLive struct {
	srv   *http.Server
	token string
}

type docShares struct {
	mu   sync.Mutex
	live map[string]*docShareLive
	// make serialises making a share, so the cap and the one-per-document rule cannot be raced past.
	make sync.Mutex
	b    docShareBinder
}

func (s *docShares) put(id string, l *docShareLive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		s.live = map[string]*docShareLive{}
	}
	s.live[id] = l
}

func (s *docShares) take(id string) *docShareLive {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.live[id]
	delete(s.live, id)
	return l
}

func (s *docShares) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live[id] != nil
}

func (d *Daemon) docShareBinder() docShareBinder {
	if d.docs.b != nil {
		return d.docs.b
	}
	return zrokDocBinder{d}
}

func (d *Daemon) docShareRoot() string {
	return filepath.Join(filepath.Dir(d.opts.DBPath), "docshares")
}

func (d *Daemon) docShareDir(id string) string { return filepath.Join(d.docShareRoot(), id) }

func docShareErr(code int, format string, a ...any) error {
	return &api.DocShareError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

type docSnapshot struct {
	Path   string         `json:"path"`
	Images map[string]int `json:"images"`
	Types  []string       `json:"types"`
}

// MakeDocShare snapshots one document of a card and puts it on a public share.
func (d *Daemon) MakeDocShare(taskID string, req api.DocShareRequest) (any, error) {
	d.docs.make.Lock()
	defer d.docs.make.Unlock()

	task, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(task.Worktree) == "" {
		return nil, docShareErr(http.StatusBadRequest, "this card has no directory")
	}
	rel := strings.TrimSpace(req.Path)
	if rel == "" {
		return nil, docShareErr(http.StatusBadRequest, "say which file")
	}
	ext := strings.ToLower(filepath.Ext(rel))
	if ext != ".md" && ext != ".markdown" {
		return nil, docShareErr(http.StatusBadRequest, "only a markdown file can be shared")
	}

	if old, err := d.st.DocShareFor(taskID, rel); err != nil {
		return nil, err
	} else if old != nil && d.docShareLiveOrDue(old) {
		return d.docShareView(*old), nil
	}
	if n, err := d.st.LiveDocShareCount(); err != nil {
		return nil, err
	} else if n >= docShareMaxLive {
		return nil, docShareErr(http.StatusBadRequest,
			"this room already has %d documents shared. stop one first", docShareMaxLive)
	}

	ttl := req.TTLDays
	if ttl <= 0 {
		ttl = docShareDefaultTTL
	}
	if ttl > docShareMaxTTL {
		ttl = docShareMaxTTL
	}

	id := store.NewDocShareID()
	dir := d.docShareDir(id)
	if err := d.writeDocSnapshot(task.Worktree, rel, req.Images, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	now := time.Now().UTC()
	rec := &store.DocShare{
		ID: id, TaskID: taskID, Path: rel, Namespace: publicNamespace, Dir: dir, Wanted: true,
		CreatedAt: now.Format(store.TimeFormat), UpdatedAt: now.Format(store.TimeFormat),
		ExpiresAt: now.Add(time.Duration(ttl) * 24 * time.Hour).Format(store.TimeFormat),
	}
	name, err := newShareName()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	rec.Name = name
	if err := d.bindDocShare(rec); err != nil {
		d.docShareBinder().unreserve(rec.Namespace, rec.Name)
		_ = os.RemoveAll(dir)
		return nil, docShareErr(http.StatusBadRequest, "%s", err.Error())
	}
	if err := d.st.PutDocShare(*rec); err != nil {
		d.releaseDocShare(rec)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	log.Printf("[atrium] sharing %s from %s at %s", rel, task.DisplayTitle(), rec.Address)
	return d.docShareView(*rec), nil
}

func (d *Daemon) docShareLiveOrDue(rec *store.DocShare) bool {
	return d.docs.has(rec.ID) || !d.st.DocShareExpired(*rec)
}

// writeDocSnapshot resolves, checks and copies the document and its images into a new folder.
func (d *Daemon) writeDocSnapshot(worktree, rel string, images []string, dir string) error {
	root := filepath.FromSlash(worktree)
	real, err := safepath.Contained(root, rel)
	if err != nil {
		return docShareErr(http.StatusForbidden, "%s", safepath.ErrOutside.Error())
	}
	fi, err := os.Stat(real)
	if err != nil {
		return docShareErr(http.StatusNotFound, "no such file")
	}
	if fi.IsDir() {
		return docShareErr(http.StatusBadRequest, "that is a directory. share one file")
	}
	if fi.Size() > docShareMaxDoc {
		return docShareErr(http.StatusBadRequest, "that document is over %d MiB", docShareMaxDoc>>20)
	}
	if docshare.SecretFileName(realRel(root, real, rel)) {
		return docShareErr(http.StatusForbidden, "%s", docShareSpeedBump)
	}
	body, err := os.ReadFile(real)
	if err != nil {
		return docShareErr(http.StatusForbidden, "cannot read that")
	}
	if rule := docshare.SecretContent(body); rule != "" {
		return docShareErr(http.StatusForbidden, "%s (%s)", docShareSpeedBump, rule)
	}

	if err := os.MkdirAll(filepath.Join(dir, "f"), 0o700); err != nil {
		return err
	}
	total := int64(len(body))
	snap := docSnapshot{Path: rel, Images: map[string]int{}, Types: []string{}}
	seen := map[string]bool{}
	for _, im := range images {
		im = strings.TrimSpace(im)
		if im == "" || seen[im] {
			continue
		}
		seen[im] = true
		if len(snap.Images) >= docShareMaxImages {
			return docShareErr(http.StatusBadRequest, "more than %d images", docShareMaxImages)
		}
		ct, ok := api.ViewImageType(filepath.Ext(im))
		if !ok {
			return docShareErr(http.StatusBadRequest, "%s is not an image type that can be shared", im)
		}
		ireal, err := safepath.Contained(root, im)
		if err != nil {
			return docShareErr(http.StatusForbidden, "%s", safepath.ErrOutside.Error())
		}
		if rct, ok := api.ViewImageType(filepath.Ext(ireal)); !ok || rct != ct {
			return docShareErr(http.StatusBadRequest, "%s is not an image type that can be shared", im)
		}
		ifi, err := os.Stat(ireal)
		if err != nil || ifi.IsDir() {
			return docShareErr(http.StatusNotFound, "no such image: %s", im)
		}
		if docshare.SecretFileName(realRel(root, ireal, im)) {
			return docShareErr(http.StatusForbidden, "%s", docShareSpeedBump)
		}
		total += ifi.Size()
		if total > docShareMaxTotal {
			return docShareErr(http.StatusBadRequest, "the snapshot is over %d MiB", docShareMaxTotal>>20)
		}
		data, err := os.ReadFile(ireal)
		if err != nil {
			return docShareErr(http.StatusForbidden, "cannot read %s", im)
		}
		n := len(snap.Types)
		if err := os.WriteFile(filepath.Join(dir, "f", strconv.Itoa(n)), data, 0o600); err != nil {
			return err
		}
		snap.Images[im] = n
		snap.Types = append(snap.Types, ct)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), body, 0o600); err != nil {
		return err
	}
	mj, _ := json.Marshal(snap)
	return os.WriteFile(filepath.Join(dir, "map.json"), mj, 0o600)
}

func realRel(root, real, fallback string) string {
	r := root
	if e, err := filepath.EvalSymlinks(r); err == nil {
		r = e
	}
	if rel, err := filepath.Rel(r, real); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return fallback
}

// bindDocShare reserves the name, creates the share and starts answering it. It is the only place that does.
func (d *Daemon) bindDocShare(rec *store.DocShare) error {
	b := d.docShareBinder()
	if err := b.reserve(rec.Namespace, rec.Name); err != nil {
		return err
	}
	h, err := d.docShareHandler(rec.Dir)
	if err != nil {
		return err
	}
	ln, token, address, err := b.bind(rec, h)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h}
	d.docs.put(rec.ID, &docShareLive{srv: srv, token: token})
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[atrium] the document share %s stopped: %v", rec.ID, err)
		}
	}()
	rec.Token, rec.Address = token, address
	return nil
}

// unbindDocShare takes a share down without giving anything up, which is what a shutdown does.
func (d *Daemon) unbindDocShare(id string) {
	l := d.docs.take(id)
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = l.srv.Shutdown(ctx)
	if l.token != "" {
		d.docShareBinder().release(l.token)
	}
}

func (d *Daemon) releaseDocShare(rec *store.DocShare) {
	d.unbindDocShare(rec.ID)
	d.docShareBinder().unreserve(rec.Namespace, rec.Name)
}

// StopDocShare ends one share for good: the listener, the share, the name, the folder and the row.
func (d *Daemon) StopDocShare(id string) error {
	rec, err := d.st.DocShareByID(id)
	if err != nil {
		return err
	}
	if rec == nil {
		return docShareErr(http.StatusNotFound, "that document is not shared")
	}
	d.endDocShare(rec)
	return nil
}

// StopDocShareFor ends the share of one document of a card.
func (d *Daemon) StopDocShareFor(taskID, path string) error {
	rec, err := d.st.DocShareFor(taskID, strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if rec == nil {
		return docShareErr(http.StatusNotFound, "that document is not shared")
	}
	d.endDocShare(rec)
	return nil
}

func (d *Daemon) endDocShare(rec *store.DocShare) {
	d.releaseDocShare(rec)
	// Only a folder this daemon made, found by the id and not by what the row says.
	if dir := d.docShareDir(rec.ID); filepath.Clean(rec.Dir) == filepath.Clean(dir) {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[atrium] could not remove the folder of document share %s: %v", rec.ID, err)
		}
	}
	if err := d.st.DeleteDocShare(rec.ID); err != nil {
		log.Printf("[atrium] could not forget document share %s: %v", rec.ID, err)
	}
	log.Printf("[atrium] stopped sharing %s", rec.Path)
}

// DocShareOf is the live share of one document, or nil.
func (d *Daemon) DocShareOf(taskID, path string) any {
	rec, err := d.st.DocShareFor(taskID, strings.TrimSpace(path))
	if err != nil || rec == nil || !d.docShareLiveOrDue(rec) {
		return nil
	}
	return d.docShareView(*rec)
}

type docShareInfo struct {
	store.DocShare
	Title    string `json:"title"`
	CardGone bool   `json:"card_gone"`
	Live     bool   `json:"live"`
}

func (d *Daemon) docShareView(rec store.DocShare) docShareInfo {
	info := docShareInfo{DocShare: rec, Live: d.docs.has(rec.ID)}
	if t, err := d.st.Get(rec.TaskID); err == nil {
		info.Title = t.DisplayTitle()
	} else {
		info.CardGone = !d.st.TaskExists(rec.TaskID)
	}
	return info
}

// DocShares is what the board lists beside the lent sessions.
func (d *Daemon) DocShares() any {
	recs, err := d.st.DocShares()
	if err != nil {
		log.Printf("[atrium] could not read the document shares: %v", err)
		return []docShareInfo{}
	}
	out := make([]docShareInfo, 0, len(recs))
	for _, r := range recs {
		out = append(out, d.docShareView(r))
	}
	return out
}

// RestoreDocShares puts back every share that is wanted and has not expired, after dropping the ones that have.
func (d *Daemon) RestoreDocShares() {
	recs, err := d.st.DocShares()
	if err != nil {
		log.Printf("[atrium] could not read the document shares: %v", err)
		return
	}
	if len(recs) == 0 {
		return
	}
	go func() {
		for i := range recs {
			rec := recs[i]
			if d.st.DocShareExpired(rec) || !rec.Wanted {
				d.endDocShare(&rec)
				continue
			}
			if d.docs.has(rec.ID) {
				continue
			}
			if err := d.bindDocShare(&rec); err != nil {
				log.Printf("[atrium] could not put %s back on its share: %v", rec.Path, err)
				continue
			}
			if err := d.st.PutDocShare(rec); err != nil {
				log.Printf("[atrium] could not record the document share %s: %v", rec.ID, err)
			}
		}
	}()
}

// sweepDocShares stops what has expired. It runs on the reaper tick.
func (d *Daemon) sweepDocShares() {
	recs, err := d.st.DocShares()
	if err != nil {
		return
	}
	for i := range recs {
		if d.st.DocShareExpired(recs[i]) {
			d.endDocShare(&recs[i])
		}
	}
}

// stopAllDocShares unbinds every share for shutdown. The rows stay wanted, so the next start puts them back.
func (d *Daemon) stopAllDocShares() {
	d.docs.mu.Lock()
	ids := make([]string, 0, len(d.docs.live))
	for id := range d.docs.live {
		ids = append(ids, id)
	}
	d.docs.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		d.unbindDocShare(id)
	}
}

// docShareHandler is the whole surface a reader of a shared document can reach. Everything not named is 404.
func (d *Daemon) docShareHandler(dir string) (http.Handler, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "map.json"))
	if err != nil {
		return nil, fmt.Errorf("the snapshot is missing: %w", err)
	}
	var snap docSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("the snapshot is unreadable: %w", err)
	}
	board := api.EmbeddedBoard()
	page, err := fs.ReadFile(board, "share.html")
	if err != nil {
		return nil, err
	}
	meta, _ := json.Marshal(map[string]any{"path": snap.Path, "images": snap.Images})
	page = []byte(strings.Replace(string(page), "__SHARE_MAP__", html.EscapeString(string(meta)), 1))

	type asset struct{ file, ct string }
	static := map[string]asset{
		"/read.js":          {"read.js", "text/javascript; charset=utf-8"},
		"/vendor/marked.js": {"vendor/marked.js", "text/javascript; charset=utf-8"},
		"/vendor/purify.js": {"vendor/purify.js", "text/javascript; charset=utf-8"},
		"/css/tokens.css":   {"css/tokens.css", "text/css; charset=utf-8"},
		"/css/themes.css":   {"css/themes.css", "text/css; charset=utf-8"},
	}
	const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
		"connect-src 'self'; base-uri 'none'; form-action 'none'"

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Content-Security-Policy", csp)
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Path
		serve := func(ct string, b []byte) {
			hd.Set("Content-Type", ct)
			if r.Method == http.MethodGet {
				_, _ = w.Write(b)
			}
		}
		switch {
		case p == "/":
			serve("text/html; charset=utf-8", page)
		case p == "/doc.md":
			b, err := os.ReadFile(filepath.Join(dir, "doc.md"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			serve("text/plain; charset=utf-8", b)
		case strings.HasPrefix(p, "/f/"):
			ns := strings.TrimPrefix(p, "/f/")
			n, err := strconv.Atoi(ns)
			if err != nil || n < 0 || n >= len(snap.Types) || strconv.Itoa(n) != ns {
				http.NotFound(w, r)
				return
			}
			b, err := os.ReadFile(filepath.Join(dir, "f", ns))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			serve(snap.Types[n], b)
		default:
			s, ok := static[p]
			if !ok {
				http.NotFound(w, r)
				return
			}
			b, err := fs.ReadFile(board, s.file)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			serve(s.ct, b)
		}
	})
	return h, nil
}

var errDocNoZrok = errors.New("this machine has no zrok environment yet. enable one under settings, expose the board")

type zrokDocBinder struct{ d *Daemon }

func (z zrokDocBinder) reserve(namespace, name string) error {
	_, err := z.d.ReserveZrokName(namespace, name)
	return err
}

func (z zrokDocBinder) unreserve(namespace, name string) {
	if err := z.d.ReleaseZrokName(namespace, name); err != nil {
		log.Printf("[atrium] could not release the name %s: %v", name, err)
	}
}

func (z zrokDocBinder) release(token string) {
	if root, err := z.d.zrokRoot(); err == nil {
		if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: token}); err != nil {
			log.Printf("[atrium] could not release the share %s: %v", token, err)
		}
	}
}

func (z zrokDocBinder) bind(rec *store.DocShare, h http.Handler) (net.Listener, string, string, error) {
	root, err := z.d.zrokRoot()
	if err != nil {
		return nil, "", "", fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return nil, "", "", errDocNoZrok
	}
	shr, err := zroksdk.CreateShare(root, &zroksdk.ShareRequest{
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.PublicShareMode,
		Target:      z.d.defaultBackend(),
		NameSelections: []zroksdk.NameSelection{
			{NamespaceToken: rec.Namespace, Name: rec.Name},
		},
	})
	if err != nil {
		return nil, "", "", zrokSays("could not share "+rec.Path, err)
	}
	if len(shr.FrontendEndpoints) == 0 {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, "", "", fmt.Errorf("zrok made the share and gave it no public address")
	}
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, "", "", zrokSays("the share was created but nothing could answer it", err)
	}
	return ln, shr.Token, strings.TrimRight(shr.FrontendEndpoints[0], "/") + "/", nil
}
