package api

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"github.com/dovholuknf/atrium/internal/cardurl"
	"github.com/dovholuknf/atrium/internal/webasset"
	"io/fs"
	"net/http"
	"os"
	pathpkg "path"
	"strings"
)

// web holds the built client. The agreed client is a React SPA, but this is a
// plain page for now: it exercises the same JSON plus SSE contract, so it
// proves the API without a node toolchain standing between us and a running
// prototype. Replacing it changes nothing on the server.
//
//go:embed web
var web embed.FS

// BuildID identifies the board this binary carries.
//
// Cache-Control stops the browser reusing a stale copy, and that is a
// different problem from the one this solves: a page ALREADY OPEN keeps the
// JavaScript it loaded. A restart replaces what is served and touches nothing
// that is running, so a popped-out terminal left open for a day is still
// executing whatever it downloaded when it opened, and a fix shipped since
// then is simply not there.
//
// That is not hypothetical and not rare, because a popped-out window is
// long-lived by design. It cost an hour of "the reconnect does not work" for
// code that reconnected correctly in every window opened afterwards.
//
// So the board is given a way to notice. This goes out with `/v1/health`, the
// page remembers what it saw first, and a different answer means the thing
// serving it is not the thing that wrote it.
// THE WHOLE TREE, not `index.html`. The board used to be one file, so hashing
// that file was hashing the board. It is now a page that loads `board.css` and
// two dozen scripts, and almost every change lands in one of those. Hashing
// only the page would leave the build id identical across a change to any of
// them, and the reload-on-new-build behaviour above would quietly stop firing
// for the changes it exists for.
var BuildID = buildID(embeddedBoard())

// EmbeddedBoard is the board compiled into this binary.
//
// Exported so a HUB can serve these files itself. The hub is the half being
// restarted while somebody edits CSS, so it holds the board, and the room holds
// the database. See `internal/link`.
func EmbeddedBoard() fs.FS { return embeddedBoard() }

// BoardID hashes any board tree the same way `BuildID` hashes the embedded one.
//
// Exported for the same reason: a hub serving files from disk has to be able to
// tell the page which board it is looking at, or the reload-on-new-build check
// above compares two different trees and every tab reloads forever.
func BoardID(fsys fs.FS) string { return buildID(fsys) }

// embeddedBoard is the board compiled into this binary.
func embeddedBoard() fs.FS {
	sub, err := fs.Sub(web, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

// board returns the disk or embedded filesystem rooted at the board itself,
// so identical files produce the same build id.
func board(dir string) fs.FS {
	if strings.TrimSpace(dir) == "" {
		return embeddedBoard()
	}
	return os.DirFS(dir)
}

func buildID(fsys fs.FS) string {
	sum := sha256.New()
	// WalkDir visits in lexical order, so the same tree always hashes the
	// same way. The path goes in as well as the bytes: a file renamed and
	// nothing else changed is still a different board.
	err := fs.WalkDir(fsys, ".", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		// Match go:embed exclusions so disk and embedded copies have the same hash.
		// Skip symlinks, including web/CLAUDE.md, plus dotfiles and underscore files.
		if !e.Type().IsRegular() {
			return nil
		}
		if base := pathpkg.Base(path); strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sum.Write([]byte(path))
		sum.Write(raw)
		return nil
	})
	if err != nil {
		return ""
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

// boardID hashes the files actually being served. Recompute for disk mode
// so the reload check notices edits without a daemon restart.
func (s *Server) boardID() string {
	if strings.TrimSpace(s.BoardDir) == "" {
		return BuildID
	}
	return buildID(board(s.BoardDir))
}

func webHandler(dir string) http.Handler {
	fsys := board(dir)
	files := http.FileServer(http.FS(fsys))
	// Gzip and ETags for every file. See internal/webasset.
	assets := webasset.New(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			// A card's readable address is the board page. See internal/cardurl.
			if page, isCard := cardurl.Page(r.URL.Path); isCard {
				if page == "" {
					cardurl.NotFound(w, r.URL.Path)
					return
				}
				if assets.Serve(w, r, strings.TrimPrefix(page, "/")) {
					return
				}
			}
			if name, ok := fileName(r.URL.Path); ok && assets.Serve(w, r, name) {
				return
			}
		}
		// Redirects, directory listings and everything that is not a file stay
		// the file server's. Nothing it answers is cached.
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		files.ServeHTTP(w, r)
	})
}

// fileName is the file a request path names, with a directory's index page for
// a path ending in a slash. `/x/index.html` is left to the file server, which
// redirects it to `/x/`.
func fileName(p string) (string, bool) {
	if !strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/index.html") {
		return "", false
	}
	name := strings.TrimPrefix(pathpkg.Clean(p), "/")
	if strings.HasSuffix(p, "/") {
		name = pathpkg.Join(name, "index.html")
	}
	if name == "" || name == "." {
		return "", false
	}
	return name, true
}
