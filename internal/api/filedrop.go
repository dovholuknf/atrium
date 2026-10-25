package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// The room's half of the hub file drop. The hub's atrium_get sends the bytes of a drop file here, over the link, and
// this writes them inside the card the tool was called from. Designed in docs/rnd/hub-file-drop.md.
//
// NOT THROUGH UPLOAD. Upload follows paste_keep, and in scrap mode it writes to ScrapDir, which is outside the card.
// This always resolves against the card's own directory with safepath.Contained, before and after it creates
// anything, and it takes a destination the caller chose, which makes it the second endpoint in atrium that needs
// containment to be right.

// dropRoot is the card's directory, or why there is not one to write into. The same three refusals uploadTargetIn
// gives, without making `.atrium/incoming`, which a drop with a destination has no use for.
func dropRoot(t *store.Task) (string, error) {
	if t.Worktree == "" {
		return "", errors.New("this card has no directory, so there is nowhere to put a file")
	}
	if t.ArchivedAt != nil {
		return "", errors.New("this card is archived. its session is over and " +
			"putting files in its directory is probably not what you meant")
	}
	root := filepath.FromSlash(t.Worktree)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory on this machine", t.Worktree)
	}
	return root, nil
}

// destEscapes reports a destination that is absolute, rooted or climbs. Contained would refuse most of these too, and
// this refuses them before anything resolves, so the answer does not depend on what is on the disk.
func destEscapes(dest string) bool {
	if filepath.IsAbs(filepath.FromSlash(dest)) || strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, `\`) {
		return true
	}
	// A colon is a drive letter, a stream name or a device on Windows, and none of them is a path inside the card.
	if strings.Contains(dest, ":") {
		return true
	}
	for _, seg := range strings.FieldsFunc(dest, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// dropIn takes raw bytes and writes them into the card.
//
//	PUT /v1/tasks/{id}/files/drop?name=&dest=&replace=
func (s *Server) dropIn(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	name := safepath.SafeName(q.Get("name"))
	dest := strings.TrimSpace(q.Get("dest"))
	replace := q.Get("replace") == "1"

	root, err := dropRoot(task)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var target string
	if dest == "" {
		// uploadTargetIn and not uploadTarget, so paste_keep scrap never applies.
		dir, err := uploadTargetIn(task)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		target = filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+name)
	} else {
		if destEscapes(dest) {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		target, err = safepath.Contained(root, dest)
		if err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		parent := filepath.Dir(target)
		// BEFORE the directory is made, and again after, for the reason uploadTargetIn gives: MkdirAll follows a
		// link it did not create, and the answers before and after are not guaranteed to agree.
		if _, err := safepath.Contained(root, parent); err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		if err := os.MkdirAll(parent, 0o700); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := safepath.Contained(root, parent); err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			writeErr(w, http.StatusBadRequest, errors.New("that is a directory"))
			return
		}
	}
	if _, err := safepath.Contained(root, target); err != nil {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFile)
	if err := writeDrop(r.Body, target, replace); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("over the %d MB limit for one file", maxUploadFile>>20))
		case errors.Is(err, os.ErrExist):
			writeErr(w, http.StatusConflict, errors.New("a file is already there, pass replace"))
		case errors.Is(err, errDropEmpty):
			writeErr(w, http.StatusBadRequest, err)
		default:
			writeErr(w, http.StatusInternalServerError, err)
		}
		return
	}

	path := filepath.ToSlash(target)
	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"by": "drop", "files": []string{path},
	}); err != nil {
		// THE FILE IS ALREADY WRITTEN, so a failed event is logged and the answer is still 200. A 500 here would
		// make the caller retry a drop that landed.
		log.Printf("drop: %s was written but its event was not: %v", path, err)
	}
	rel, _ := realRelative(task.Worktree, target)
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "rel": rel})
}

var errDropEmpty = errors.New("an empty file was sent, nothing was saved")

// writeDrop copies body to target. Without replace the file is made exclusively, so a file already there is
// os.ErrExist. With it the bytes go to a temp file beside the target and are renamed over it, so a reader never sees
// half a file. Whatever was written is removed on a failure, and an empty body writes nothing.
func writeDrop(body io.Reader, target string, replace bool) error {
	dst := target
	if replace {
		var rnd [6]byte
		_, _ = rand.Read(rnd[:])
		dst = filepath.Join(filepath.Dir(target), ".atrium-drop-"+hex.EncodeToString(rnd[:]))
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n == 0 {
		err = errDropEmpty
	}
	if err == nil && replace {
		err = os.Rename(dst, target)
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
