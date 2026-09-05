package api

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// Moving files to and from a card's working directory.
//
// Designed in docs/file-transfer-design.md, which is worth reading before
// changing any of this. The short version of why it is shaped this way:
//
// UPLOAD takes no destination. The caller says which card and nothing else,
// and atrium computes where the bytes land. That is the whole security
// argument for the first version: a caller-supplied destination needs
// containment to be right, and a computed one is correct even if containment
// is wrong, because there is no caller input in the path at all.
//
// DOWNLOAD does take a path, and is therefore the first thing in atrium that
// actually needs safepath.Contained.

const (
	// maxUploadFile is one file. A screenshot is under a megabyte and a heap
	// dump is not a thing to paste into a chat.
	maxUploadFile = 32 << 20
	// maxUploadRequest is the whole request. Enforced by MaxBytesReader, so
	// the limit is hit while reading rather than after.
	maxUploadRequest = 64 << 20
	// incomingDir is where uploads land, relative to the card's directory.
	//
	// Not the directory root, for three reasons that each stand alone: a
	// repository does not want a screenshot in its root turning up in
	// `git status`, a folder of uploads is something a person can clear out
	// without guessing which files were theirs, and one `.gitignore` line
	// covers it forever.
	incomingDir = ".atrium/incoming"
)

// SettingPasteKeep decides whether a pasted file is kept.
//
// Two honest answers and no third one:
//
//	keep   the card's own `.atrium/incoming`, where it stays. You have the
//	       screenshot tomorrow, and you also have every screenshot you ever
//	       pasted.
//	scrap  a directory beside atrium's own state, emptied on every daemon
//	       start. The path still reaches the agent, the file still works for
//	       as long as the conversation does, and your repository does not
//	       accumulate a folder of pictures you looked at once.
//
// There is no option that hands the bytes straight to the model. A pseudo
// terminal carries input characters, and an image is not one. Claude Code gets
// a pasted image by reading the clipboard ITSELF, on its own machine, which is
// exactly what a browser two machines away cannot do.
const SettingPasteKeep = "paste_keep"

// SettingPastePreamble is what gets typed in front of the path.
//
// A bare path is what a person types when they mean "look at this", and it is
// not what they say. Nothing is submitted either way: the text and the path
// are spliced into the line being written, and pressing enter stays yours.
const SettingPastePreamble = "paste_preamble"

// DefaultPastePreamble reads as an instruction rather than as a fragment.
const DefaultPastePreamble = "check out the image here: "

// PastePreambleNone is how "no preamble at all" is stored.
//
// A word rather than an empty string, because an empty string is what a
// setting that has never been written reads as, and "never chose" and "chose
// nothing" have to be two different answers or the default can never be turned
// off. Nobody types this: the board writes it when the box is unticked.
const PastePreambleNone = "none"

// ScrapDir is where uploads go when they are not being kept. Set by the daemon
// beside the rest of its state, and emptied at every start.
var ScrapDir string

// uploadTarget resolves a card to the directory its files belong in, making it
// if it is not there.
//
// Returns the directory and the root every written path has to stay inside.
// The two differ by mode, and the check downstream is against the root rather
// than against the card, or the scrap directory would be refused for being
// exactly where it was asked to be.
func (s *Server) uploadTarget(t *store.Task) (string, string, error) {
	// Not kept, so not in the repository. The path has no caller input in it,
	// the filename is sanitised where it is written, and the root below is this
	// directory, so a filename that tried to climb out is still refused.
	if s.pasteIsScrap() && strings.TrimSpace(ScrapDir) != "" {
		if err := os.MkdirAll(ScrapDir, 0o700); err != nil {
			return "", "", err
		}
		return ScrapDir, ScrapDir, nil
	}
	dir, err := uploadTargetIn(t)
	if err != nil {
		return "", "", err
	}
	return dir, filepath.FromSlash(t.Worktree), nil
}

// pasteIsScrap reports whether uploads are being thrown away on the next start.
func (s *Server) pasteIsScrap() bool {
	v, err := s.st.Setting(SettingPasteKeep)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(v), "scrap")
}

func uploadTargetIn(t *store.Task) (string, error) {
	if t.Worktree == "" {
		return "", errors.New("this card has no directory, so there is nowhere to put a file")
	}
	if t.ArchivedAt != nil {
		// Uploading into the working directory of a session that ended is
		// almost always a mistake about which card is which.
		return "", errors.New("this card is archived. its session is over and " +
			"putting files in its directory is probably not what you meant")
	}
	root := filepath.FromSlash(t.Worktree)
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory on this machine", t.Worktree)
	}
	dir := filepath.Join(root, filepath.FromSlash(incomingDir))

	// Checked BEFORE it is created, not after.
	//
	// The path is computed and has no caller input in it, which is what makes
	// the rest of this safe, and that is not enough on its own: if `.atrium`
	// already exists as a symlink or a junction pointing somewhere else, then
	// creating `incoming` under it makes a directory outside the card before
	// anything has refused anything. Resolving first and creating second is
	// the whole difference, and getting it backwards would undercut the
	// containment primitive on the first endpoint built around it.
	if _, err := safepath.Contained(root, dir); err != nil {
		return "", fmt.Errorf("%s does not resolve inside this card's directory, "+
			"so nothing will be written there: %w", incomingDir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// And again once it exists, because MkdirAll resolves symlinks it did not
	// create and the answer before and after are not guaranteed to agree.
	real, err := safepath.Contained(root, dir)
	if err != nil {
		return "", err
	}
	return real, nil
}

// uploadFiles takes bytes and puts them where the card can reach them.
func (s *Server) uploadFiles(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	dir, root, err := s.uploadTarget(task)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequest)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"could not read the upload: %w. the limit is %d MB per request",
			err, maxUploadRequest>>20))
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	headers := r.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("no files were sent"))
		return
	}

	stamp := time.Now().Format("20060102-150405")
	written := make([]string, 0, len(headers))
	for i, h := range headers {
		if h.Size > maxUploadFile {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf(
				"%s is %d MB, over the %d MB limit for one file",
				h.Filename, h.Size>>20, maxUploadFile>>20))
			return
		}
		name := safepath.SafeName(h.Filename)
		// The stamp goes first so the directory sorts by when things arrived,
		// and the index disambiguates a paste of two files in one second.
		leaf := stamp + "-" + name
		if i > 0 {
			leaf = fmt.Sprintf("%s-%d-%s", stamp, i+1, name)
		}
		dest := filepath.Join(dir, leaf)

		// Checked anyway. Nothing caller-supplied is in this path, so this can
		// only fail if the directory has moved under a symlink, and that is
		// worth refusing rather than assuming.
		if _, err := safepath.Contained(root, dest); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if err := saveUpload(h, dest); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		written = append(written, filepath.ToSlash(dest))
	}

	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"by": "upload", "files": written,
	}); err != nil {
		s.fail(w, err)
		return
	}
	// The daemon's own forward-slash form, which is what every path in atrium
	// looks like and what gets spliced into a prompt. Both pwsh and claude
	// accept it on Windows, so there is one path convention rather than one
	// for the API and another for the terminal.
	writeJSON(w, http.StatusOK, map[string]any{"paths": written})
}

// saveUpload writes one part to disk.
func saveUpload(h *multipart.FileHeader, dest string) error {
	src, err := h.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	// Exclusive create, so an upload never silently overwrites something.
	// The name carries a timestamp, so a collision means two files in the same
	// second with the same name, and the honest answer is to say so.
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(src, maxUploadFile)); err != nil {
		out.Close()
		_ = os.Remove(dest)
		return err
	}
	return out.Close()
}

// downloadFile serves one file out of a card's directory.
//
// This is the first caller-supplied path in atrium that is bounded, and the
// reason safepath exists.
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if task.Worktree == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		writeErr(w, http.StatusBadRequest, errors.New("say which file, with ?path="))
		return
	}

	real, err := safepath.Contained(filepath.FromSlash(task.Worktree), want)
	if err != nil {
		// Everything OUTSIDE the card answers the same, whether or not it is
		// there, so this cannot be used to find out what is on the machine.
		// The error is the constant rather than the one that came back, since
		// a resolution failure and a refusal must not read differently.
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	fi, err := os.Stat(real)
	if err != nil {
		// Inside the card and not there. A different answer on purpose, and it
		// leaks nothing: anyone who can ask this can already see that
		// directory.
		writeErr(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	if fi.IsDir() {
		// A zip stream is a second mechanism with its own bounds, its own
		// traversal problems and its own partial-failure story. The case that
		// comes up is one file.
		writeErr(w, http.StatusBadRequest, errors.New(
			"that is a directory. ask for one file"))
		return
	}

	f, err := os.Open(real)
	if err != nil {
		writeErr(w, http.StatusForbidden, errors.New("cannot read that"))
		return
	}
	defer f.Close()

	// ALWAYS an attachment, and always octet-stream. Serving an HTML file from
	// a working directory inline would be serving attacker-authored script on
	// the board's own origin, and the board holds the grouping expression, the
	// settings and every card.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+safepath.SafeName(filepath.Base(real))+`"`)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}
