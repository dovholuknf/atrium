package link

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/deployready"
)

// The deploy queue, `GET /_hub/deploy-queue`: the landed commits not yet live, and for each whether a hub deploy, a room
// deploy or both puts it live. Live is what the hub and each attached room report as their running commit, so there is
// no list to keep by hand. See internal/deployready/queue.go for how a commit is placed.
//
// A room that does not report a commit (an older build) is listed as behind and named under `unreported`, never
// skipped: a queue that hid a commit could miss a deploy, and one that over-lists costs a look.

// SetRunningCommit says which commit this hub's own binary was built from. Empty means the hub cannot say, and the
// queue then falls back to the installed binary's commit.
func (p *Proxy) SetRunningCommit(commit string) {
	st := p.deployReady()
	if st == nil {
		return
	}
	st.mu.Lock()
	st.running = strings.TrimSpace(commit)
	st.mu.Unlock()
}

// deployQueue reads the queue now. The caller has checked that deploy-ready is on.
func (p *Proxy) deployQueue(ctx context.Context) deployready.Queue {
	st := p.deployReady()
	bctx, cancel := context.WithTimeout(ctx, deployReadyBound)
	defer cancel()
	repo, err := p.repoFor("")
	if err != nil {
		return deployready.Queue{Entries: []deployready.QueueEntry{}, Error: err.Error(),
			Line: "deploy queue unknown: " + err.Error()}
	}
	st.mu.Lock()
	hub := st.running
	st.mu.Unlock()
	if hub == "" {
		if hub, err = st.installed(bctx, p); err != nil {
			return deployready.Queue{Branch: repo.Branch, Entries: []deployready.QueueEntry{}, Error: err.Error(),
				Line: "deploy queue unknown: " + err.Error()}
		}
	}
	var rooms []deployready.Live
	if p.hub != nil {
		for _, a := range p.hub.Rooms() {
			rooms = append(rooms, deployready.Live{Name: a.Name, Commit: a.Commit})
		}
	}
	st.mu.Lock()
	c := p.checkerFor(st, repo)
	st.mu.Unlock()
	q := c.Queue(bctx, hub, rooms)
	if bctx.Err() != nil {
		return deployready.Queue{Branch: repo.Branch, Entries: []deployready.QueueEntry{},
			Error: "timed out reading git", Line: "deploy queue unknown: timed out reading git"}
	}
	return q
}

func (p *Proxy) serveDeployQueue(w http.ResponseWriter, r *http.Request) {
	if p.deployReady() == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "that has to be a GET"})
		return
	}
	q := p.deployQueue(r.Context())
	if r.URL.Query().Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(q.Markdown()))
		return
	}
	_ = json.NewEncoder(w).Encode(q)
}
