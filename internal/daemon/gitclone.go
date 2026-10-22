package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
//
// A pasted link asks nothing: see prWorktreeClone, which adopts the operator's clone itself.

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
		From:   from,
		Runner: gitsync.Default,
		Root: func() string {
			v, _ := d.st.Setting(gitsync.SettingSCMRoot)
			return gitsync.EffectiveSCMRoot(v)
		},
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

// cloneFromHub is the launch dialog's "clone from hub": the scm clone, refused when this room has no hub, because
// without one the clone would read the forge and a room never does.
func (d *Daemon) cloneFromHub(ctx context.Context, url string) (gitsync.SCMResult, error) {
	if d.HubForge() == nil {
		return gitsync.SCMResult{}, errors.New("this room has no hub to clone from, and a room never reads a forge")
	}
	return d.prWorktreeClone(ctx, url)
}

// prWorktreeHas is whether the scm folder already holds the clone a PR worktree would hang off. It makes nothing.
func (d *Daemon) prWorktreeHas(url string) bool {
	ref, err := gitsync.ParseURL(url)
	if err != nil {
		return false
	}
	_, dest, err := d.scm(nil).ClonePath(ref)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dest, ".git"))
	return err == nil
}

// prWorktreeClone is the scm clone path for a pull request's worktree: the clone with its `hub` remote, the same as
// atrium_git_clone.
//
// A CLONE THE OPERATOR MADE IS ADOPTED WITHOUT A QUESTION, the way gwt hangs its worktrees off whatever clone is
// there. A pasted link has no card to ask on, and a question with nowhere to answer it was a dead end that sent the
// operator to run atrium_git_clone from some other card. The adoption is recorded in the clone itself
// (`atrium.adopted=yes`, which atrium_git_clone reads as the yes already given) and in the room's log.
func (d *Daemon) prWorktreeClone(ctx context.Context, url string) (gitsync.SCMResult, error) {
	s := d.scm(nil)
	s.Yes = func(_ context.Context, clone string) error {
		log.Printf("[atrium scm] adopting the operator's clone at %s for a pasted link: atrium's hub remote goes in", filepath.ToSlash(clone))
		return nil
	}
	return s.Clone(ctx, url)
}
