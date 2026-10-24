package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/land"
)

// "Land now": the landed requests of a repository since the last land become one commit on the hub's main. See
// docs/rnd/merge-queue.md, section "Landing main".
//
//	GET  /_hub/merge/land?repo     what a land would carry, and the last lands with whether each is on main
//	POST /_hub/merge/land          {repo}: cut the land. The operator's only, never relayed from a room
//
// THE HUB NEVER PUSHES TO THE FORGE. The commit is cut in the hub's store, by the operator's git identity, unsigned,
// and written to main. claude/main is not moved. Putting main on the forge is the operator's own push.

// mergeLandMsgMax bounds the land commit's one line.
const mergeLandMsgMax = 200

type mergeLandInfo struct {
	Repo     string                  `json:"repo"`
	Unlanded []hubstore.MergeRequest `json:"unlanded"`
	Lands    []mergeLandOut          `json:"lands"`
}

type mergeLandOut struct {
	hubstore.MergeLand
	OnMain bool `json:"on_main"`
}

// operatorAuthor is the land's author: the operator's global git identity on the hub's machine.
func operatorAuthor(ctx context.Context) (gitsync.LandAuthor, error) {
	a, err := gitsync.OperatorAuthor(ctx)
	if err != nil {
		return a, err
	}
	return a, land.CheckEmail(a.Email, "git config --global")
}

func (p *Proxy) mergeLandGet(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	repo := hubstore.MergeRepoName(r.URL.Query().Get("repo"))
	if repo == "" {
		crFail(w, http.StatusBadRequest, "say which repository: repo=<host>/<owner>/<repo>")
		return
	}
	info, err := p.mergeLandRead(r, st, repo)
	if err != nil {
		ffail(w, err)
		return
	}
	crJSON(w, http.StatusOK, info)
}

func (p *Proxy) mergeLandRead(r *http.Request, st *hubstore.Store, repo string) (mergeLandInfo, error) {
	info := mergeLandInfo{Repo: repo}
	var err error
	if info.Unlanded, err = st.MergeUnlanded(repo); err != nil {
		return info, err
	}
	lands, err := st.MergeLands(repo, 10)
	if err != nil {
		return info, err
	}
	info.Lands = []mergeLandOut{}
	g := p.git()
	for _, l := range lands {
		o := mergeLandOut{MergeLand: l}
		if g != nil {
			o.OnMain, _ = g.Store().OnMain(r.Context(), repo, l.Commit)
		}
		info.Lands = append(info.Lands, o)
	}
	return info, nil
}

func (p *Proxy) mergeLandRoute(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct{ Repo, By, Room string }
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if in.By != "" || in.Room != "" {
		crFail(w, http.StatusForbidden, "landing is the operator's: the board's Land now, or atrium merge land on the hub's machine")
		return
	}
	g := p.git()
	if g == nil {
		crFail(w, http.StatusServiceUnavailable, "this hub keeps no git store, so it has nothing to land")
		return
	}
	repo := hubstore.MergeRepoName(in.Repo)
	if repo == "" {
		crFail(w, http.StatusBadRequest, "say which repository: <host>/<owner>/<repo>")
		return
	}
	author := p.merges.author
	if author == nil {
		author = operatorAuthor
	}
	by, err := author(r.Context())
	if err != nil {
		crFail(w, http.StatusBadRequest, "cannot land: "+err.Error())
		return
	}
	// The queue is paused for the cut, so claude/main does not move under it.
	p.merges.run.Lock()
	defer p.merges.run.Unlock()
	open, err := st.MergeList(hubstore.MergeFilter{Repo: repo, Open: true, Limit: 500})
	if err != nil {
		ffail(w, err)
		return
	}
	for _, m := range open {
		if m.State == hubstore.MergeMerging || m.State == hubstore.MergeTesting {
			crFail(w, http.StatusConflict, fmt.Sprintf("request %d (%s) is %s. land when it ends", m.N, m.Branch, m.State))
			return
		}
	}
	batch, err := st.MergeUnlanded(repo)
	if err != nil {
		ffail(w, err)
		return
	}
	if len(batch) == 0 {
		crFail(w, http.StatusBadRequest, "nothing has landed on claude/main of "+repo+" since the last land")
		return
	}
	n, err := st.MergeLandNext()
	if err != nil {
		ffail(w, err)
		return
	}
	cut, err := g.Store().MergeLandCut(r.Context(), repo, n, by, mergeLandMessage(n, batch))
	switch {
	case err != nil && errors.Is(err, gitsync.ErrRefused):
		crFail(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		crFail(w, http.StatusServiceUnavailable, "the hub could not cut the land: "+err.Error())
		return
	}
	ns := make([]int64, len(batch))
	for i, m := range batch {
		ns[i] = m.N
	}
	l, err := st.MergeLandAdd(hubstore.MergeLand{N: n, Repo: repo, Commit: cut.Commit, MainBefore: cut.MainBefore,
		ClaudeBefore: cut.ClaudeBefore, Requests: ns, By: "operator"})
	if err != nil {
		ffail(w, err)
		return
	}
	p.RecordAudit("", "merge-land", fmt.Sprintf("land %d of %s: main %s..%s, %d requests", n, repo,
		shortHex(cut.MainBefore), shortHex(cut.Commit), len(ns)))
	p.backlogEvent("merge", map[string]string{"land": strconv.FormatInt(n, 10), "repo": repo})
	crJSON(w, http.StatusCreated, mergeLandOut{MergeLand: l, OnMain: true})
}

// mergeLandMessage is the land commit's one line: the land and the branches it carries, cut short with a count.
func mergeLandMessage(n int64, batch []hubstore.MergeRequest) string {
	head := fmt.Sprintf("Land %d: ", n)
	var parts []string
	for i, m := range batch {
		part := fmt.Sprintf("#%d %s", m.N, strings.TrimPrefix(m.Branch, "claude/"))
		more := fmt.Sprintf(" and %d more", len(batch)-i)
		if len(head)+len(strings.Join(append(parts, part), ", "))+len(more) > mergeLandMsgMax && i < len(batch)-1 {
			return land.Message(head + strings.Join(parts, ", ") + more)
		}
		parts = append(parts, part)
	}
	return land.Message(head + strings.Join(parts, ", "))
}
