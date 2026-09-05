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

	// `path` repeats. One is the old shape and means "everything under here";
	// several is a selection and means exactly those, files and directories
	// alike.
	//
	// A selection rather than a whole directory is what people actually want.
	// "Everything under here" is a guess that is usually wrong by a build
	// directory, and it made the one useful case, four files out of two
	// hundred, unreachable.
	wanted := r.URL.Query()["path"]
	if len(wanted) == 0 {
		wanted = []string{root}
	}

	roots := make([]string, 0, len(wanted))
	for _, want := range wanted {
		want = strings.TrimSpace(want)
		if want == "" {
			want = root
		}
		p, err := safepath.Contained(root, want)
		if err != nil {
			// One answer for outside and for missing, the same rule the
			// listing and the single-file download follow, so this cannot be
			// used to find out what is on the machine outside the card.
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		if _, err := os.Stat(p); err != nil {
			writeErr(w, http.StatusForbidden, safepath.ErrOutside)
			return
		}
		roots = append(roots, p)
	}

	// What the archive is called, and what paths inside it are relative to.
	//
	// For a selection that is the directory they share, so `src/a.go` and
	// `src/b.go` unpack into `src/`. Falls back to the card's own directory,
	// which is always an ancestor because everything here passed containment.
	base := commonDir(roots)
	if base == "" {
		base = root
	}
	name := filepath.Base(base)
	if name == "" || name == string(filepath.Separator) {
		name = "files"
	}
	if len(wanted) > 1 {
		name = name + "-selection"
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

	// Every selected path is walked. A file is a walk of one, so files and
	// directories need no separate branch.
	//
	// Deduplicated by the archive's own naming: two selections that overlap
	// produce the same relative name, and `seen` drops the second rather than
	// writing a duplicate entry that most unzip tools handle badly.
	seen := map[string]bool{}
	walk := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is noted and stepped over. One
			// unreadable subtree must not lose the rest of the archive.
			notes = append(notes, relOf(base, p)+": "+err.Error())
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
			notes = append(notes, relOf(base, p)+": outside this card, not included")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			notes = append(notes, relOf(base, p)+": "+err.Error())
			return nil
		}
		// Regular files only. A device, a socket or a named pipe has no
		// contents to copy and reading one can block forever.
		if !info.Mode().IsRegular() {
			return nil
		}

		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			notes = append(notes, relOf(base, p)+": "+err.Error())
			return nil
		}
		hdr.Name = filepath.ToSlash(relOf(base, p))
		if seen[hdr.Name] {
			return nil
		}
		seen[hdr.Name] = true
		hdr.Method = zip.Deflate
		out, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			notes = append(notes, relOf(base, p)+": "+err.Error())
			return nil
		}
		n, copyErr := io.Copy(out, f)
		f.Close()
		if copyErr != nil {
			notes = append(notes, relOf(base, p)+": "+copyErr.Error())
		}
		files++
		written += n
		return nil
	}

	for _, p := range roots {
		if stopped != "" {
			break
		}
		if err := filepath.WalkDir(p, walk); err != nil && !errors.Is(err, fs.SkipAll) {
			notes = append(notes, "walk: "+err.Error())
		}
	}

	if stopped != "" || len(notes) > 0 {
		writeZipNote(zw, base, files, written, stopped, notes)
	}
	log.Printf("[atrium api] zipped %d file(s), %d byte(s) from %s", files, written, base)
}

// commonDir is the deepest directory every path is inside.
//
// What the archive's entries are named relative to. Two files in `src` unpack
// into `src`, and a selection spread across the tree falls back toward the
// card's own directory, which is as far as it can go: everything here has
// already passed containment against it.
func commonDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	dirOf := func(p string) string {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		return filepath.Dir(p)
	}
	common := dirOf(paths[0])
	for _, p := range paths[1:] {
		common = sharedPrefix(common, dirOf(p))
	}
	return common
}

// sharedPrefix walks two paths back until they agree, segment by segment.
//
// Segments rather than characters: `C:\work\atrium` and `C:\work\atrium-afk`
// share fourteen characters and no directory, and trimming to the character
// count would name a directory that does not exist.
func sharedPrefix(a, b string) string {
	as := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bs := strings.Split(filepath.Clean(b), string(filepath.Separator))
	n := 0
	for n < len(as) && n < len(bs) && strings.EqualFold(as[n], bs[n]) {
		n++
	}
	if n == 0 {
		return ""
	}
	return strings.Join(as[:n], string(filepath.Separator))
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
