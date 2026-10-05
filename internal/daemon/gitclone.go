package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// atrium_git_clone, room side: `POST /v1/tasks/{id}/git/clone {url}`. The clone, its path rules and
// its remotes are gitsync.SCM. This file is the room's settings, the agent address the `hub` remote
// points at, and the ONE YES for a clone the operator made.
//
// THE YES IS A PERMISSION ROW on the asking card, `atrium_git_clone`, so the operator answers it
// where they answer everything else and no new request type exists. It is keyed exactly, so an
// answer holds for as long as the row does (the 2-minute replay window would ask again). The tool
// call returns at once with "asked"; the card calls again after the operator has answered.

// cloneTimeout bounds one request. The runner bounds each git command on its own.
const cloneTimeout = 12 * time.Minute

func (d *Daemon) scm(task *store.Task) *gitsync.SCM {
	setting := func(key string) func() string {
		return func() string {
			v, _ := d.st.Setting(key)
			return v
		}
	}
	agent := d.agentAddr()
	var from func(ctx context.Context, r gitsync.Ref) (string, func(), error)
	if d.HubForge() != nil {
		from = d.hubClone
	}
	return &gitsync.SCM{
		From:             from,
		Runner:           gitsync.Default,
		Root:             setting(gitsync.SettingSCMRoot),
		CredentialHelper: setting(gitsync.SettingCredentialHelper),
		CredentialHosts:  setting(gitsync.SettingCredentialHosts),
		Yes:              func(ctx context.Context, clone string) error { return d.askToAdopt(task, clone) },
		HubURL: func(r gitsync.Ref) (string, error) {
			if agent == "" {
				return "", errors.New("this room has no agent listener to point the hub remote at")
			}
			return gitsync.StableHubURL(agent, r), nil
		},
	}
}

// askToAdopt is the one yes, as a permission on the card that asked.
func (d *Daemon) askToAdopt(task *store.Task, clone string) error {
	p, decided, err := d.st.RecordPermission(task.ID, "atrium_git_clone",
		"add atrium's remotes to the clone at "+clone,
		store.ExactKey("scm-adopt:"+clone),
		"This clone was made by you, not by atrium. Yes adds a `hub` remote (atrium's, `atrium-hub` if "+
			"`hub` is taken) and sets origin's push URL so nothing can push to the forge from it. "+
			"Asked once for this clone.")
	if err != nil {
		return err
	}
	if !decided {
		d.publishTask(task.ID)
		d.ap.Broadcast("permission", p)
		return gitsync.ErrAsked
	}
	if p.Decision != "approve" {
		return gitsync.ErrDenied
	}
	return nil
}

func (d *Daemon) handleGitClone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cloneTimeout)
	defer cancel()
	res, err := d.scm(task).Clone(ctx, in.URL)
	if err != nil {
		code := http.StatusBadRequest
		switch {
		case errors.Is(err, gitsync.ErrAsked):
			// Not a failure: the question is on the board.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "asked", "note": err.Error()})
			return
		case errors.Is(err, gitsync.ErrDenied):
			code = http.StatusForbidden
		case errors.Is(err, gitsync.ErrStopped):
			code = http.StatusServiceUnavailable
		}
		writeJSONErr(w, code, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// errPRCloneAdopt is what a PR worktree answers for a clone the operator made and has not said yes to. There is no
// card to ask on, so the one yes comes from a card's atrium_git_clone.
var errPRCloneAdopt = errors.New("the clone of this repo in the scm folder was made by the operator and atrium has " +
	"not been given a yes to add its remotes to it. Run atrium_git_clone for it from a card, answer the question " +
	"on the board, and ask again")

// prWorktreeClone is the scm clone path for a pull request's worktree: the clone with its `hub` remote and `origin`
// push guarded, the same as atrium_git_clone. A clone the operator made is not touched without the yes.
func (d *Daemon) prWorktreeClone(ctx context.Context, url string) (gitsync.SCMResult, error) {
	s := d.scm(nil)
	s.Yes = func(context.Context, string) error { return errPRCloneAdopt }
	return s.Clone(ctx, url)
}
