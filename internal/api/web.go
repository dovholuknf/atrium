package api

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"os"
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

// embeddedBoard is the board compiled into this binary.
func embeddedBoard() fs.FS {
	sub, err := fs.Sub(web, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

// board is the tree the board is served and hashed from.
//
// A directory when one was named, the embed otherwise. Both are rooted at the
// board itself rather than at a `web/` above it, so an unmodified directory
// hashes to the same build id as the embed and the page has nothing to notice.
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

// boardID is the build id of the board this server is actually serving.
//
// Recomputed on every call when the board comes off disk, because that is the
// whole point of serving it off disk: the files change under a running daemon.
// A build id frozen at start would be the reload check switched off in exactly
// the mode that needs it most.
func (s *Server) boardID() string {
	if strings.TrimSpace(s.BoardDir) == "" {
		return BuildID
	}
	return buildID(board(s.BoardDir))
}

func webHandler(dir string) http.Handler {
	files := http.FileServer(http.FS(board(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The board is compiled into the binary, so a rebuild is the only way
		// it changes, and a cached copy after a rebuild looks exactly like a
		// bug that was not fixed. Vendored libraries never change, so they
		// keep caching.
		if strings.HasPrefix(r.URL.Path, "/vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-store, must-revalidate")
		}
		files.ServeHTTP(w, r)
	})
}
