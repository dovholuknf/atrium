package link

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/land"
)

// "Land now": the landed requests of a repository since the last land become one commit on main. See
// docs/rnd/merge-queue.md, section "Landing origin/main".
//
//	GET  /_hub/merge/land?repo     what a land would carry, and the last lands with whether each is on main
//	POST /_hub/merge/land          {repo}: cut the land. The operator's only, never relayed from a room
//
// THE HUB NEVER PUSHES TO THE FORGE. The commit is cut in the hub's store on land/<n>, signed as the operator, and
// claude/main is moved onto it. The answer carries the one push that puts it on origin's main, which the operator
// runs. The next land waits until the last is on main, so a batch is never cut onto a main that lacks the one before.

// mergeLandMsgMax bounds the land commit's one line.
const mergeLandMsgMax = 200

type mergeLandInfo struct {
	Repo     string                  `json:"repo"`
	Unlanded []hubstore.MergeRequest `json:"unlanded"`
	Lands    []mergeLandOut          `json:"lands"`
}

type mergeLandOut struct {
	hubstore.MergeLand
	Branch string `json:"branch"`
	OnMain bool   `json:"on_main"`
	Push   string `json:"push,omitempty"`
}

// operatorSign is the identity and the signer of the operator's own git config, read the way the land script reads it.
func operatorSign() (land.Identity, land.Signer, error) {
	cfg := func(k string) string {
		out, _ := exec.Command("git", "config", "--global", "--get", k).Output()
		return strings.TrimSpace(string(out))
	}
	id := land.Identity{Name: cfg("user.name"), Email: cfg("user.email")}
	if id.Name == "" || id.Email == "" {
		return id, nil, errors.New("set git config --global user.name and user.email on the hub's machine")
	}
	if err := land.CheckEmail(id.Email, "git config --global"); err != nil {
		return id, nil, err
	}
	if cfg("commit.gpgsign") != "true" {
		return id, nil, nil
	}
	sign, err := land.GitSigner(cfg)
	return id, sign, err
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
		o := mergeLandOut{MergeLand: l, Branch: gitsync.LandBranch(l.N)}
		if g != nil {
			o.OnMain, _ = g.Store().OnMain(r.Context(), repo, l.Commit)
		}
		if !o.OnMain {
			o.Push = mergeLandPush(r, repo, l.N)
		}
		info.Lands = append(info.Lands, o)
	}
	return info, nil
}

// mergeLandPush is the command that puts land n on origin's main, from a clone of the repository.
func mergeLandPush(r *http.Request, repo string, n int64) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("git fetch %s://%s%s%s.git %s && git push origin FETCH_HEAD:main", scheme, r.Host,
		gitsync.StorePrefix, repo, gitsync.LandBranch(n))
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
	// The queue is paused for the cut, so nothing lands on claude/main while it is made.
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
	last, err := st.MergeLands(repo, 1)
	if err != nil {
		ffail(w, err)
		return
	}
	mustHave := ""
	if len(last) > 0 {
		mustHave = last[0].Commit
	}
	sign := p.merges.sign
	if sign == nil {
		sign = operatorSign
	}
	id, signer, err := sign()
	if err != nil {
		crFail(w, http.StatusBadRequest, "cannot sign the land: "+err.Error())
		return
	}
	n, err := st.MergeLandNext()
	if err != nil {
		ffail(w, err)
		return
	}
	msg := mergeLandMessage(n, batch)
	stamp := fmt.Sprintf("x %d +0000", time.Now().Unix())
	cut, err := g.Store().MergeLandCut(r.Context(), repo, n, mustHave, func(tree, parent string) ([]byte, error) {
		body := land.Body(tree, []string{parent}, id, stamp, stamp, msg, "")
		if signer == nil {
			return body, nil
		}
		sig, err := signer(body)
		if err != nil {
			return nil, fmt.Errorf("signing the land: %w", err)
		}
		return land.Body(tree, []string{parent}, id, stamp, stamp, msg, sig), nil
	})
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
	p.RecordAudit("", "merge-land", fmt.Sprintf("land %d of %s: %s, %d requests", n, repo, shortHex(cut.Commit), len(ns)))
	p.backlogEvent("merge", map[string]string{"land": strconv.FormatInt(n, 10), "repo": repo})
	crJSON(w, http.StatusCreated, mergeLandOut{MergeLand: l, Branch: cut.Branch, Push: mergeLandPush(r, repo, n)})
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

// mergeLandState is what the board's Land now needs: the landed requests no land carries, by repository, and each
// repository's last land while it is not on main.
func (p *Proxy) mergeLandState(r *http.Request, st *hubstore.Store) (map[string]int, []mergeLandOut, error) {
	counts, err := st.MergeUnlandedCounts()
	if err != nil {
		return nil, nil, err
	}
	last, err := st.MergeLandsLast()
	if err != nil {
		return nil, nil, err
	}
	pending := []mergeLandOut{}
	g := p.git()
	for _, l := range last {
		on := false
		if g != nil {
			on, _ = g.Store().OnMain(r.Context(), l.Repo, l.Commit)
		}
		if !on {
			pending = append(pending, mergeLandOut{MergeLand: l, Branch: gitsync.LandBranch(l.N), Push: mergeLandPush(r, l.Repo, l.N)})
		}
	}
	return counts, pending, nil
}
