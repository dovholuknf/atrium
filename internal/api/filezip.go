package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
)

// A whole directory, as one download.
//
// One file at a time is the right shape for fetching a log somebody mentioned
// and the wrong shape for everything else: a build output, a set of screenshots,
// the directory an agent just wrote. This is the same containment and the same
// transport as the single-file download, so it works over an overlay for free
// and publishes nothing new.
//
// Streamed, not buffered. The zip is written straight to the response as the
// tree is walked, so a large directory costs time rather than memory, and the
// browser starts saving immediately. The cost of that choice is that a failure
// halfway through cannot become an HTTP error: the status line is long gone. So
// anything that goes wrong after the first byte is written INTO the archive as
// a note, where the person who opens it will see it.

// What one archive will carry before it stops. Bounds rather than limits: they
// exist so that pointing this at a directory with a `node_modules` in it
// produces something rather than running until the browser gives up.
const (
	maxZipFiles = 20000
	maxZipBytes = int64(2) << 30
)

// zipNote is where a truncation or a skipped file is recorded. Named so it
// sorts to the top of most listings, and prefixed so it cannot collide with
// anything real in a source tree.
const zipNote = "_atrium-note.txt"

func (s *Server) zipFiles(w http.ResponseWriter, r *http.Request) {
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

	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		want = root
	}
	dir, err := safepath.Contained(root, want)
	if err != nil {
		// One answer for outside and for missing, the same rule the listing and
		// the single-file download follow, so this cannot be used to find out
		// what is on the machine outside the card.
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		writeErr(w, http.StatusForbidden, safepath.ErrOutside)
		return
	}

	name := filepath.Base(dir)
	if name == "" || name == string(filepath.Separator) {
		name = "files"
	}
	w.Header().Set("Content-Type", "application/zip")
	// Quoted, and the name sanitised, because a directory name is not under
	// atrium's control and a header takes anything.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", safepath.SafeName(name)+".zip"))

	zw := zip.NewWriter(w)
	defer zw.Close()

	var (
		files   int
		written int64
		notes   []string
	)
	stopped := ""

	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is noted and stepped over. One
			// unreadable subtree must not lose the rest of the archive.
			notes = append(notes, relOf(dir, p)+": "+err.Error())
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if files >= maxZipFiles {
			stopped = fmt.Sprintf("stopped after %d files", maxZipFiles)
			return fs.SkipAll
		}
		if written >= maxZipBytes {
			stopped = fmt.Sprintf("stopped after %d bytes", maxZipBytes)
			return fs.SkipAll
		}
		// Every entry is re-checked, not just the directory that was asked
		// for. A symlink inside the tree pointing out of the card would
		// otherwise be followed and copied into an archive the card is not
		// entitled to, which is the whole reason `internal/safepath` resolves
		// symlinks on both sides.
		if _, err := safepath.Contained(root, p); err != nil {
			notes = append(notes, relOf(dir, p)+": outside this card, not included")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			notes = append(notes, relOf(dir, p)+": "+err.Error())
			return nil
		}
		// Regular files only. A device, a socket or a named pipe has no
		// contents to copy and reading one can block forever.
		if !info.Mode().IsRegular() {
			return nil
		}

		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			notes = append(notes, relOf(dir, p)+": "+err.Error())
			return nil
		}
		hdr.Name = filepath.ToSlash(relOf(dir, p))
		hdr.Method = zip.Deflate
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			notes = append(notes, relOf(dir, p)+": "+err.Error())
			return nil
		}
		n, copyErr := io.Copy(out, f)
		f.Close()
		if copyErr != nil {
			notes = append(notes, relOf(dir, p)+": "+copyErr.Error())
		}
		files++
		written += n
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		notes = append(notes, "walk: "+walkErr.Error())
	}

	if stopped != "" || len(notes) > 0 {
		writeZipNote(zw, dir, files, written, stopped, notes)
	}
	log.Printf("[atrium api] zipped %d file(s), %d byte(s) from %s", files, written, dir)
}

// relOf is a path as it should appear inside the archive.
func relOf(dir, p string) string {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return filepath.Base(p)
	}
	return rel
}

// writeZipNote puts anything worth saying inside the archive itself.
//
// The response has been streaming since the first file, so there is no status
// code left to change and no error the browser would show. A file in the zip is
// the only channel that still reaches the person who asked.
func writeZipNote(zw *zip.Writer, dir string, files int, written int64, stopped string, notes []string) {
	out, err := zw.Create(zipNote)
	if err != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "atrium archive of %s\n", filepath.ToSlash(dir))
	fmt.Fprintf(&b, "made %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "%d file(s), %d byte(s) before compression\n", files, written)
	if stopped != "" {
		fmt.Fprintf(&b, "\nINCOMPLETE: %s.\nNarrow the directory and try again.\n", stopped)
	}
	if len(notes) > 0 {
		b.WriteString("\nnot included:\n")
		for i, n := range notes {
			// Bounded, because one unreadable tree can produce thousands of
			// these and the note is meant to be read.
			if i >= 200 {
				fmt.Fprintf(&b, "  and %d more\n", len(notes)-i)
				break
			}
			fmt.Fprintf(&b, "  %s\n", n)
		}
	}
	_, _ = io.WriteString(out, b.String())
}
