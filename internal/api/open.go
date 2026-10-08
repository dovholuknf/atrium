package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// OPEN A LINK: one call from a pasted link to a live card in its own worktree. Design:
// docs/rnd/card-lifecycle-design.md, section 3. Item r-open-verb-pr.
//
//	POST /v1/open {url, why?, harness?}
//
// On a hub the same path is placed first: on the room that already holds the link, or the least busy one (see
// internal/link/openroute.go). Here, on the room, it is the whole chain the launch dialog used to run in the browser:
//
//  1. recognise the link. Nothing matching is 422 no_recogniser.
//  2. a pull request already under review whose walker card is live answers that card, created false.
//  3. the worktree, made or found (prWorktree). No provider row is needed.
//  4. the review row (POST /v1/prs, claimed for this room).
//  5. the card, launched in the worktree with the recogniser's title, prompt and tags.
//  6. the card set as the row's walker.
//
// A STEP THAT FAILS UNDOES THE STEPS BEFORE IT, newest first, and answers the failing step's sentence. A worktree it
// found and a row that was already there are not touched, only what this call made.
//
// That is a pull request. An issue, a branch and a support link take openOther (openkinds.go): a worktree or a
// scratch folder and a card, with no review row.

// openHeldKey marks the review row's claim as asked by the open verb. See postPR.
type openHeldKey struct{}

// openRequest is the link, and what the launch dialog let somebody change first (shift-enter): an empty field takes
// the recogniser's.
type openRequest struct {
	URL     string `json:"url"`
	Why     string `json:"why"`
	Harness string `json:"harness"`
	Title   string `json:"title"`
	Prompt  string `json:"prompt"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	// Repo is where a link that names no repo opens: "host/org/repo", "none" for a scratch folder, or empty for the
	// recogniser's default repo. Ignored for a link that names its own.
	Repo string `json:"repo"`
	// Progress is an id the board made up. Each step of the open is broadcast as an `open-progress` event carrying it,
	// so the board can name the step that is running. Empty says nothing.
	Progress string `json:"progress"`
}

// openAnswer is the card a link opened. On a hub `room` is filled and `card` and `pr` carry the room's tag. Kind is
// pr, issue, branch or support. Repo is "host/org/repo" of the worktree, "" for a scratch folder.
type openAnswer struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Recogniser string `json:"recogniser"`
	Card       string `json:"card"`
	PR         string `json:"pr,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	Repo       string `json:"repo"`
	Created    bool   `json:"created"`
	Title      string `json:"title,omitempty"`
}

// openFail is a refusal with the step it came from, so the board can say which part did not happen.
func openFail(w http.ResponseWriter, status int, step, code, msg string) {
	prError(w, status, code, msg, map[string]any{"step": step})
}

// POST /v1/open
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	if s.Recognise == nil || s.Launch == nil {
		openFail(w, http.StatusNotImplemented, "recognise", "not_wired", "no daemon wired")
		return
	}
	var in openRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		openFail(w, http.StatusBadRequest, "recognise", "bad_request", "the body is not json: "+err.Error())
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	if in.URL == "" {
		openFail(w, http.StatusBadRequest, "recognise", "bad_request", "url is required")
		return
	}
	if len(in.Why) > store.MaxPRWhy {
		openFail(w, http.StatusBadRequest, "recognise", "bad_request", fmt.Sprintf("why is over %d characters", store.MaxPRWhy))
		return
	}

	steps := s.newOpSteps(strings.TrimSpace(in.Progress), in.URL)
	defer steps.end()

	// 1. recognise
	steps.begin("recognise", "recognising the link")
	got, err := s.Recognise(in.URL)
	if errors.Is(err, store.ErrNoRecogniser) {
		openFail(w, http.StatusUnprocessableEntity, "recognise", "no_recogniser",
			"nothing here knows what that is yet. add a row for it under runners, recognisers.")
		return
	}
	if err != nil {
		openFail(w, http.StatusBadRequest, "recognise", "bad_request", err.Error())
		return
	}
	k := classify(got)
	switch k.kind {
	case linkPR:
	case linkIssue, linkBranch, linkSupport:
		s.openOther(w, r, in, got, k, steps)
		return
	default:
		openFail(w, http.StatusUnprocessableEntity, "recognise", "not_openable",
			"that link names no pull request, issue, branch or ticket, so there is no piece of work to open. "+
				"use the launch dialog for this one: it fills in what the recogniser knew.")
		return
	}
	host, org, repo, num := k.host, k.org, k.repo, k.num
	key := store.PRKey(host, org, repo, num)
	ans := openAnswer{Key: key, Kind: linkPR, Recogniser: got.Recogniser, Title: got.Title, Repo: host + "/" + org + "/" + repo}

	// 2. a live card for the link already
	if live, err := s.st.LivePR(host, org, repo, num); err == nil && live != nil {
		if t := s.liveWalker(live); t != nil {
			ans.Card, ans.PR, ans.Worktree = t.ID, live.ID, t.Worktree
			writeJSON(w, http.StatusOK, ans)
			return
		}
	}

	// 3. the worktree
	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeDeadline)
	defer cancel()
	wt, status, err := s.prWorktree(withOpSteps(ctx, steps), s.providerByHost(host), prWorktreeRequest{Host: host, Org: org, Repo: repo, Number: num})
	if err != nil {
		openFail(w, status, "worktree", "worktree_failed", err.Error())
		return
	}
	ans.Worktree = wt.Path

	// THE INVENTORY, held under a pending owner until the card exists, then handed to it. A worktree that was already
	// there is somebody else's and is not recorded. See resources.go.
	pending := "pending:" + key + ":" + strconv.FormatInt(time.Now().UnixNano(), 36)
	own := func(kind, ref, detail string) {
		if _, err := s.st.AddResource(pending, kind, ref, detail); err != nil {
			log.Printf("[atrium api] open %s: the inventory did not take %s %s: %v", key, kind, ref, err)
		}
	}
	disown := func() {
		rows, _ := s.st.Resources(pending)
		for _, r := range rows {
			_ = s.st.FreeResource(pending, r.Seq, "")
		}
	}
	if !wt.Existed {
		own(store.ResWorktree, wt.Path, wt.Repo)
		own(store.ResRef, prWorktreeRef(num), wt.Repo)
		if wt.CreatedBranch {
			own(store.ResBranch, wt.Branch, wt.Repo)
		}
	}

	// 4. the review row, claimed for this room
	steps.begin("review", "starting the review")
	row, created, failStatus, failBody := s.openRow(r, in)
	if row == nil {
		undoPRWorktree(wt)
		disown()
		if failBody == nil {
			failBody = map[string]any{"error": "the review row was not made", "code": "row_failed"}
		}
		failBody["step"] = "review"
		writeJSON(w, failStatus, failBody)
		return
	}
	ans.PR = row.ID
	if created {
		own(store.ResReview, row.ID, "")
	}
	undoRow := func() {
		if !created {
			return
		}
		prOpMu.Lock()
		if _, err := s.st.MovePR(row.ID, []string{store.PRQueued, store.PRFetching, store.PRRunning},
			store.PRAborted, "", "the card for it did not start"); err == nil {
			s.prRunner().Abort(row.ID)
		}
		prOpMu.Unlock()
		if p, err := s.st.PRByID(row.ID); err == nil {
			s.removeRunFolder(p)
		}
		if _, err := s.st.ArchivePR(row.ID); err != nil {
			log.Printf("[atrium api] open %s: the row %s did not archive: %v", key, row.ID, err)
		}
		s.PublishPR(row.ID)
	}
	// A row already there may have a live walker by now, made by a paste that raced this one.
	if !created {
		if t := s.liveWalker(row); t != nil {
			// A worktree this call made beside the live card's is not wanted.
			undoPRWorktree(wt)
			disown()
			ans.Card, ans.Worktree = t.ID, t.Worktree
			writeJSON(w, http.StatusOK, ans)
			return
		}
	}

	// 5. the card
	steps.begin("card", "starting the card")
	task, err := s.Launch(s.openLaunchBody(in, got, wt, key, org, repo, num))
	if err != nil {
		undoRow()
		undoPRWorktree(wt)
		disown()
		openFail(w, http.StatusBadRequest, "card", "launch_failed", "the card did not start: "+err.Error())
		return
	}
	s.PublishTask(task)
	ans.Card, ans.Created = task.ID, true

	// 6. the walker
	if _, err := s.st.SetPRWalker(row.ID, task.ID); err != nil {
		if s.Kill != nil {
			_ = s.Kill(task.ID)
		}
		undoRow()
		undoPRWorktree(wt)
		disown()
		openFail(w, http.StatusInternalServerError, "walker", "walker_failed", "the card is not tied to the review: "+err.Error())
		return
	}
	s.PublishPR(row.ID)
	if err := s.st.MoveResources(pending, task.ID); err != nil {
		log.Printf("[atrium api] open %s: the inventory was not handed to %s: %v", key, task.ID, err)
	}
	s.measureAfterOpen(task)
	writeJSON(w, http.StatusCreated, ans)
}

// openMeasureWait is how long the measure after an open may take.
const openMeasureWait = 5 * time.Second

// measureAfterOpen counts the disk the open made once the answer has gone, so the card starts without waiting for a
// walk of the tree. A tree too big for the budget is counted as far as the walk got, and the card's measure button
// counts the rest. The card is published again when the count is in.
func (s *Server) measureAfterOpen(task *store.Task) {
	go func() {
		mctx, mcancel := context.WithTimeout(context.Background(), openMeasureWait)
		defer mcancel()
		s.measureResources(mctx, task.ID)
		s.PublishTask(task)
	}()
}

// liveWalker is a row's walker card when it is still running or waiting, or nil.
func (s *Server) liveWalker(p *store.PRReview) *store.Task {
	if p == nil || p.WalkerTask == "" {
		return nil
	}
	t, err := s.st.Get(p.WalkerTask)
	if err != nil || t.Status == store.StatusDone || t.Status == store.StatusDead {
		return nil
	}
	return t
}

// openRow makes or finds the review row through postPR, so the claim, the run folder and the runner start exactly as
// a paste on the pulls tab does. A nil row is a refusal, with the status and body to answer.
func (s *Server) openRow(r *http.Request, in openRequest) (row *store.PRReview, created bool, status int, body map[string]any) {
	raw, _ := json.Marshal(map[string]string{"url": in.URL, "why": in.Why})
	req := httptest.NewRequest(http.MethodPost, "/v1/prs", bytes.NewReader(raw)).
		WithContext(context.WithValue(r.Context(), openHeldKey{}, true))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.postPR(rec, req)
	var out struct {
		PR      *store.PRReview `json:"pr"`
		Created bool            `json:"created"`
		HeldBy  string          `json:"held_by"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	switch {
	case out.HeldBy != "":
		// Another room claimed the link between the hub's placement and here. Its card is there.
		return nil, false, http.StatusConflict, map[string]any{"code": "held", "held_by": out.HeldBy,
			"error": "that pull request is held by " + out.HeldBy + ". open it there"}
	case rec.Code >= 300 || out.PR == nil:
		code := rec.Code
		if code < 300 {
			code = http.StatusInternalServerError
		}
		return nil, false, code, body
	}
	return out.PR, out.Created, rec.Code, nil
}

// openLaunchBody is the card's launch: the worktree, what the recogniser filled in, and the tags that tie it to the
// link. The harness is the caller's, else the review recipe's for the repo.
func (s *Server) openLaunchBody(in openRequest, got *store.Resolved, wt prWorktreeResult, key, org, repo string, num int) []byte {
	harness := strings.TrimSpace(in.Harness)
	if harness == "" {
		if rec, err := s.st.RecipeFor(org + "/" + repo); err == nil && rec != nil {
			harness = rec.Harness
		}
	}
	tags := append([]string{}, got.Tags...)
	tags = append(tags, "pr", fmt.Sprintf("pr:%s/%s#%d", org, repo, num), "link:"+key)
	or := func(mine, theirs string) string {
		if strings.TrimSpace(mine) != "" {
			return strings.TrimSpace(mine)
		}
		return theirs
	}
	title := or(in.Title, got.Title)
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("%s/%s#%d", org, repo, num)
	}
	body, _ := json.Marshal(map[string]any{
		"harness": harness, "cwd": wt.Path, "title": title, "why": in.Why, "prompt": or(in.Prompt, got.Prompt),
		"tags": tags, "model": strings.TrimSpace(in.Model), "effort": strings.TrimSpace(in.Effort),
		"repo": repo, "org": org, "host": got.Host, "branch": wt.Branch, "window": got.Window, "theme": got.Theme,
		"source_kind": got.Kind, "source_url": in.URL,
	})
	return body
}
