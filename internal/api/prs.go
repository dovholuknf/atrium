package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// The pulls routes: starting a review of a pull request and reading the index.
//
// The contract is docs/review/pulls-api.md and it does not change incompatibly,
// because the pulls view is built against it. None of this makes a card, and a
// board pause does not apply: a review somebody asked for is not a director's
// work.

// PRRunner runs a review. The daemon's runner replaces the stub that stands
// here until one is built.
//
// Start hands a queued row to the runner and returns once the row is in the
// state the runner left it. Abort stops a run that is going. Neither returns an
// error: a runner that cannot do its job writes `failed` and a reason on the
// row, which is where the person looks.
type PRRunner interface {
	Start(id string)
	Abort(id string)
}

// unbuiltPRRunner fails every run at once and says why on the row.
type unbuiltPRRunner struct{ st *store.Store }

func (u unbuiltPRRunner) Start(id string) {
	if _, err := u.st.SetPRState(id, store.PRFailed, "", "runner: runner not built"); err != nil {
		log.Printf("[atrium api] pr %s: %v", id, err)
	}
}

func (u unbuiltPRRunner) Abort(string) {}

func (s *Server) prRunner() PRRunner {
	if s.PRRunner != nil {
		return s.PRRunner
	}
	return unbuiltPRRunner{s.st}
}

// prOpMu serialises the operations that start, restart or stop a run. Each is
// short: Start returns once the row is in the state the runner left it. The
// conditional UPDATEs in the store are what refuse a second caller, and this lock
// is what makes the row the runner moved visible to that caller's check.
var prOpMu sync.Mutex

// prView is a row with the counts that are read from its run folder.
type prView struct {
	*store.PRReview
	Findings PRFindingCounts `json:"findings"`
	Walk     PRWalkCounts    `json:"walk"`
	// DiskBytes is the walker card's measured inventory: the worktree and the run folder. See resources.go.
	DiskBytes int64 `json:"disk_bytes,omitempty"`
}

// viewPRDisk is viewPR with the walker card's disk.
func (s *Server) viewPRDisk(p *store.PRReview) prView {
	v := viewPR(p)
	if p.WalkerTask != "" {
		v.DiskBytes = s.cardDisk(p.WalkerTask)
	}
	return v
}

// viewPR reads the run folder as it is now. The counts are zero until the row is
// ready, since a run that is still writing has findings that are not final.
func viewPR(p *store.PRReview) prView {
	v := prView{PRReview: p}
	if p.State == store.PRReady {
		v.Findings, v.Walk = readPRCounts(p.RunDir)
	}
	return v
}

// PublishPR sends a row's current state on the event stream. The runner calls it
// as steps finish.
func (s *Server) PublishPR(id string) {
	p, err := s.st.PRByID(id)
	if err != nil {
		return
	}
	s.Broadcast("pr", map[string]any{"pr": s.viewPRDisk(p)})
}

func prError(w http.ResponseWriter, status int, code, msg string, extra map[string]any) {
	body := map[string]any{"error": msg, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// prFail answers a store error: a missing row is the pulls 404, anything else is
// the halt or a 500.
func (s *Server) prFail(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		prError(w, http.StatusNotFound, "not_found", "no such pr", nil)
		return
	}
	s.fail(w, err)
}

// prAfterStart reads the row back, publishes it and answers.
func (s *Server) prAnswer(w http.ResponseWriter, status int, id string, extra map[string]any) {
	p, err := s.st.PRByID(id)
	if err != nil {
		s.prFail(w, err)
		return
	}
	s.Broadcast("pr", map[string]any{"pr": s.viewPRDisk(p)})
	body := map[string]any{"pr": s.viewPRDisk(p)}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, status, body)
}

// POST /v1/prs
func (s *Server) postPR(w http.ResponseWriter, r *http.Request) {
	if s.Recognise == nil {
		prError(w, http.StatusNotImplemented, "not_wired", "no daemon wired", nil)
		return
	}
	var body struct {
		URL  string `json:"url"`
		Why  string `json:"why"`
		Head string `json:"head"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		prError(w, http.StatusBadRequest, "bad_request", "the body is not json: "+err.Error(), nil)
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	body.Head = strings.TrimSpace(body.Head)
	switch {
	case body.URL == "":
		prError(w, http.StatusBadRequest, "bad_request", "url is required", nil)
		return
	case !store.ValidPRHead(body.Head):
		prError(w, http.StatusBadRequest, "bad_request", "head is not a commit hash", nil)
		return
	case len(body.Why) > store.MaxPRWhy:
		prError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("why is over %d characters", store.MaxPRWhy), nil)
		return
	}
	got, err := s.Recognise(body.URL)
	if errors.Is(err, store.ErrNoRecogniser) {
		prError(w, http.StatusUnprocessableEntity, "no_recogniser", "no recogniser matches this", nil)
		return
	}
	if err != nil {
		// A halted store inside the recogniser is the halt, not a bad url.
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		prError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	host, org, repo := got.Vars["host"], got.Vars["org"], got.Vars["repo"]
	num, _ := strconv.Atoi(strings.TrimSpace(got.Vars["num"]))
	if host == "" || org == "" || repo == "" || num <= 0 {
		prError(w, http.StatusUnprocessableEntity, "not_a_pr",
			"that url matched a recogniser, but it is not a pull request: it has to capture host, org, repo and num", nil)
		return
	}
	// ASK THE HUB BEFORE MAKING A ROW, and not under prOpMu: the hub may hand the paste to another room. A room that
	// already has a live row for the PR says so, and is recorded as its owner. See prclaim.go.
	claim := ""
	if s.ClaimPR != nil {
		// The open verb was placed here by the hub already, so this room is the owner unless another holds the key.
		held := r.Context().Value(openHeldKey{}) != nil
		if !held && body.Head == "" {
			if live, err := s.st.LivePR(host, org, repo, num); err == nil && live != nil {
				held = true
			}
		}
		var answered bool
		claim, answered = s.claimPR(w, r, PRClaimAsk{Key: store.PRKey(host, org, repo, num), URL: body.URL,
			Why: body.Why, Head: body.Head, Held: held})
		if answered {
			return
		}
	}
	prOpMu.Lock()
	defer prOpMu.Unlock()
	// With no head the folder is named `pending` and the fetch step moves it, so a
	// second paste would name a folder no row has. The pull request is the identity
	// while a review of it is under way.
	if body.Head == "" {
		live, err := s.st.LivePR(host, org, repo, num)
		if err != nil {
			s.prFail(w, err)
			return
		}
		if live != nil {
			s.prAnswer(w, http.StatusOK, live.ID, map[string]any{"created": false})
			return
		}
	}
	// The folder is only named here. It is made after the row is, so a refused
	// create leaves nothing behind.
	root := s.st.ReviewsRoot()
	named, err := store.RunFolderPath(root, host, org, repo, num, body.Head)
	if err != nil {
		prError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	row, created, err := s.st.CreatePR(store.NewPR{URL: body.URL, Why: body.Why, Host: host, Org: org,
		Repo: repo, Number: num, Head: body.Head, RunDir: named, Claim: claim})
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		prError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	if created || row.State == store.PRFailed || row.State == store.PRAborted {
		if _, err := s.st.RunFolderOn(host, org, repo, num, body.Head); err != nil {
			s.st.SetPRState(row.ID, store.PRFailed, "", "folder: "+err.Error())
			s.prAnswer(w, http.StatusInternalServerError, row.ID, map[string]any{"created": created})
			return
		}
	}
	if created {
		s.prRunner().Start(row.ID)
		s.prAnswer(w, http.StatusCreated, row.ID, map[string]any{"created": true})
		return
	}
	if row.State == store.PRFailed || row.State == store.PRAborted {
		switch _, err := s.st.ResetPR(row.ID, named, store.PRFailed, store.PRAborted); {
		case err == nil:
			s.prRunner().Start(row.ID)
		case errors.Is(err, store.ErrPRState):
			// Somebody else retried it first. Theirs is the run.
		default:
			s.prFail(w, err)
			return
		}
	}
	s.prAnswer(w, http.StatusOK, row.ID, map[string]any{"created": false})
}

// GET /v1/prs
func (s *Server) listPRs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.PRFilter{OrgRepo: q.Get("org_repo"), Archived: q.Get("archived") == "1"}
	for _, st := range q["state"] {
		ok := false
		for _, v := range store.PRStates {
			ok = ok || v == st
		}
		if !ok {
			prError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("%q is not a review state", st), nil)
			return
		}
		f.States = append(f.States, st)
	}
	rows, err := s.st.PRs(f)
	if err != nil {
		s.prFail(w, err)
		return
	}
	counts, err := s.st.PRCounts()
	if err != nil {
		s.prFail(w, err)
		return
	}
	views := make([]prView, 0, len(rows))
	for _, p := range rows {
		views = append(views, s.viewPRDisk(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"prs": views, "counts": counts, "nav_count": counts[store.PRReady] + counts[store.PRFailed],
	})
}

// GET /v1/prs/{id}
func (s *Server) getPR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pr": s.viewPRDisk(p), "run_log": tailLog(s.st.ReviewsRoot(), p.RunDir, 8<<10)})
}

// POST /v1/prs/{id}/retry
func (s *Server) retryPR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	prOpMu.Lock()
	defer prOpMu.Unlock()
	dir := p.RunDir
	if p.State == store.PRAborted {
		// An abort deleted the folder, so a retry makes a new one.
		if dir, err = store.RunFolderPath(s.st.ReviewsRoot(), p.Host, p.Org, p.Repo, p.Number, p.Head); err != nil {
			s.fail(w, err)
			return
		}
	}
	// The state check and the reset are one statement. A second retry of the same
	// row finds it queued by then and gets the 409.
	if _, err := s.st.ResetPR(p.ID, dir, store.PRFailed, store.PRAborted); err != nil {
		s.prStateConflict(w, p.ID, err, "not_retryable", "only a failed or aborted row can be retried")
		return
	}
	if _, err := s.st.RunFolderOn(p.Host, p.Org, p.Repo, p.Number, p.Head); err != nil {
		s.st.SetPRState(p.ID, store.PRFailed, "", "folder: "+err.Error())
		s.prAnswer(w, http.StatusInternalServerError, p.ID, nil)
		return
	}
	s.prRunner().Start(p.ID)
	s.prAnswer(w, http.StatusAccepted, p.ID, nil)
}

// prStateConflict answers the store's refusal to move a row that was in the wrong
// state as a 409 carrying the state it is in, and anything else as the usual
// failure.
func (s *Server) prStateConflict(w http.ResponseWriter, id string, err error, code, msg string) {
	if !errors.Is(err, store.ErrPRState) {
		s.prFail(w, err)
		return
	}
	state := ""
	if now, gerr := s.st.PRByID(id); gerr == nil {
		state = now.State
	}
	prError(w, http.StatusConflict, code, msg, map[string]any{"state": state})
}

// POST /v1/prs/{id}/start
func (s *Server) startPR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	prOpMu.Lock()
	defer prOpMu.Unlock()
	// Read again under the lock: Start moves the row out of queued before it
	// returns, so a second click that waited here sees that and gets the 409.
	if p, err = s.st.PRByID(p.ID); err != nil {
		s.prFail(w, err)
		return
	}
	if p.State != store.PRQueued {
		prError(w, http.StatusConflict, "not_startable", "only a queued row can be started",
			map[string]any{"state": p.State})
		return
	}
	s.prRunner().Start(p.ID)
	s.prAnswer(w, http.StatusAccepted, p.ID, nil)
}

// POST /v1/prs/{id}/abort
func (s *Server) abortPR(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.PRByID(r.PathValue("id"))
	if err != nil {
		s.prFail(w, err)
		return
	}
	prOpMu.Lock()
	defer prOpMu.Unlock()
	// The row is claimed first, in one statement that names the states it may be
	// in. A run that finished between the read and here is ready, and is refused.
	if _, err := s.st.MovePR(p.ID, []string{store.PRQueued, store.PRFetching, store.PRRunning},
		store.PRAborted, "", ""); err != nil {
		s.prStateConflict(w, p.ID, err, "not_abortable", "only a queued, fetching or running row can be aborted")
		return
	}
	s.prRunner().Abort(p.ID)
	s.removeRunFolder(p)
	s.prAnswer(w, http.StatusOK, p.ID, nil)
}

// removeRunFolder deletes a run's folder (standing rule 44), and only when it is
// inside the reviews root. A row whose run_dir points anywhere else is left
// alone: deleting is the one thing here that cannot be undone.
func (s *Server) removeRunFolder(p *store.PRReview) {
	if p.RunDir == "" {
		return
	}
	root := filepath.FromSlash(s.st.ReviewsRoot())
	dir, err := safepath.Contained(root, filepath.FromSlash(p.RunDir))
	if err != nil {
		log.Printf("[atrium api] pr %s: not deleting %s: outside the reviews root", p.ID, p.RunDir)
		return
	}
	if rootAbs, err := filepath.Abs(root); err == nil && dir == rootAbs {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("[atrium api] pr %s: deleting %s: %v", p.ID, dir, err)
	}
}
