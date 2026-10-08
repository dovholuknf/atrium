package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/forge"
)

// THE HUB READS CI FOR ITS ROOMS: runs, jobs and steps, failed-job logs and artifacts, all read only, through the
// forge CLI on the hub (gh), which no room runs.
//
//	POST /_forge/ci     (a room, on the git kind)  forge.HubCIAsk   asked by a room's `atrium_ci` over the link
//	POST /_hub/forge/ci                            forge.HubCIAsk   asked by the hub's own control MCP
//
// Both answer forge.HubCI. A missing or expired login answers the one command to run on the hub, as a pull request
// question does (forgeRefusal). Bitbucket answers `not_supported`. See ci_door.go for the tool.

// SetCIDir is the hub's folder an artifact is downloaded under. Unset, it is a folder in the hub machine's temp.
func (p *Proxy) SetCIDir(dir string) {
	p.mu.Lock()
	p.ciDir = dir
	p.mu.Unlock()
}

func (p *Proxy) ciRoot() string {
	p.mu.Lock()
	dir := p.ciDir
	p.mu.Unlock()
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "atrium-ci-artifacts")
	}
	return dir
}

func ciValid(a forge.HubCIAsk) string {
	if why := validAsk(forge.HubAsk{Host: a.Host, Org: a.Org, Repo: a.Repo}, false); why != "" {
		return why
	}
	switch a.Action {
	case forge.CIRuns:
	case forge.CIRun, forge.CIArtifacts:
		if a.RunID <= 0 {
			return "say the run id (run_id)"
		}
	case forge.CILogs:
		if a.RunID <= 0 && a.JobID <= 0 {
			return "say job_id or run_id"
		}
	case forge.CIArtifact:
		if a.RunID <= 0 || strings.TrimSpace(a.Name) == "" {
			return "say the run id (run_id) and the artifact's name"
		}
	default:
		return "action is one of runs, run, log, artifacts, artifact"
	}
	return ""
}

// forgeCI runs one CI question.
func (p *Proxy) forgeCI(ctx context.Context, f *hubForge, ask forge.HubCIAsk) (forge.HubCI, error) {
	var ans forge.HubCI
	fg, err := f.forgeFor(strings.ToLower(ask.Host))
	if err != nil {
		return ans, err
	}
	ans.Kind = fg.Kind()
	wrap := func(err error) error {
		if err == nil {
			return nil
		}
		return &forgeAccess{kind: ans.Kind, err: err}
	}
	rd, err := forge.CIOf(fg)
	if err != nil {
		return ans, err
	}
	ref := forge.Ref{Host: strings.ToLower(ask.Host), Org: ask.Org, Repo: ask.Repo}
	switch ask.Action {
	case forge.CIRuns:
		ans.Runs, err = rd.Runs(ctx, ref, forge.RunQuery{Branch: ask.Branch, SHA: ask.SHA, Limit: ask.Limit})
	case forge.CIRun:
		ans.Run, err = rd.RunDetail(ctx, ref, ask.RunID)
	case forge.CILogs:
		// A whole run's log is huge, so a run is read failed-only unless the caller says otherwise. A job is read whole.
		failed := ask.JobID <= 0
		if ask.FailedOnly != nil {
			failed = *ask.FailedOnly
		}
		ans.Log, err = rd.Log(ctx, ref, forge.LogQuery{JobID: ask.JobID, RunID: ask.RunID, FailedOnly: failed,
			Tail: ask.Tail})
	case forge.CIArtifacts:
		ans.Artifacts, err = rd.Artifacts(ctx, ref, ask.RunID)
		if ans.Artifacts == nil && err == nil {
			ans.Artifacts = []forge.CIArtifactInfo{}
		}
	case forge.CIArtifact:
		ans.Download, err = rd.Download(ctx, ref, ask.RunID, strings.TrimSpace(ask.Name), p.ciRoot(), forge.ArtifactBytes)
		if err == nil {
			if ask.File != "" {
				err = forge.ReadFile(ans.Download, ask.File, ask.Tail)
			}
			ans.Download.Note = ciDownloadNote(ans.Download, ask.File != "")
		}
	}
	if _, ok := err.(*forge.NotSupportedError); ok {
		return ans, err
	}
	return ans, wrap(err)
}

func ciDownloadNote(d *forge.CIDownload, read bool) string {
	if d.Note != "" {
		return d.Note
	}
	n := fmt.Sprintf("the artifact is on the hub machine at %s (%d bytes, %d files). A room cannot read the hub's disk: "+
		"ask for one of its files with `file`, which answers its last lines", d.Dir, d.Bytes, len(d.Files))
	if read {
		n = fmt.Sprintf("the artifact is on the hub machine at %s. `text` is the end of %s", d.Dir, d.File)
	}
	return n
}

// ciFail answers a CI question's failure the way a pull request question's is: the hub's sentence, and the alert.
func (p *Proxy) ciFail(w http.ResponseWriter, f *hubForge, room string, ask forge.HubCIAsk, err error) {
	code, he := p.forgeRefusal(f, room, forge.HubAsk{Host: strings.ToLower(ask.Host), Org: ask.Org, Repo: ask.Repo}, err)
	forgeFail(w, code, he)
}

// serveCI answers a CI question: the room's, on the link, or the hub's own control MCP's.
func (p *Proxy) serveCI(w http.ResponseWriter, r *http.Request, f *hubForge, room string) {
	var ask forge.HubCIAsk
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&ask); err != nil {
		forgeFail(w, http.StatusBadRequest, &forge.HubError{Code: forge.CodeOther, Message: "that is not a CI question"})
		return
	}
	if why := ciValid(ask); why != "" {
		forgeFail(w, http.StatusBadRequest, &forge.HubError{Code: forge.CodeOther, Message: why})
		return
	}
	ans, err := p.forgeCI(r.Context(), f, ask)
	if err != nil {
		p.ciFail(w, f, room, ask, err)
		return
	}
	f.worked(p, ans.Kind, strings.ToLower(ask.Host))
	crJSON(w, http.StatusOK, ans)
}
