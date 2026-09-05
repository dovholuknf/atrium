package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// A picture on a card, for the desktop notification it sends.
//
// A glyph covers most of it and is still the default: one character is drawn
// to a canvas, needs no storage, and cannot be anything but a picture. This is
// the other half, for a project that has a logo.
//
// **Stored beside the database rather than in it.** A card is a row somebody
// reads in a terminal, and a base64 image in a text column makes every query
// that touches it worse for one feature. The file is the truth; the card holds
// only the name.
//
// SVG is accepted and served with a policy that neutralises it. An SVG is a
// document, not an image: it can carry script, and served same-origin from the
// board's own port a navigation to it would run in the board's context. The
// headers below are the documented answer, and they are why this is not just
// `http.ServeFile`.

// maxIconBytes is what will be stored. Generous for a logo and far under what
// a notification will do anything useful with.
const maxIconBytes = 512 << 10

// IconDir is where card icons live. Set by the daemon, which knows where the
// rest of atrium's state is. Empty disables upload entirely rather than
// guessing at a directory.
var IconDir string

// iconTypes maps what a file starts with to what it is.
//
// Sniffed rather than trusted. The filename and the multipart content type
// both come from the browser, and neither is evidence.
var iconTypes = []struct {
	magic []byte
	ext   string
	mime  string
}{
	{[]byte("\x89PNG\r\n\x1a\n"), ".png", "image/png"},
	{[]byte("\xff\xd8\xff"), ".jpg", "image/jpeg"},
	{[]byte("GIF87a"), ".gif", "image/gif"},
	{[]byte("GIF89a"), ".gif", "image/gif"},
	{[]byte("RIFF"), ".webp", "image/webp"},
}

func sniffIcon(b []byte) (string, string, bool) {
	for _, t := range iconTypes {
		if len(b) >= len(t.magic) && string(b[:len(t.magic)]) == string(t.magic) {
			return t.ext, t.mime, true
		}
	}
	// SVG has no magic number. A leading `<` plus the word somewhere near the
	// front is as much as anybody gets, and it is enough: the serving side
	// treats it as hostile regardless.
	head := strings.ToLower(string(b[:min(len(b), 1024)]))
	if strings.Contains(head, "<svg") {
		return ".svg", "image/svg+xml", true
	}
	return "", "", false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) putIcon(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(IconDir) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no place to keep icons"))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("no file"))
		return
	}
	defer f.Close()

	// One byte over the limit is read on purpose, so the difference between
	// "exactly at the cap" and "too large" is knowable.
	raw, err := io.ReadAll(io.LimitReader(f, maxIconBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(raw) > maxIconBytes {
		writeErr(w, http.StatusBadRequest, errors.New(
			"that image is too large. half a megabyte is plenty for an icon"))
		return
	}
	ext, _, ok := sniffIcon(raw)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New(
			"that is not an image atrium recognises. png, jpeg, gif, webp or svg"))
		return
	}

	if err := os.MkdirAll(IconDir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Named by the card, so there is exactly one per card and replacing it
	// needs no cleanup. The old extension is removed first, or changing from a
	// png to an svg would leave both and the wrong one could win.
	dropIconFiles(task.ID)
	name := task.ID + ext
	if err := os.WriteFile(filepath.Join(IconDir, name), raw, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// The card records that it has one, and which. `img:` rather than a second
	// column: the field already means "the mark this card wears", and a glyph
	// and a filename are two spellings of that.
	if err := s.st.SetIcon(task.ID, "img:"+name); err != nil {
		s.fail(w, err)
		return
	}
	s.publishCard(task.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "icon": "img:" + name})
}

func (s *Server) getIcon(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	name := strings.TrimPrefix(task.Icon, "img:")
	if name == task.Icon || name == "" || strings.TrimSpace(IconDir) == "" {
		http.NotFound(w, r)
		return
	}
	// The name came from this server and is the card id plus a known
	// extension, but it made a round trip through a database, so it is checked
	// rather than trusted.
	if filepath.Base(name) != name {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(filepath.Join(IconDir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, mime, ok := sniffIcon(raw)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// An uploaded SVG is a document that can carry script, and this is served
	// from the board's own origin. The sandbox and the empty default policy
	// mean a navigation to it renders a picture and runs nothing. `nosniff`
	// stops the browser deciding it is something else entirely.
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(raw)
}

func (s *Server) deleteIcon(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	dropIconFiles(task.ID)
	if err := s.st.SetIcon(task.ID, ""); err != nil {
		s.fail(w, err)
		return
	}
	s.publishCard(task.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// dropIconFiles removes whatever this card had, whatever it was called.
func dropIconFiles(taskID string) {
	if strings.TrimSpace(IconDir) == "" {
		return
	}
	for _, t := range iconTypes {
		_ = os.Remove(filepath.Join(IconDir, taskID+t.ext))
	}
	_ = os.Remove(filepath.Join(IconDir, taskID+".svg"))
}

// publishCard re-reads a card and pushes it, so every open board repaints
// without waiting for the poll.
//
// A failure to re-read is dropped rather than returned: the write it follows
// already succeeded, and reporting an error for the notification about it
// would say the wrong thing happened.
func (s *Server) publishCard(id string) {
	t, err := s.st.Get(id)
	if err != nil {
		return
	}
	s.Broadcast("task", toView(t))
}
