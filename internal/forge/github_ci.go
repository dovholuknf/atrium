package forge

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ghDownloadTimeout = 5 * time.Minute

type ghRun struct {
	ID         int64  `json:"databaseId"`
	Workflow   string `json:"workflowName"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSha    string `json:"headSha"`
	HeadBranch string `json:"headBranch"`
	CreatedAt  string `json:"createdAt"`
	URL        string `json:"url"`
}

func (r ghRun) info() CIRunInfo {
	return CIRunInfo{ID: r.ID, Workflow: r.Workflow, Status: r.Status, Conclusion: r.Conclusion, Head: r.HeadSha,
		Branch: r.HeadBranch, Created: r.CreatedAt, URL: r.URL}
}

const ghRunFields = "databaseId,workflowName,status,conclusion,headSha,headBranch,createdAt,url"

// Runs lists the runs of a repository, newest first, for a branch or a commit.
func (g *github) Runs(ctx context.Context, ref Ref, q RunQuery) ([]CIRunInfo, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultRuns
	}
	if limit > MaxRuns {
		limit = MaxRuns
	}
	args := []string{"run", "list", "--repo", repoArg(ref), "--limit", strconv.Itoa(limit), "--json", ghRunFields}
	if b := strings.TrimSpace(q.Branch); b != "" {
		args = append(args, "--branch", b)
	}
	if s := strings.TrimSpace(q.SHA); s != "" {
		args = append(args, "--commit", s)
	}
	out, err := g.do(ctx, ref, ghJSONLimit, args...)
	if err != nil {
		return nil, err
	}
	var rows []ghRun
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("%s run list printed something that is not json: %w", g.cmd, err)
	}
	runs := make([]CIRunInfo, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, r.info())
	}
	return runs, nil
}

// RunDetail is one run with its jobs and their steps.
func (g *github) RunDetail(ctx context.Context, ref Ref, id int64) (*CIRunDetail, error) {
	out, err := g.do(ctx, ref, ghJSONLimit, "run", "view", strconv.FormatInt(id, 10), "--repo", repoArg(ref),
		"--json", ghRunFields+",jobs")
	if err != nil {
		return nil, err
	}
	var v struct {
		ghRun
		Jobs []struct {
			ID         int64    `json:"databaseId"`
			Name       string   `json:"name"`
			Status     string   `json:"status"`
			Conclusion string   `json:"conclusion"`
			URL        string   `json:"url"`
			Started    string   `json:"startedAt"`
			Completed  string   `json:"completedAt"`
			Steps      []CIStep `json:"steps"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s run view printed something that is not json: %w", g.cmd, err)
	}
	d := &CIRunDetail{CIRunInfo: v.info(), Jobs: []CIJob{}}
	if d.ID == 0 {
		d.ID = id
	}
	for _, j := range v.Jobs {
		steps := j.Steps
		if steps == nil {
			steps = []CIStep{}
		}
		d.Jobs = append(d.Jobs, CIJob{ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion, URL: j.URL,
			Started: j.Started, Completed: j.Completed, Steps: steps})
	}
	return d, nil
}

// Log is the end of a job's log, or of the failed jobs of a run, like `gh run view --log-failed`. The bound is
// applied while the log is read: the runner keeps the last lines and never holds the whole of it.
func (g *github) Log(ctx context.Context, ref Ref, q LogQuery) (*CILog, error) {
	tail := ClampTail(q.Tail)
	args := []string{"run", "view", "--repo", repoArg(ref)}
	switch {
	case q.JobID > 0:
		args = append(args, "--job", strconv.FormatInt(q.JobID, 10))
	case q.RunID > 0:
		args = append(args[:2], strconv.FormatInt(q.RunID, 10), "--repo", repoArg(ref))
	default:
		return nil, fmt.Errorf("say which job or which run")
	}
	if q.FailedOnly {
		args = append(args, "--log-failed")
	} else {
		args = append(args, "--log")
	}
	c := Cmd{Name: g.cmd, Args: args, Timeout: ghTimeout, Limit: LogBytes, Tail: tail}
	out, err := g.run(ctx, c)
	if err != nil {
		return nil, access(g.cmd, host(ref), err)
	}
	text, kept, truncated := Tail(string(out), tail, LogBytes)
	return &CILog{JobID: q.JobID, RunID: q.RunID, FailedOnly: q.FailedOnly, Lines: kept, Truncated: truncated,
		Text: text}, nil
}

// Artifacts lists a run's artifacts.
func (g *github) Artifacts(ctx context.Context, ref Ref, runID int64) ([]CIArtifactInfo, error) {
	path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/artifacts?per_page=100", ref.Org, ref.Repo, runID)
	args := []string{"api"}
	if h := host(ref); !strings.EqualFold(h, "github.com") {
		args = append(args, "--hostname", h)
	}
	out, err := g.do(ctx, ref, ghJSONLimit, append(args, path)...)
	if err != nil {
		return nil, err
	}
	var v struct {
		Artifacts []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Size    int64  `json:"size_in_bytes"`
			Expired bool   `json:"expired"`
			Created string `json:"created_at"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("%s api printed something that is not json: %w", g.cmd, err)
	}
	list := make([]CIArtifactInfo, 0, len(v.Artifacts))
	for _, a := range v.Artifacts {
		list = append(list, CIArtifactInfo{ID: a.ID, Name: a.Name, Size: a.Size, Expired: a.Expired, Created: a.Created})
	}
	return list, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DownloadDir is where an artifact goes under dest: a folder per host, repository, run and artifact name.
func DownloadDir(dest string, ref Ref, runID int64, name string) string {
	return filepath.Join(dest, unsafeName.ReplaceAllString(host(ref), "_"), unsafeName.ReplaceAllString(ref.Org, "_"),
		unsafeName.ReplaceAllString(ref.Repo, "_"), strconv.FormatInt(runID, 10), unsafeName.ReplaceAllString(name, "_"))
}

// Download puts one artifact of a run in a folder under dest on this machine, the hub, unless it is there already. The
// size the forge lists is checked before anything is downloaded, and the folder is checked again after, since an
// artifact is unpacked.
func (g *github) Download(ctx context.Context, ref Ref, runID int64, name, dest string, maxBytes int64) (*CIDownload, error) {
	if maxBytes <= 0 {
		maxBytes = ArtifactBytes
	}
	list, err := g.Artifacts(ctx, ref, runID)
	if err != nil {
		return nil, err
	}
	var found *CIArtifactInfo
	var names []string
	for i := range list {
		names = append(names, list[i].Name)
		if list[i].Name == name {
			found = &list[i]
		}
	}
	switch {
	case found == nil:
		return nil, fmt.Errorf("run %d has no artifact named %q. it has: %s", runID, name, strings.Join(names, ", "))
	case found.Expired:
		return nil, fmt.Errorf("artifact %q of run %d has expired on the forge and cannot be downloaded", name, runID)
	case found.Size > maxBytes:
		return nil, fmt.Errorf("artifact %q is %d bytes, over the %d byte cap for a download to the hub", name,
			found.Size, maxBytes)
	}
	dir := DownloadDir(dest, ref, runID, name)
	if rel, err := filepath.Rel(dest, dir); err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("that artifact name does not make a folder under the hub's download folder")
	}
	// ONE DOWNLOAD OF A FOLDER AT A TIME, so two asks for the same artifact do not unpack over each other. The second
	// finds the first's folder and reads it.
	mu := dirLock(dir)
	mu.Lock()
	defer mu.Unlock()
	// IN USE FROM HERE UNTIL THE CALLER RELEASES IT, so no prune, this one's or another download's, takes the folder
	// while it is scanned or read.
	release := acquire(dir)
	kept := false
	defer func() {
		if !kept {
			release()
		}
	}()
	PruneDownloads(dest, pruneAge, pruneBytes)
	if d, err := scanDir(dir, maxBytes); err == nil && len(d.Files) > 0 {
		// A hit is a use: it keeps the folder from aging out.
		now := time.Now()
		_ = os.Chtimes(dir, now, now)
		d.Name = name
		d.release, kept = release, true
		return d, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, fmt.Errorf("the hub could not make %s: %w", filepath.Dir(dir), err)
	}
	// Unpacked into a folder of its own and renamed into place, so a failure leaves nothing half there.
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+tmpMark)
	if err != nil {
		return nil, fmt.Errorf("the hub could not make a folder under %s: %w", filepath.Dir(dir), err)
	}
	defer os.RemoveAll(tmp)
	// gh never unpacks: it writes the zip to a file, bounded to the compressed size the forge listed plus a margin, and
	// atrium unpacks it below with its own bounds.
	zipPath := filepath.Join(tmp, ".artifact.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		return nil, err
	}
	args := []string{"api"}
	if h := host(ref); !strings.EqualFold(h, "github.com") {
		args = append(args, "--hostname", h)
	}
	args = append(args, fmt.Sprintf("repos/%s/%s/actions/artifacts/%d/zip", ref.Org, ref.Repo, found.ID))
	_, err = g.run(ctx, Cmd{Name: g.cmd, Args: args, Timeout: ghDownloadTimeout, Limit: int(min(maxBytes, found.Size+(1<<20))),
		Sink: zf})
	zf.Close()
	if err != nil {
		return nil, access(g.cmd, host(ref), err)
	}
	if err := unzipBounded(zipPath, tmp, maxBytes); err != nil {
		return nil, err
	}
	_ = os.Remove(zipPath)
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return nil, fmt.Errorf("the hub could not put the artifact in place: %w", err)
	}
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
	d, err := scanDir(dir, maxBytes)
	if err != nil {
		return nil, err
	}
	d.Name = name
	d.release, kept = release, true
	return d, nil
}

// scanDir lists a downloaded artifact's folder and refuses one over the cap.
func scanDir(dir string, maxBytes int64) (*CIDownload, error) {
	d := &CIDownload{Dir: dir, Files: []CIFile{}}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		d.Bytes += info.Size()
		if d.Bytes > maxBytes {
			return fmt.Errorf("artifact is over the %d byte cap once unpacked, so it was removed", maxBytes)
		}
		if len(d.Files) >= 200 {
			d.More = true
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		d.Files = append(d.Files, CIFile{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// ReadFile is the end of one file of a downloaded artifact, bounded while it is read. A file that is not text is
// named and not read.
func ReadFile(d *CIDownload, rel string, lines int) error {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is not a file inside the artifact", rel)
	}
	f, err := os.Open(filepath.Join(d.Dir, clean))
	if err != nil {
		return fmt.Errorf("the artifact has no file %q. its files are listed in `files`", rel)
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		return fmt.Errorf("%q is a folder, not a file", rel)
	}
	w := &TailWriter{Lines: ClampTail(lines), Bytes: FileBytes}
	buf := make([]byte, 32<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if strings.IndexByte(string(buf[:n]), 0) >= 0 {
				d.File, d.Text = rel, ""
				d.Note = "that file is binary, so it was not read. it is on the hub at " + filepath.Join(d.Dir, clean)
				return nil
			}
			_, _ = w.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	text, _, dropped := w.Result()
	d.File, d.Text, d.Truncated = rel, text, dropped > 0
	return nil
}

// The bounds of the hub's download folder.
const (
	// ArtifactMaxAge is how long a download is kept.
	ArtifactMaxAge = 24 * time.Hour
	// ArtifactStoreBytes is the most the whole download folder may hold. The oldest downloads go first.
	ArtifactStoreBytes = 1 << 30
	// maxZipEntries is the most files one artifact may unpack to.
	maxZipEntries = 10000
	tmpMark       = ".dl-"
)

// pruneAge and pruneBytes are the bounds a download prunes by. Variables so a test can make them small.
var (
	pruneAge   = ArtifactMaxAge
	pruneBytes = int64(ArtifactStoreBytes)
)

var (
	useMu sync.Mutex
	inUse = map[string]int{}
)

// acquire marks a download folder in use and answers the release. PruneDownloads never removes a folder in use.
func acquire(dir string) (release func()) {
	useMu.Lock()
	inUse[dir]++
	useMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			useMu.Lock()
			if inUse[dir]--; inUse[dir] <= 0 {
				delete(inUse, dir)
			}
			useMu.Unlock()
		})
	}
}

// removeUnused removes a download folder unless it is in use. The check and the removal are one step, so a use cannot
// begin between them.
func removeUnused(dir string) bool {
	useMu.Lock()
	defer useMu.Unlock()
	if inUse[dir] > 0 {
		return false
	}
	return os.RemoveAll(dir) == nil
}

var dirLocks sync.Map

// dirLock is the lock of one download folder.
func dirLock(dir string) *sync.Mutex {
	m, _ := dirLocks.LoadOrStore(dir, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// unzipBounded unpacks a zip into dir, counting the bytes of every entry AS IT IS WRITTEN, so an entry that says it is
// small and is not, or a zip of many small ones, stops at maxBytes in all. An entry name that is absolute or leaves
// dir, and anything that is not a plain file or folder, is refused.
func unzipBounded(zipPath, dir string, maxBytes int64) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("the artifact is not a readable zip: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxZipEntries {
		return fmt.Errorf("the artifact has %d entries, over the %d the hub unpacks", len(zr.File), maxZipEntries)
	}
	var total int64
	for _, e := range zr.File {
		if !safeEntryName(e.Name) {
			return fmt.Errorf("the artifact has an entry named %q, outside its own folder, so it was refused", e.Name)
		}
		name := filepath.FromSlash(e.Name)
		target := filepath.Join(dir, name)
		switch {
		case e.FileInfo().IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case !e.Mode().IsRegular():
			return fmt.Errorf("the artifact has %q, which is not a plain file, so it was refused", e.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := e.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			in.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(in, maxBytes-total+1))
		in.Close()
		out.Close()
		total += n
		if total > maxBytes {
			return fmt.Errorf("the artifact is over the %d byte cap once unpacked, so it was stopped and removed", maxBytes)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// PruneDownloads removes the downloads under dest older than maxAge, then the oldest of the rest until the folder holds
// at most maxBytes. A download is a folder `<host>/<owner>/<repo>/<run>/<name>`. One being written is never taken.
func PruneDownloads(dest string, maxAge time.Duration, maxBytes int64) {
	type dl struct {
		path string
		mod  time.Time
		size int64
	}
	var all []dl
	var total int64
	now := time.Now()
	for _, d := range globDepth(dest, 5) {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			continue
		}
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge && removeUnused(d) {
			continue
		}
		if strings.Contains(filepath.Base(d), tmpMark) {
			continue
		}
		var size int64
		_ = filepath.WalkDir(d, func(_ string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				if i, err := e.Info(); err == nil {
					size += i.Size()
				}
			}
			return nil
		})
		all = append(all, dl{d, info.ModTime(), size})
		total += size
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, d := range all {
		if total <= maxBytes {
			break
		}
		if removeUnused(d.path) {
			total -= d.size
		}
	}
}

// globDepth lists the folders exactly depth levels under root.
func globDepth(root string, depth int) []string {
	level := []string{root}
	for i := 0; i < depth; i++ {
		var next []string
		for _, p := range level {
			ents, err := os.ReadDir(p)
			if err != nil {
				continue
			}
			for _, e := range ents {
				if e.IsDir() {
					next = append(next, filepath.Join(p, e.Name()))
				}
			}
		}
		level = next
	}
	return level
}

var reservedName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])$`)

// safeEntryName says whether a zip entry's name is a plain relative path, JUDGED THE SAME ON EVERY OS, since the hub
// may run on Windows and a zip made anywhere unpacks there. No colon (a drive or a stream), no backslash, no control
// character, no empty, `.` or `..` part, no part ending in a dot or a space, and no Windows reserved device name with
// or without an extension.
func safeEntryName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, ":\\") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ") {
			return false
		}
		for _, r := range p {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
		if reservedName.MatchString(strings.SplitN(p, ".", 2)[0]) {
			return false
		}
	}
	return true
}
