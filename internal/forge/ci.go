package forge

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CI runs, jobs, logs and artifacts, read through the forge's CLI on the hub. Every call is read only. A room never
// runs a forge CLI, so these are answered by the hub: see link/forgeroute.go (the route) and the control tool
// `atrium_ci` (link/ci_door.go).

// The hub's CI route, on the link's git kind next to the pull request ones.
const HubCIPath = "/_forge/ci"

// The CI actions, as `atrium_ci` and the route name them.
const (
	CIRuns      = "runs"
	CIRun       = "run"
	CILogs      = "log"
	CIArtifacts = "artifacts"
	CIArtifact  = "artifact"
)

// The bounds. A log is read with them while it is read, not cut afterwards.
const (
	// DefaultTail is the lines of a log answered when the caller says none.
	DefaultTail = 400
	// MaxTail is the most lines a caller may ask for.
	MaxTail = 5000
	// LogBytes is the cap on a log's answer, whatever the lines say. The tail is kept.
	LogBytes = 64 << 10
	// DefaultRuns and MaxRuns bound a list of runs.
	DefaultRuns = 10
	MaxRuns     = 50
	// ArtifactBytes is the cap on an artifact the hub downloads, and on what it answers of one of its files.
	ArtifactBytes = 200 << 20
	// FileBytes is the cap on one file of an artifact read back to a room.
	FileBytes = 64 << 10
	// TailMarker begins the line a bounded read puts first when it dropped earlier lines.
	TailMarker = "[atrium: "
)

// CIRunInfo is one workflow run.
type CIRunInfo struct {
	ID         int64  `json:"id"`
	Workflow   string `json:"workflow"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Head       string `json:"head_sha"`
	Branch     string `json:"branch"`
	Created    string `json:"created"`
	URL        string `json:"url"`
}

// CIStep is one step of a job.
type CIStep struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// CIJob is one job of a run, with its steps.
type CIJob struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	Conclusion string   `json:"conclusion"`
	URL        string   `json:"url"`
	Started    string   `json:"started,omitempty"`
	Completed  string   `json:"completed,omitempty"`
	Steps      []CIStep `json:"steps"`
}

// CIRunDetail is a run and its jobs.
type CIRunDetail struct {
	CIRunInfo
	Jobs []CIJob `json:"jobs"`
}

// CILog is a bounded log. Truncated says earlier lines were dropped, so what is here is the end of it.
type CILog struct {
	JobID      int64  `json:"job_id,omitempty"`
	RunID      int64  `json:"run_id,omitempty"`
	FailedOnly bool   `json:"failed_only"`
	Lines      int    `json:"lines"`
	Truncated  bool   `json:"truncated"`
	Text       string `json:"text"`
}

// CIArtifactInfo is one artifact of a run.
type CIArtifactInfo struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Size    int64  `json:"size_bytes"`
	Expired bool   `json:"expired"`
	Created string `json:"created,omitempty"`
}

// CIFile is one file of a downloaded artifact.
type CIFile struct {
	Path string `json:"path"`
	Size int64  `json:"size_bytes"`
}

// CIDownload is an artifact on the hub's disk. Dir is a path on the hub machine. Text, when a file was asked for, is
// that file's end, bounded.
type CIDownload struct {
	Name      string   `json:"name"`
	Dir       string   `json:"dir"`
	Bytes     int64    `json:"size_bytes"`
	Files     []CIFile `json:"files"`
	More      bool     `json:"more_files,omitempty"`
	File      string   `json:"file,omitempty"`
	Text      string   `json:"text,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Note      string   `json:"note"`
}

// RunQuery filters a list of runs.
type RunQuery struct {
	Branch string
	SHA    string
	Limit  int
}

// LogQuery names a log: a job's, or the failed jobs of a run.
type LogQuery struct {
	JobID      int64
	RunID      int64
	FailedOnly bool
	Tail       int
}

// CIReader is a forge that can say what its CI did. Dest is the hub's folder an artifact is downloaded under.
type CIReader interface {
	Runs(ctx context.Context, ref Ref, q RunQuery) ([]CIRunInfo, error)
	RunDetail(ctx context.Context, ref Ref, id int64) (*CIRunDetail, error)
	Log(ctx context.Context, ref Ref, q LogQuery) (*CILog, error)
	Artifacts(ctx context.Context, ref Ref, runID int64) ([]CIArtifactInfo, error)
	Download(ctx context.Context, ref Ref, runID int64, name, dest string, maxBytes int64) (*CIDownload, error)
}

// NotSupportedError is a forge that has no equivalent of the question. Code is `not_supported`.
type NotSupportedError struct {
	Kind string
}

func (e *NotSupportedError) Code() string { return "not_supported" }

func (e *NotSupportedError) Error() string {
	return fmt.Sprintf("not supported on %s: atrium reads CI runs, logs and artifacts through GitHub's gh only. "+
		"Read the pipeline in the %s web page, or ask the operator to", e.Kind, e.Kind)
}

// CIOf is the CI reader of a forge, or the NotSupportedError that says it has none.
func CIOf(f Forge) (CIReader, error) {
	if r, ok := f.(CIReader); ok {
		return r, nil
	}
	return nil, &NotSupportedError{Kind: f.Kind()}
}

// ClampTail is the lines a read keeps: the default for none, the maximum for too many.
func ClampTail(n int) int {
	switch {
	case n <= 0:
		return DefaultTail
	case n > MaxTail:
		return MaxTail
	}
	return n
}

// TailWriter keeps the last Lines lines of what is written to it, within Bytes, WHILE IT IS WRITTEN: what it holds
// never grows past the two bounds, however much is written. Dropped says how many earlier lines were let go.
type TailWriter struct {
	Lines int
	Bytes int

	lines   []string
	size    int
	partial []byte
	dropped int
}

func (t *TailWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			t.partial = append(t.partial, p...)
			if t.Bytes > 0 && len(t.partial) > t.Bytes {
				t.partial = append(t.partial[:0], t.partial[len(t.partial)-t.Bytes:]...)
			}
			break
		}
		t.partial = append(t.partial, p[:i]...)
		t.push(string(bytes.TrimSuffix(t.partial, []byte("\r"))))
		t.partial = t.partial[:0]
		p = p[i+1:]
	}
	return n, nil
}

func (t *TailWriter) push(line string) {
	if t.Bytes > 0 && len(line) > t.Bytes {
		line = line[len(line)-t.Bytes:]
	}
	t.lines = append(t.lines, line)
	t.size += len(line) + 1
	for len(t.lines) > 1 && ((t.Lines > 0 && len(t.lines) > t.Lines) || (t.Bytes > 0 && t.size > t.Bytes)) {
		t.size -= len(t.lines[0]) + 1
		t.lines = t.lines[1:]
		t.dropped++
	}
	// Let the dropped head be collected.
	if cap(t.lines) > 4*len(t.lines)+64 {
		t.lines = append([]string(nil), t.lines...)
	}
}

// Dropped is the number of earlier lines let go.
func (t *TailWriter) Dropped() int { return t.dropped }

// Result is the kept lines, with a first line saying how many were dropped when some were.
func (t *TailWriter) Result() (text string, kept int, dropped int) {
	lines := t.lines
	if len(t.partial) > 0 {
		lines = append(append([]string(nil), lines...), string(t.partial))
	}
	var b strings.Builder
	if t.dropped > 0 {
		b.WriteString(TailMarker + strconv.Itoa(t.dropped) + " earlier lines left out]\n")
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String(), len(lines), t.dropped
}

// Tail bounds text already in memory the way TailWriter bounds a stream, for a runner that does not.
func Tail(text string, lines, maxBytes int) (out string, kept int, truncated bool) {
	if strings.HasPrefix(text, TailMarker) {
		truncated = true
	}
	w := &TailWriter{Lines: lines, Bytes: maxBytes}
	_, _ = w.Write([]byte(text))
	out, kept, dropped := w.Result()
	return out, kept, truncated || dropped > 0
}

// HubCIAsk is a CI question for the hub. Action is one of the CI constants. Run answers `run`, `log` of a run,
// `artifacts` and `artifact`. File, with `artifact`, asks for the end of one file in it.
type HubCIAsk struct {
	Host       string `json:"host"`
	Org        string `json:"org"`
	Repo       string `json:"repo"`
	Action     string `json:"action"`
	Branch     string `json:"branch,omitempty"`
	SHA        string `json:"sha,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	RunID      int64  `json:"run_id,omitempty"`
	JobID      int64  `json:"job_id,omitempty"`
	FailedOnly *bool  `json:"failed_only,omitempty"`
	Tail       int    `json:"tail,omitempty"`
	Name       string `json:"name,omitempty"`
	File       string `json:"file,omitempty"`
}

// HubCI is the hub's answer. One field is set, by the action.
type HubCI struct {
	Kind      string           `json:"kind"`
	Runs      []CIRunInfo      `json:"runs,omitempty"`
	Run       *CIRunDetail     `json:"run,omitempty"`
	Log       *CILog           `json:"log,omitempty"`
	Artifacts []CIArtifactInfo `json:"artifacts,omitempty"`
	Download  *CIDownload      `json:"download,omitempty"`
}

// CI asks the hub a CI question over the link.
func (r *Remote) CI(ctx context.Context, in HubCIAsk) (HubCI, error) {
	var out HubCI
	err := r.Call(ctx, HubCIPath, in, &out)
	return out, err
}
