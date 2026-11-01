package publish

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
	DocShareMaxDoc     = 2 << 20
	DocShareMaxTotal   = 32 << 20
	DocShareMaxImages  = 200
	DocShareMaxLive    = 20
	DocShareDefaultTTL = 7
	DocShareMaxTTL     = 30
)

const DocShareSpeedBump = "this looks like it holds a secret, so it was not shared. That check is a speed bump, " +
	"not a guarantee: it cannot see a secret in a picture or a password written as a sentence"

// docShareBinder puts a snapshot on the network. The zrok one is the default and a test replaces it.
type DocShareBinder interface {
	Bind(rec *store.DocShare, h http.Handler) (ln net.Listener, token, address string, err error)
	Release(token string)
	Reserve(namespace, name string) error
	Unreserve(namespace, name string)
}

type DocShareLive struct {
	Srv   *http.Server
	Token string
}

type DocShares struct {
	Mu   sync.Mutex
	Live map[string]*DocShareLive
	// make serialises making a share, so the cap and the one-per-document rule cannot be raced past.
	Make sync.Mutex
	B    DocShareBinder
}

func (s *DocShares) Put(id string, l *DocShareLive) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.Live == nil {
		s.Live = map[string]*DocShareLive{}
	}
	s.Live[id] = l
}

func (s *DocShares) Take(id string) *DocShareLive {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	l := s.Live[id]
	delete(s.Live, id)
	return l
}

func (s *DocShares) Has(id string) bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Live[id] != nil
}

func (d *Service) DocShareBinder() DocShareBinder {
	if d.Docs.B != nil {
		return d.Docs.B
	}
	return ZrokDocBinder{d}
}

func (d *Service) DocShareRoot() string {
	return filepath.Join(filepath.Dir(d.opts().DBPath), "docshares")
}

func (d *Service) DocShareDir(id string) string { return filepath.Join(d.DocShareRoot(), id) }

func DocShareErr(code int, format string, a ...any) error {
	return &api.DocShareError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

type DocSnapshot struct {
	Path   string         `json:"path"`
	Images map[string]int `json:"images"`
	Types  []string       `json:"types"`
}

// MakeDocShare snapshots one document of a card and puts it on a public share.
func (d *Service) MakeDocShare(taskID string, req api.DocShareRequest) (any, error) {
	d.Docs.Make.Lock()
	defer d.Docs.Make.Unlock()

	task, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(task.Worktree) == "" {
		return nil, DocShareErr(http.StatusBadRequest, "this card has no directory")
	}
	rel := strings.TrimSpace(req.Path)
	if rel == "" {
		return nil, DocShareErr(http.StatusBadRequest, "say which file")
	}
	ext := strings.ToLower(filepath.Ext(rel))
	if ext != ".md" && ext != ".markdown" {
		return nil, DocShareErr(http.StatusBadRequest, "only a markdown file can be shared")
	}

	if old, err := d.st.DocShareFor(taskID, rel); err != nil {
		return nil, err
	} else if old != nil && d.DocShareLiveOrDue(old) {
		return d.DocShareView(*old), nil
	}
	if n, err := d.st.LiveDocShareCount(); err != nil {
		return nil, err
	} else if n >= DocShareMaxLive {
		return nil, DocShareErr(http.StatusBadRequest,
			"this room already has %d documents shared. stop one first", DocShareMaxLive)
	}

	ttl := req.TTLDays
	if ttl <= 0 {
		ttl = DocShareDefaultTTL
	}
	if ttl > DocShareMaxTTL {
		ttl = DocShareMaxTTL
	}

	id := store.NewDocShareID()
	dir := d.DocShareDir(id)
	if err := d.WriteDocSnapshot(task.Worktree, rel, req.Images, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	now := time.Now().UTC()
	rec := &store.DocShare{
		ID: id, TaskID: taskID, Path: rel, Namespace: PublicNamespace, Dir: dir, Wanted: true,
		CreatedAt: now.Format(store.TimeFormat), UpdatedAt: now.Format(store.TimeFormat),
		ExpiresAt: now.Add(time.Duration(ttl) * 24 * time.Hour).Format(store.TimeFormat),
	}
	name, err := NewShareName()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	rec.Name = name
	if err := d.BindDocShare(rec); err != nil {
		d.DocShareBinder().Unreserve(rec.Namespace, rec.Name)
		_ = os.RemoveAll(dir)
		return nil, DocShareErr(http.StatusBadRequest, "%s", err.Error())
	}
	if err := d.st.PutDocShare(*rec); err != nil {
		d.ReleaseDocShare(rec)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	log.Printf("[atrium] sharing %s from %s at %s", rel, task.DisplayTitle(), rec.Address)
	return d.DocShareView(*rec), nil
}

func (d *Service) DocShareLiveOrDue(rec *store.DocShare) bool {
	return d.Docs.Has(rec.ID) || !d.st.DocShareExpired(*rec)
}

// writeDocSnapshot resolves, checks and copies the document and its images into a new folder.
func (d *Service) WriteDocSnapshot(worktree, rel string, images []string, dir string) error {
	root := filepath.FromSlash(worktree)
	real, err := safepath.Contained(root, rel)
	if err != nil {
		return DocShareErr(http.StatusForbidden, "%s", safepath.ErrOutside.Error())
	}
	fi, err := os.Stat(real)
	if err != nil {
		return DocShareErr(http.StatusNotFound, "no such file")
	}
	if fi.IsDir() {
		return DocShareErr(http.StatusBadRequest, "that is a directory. share one file")
	}
	if fi.Size() > DocShareMaxDoc {
		return DocShareErr(http.StatusBadRequest, "that document is over %d MiB", DocShareMaxDoc>>20)
	}
	if docshare.SecretFileName(RealRel(root, real, rel)) {
		return DocShareErr(http.StatusForbidden, "%s", DocShareSpeedBump)
	}
	body, err := os.ReadFile(real)
	if err != nil {
		return DocShareErr(http.StatusForbidden, "cannot read that")
	}
	if rule := docshare.SecretContent(body); rule != "" {
		return DocShareErr(http.StatusForbidden, "%s (%s)", DocShareSpeedBump, rule)
	}

	if err := os.MkdirAll(filepath.Join(dir, "f"), 0o700); err != nil {
		return err
	}
	total := int64(len(body))
	snap := DocSnapshot{Path: rel, Images: map[string]int{}, Types: []string{}}
	seen := map[string]bool{}
	for _, im := range images {
		im = strings.TrimSpace(im)
		if im == "" || seen[im] {
			continue
		}
		seen[im] = true
		if len(snap.Images) >= DocShareMaxImages {
			return DocShareErr(http.StatusBadRequest, "more than %d images", DocShareMaxImages)
		}
		ct, ok := api.ViewImageType(filepath.Ext(im))
		if !ok {
			return DocShareErr(http.StatusBadRequest, "%s is not an image type that can be shared", im)
		}
		ireal, err := safepath.Contained(root, im)
		if err != nil {
			return DocShareErr(http.StatusForbidden, "%s", safepath.ErrOutside.Error())
		}
		if rct, ok := api.ViewImageType(filepath.Ext(ireal)); !ok || rct != ct {
			return DocShareErr(http.StatusBadRequest, "%s is not an image type that can be shared", im)
		}
		ifi, err := os.Stat(ireal)
		if err != nil || ifi.IsDir() {
			return DocShareErr(http.StatusNotFound, "no such image: %s", im)
		}
		if docshare.SecretFileName(RealRel(root, ireal, im)) {
			return DocShareErr(http.StatusForbidden, "%s", DocShareSpeedBump)
		}
		total += ifi.Size()
		if total > DocShareMaxTotal {
			return DocShareErr(http.StatusBadRequest, "the snapshot is over %d MiB", DocShareMaxTotal>>20)
		}
		data, err := os.ReadFile(ireal)
		if err != nil {
			return DocShareErr(http.StatusForbidden, "cannot read %s", im)
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

func RealRel(root, real, fallback string) string {
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
func (d *Service) BindDocShare(rec *store.DocShare) error {
	b := d.DocShareBinder()
	if err := b.Reserve(rec.Namespace, rec.Name); err != nil {
		return err
	}
	h, err := d.DocShareHandler(rec.Dir)
	if err != nil {
		return err
	}
	ln, token, address, err := b.Bind(rec, h)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h}
	d.Docs.Put(rec.ID, &DocShareLive{Srv: srv, Token: token})
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[atrium] the document share %s stopped: %v", rec.ID, err)
		}
	}()
	rec.Token, rec.Address = token, address
	return nil
}

// unbindDocShare takes a share down without giving anything up, which is what a shutdown does.
func (d *Service) UnbindDocShare(id string) {
	l := d.Docs.Take(id)
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = l.Srv.Shutdown(ctx)
	if l.Token != "" {
		d.DocShareBinder().Release(l.Token)
	}
}

func (d *Service) ReleaseDocShare(rec *store.DocShare) {
	d.UnbindDocShare(rec.ID)
	d.DocShareBinder().Unreserve(rec.Namespace, rec.Name)
}

// StopDocShare ends one share for good: the listener, the share, the name, the folder and the row.
func (d *Service) StopDocShare(id string) error {
	rec, err := d.st.DocShareByID(id)
	if err != nil {
		return err
	}
	if rec == nil {
		return DocShareErr(http.StatusNotFound, "that document is not shared")
	}
	d.EndDocShare(rec)
	return nil
}

// StopDocShareFor ends the share of one document of a card.
func (d *Service) StopDocShareFor(taskID, path string) error {
	rec, err := d.st.DocShareFor(taskID, strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if rec == nil {
		return DocShareErr(http.StatusNotFound, "that document is not shared")
	}
	d.EndDocShare(rec)
	return nil
}

func (d *Service) EndDocShare(rec *store.DocShare) {
	d.ReleaseDocShare(rec)
	// Only a folder this daemon made, found by the id and not by what the row says.
	if dir := d.DocShareDir(rec.ID); filepath.Clean(rec.Dir) == filepath.Clean(dir) {
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
func (d *Service) DocShareOf(taskID, path string) any {
	rec, err := d.st.DocShareFor(taskID, strings.TrimSpace(path))
	if err != nil || rec == nil || !d.DocShareLiveOrDue(rec) {
		return nil
	}
	return d.DocShareView(*rec)
}

type DocShareInfo struct {
	store.DocShare
	Title    string `json:"title"`
	CardGone bool   `json:"card_gone"`
	Live     bool   `json:"live"`
}

func (d *Service) DocShareView(rec store.DocShare) DocShareInfo {
	info := DocShareInfo{DocShare: rec, Live: d.Docs.Has(rec.ID)}
	if t, err := d.st.Get(rec.TaskID); err == nil {
		info.Title = t.DisplayTitle()
	} else {
		info.CardGone = !d.st.TaskExists(rec.TaskID)
	}
	return info
}

// DocShares is what the board lists beside the lent sessions.
func (d *Service) DocShares() any {
	recs, err := d.st.DocShares()
	if err != nil {
		log.Printf("[atrium] could not read the document shares: %v", err)
		return []DocShareInfo{}
	}
	out := make([]DocShareInfo, 0, len(recs))
	for _, r := range recs {
		out = append(out, d.DocShareView(r))
	}
	return out
}

// RestoreDocShares puts back every share that is wanted and has not expired, after dropping the ones that have.
func (d *Service) RestoreDocShares() {
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
				d.EndDocShare(&rec)
				continue
			}
			if d.Docs.Has(rec.ID) {
				continue
			}
			if err := d.BindDocShare(&rec); err != nil {
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
func (d *Service) SweepDocShares() {
	recs, err := d.st.DocShares()
	if err != nil {
		return
	}
	for i := range recs {
		if d.st.DocShareExpired(recs[i]) {
			d.EndDocShare(&recs[i])
		}
	}
}

// stopAllDocShares unbinds every share for shutdown. The rows stay wanted, so the next start puts them back.
func (d *Service) StopAllDocShares() {
	d.Docs.Mu.Lock()
	ids := make([]string, 0, len(d.Docs.Live))
	for id := range d.Docs.Live {
		ids = append(ids, id)
	}
	d.Docs.Mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		d.UnbindDocShare(id)
	}
}

// docShareHandler is the whole surface a reader of a shared document can reach. Everything not named is 404.
func (d *Service) DocShareHandler(dir string) (http.Handler, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "map.json"))
	if err != nil {
		return nil, fmt.Errorf("the snapshot is missing: %w", err)
	}
	var snap DocSnapshot
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

	type asset struct{ File, Ct string }
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
			b, err := fs.ReadFile(board, s.File)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			serve(s.Ct, b)
		}
	})
	return h, nil
}

var ErrDocNoZrok = errors.New("this machine has no zrok environment yet. enable one under settings, expose the board")

type ZrokDocBinder struct{ D *Service }

func (z ZrokDocBinder) Reserve(namespace, name string) error {
	_, err := z.D.ReserveZrokName(namespace, name)
	return err
}

func (z ZrokDocBinder) Unreserve(namespace, name string) {
	if err := z.D.ReleaseZrokName(namespace, name); err != nil {
		log.Printf("[atrium] could not release the name %s: %v", name, err)
	}
}

func (z ZrokDocBinder) Release(token string) {
	if root, err := z.D.ZrokRoot(); err == nil {
		if err := zroksdk.DeleteShare(root, &zroksdk.Share{Token: token}); err != nil {
			log.Printf("[atrium] could not release the share %s: %v", token, err)
		}
	}
}

func (z ZrokDocBinder) Bind(rec *store.DocShare, h http.Handler) (net.Listener, string, string, error) {
	root, err := z.D.ZrokRoot()
	if err != nil {
		return nil, "", "", fmt.Errorf("could not read the zrok environment: %w", err)
	}
	if !root.IsEnabled() {
		return nil, "", "", ErrDocNoZrok
	}
	shr, err := zroksdk.CreateShare(root, &zroksdk.ShareRequest{
		BackendMode: zroksdk.ProxyBackendMode,
		ShareMode:   zroksdk.PublicShareMode,
		Target:      z.D.DefaultBackend(),
		NameSelections: []zroksdk.NameSelection{
			{NamespaceToken: rec.Namespace, Name: rec.Name},
		},
	})
	if err != nil {
		return nil, "", "", ZrokSays("could not share "+rec.Path, err)
	}
	if len(shr.FrontendEndpoints) == 0 {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, "", "", fmt.Errorf("zrok made the share and gave it no public address")
	}
	ln, err := zroksdk.NewListener(shr.Token, root)
	if err != nil {
		_ = zroksdk.DeleteShare(root, shr)
		return nil, "", "", ZrokSays("the share was created but nothing could answer it", err)
	}
	return ln, shr.Token, strings.TrimRight(shr.FrontendEndpoints[0], "/") + "/", nil
}
