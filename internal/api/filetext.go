package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// Reading a file as text, and writing it back.
//
// The counterpart to download, for the case where you want to change one line
// rather than fetch the whole thing: a TODO, a note, a config the agent got
// nearly right. Over an overlay there is no other way to do it at all, because
// the file is on the daemon's machine and `files/open` starts an editor there,
// where nobody is sitting.
//
// **A write carries the hash of what was read, and the daemon refuses if the
// file has moved on.** This is the one thing that makes editing a file an agent
// is also editing safe to offer. Without it a save is last-write-wins against
// something working at machine speed, and the loss is silent: the agent's edit
// is simply gone and nothing anywhere says so. `docs/backlog.md` names this as
// worth having with or without an editor, and it is.
//
// Optimistic, not a lock. Nothing is held, nothing has to be released, and a
// session that crashes mid-edit blocks nobody. A refused write hands back the
// current content and its hash, so the answer to a conflict is to look at what
// changed rather than to be told to try again.

// maxTextBytes is how large a file this will read.
//
// A text box is the wrong tool past this, and a browser that has to hold a
// hundred megabytes of string is a browser that stops responding. Download
// exists for everything else and has no such limit.
const maxTextBytes = 2 << 20 // 2 MiB

type textOut struct {
	Path string `json:"path"`
	Text string `json:"text"`
	// Hash is what a later write must quote back. Of the BYTES ON DISK, so a
	// caller cannot compute it from the text after transforming line endings
	// and get a different answer.
	Hash string `json:"hash"`
	// Eol is what the file used, so a write can put back what it found rather
	// than imposing the platform's own.
	Eol string `json:"eol"`
}

func (s *Server) readText(w http.ResponseWriter, r *http.Request) {
	full, ok := s.resolveCardFile(w, r)
	if !ok {
		return
	}

	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	if fi.Size() > maxTextBytes {
		writeErr(w, http.StatusBadRequest, errors.New(
			"that file is too large to edit here. download it instead"))
		return
	}

	raw, err := os.ReadFile(full)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	// Refused rather than mangled. Handing a binary to a text box produces a
	// string that cannot round trip, and saving it destroys the file with no
	// error anywhere.
	if !utf8.Valid(raw) {
		writeErr(w, http.StatusBadRequest, errors.New(
			"that file is not text. download it instead"))
		return
	}

	eol := "\n"
	if strings.Contains(string(raw), "\r\n") {
		eol = "\r\n"
	}
	writeJSON(w, http.StatusOK, textOut{
		Path: filepath.ToSlash(full),
		// Normalised on the way out so the browser sees one kind of line, and
		// put back on the way in. A textarea rewrites line endings whatever
		// arrives, so a file would silently change ending on the first save.
		Text: strings.ReplaceAll(string(raw), "\r\n", "\n"),
		Hash: hashOf(raw),
		Eol:  eol,
	})
}

type textIn struct {
	Text string `json:"text"`
	// Hash is what the caller last read. Required: a write with no hash is a
	// write that has not looked, and this endpoint exists to stop those.
	Hash string `json:"hash"`
	Eol  string `json:"eol"`
}

func (s *Server) writeText(w http.ResponseWriter, r *http.Request) {
	full, ok := s.resolveCardFile(w, r)
	if !ok {
		return
	}

	var body textIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTextBytes+(1<<16))).
		Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	if strings.TrimSpace(body.Hash) == "" {
		writeErr(w, http.StatusBadRequest, errors.New(
			"a write has to say what it was based on"))
		return
	}

	current, err := os.ReadFile(full)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	if got := hashOf(current); got != body.Hash {
		// 409, and the current content with it. The useful answer to a
		// conflict is what it says now, not an instruction to try again.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "that file changed while you were editing it. " +
				"nothing was written.",
			"text": strings.ReplaceAll(string(current), "\r\n", "\n"),
			"hash": got,
		})
		return
	}

	out := body.Text
	if body.Eol == "\r\n" {
		out = strings.ReplaceAll(strings.ReplaceAll(out, "\r\n", "\n"), "\n", "\r\n")
	}
	// Written in place rather than through a temp file and a rename. A rename
	// would break a symlink the operator put there on purpose, and the file is
	// already inside the card by the time this runs.
	if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"hash": hashOf([]byte(out)),
	})
}

// deleteFiles removes the selected files.
//
// FILES ONLY. A directory is refused rather than removed recursively: losing a
// tree to one press in a file picker is not a risk worth carrying for a
// convenience nobody asked for, and `rm` is one terminal away for the times it
// is genuinely wanted.
//
// Each path is resolved through `internal/safepath` on its own, so a selection
// cannot smuggle one past the check by being long.
func (s *Server) deleteFiles(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	root := filepath.FromSlash(task.Worktree)

	wanted := r.URL.Query()["path"]
	if len(wanted) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("no path"))
		return
	}

	// Resolved and checked in full BEFORE anything is removed, so a selection
	// with one bad entry deletes nothing rather than half of itself.
	full := make([]string, 0, len(wanted))
	for _, want := range wanted {
		p, err := safepath.Contained(root, strings.TrimSpace(want))
		if err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		fi, err := os.Stat(p)
		if err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		if fi.IsDir() {
			writeErr(w, http.StatusBadRequest, errors.New(
				"that is a directory. delete a directory from a terminal, "+
					"where you can see what is in it"))
			return
		}
		full = append(full, p)
	}

	gone := 0
	for _, p := range full {
		if err := os.Remove(p); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		gone++
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": gone})
}

// resolveCardFile turns `?path=` into a real path inside this card, or answers
// and reports that it did not.
//
// The same rule every file endpoint follows: one answer for outside and for
// missing, so this is not an oracle for what is on the machine.
func (s *Server) resolveCardFile(w http.ResponseWriter, r *http.Request) (string, bool) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return "", false
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return "", false
	}
	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no path"))
		return "", false
	}
	full, err := safepath.Contained(filepath.FromSlash(task.Worktree), want)
	if err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return "", false
	}
	return full, true
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
