package api

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// A REVIEW MOVES WITH ITS PR. The hub's move of a claim (internal/link/prclaim.go) asks the old room for this archive,
// hands it to the new room and then has the old room archive its row. The hub streams the bytes and keeps none.
//
//	GET  /v1/prs/export?key=host/org/repo/number   the live row and its run folder as a tar.gz
//	POST /v1/prs/import                            the same tar.gz, makes the row here
//	POST /v1/prs/{id}/archive                      the old room's last step: archive the row, stop its run
//
// The archive is `row.json` first, then `files/<path relative to the run folder>`. The checkout under `src/` is left
// out, since the fetch step makes it again from the hub when the run resumes. Nothing in an archive is a link, and
// every file on both sides goes through internal/safepath.

// PRArchiveCap bounds an archive, both as sent and as unpacked. A variable so a test need not make 64 MiB.
var PRArchiveCap int64 = 64 << 20

// maxArchiveEntries bounds the entries an import will read.
const maxArchiveEntries = 20000

// PRHeaderID is the header an export carries the old room's row id in, for the archive call that follows.
const PRHeaderID = "X-Atrium-PR-ID"

// prArchiveRow is `row.json`.
type prArchiveRow struct {
	Version int              `json:"version"`
	Key     string           `json:"key"`
	Row     store.PRTransfer `json:"row"`
}

// parsePRKey splits a canonical key, host/org/repo/number.
func parsePRKey(key string) (host, org, repo string, n int, ok bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "/")
	if len(parts) != 4 {
		return
	}
	num, err := strconv.Atoi(parts[3])
	if err != nil || num <= 0 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return
	}
	return parts[0], parts[1], parts[2], num, true
}

// GET /v1/prs/export
func (s *Server) exportPR(w http.ResponseWriter, r *http.Request) {
	host, org, repo, num, ok := parsePRKey(r.URL.Query().Get("key"))
	if !ok {
		prError(w, http.StatusBadRequest, "bad_request", "key is host/org/repo/number", nil)
		return
	}
	p, err := s.st.PRByKey(host, org, repo, num)
	if err != nil {
		s.prFail(w, err)
		return
	}
	if p == nil {
		prError(w, http.StatusNotFound, "not_found", "no review of that pr here", nil)
		return
	}
	dir, ok := s.prFolder(w, p)
	if !ok {
		return
	}
	files, total, err := listRunFiles(dir)
	if err != nil {
		prError(w, http.StatusInternalServerError, "export_failed", err.Error(), nil)
		return
	}
	if total > PRArchiveCap {
		prError(w, http.StatusRequestEntityTooLarge, "too_big",
			fmt.Sprintf("the review's folder is %d bytes, over the %d the archive is capped at", total, PRArchiveCap), nil)
		return
	}
	t, err := s.st.PRTransferOf(p.ID)
	if err != nil {
		s.prFail(w, err)
		return
	}
	head, _ := json.Marshal(prArchiveRow{Version: 1, Key: store.PRKey(host, org, repo, num), Row: t})
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set(PRHeaderID, p.ID)
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	werr := tw.WriteHeader(&tar.Header{Name: "row.json", Mode: 0o644, Size: int64(len(head)), Typeflag: tar.TypeReg})
	if werr == nil {
		_, werr = tw.Write(head)
	}
	for _, f := range files {
		if werr != nil {
			break
		}
		werr = addRunFile(tw, dir, f)
	}
	if werr == nil {
		werr = tw.Close()
	}
	if werr == nil {
		werr = gz.Close()
	}
	if werr != nil {
		// The status is sent, so the only honest answer left is a broken stream, which the importer refuses.
		panic(http.ErrAbortHandler)
	}
}

type runFile struct {
	rel  string
	size int64
}

// listRunFiles lists the regular files of a run folder, in order, and their total size. A link is not followed and
// not listed, and the checkout `src/` at the top is skipped. Every path is checked against the folder.
func listRunFiles(dir string) ([]runFile, int64, error) {
	var out []runFile
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if rel == "src" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if _, err := safepath.Contained(dir, path); err != nil {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, runFile{rel: filepath.ToSlash(rel), size: fi.Size()})
		total += fi.Size()
		return nil
	})
	return out, total, err
}

func addRunFile(tw *tar.Writer, dir string, f runFile) error {
	path, err := safepath.Contained(dir, filepath.FromSlash(f.rel))
	if err != nil {
		return err
	}
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return errors.New("not a regular file: " + f.rel)
	}
	// A file that grew since the list is cut at the size the header says, and one that shrank breaks the stream.
	if err := tw.WriteHeader(&tar.Header{Name: "files/" + f.rel, Mode: 0o644, Size: fi.Size(), Typeflag: tar.TypeReg,
		ModTime: fi.ModTime()}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, fh, fi.Size())
	return err
}

// archiveRel checks a name in an archive and gives the path inside the run folder. Nothing that leaves the folder,
// is absolute, names a drive, or holds a backslash gets through.
func archiveRel(name string) (string, error) {
	rel, ok := strings.CutPrefix(name, "files/")
	if !ok {
		return "", fmt.Errorf("%q is not under files/", name)
	}
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || strings.ContainsAny(rel, "\\:\x00") || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%q is not a path inside the folder", name)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%q leaves the folder", name)
		}
	}
	return rel, nil
}

// errArchive is an archive this room refuses, with the sentence to give.
type errArchive struct {
	status int
	code   string
	msg    string
}

func (e *errArchive) Error() string { return e.msg }

func badArchive(format string, a ...any) error {
	return &errArchive{http.StatusBadRequest, "bad_archive", fmt.Sprintf(format, a...)}
}

// POST /v1/prs/import
func (s *Server) importPR(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, PRArchiveCap)
	prOpMu.Lock()
	defer prOpMu.Unlock()
	row, created, err := s.importArchive(body)
	// Whatever is left of the body is read and dropped, so the sender finishes writing and hears the answer.
	_, _ = io.Copy(io.Discard, body)
	if err != nil {
		var ea *errArchive
		if errors.As(err, &ea) {
			prError(w, ea.status, ea.code, ea.msg, nil)
			return
		}
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			prError(w, http.StatusRequestEntityTooLarge, "too_big",
				fmt.Sprintf("the archive is over the %d bytes it is capped at", PRArchiveCap), nil)
			return
		}
		prError(w, http.StatusInternalServerError, "import_failed", err.Error(), nil)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		if row.State == store.PRQueued {
			s.prRunner().Start(row.ID)
		}
	}
	s.prAnswer(w, status, row.ID, map[string]any{"created": created})
}

// importArchive reads the archive and makes the row and its folder. A key this room already has a live row for
// answers that row and writes nothing. It holds prOpMu.
func (s *Server) importArchive(in io.Reader) (*store.PRReview, bool, error) {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, false, badArchive("that is not a gzip archive")
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "row.json" || hdr.Typeflag != tar.TypeReg {
		return nil, false, badArchive("the archive has to start with row.json")
	}
	var head prArchiveRow
	if hdr.Size > 1<<20 || json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&head) != nil {
		return nil, false, badArchive("row.json is not the row")
	}
	t := head.Row
	if t.Host == "" || t.Org == "" || t.Repo == "" || t.Number <= 0 {
		return nil, false, badArchive("row.json names no pull request")
	}
	if live, err := s.st.PRByKey(t.Host, t.Org, t.Repo, t.Number); err != nil {
		return nil, false, err
	} else if live != nil {
		return live, false, nil
	}
	dir, err := s.freshRunFolder(t)
	if err != nil {
		return nil, false, err
	}
	fail := func(err error) (*store.PRReview, bool, error) {
		_ = os.RemoveAll(dir)
		return nil, false, err
	}
	if err := unpackRunFiles(tr, dir); err != nil {
		return fail(err)
	}
	row, err := s.st.ImportPR(t, filepath.ToSlash(dir))
	if err != nil {
		return fail(badArchive("%s", err.Error()))
	}
	return row, true, nil
}

// freshRunFolder makes a run folder for the row under this room's reviews root, from RunFolderOn when its own name is
// free. A name an earlier row holds, which an archived row does when the review comes back to a room it once left, or
// a folder that has something in it, gets `-moved-N` after it. The folder is empty and ours to remove on a failure.
func (s *Server) freshRunFolder(t store.PRTransfer) (string, error) {
	root := s.st.ReviewsRoot()
	base, err := store.RunFolderPath(root, t.Host, t.Org, t.Repo, t.Number, t.Head)
	if err != nil {
		return "", badArchive("%s", err.Error())
	}
	for n := 0; n < 1000; n++ {
		cand := base
		if n > 0 {
			cand = base + "-moved-" + strconv.Itoa(n)
		}
		if taken, err := s.st.PRRunDirTaken(cand); err != nil {
			return "", err
		} else if taken {
			continue
		}
		if ents, err := os.ReadDir(filepath.FromSlash(cand)); err == nil && len(ents) > 0 {
			continue
		}
		if n == 0 {
			got, err := s.st.RunFolderOn(t.Host, t.Org, t.Repo, t.Number, t.Head)
			if err != nil {
				return "", err
			}
			cand = got
		} else if err := os.MkdirAll(filepath.FromSlash(cand), 0o755); err != nil {
			return "", err
		}
		if _, err := safepath.Contained(filepath.FromSlash(root), filepath.FromSlash(cand)); err != nil {
			_ = os.RemoveAll(filepath.FromSlash(cand))
			return "", badArchive("the folder is outside the reviews root")
		}
		return filepath.FromSlash(cand), nil
	}
	return "", errors.New("no free run folder")
}

// unpackRunFiles writes the archive's files into dir. A link, a device, a name that leaves the folder, a repeated
// name, more entries than the cap or more bytes than it all end the import with a refusal.
func unpackRunFiles(tr *tar.Reader, dir string) error {
	left := PRArchiveCap
	for n := 0; ; n++ {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			var mb *http.MaxBytesError
			if errors.As(err, &mb) {
				return err
			}
			return badArchive("the archive is cut short or broken")
		}
		if n >= maxArchiveEntries {
			return &errArchive{http.StatusRequestEntityTooLarge, "too_big", "the archive has too many entries"}
		}
		rel, err := archiveRel(hdr.Name)
		if err != nil {
			return badArchive("%s", err.Error())
		}
		dest, err := safepath.Contained(dir, filepath.FromSlash(rel))
		if err != nil {
			return badArchive("%q leaves the folder", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size < 0 || hdr.Size > left {
				return &errArchive{http.StatusRequestEntityTooLarge, "too_big",
					fmt.Sprintf("the archive is over the %d bytes it is capped at", PRArchiveCap)}
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			// Checked again now the parent exists, so a name under a link made by an earlier entry is caught.
			if _, err := safepath.Contained(dir, dest); err != nil {
				return badArchive("%q leaves the folder", hdr.Name)
			}
			f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				if errors.Is(err, fs.ErrExist) {
					return badArchive("%q is in the archive twice", hdr.Name)
				}
				return err
			}
			got, cerr := io.Copy(f, io.LimitReader(tr, hdr.Size))
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
			if got != hdr.Size {
				return badArchive("%q is cut short", hdr.Name)
			}
			left -= got
		default:
			return badArchive("%q is a link or a special file, and an archive holds only files", hdr.Name)
		}
	}
}

// POST /v1/prs/{id}/archive
//
// The old room's last step of a move. The row is archived and a run that is going is stopped. THE FOLDER STAYS, so
// nothing is lost if the import on the other room failed.
func (s *Server) archivePR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	prOpMu.Lock()
	defer prOpMu.Unlock()
	if _, err := s.st.MovePR(p.ID, []string{store.PRQueued, store.PRFetching, store.PRRunning},
		store.PRAborted, "", ""); err == nil {
		s.prRunner().Abort(p.ID)
	} else if !errors.Is(err, store.ErrPRState) && !errors.Is(err, sql.ErrNoRows) {
		s.prFail(w, err)
		return
	}
	if _, err := s.st.ArchivePR(p.ID); err != nil {
		s.prFail(w, err)
		return
	}
	s.prAnswer(w, http.StatusOK, p.ID, nil)
}
