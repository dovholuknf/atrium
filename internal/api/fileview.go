package api

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// A file from a card, streamed to a browser tab to be READ.
//
// `downloadFile` is always an attachment, so a person who wants to look at
// `notes.md` gets a save dialog. This is the same bytes from the same card
// through the same containment, without the dialog, and it is the address a
// tab opens. It is served by the room that holds the card, so a card on another
// room comes through the hub's card routing like every other `/v1/tasks/<id>/`
// call, and the browser never needs to reach the room directly.
//
// WHAT IT WILL NOT DO is the rule download already states, kept: an agent
// writes these files, and HTML or SVG served as itself runs script on the origin
// that holds the settings, the grouping expression and every card. So:
//
//   - an image is served as its own image type, and only the raster ones. SVG is
//     a document that can carry script and is NOT an image here.
//   - EVERYTHING ELSE is text/plain, including .html, .svg and .md. A browser
//     shows it as text and runs nothing. Markdown is rendered by the page in
//     `read.html`, from these same text bytes, after sanitising.
//   - `nosniff`, so a browser does not decide a text/plain body is HTML.
//   - `Content-Security-Policy: sandbox`, so if some browser did treat it as a
//     document it would be in an opaque origin with scripts off.
//
// Content-Type comes from this table and never from `mime.TypeByExtension`,
// which reads the operator's own machine and is not a thing to trust with this.
var viewImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".avif": "image/avif",
}

func (s *Server) viewFile(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		writeErr(w, http.StatusBadRequest, errors.New("say which file, with ?path="))
		return
	}
	// Containment first, exactly as download does it, with the same single
	// answer for "outside" and "not there".
	real, err := safepath.Contained(filepath.FromSlash(task.Worktree), want)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	fi, err := os.Stat(real)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	if fi.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New("that is a directory. ask for one file"))
		return
	}
	f, err := os.Open(real)
	if err != nil {
		writeErr(w, http.StatusForbidden, errors.New("cannot read that"))
		return
	}
	defer f.Close()

	// By the extension of what the path RESOLVED to, so a link called
	// `pic.png` pointing at `page.html` is served as the text it is.
	ct := "text/plain; charset=utf-8"
	if t, ok := viewImageTypes[strings.ToLower(filepath.Ext(real))]; ok {
		ct = t
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Content-Disposition", `inline; filename="`+safepath.SafeName(path.Base(filepath.ToSlash(real)))+`"`)
	h.Set("Cache-Control", "no-store")
	if rel, ok := realRelative(task.Worktree, real); ok {
		h.Set(RealPathHeader, url.PathEscape(rel))
	}
	http.ServeContent(w, r, "", fi.ModTime(), f)
}
