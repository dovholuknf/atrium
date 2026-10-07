// Package webasset serves the board's files: gzipped when the browser takes it,
// with an ETag so a reload revalidates to a 304 instead of downloading again.
//
// The board is some eighty files. Over a share to a phone every one of them is a
// round trip, and uncompressed they came to 2.7 MB, which is seconds on a phone.
// The room (internal/api) and the hub (internal/link) both serve the board, so
// both use this, and the two cannot drift.
//
// ── caching ──────────────────────────────────────────────
//
// `no-cache`, not `no-store`. A rebuilt board has to be picked up at once, since a
// cached copy after a rebuild looks exactly like a fix that did not work. no-cache
// still asks every time, and the ETag makes the answer a 304 with no body when
// nothing changed. /vendor/ never changes, so it keeps a day's max-age.
//
// ── compression ──────────────────────────────────────────
//
// Each file is read, hashed and gzipped once, the first time it is served, and
// kept. Not all at start: a test builds a server per case, and most never ask for
// a file. A board served from disk is re-read when its size or modification time
// changes, so an edit shows on the next reload without a restart.
package webasset

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

// The phone page's manifest has no type on a machine whose registry does not name one, and a browser ignores a
// manifest served as text. A script's type is pinned too: a Windows registry can say application/javascript over
// Go's own table.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
}

// Server serves the files of one board tree.
type Server struct {
	fsys fs.FS
	mu   sync.Mutex
	m    map[string]*asset
}

type asset struct {
	size  int64
	mod   time.Time
	ctype string
	etag  string
	raw   []byte
	gz    []byte // nil when gzip does not make it smaller, or the type is not text
}

// New serves fsys.
func New(fsys fs.FS) *Server {
	return &Server{fsys: fsys, m: map[string]*asset{}}
}

// Serve answers r with the file name, a path inside the tree with no leading
// slash. It reports false when name is not a regular file, and writes nothing.
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, name string) bool {
	a, ok := s.get(name)
	if !ok {
		return false
	}
	h := w.Header()
	if strings.HasPrefix(name, "vendor/") {
		h.Set("Cache-Control", "public, max-age=86400")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("Content-Type", a.ctype)
	body, etag := a.raw, a.etag
	if a.gz != nil {
		h.Add("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			// Its own ETag: the same tag on two different bodies would let a cache
			// hand a gzipped one to a client that never asked for it.
			body, etag = a.gz, strings.TrimSuffix(a.etag, `"`)+`-gz"`
			h.Set("Content-Encoding", "gzip")
		}
	}
	h.Set("ETag", etag)
	// A zero time, so ServeContent sends no Last-Modified and the ETag is the one
	// validator. An embedded file has no modification time to give.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	return true
}

// get returns the file name, read and compressed once and re-read when it has
// changed on disk.
func (s *Server) get(name string) (*asset, bool) {
	f, err := s.fsys.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	s.mu.Lock()
	a := s.m[name]
	s.mu.Unlock()
	if a != nil && a.size == st.Size() && a.mod.Equal(st.ModTime()) {
		return a, true
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	a = &asset{
		size: st.Size(), mod: st.ModTime(), raw: raw,
		ctype: contentType(name, raw),
		etag:  `"` + hex.EncodeToString(sum[:8]) + `"`,
	}
	if compressible(a.ctype) {
		a.gz = gzipped(raw)
	}
	s.mu.Lock()
	s.m[name] = a
	s.mu.Unlock()
	return a, true
}

func contentType(name string, raw []byte) string {
	if t := mime.TypeByExtension(pathpkg.Ext(name)); t != "" {
		return t
	}
	return http.DetectContentType(raw)
}

func compressible(ctype string) bool {
	t, _, _ := strings.Cut(ctype, ";")
	t = strings.TrimSpace(t)
	return strings.HasPrefix(t, "text/") || strings.HasSuffix(t, "+json") || strings.HasSuffix(t, "+xml") ||
		t == "application/javascript" || t == "application/json" || t == "image/svg+xml" ||
		t == "application/wasm"
}

// gzipped returns raw compressed, or nil when that is not smaller.
func gzipped(raw []byte) []byte {
	var b bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if _, err := zw.Write(raw); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil || b.Len() >= len(raw) {
		return nil
	}
	return b.Bytes()
}

// acceptsGzip reads Accept-Encoding, honouring an explicit `gzip;q=0`.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "*" {
			continue
		}
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}
