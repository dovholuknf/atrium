package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	if d, err := scanDir(dir, maxBytes); err == nil && len(d.Files) > 0 {
		d.Name = name
		return d, nil
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("the hub could not make %s: %w", dir, err)
	}
	_, err = g.run(ctx, Cmd{Name: g.cmd, Timeout: ghDownloadTimeout, Limit: 1 << 20,
		Args: []string{"run", "download", strconv.FormatInt(runID, 10), "--repo", repoArg(ref), "--name", name,
			"--dir", dir}})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, access(g.cmd, host(ref), err)
	}
	d, err := scanDir(dir, maxBytes)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	d.Name = name
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
