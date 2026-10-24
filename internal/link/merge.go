package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/land"
)

// The merge queue's hub half. See docs/rnd/merge-queue.md, internal/hubstore/merge.go for the rows and
// internal/gitsync/merge.go and mergerun.go for the git and the room's test run.
//
//	POST /_hub/merge               {repo, branch, sha?, by, room}: ask. Answers the open request when there is one
//	GET  /_hub/merge?repo&open&all the requests, the open ones first, why a queue is held, and what a land waits on
//	GET  /_hub/merge/<n>           one, with its test output
//	POST /_hub/merge/<n>/cancel    {by}: a queued request, by its requester, its launcher or the operator
//	GET  /_hub/merge/room          the configured merge room and the one the queue would use now
//	POST /_hub/merge/room          {room}: set it, "" unsets. The operator's, never relayed from a room
//	POST /_hub/merge/report        the merge room's verdict, relayed with `room` set from the link
//	GET, POST /_hub/merge/land     land now, in mergeland.go
//
// ONE REQUEST PER REPOSITORY AT A TIME, IN ORDER, made by one runner: a 30 second tick and a kick on every change,
// serial like the task scheduler. The hub merges in its store, the merge room tests, the hub lands.
//
// WHO MAY WRITE is what the flow allows: the machine the hub runs on, where the control MCP and the room relay ask
// from. The requester is `by`, which the relay sets from the link.

const (
	mergeTick  = 30 * time.Second
	mergeAsk   = 30 * time.Second
	mergeRetry = 1
	mergeFrom  = "atrium-merge@hub"
	// mergeGrace is how long a run the merge room does not know of is waited on, mergeSilent how long past the test
	// bound a room that does not answer is.
	mergeGrace  = 2 * time.Minute
	mergeSilent = gitsync.MergeTestBound + 10*time.Minute
	// mergeReportBody fits the largest output a room sends, escaped, and the envelope.
	mergeReportBody = 2 << 20
)

// mergeQueue is the runner's state on the Proxy.
type mergeQueue struct {
	run sync.Mutex
	// verdict serializes verdicts, so a duplicate never reads a row another verdict is moving.
	verdict sync.Mutex
	mu      sync.Mutex
	held    map[string]string
	stalls  map[string]mergeStall
	// now is the clock the watch ages a run by, time.Now when nil.
	now func() time.Time
	// sign is the land's identity and signer, operatorSign when nil.
	sign func() (land.Identity, land.Signer, error)
	// cardID is the id of the card that answers to agent on room, "" when none does. mergeCardOf when nil.
	cardID func(ctx context.Context, room, agent string) string
}

func (q *mergeQueue) hold(repo, why string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.held == nil {
		q.held = map[string]string{}
	}
	if why == "" {
		delete(q.held, repo)
		return
	}
	q.held[repo] = why
}

func (q *mergeQueue) holds() map[string]string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := map[string]string{}
	for k, v := range q.held {
		out[k] = v
	}
	return out
}

// age is how long ago a row's time was, 0 when it cannot be read.
func (q *mergeQueue) age(at string) time.Duration {
	t, err := time.Parse(hubstore.TimeFormat, at)
	if err != nil {
		return 0
	}
	return q.clock().Sub(t)
}

func (q *mergeQueue) clock() time.Time {
	if q.now != nil {
		return q.now()
	}
	return time.Now()
}

// mergeStall is a repository whose queue waits on the hub or the merge room, not on a request.
type mergeStall struct {
	until time.Time
	why   string
	times int
}

// stall holds repo's queue for a wait that doubles from a minute to sixteen, with why shown as the hold.
func (q *mergeQueue) stall(repo, why string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stalls == nil {
		q.stalls = map[string]mergeStall{}
	}
	s := q.stalls[repo]
	s.times++
	s.until, s.why = q.clock().Add(time.Minute<<min(s.times-1, 4)), why
	q.stalls[repo] = s
	if q.held == nil {
		q.held = map[string]string{}
	}
	q.held[repo] = why
}

// stalled is why repo's queue waits, while it does.
func (q *mergeQueue) stalled(repo string) (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	s, ok := q.stalls[repo]
	if !ok || !q.clock().Before(s.until) {
		return "", false
	}
	return s.why, true
}

// unstall is a run that came to a verdict: the room works again.
func (q *mergeQueue) unstall(repo string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.stalls, repo)
}

func (p *Proxy) serveMerge(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.backlogStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if write {
		switch {
		case r.Method != http.MethodPost:
			crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
			return
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "the merge queue is written only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "merge"), "/")
	switch {
	case rest == "" && !write:
		p.mergeList(w, r, st)
	case rest == "":
		p.mergeAskRoute(w, r, st)
	case rest == "room" && !write:
		configured, err := st.MergeRoom()
		if err != nil {
			ffail(w, err)
			return
		}
		room, why := p.mergeRoomPick(st)
		crJSON(w, http.StatusOK, map[string]any{"room": configured, "using": room, "why": why})
	case rest == "room":
		var in struct{ Room, By string }
		if err := crDecode(r, &in); err != nil {
			crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		if err := st.SetMergeRoom(in.Room); err != nil {
			ffail(w, err)
			return
		}
		p.RecordAudit("", "merge-room", strings.TrimSpace(in.Room)+" by "+in.By)
		p.mergeKick()
		room, why := p.mergeRoomPick(st)
		crJSON(w, http.StatusOK, map[string]any{"room": strings.ToLower(strings.TrimSpace(in.Room)), "using": room, "why": why})
	case rest == "land" && !write:
		p.mergeLandGet(w, r, st)
	case rest == "land":
		p.mergeLandRoute(w, r, st)
	case rest == "report" && write:
		p.mergeReport(w, r, st)
	case strings.HasSuffix(rest, "/cancel") && write:
		n, ok := mergeN(strings.TrimSuffix(rest, "/cancel"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.mergeCancel(w, r, st, n)
	case !write:
		n, ok := mergeN(rest)
		if !ok {
			http.NotFound(w, r)
			return
		}
		m, err := st.MergeGet(n)
		if errors.Is(err, hubstore.ErrMergeNotFound) {
			crFail(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			ffail(w, err)
			return
		}
		crJSON(w, http.StatusOK, m)
	default:
		http.NotFound(w, r)
	}
}

func mergeN(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

func (p *Proxy) mergeList(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	q := r.URL.Query()
	f := hubstore.MergeFilter{Repo: q.Get("repo"), Branch: q.Get("branch"), Open: q.Get("open") != "", Limit: 50}
	if q.Get("all") != "" {
		f.Limit = 500
	}
	ms, err := st.MergeList(f)
	if err != nil {
		ffail(w, err)
		return
	}
	room, why := p.mergeRoomPick(st)
	unlanded, pending, err := p.mergeLandState(r, st)
	if err != nil {
		ffail(w, err)
		return
	}
	crJSON(w, http.StatusOK, map[string]any{"requests": ms, "held": p.merges.holds(), "room": room, "why": why,
		"unlanded": unlanded, "pending": pending})
}

// mergeAskRoute queues a request. The branch has to be on the hub at the sha asked, so "push first" is said here and
// not after the request waited its turn.
func (p *Proxy) mergeAskRoute(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct {
		Repo, Branch, SHA, By, Room string
		Of                          int64
	}
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	g := p.git()
	if g == nil {
		crFail(w, http.StatusServiceUnavailable, "this hub keeps no git store, so it has nothing to merge")
		return
	}
	agent, room := mergeWho(in.By, in.Room)
	if in.By == "" && in.Room == "" {
		agent = "operator"
	} else if agent == "" || room == "" {
		crFail(w, http.StatusBadRequest, "a merge is asked by a card: by is <card>@<room>")
		return
	}
	in.Repo, in.Branch = hubstore.MergeRepoName(in.Repo), strings.TrimSpace(in.Branch)
	tip, ok, err := g.Store().BranchSHA(r.Context(), in.Repo, in.Branch)
	switch {
	case err != nil && errors.Is(err, gitsync.ErrRefused):
		crFail(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		crFail(w, http.StatusServiceUnavailable, "the hub could not read its store: "+err.Error())
		return
	case !ok:
		crFail(w, http.StatusBadRequest, fmt.Sprintf("the hub has no %s in %s. push it first: git push hub %s", in.Branch, in.Repo, in.Branch))
		return
	}
	sha := strings.ToLower(strings.TrimSpace(in.SHA))
	if sha == "" {
		sha = tip
	} else if len(sha) < gitsync.MinSHAPrefix || !strings.HasPrefix(tip, sha) {
		crFail(w, http.StatusBadRequest, fmt.Sprintf("the hub's %s is at %s, not %s. push again, then ask again", in.Branch, shortHex(tip), shortHex(sha)))
		return
	}
	if in.Of != 0 {
		of, err := st.MergeGet(in.Of)
		switch {
		case err != nil && !errors.Is(err, hubstore.ErrMergeNotFound):
			ffail(w, err)
			return
		case err != nil || of.State != hubstore.MergeConflict || in.Branch != MergerBranch(in.Of):
			crFail(w, http.StatusBadRequest, fmt.Sprintf("of names a request in conflict, asked for from its merger's branch %s", MergerBranch(in.Of)))
			return
		}
		if agent != "operator" && !p.mergeIsMerger(r.Context(), of, agent, room) {
			crFail(w, http.StatusForbidden, fmt.Sprintf("only request %d's merger asks with of %d", of.N, of.N))
			return
		}
	}
	if agent != "operator" {
		if why := p.mergeOwns(r.Context(), g, in.Repo, in.Branch, agent, room); why != "" {
			crFail(w, http.StatusForbidden, why)
			return
		}
	}
	ask := hubstore.MergeAsk{Repo: in.Repo, Branch: in.Branch, SHA: tip, Requester: agent, RequesterRoom: room, Continues: in.Of}
	if room != "" {
		ask.Launcher, ask.LauncherRoom = p.mergeLauncher(r.Context(), room, agent)
	}
	m, existing, err := st.MergeAsked(ask)
	if err != nil {
		ffail(w, err)
		return
	}
	code := http.StatusCreated
	if existing {
		code = http.StatusOK
	} else {
		p.RecordAudit(room, "merge-ask", fmt.Sprintf("#%d %s %s %s by %s", m.N, m.Repo, m.Branch, shortHex(m.SHA), agent))
		p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": m.State})
		p.mergeKick()
	}
	crJSON(w, code, map[string]any{"request": m, "existing": existing})
}

func (p *Proxy) mergeCardID(ctx context.Context, room, agent string) string {
	if p.merges.cardID != nil {
		return p.merges.cardID(ctx, room, agent)
	}
	if c := p.mergeCardOf(ctx, room, agent); c != nil {
		return c.ID
	}
	return ""
}

// mergeOwns says why the card agent on room may not ask for branch, or "". The owner is the push log's: the first
// card to push the branch since it was last released, as the push path decides it.
func (p *Proxy) mergeOwns(ctx context.Context, g *gitsync.Hub, repo, branch, agent, room string) string {
	if g.PushLog == nil {
		return "the hub keeps no push log, so it cannot tell whose " + branch + " it is"
	}
	oroom, ocard, ok, err := g.PushLog.Owner(ctx, repo, "refs/heads/"+branch)
	switch {
	case err != nil:
		return "the hub could not read who owns " + branch + ": " + err.Error()
	case !ok:
		return "nobody owns " + branch + ". push it from your own card, then ask"
	case ocard == "":
		return branch + " is the operator's. the operator asks for it"
	case !strings.EqualFold(oroom, room):
		return fmt.Sprintf("%s is a card's on %s, not yours", branch, oroom)
	}
	if id := p.mergeCardID(ctx, room, agent); id == "" || id != ocard {
		return fmt.Sprintf("%s is card %s's, and %s@%s is not that card", branch, ocard, agent, room)
	}
	return ""
}

// mergeIsMerger is whether agent on room is the merger recorded for the conflict of.
func (p *Proxy) mergeIsMerger(ctx context.Context, of hubstore.MergeRequest, agent, room string) bool {
	handle, mroom, _ := strings.Cut(of.Merger, "@")
	if handle == "" || !strings.EqualFold(mroom, room) {
		return false
	}
	want := p.mergeCardID(ctx, mroom, handle)
	return want != "" && want == p.mergeCardID(ctx, room, agent)
}

// mergeToken is a fresh run's capability.
func mergeToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// mergeWho splits `by` into the card and its room. The room in `by` wins, since the relay writes both from the link.
func mergeWho(by, room string) (string, string) {
	agent, at, _ := strings.Cut(strings.TrimSpace(by), "@")
	if at != "" {
		room = at
	}
	return strings.TrimSpace(agent), strings.ToLower(strings.TrimSpace(room))
}

func shortHex(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// mergeCard is a card as the queue needs it: who it is, its tags and who launched it.
type mergeCard struct {
	ctlCard
	SpawnedBy   string `json:"spawned_by"`
	SpawnedByID string `json:"spawned_by_id"`
}

// mergeCardOf reads the card that answers to agent on room, nil when it cannot be read.
func (p *Proxy) mergeCardOf(ctx context.Context, room, agent string) *mergeCard {
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if c == nil || room == "" || agent == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var body struct {
		Tasks []mergeCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return nil
	}
	cards := make([]ctlCard, len(body.Tasks))
	for i := range body.Tasks {
		cards[i] = body.Tasks[i].ctlCard
	}
	best := pickCard(cards, agent)
	if best == nil {
		return nil
	}
	for i := range body.Tasks {
		if body.Tasks[i].ID == best.ID {
			return &body.Tasks[i]
		}
	}
	return nil
}

// mergeLauncher is who launched the card, from its lineage on its room, as card and room. Nothing when it cannot be
// read: the request still runs and the requester still hears.
func (p *Proxy) mergeLauncher(ctx context.Context, room, agent string) (string, string) {
	t := p.mergeCardOf(ctx, room, agent)
	if t == nil || t.SpawnedBy == "" || strings.HasPrefix(t.SpawnedBy, "@") {
		return "", ""
	}
	name, at, _ := strings.Cut(t.SpawnedBy, "@")
	lroom := room
	if at != "" {
		lroom = at
	}
	if r, id := splitTag(t.SpawnedByID); id != "" {
		if r != "" {
			lroom = r
		}
		return id, strings.ToLower(lroom)
	}
	return name, strings.ToLower(lroom)
}

func (p *Proxy) mergeCancel(w http.ResponseWriter, r *http.Request, st *hubstore.Store, n int64) {
	var in struct{ By string }
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	m, err := st.MergeGet(n)
	if errors.Is(err, hubstore.ErrMergeNotFound) {
		crFail(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		ffail(w, err)
		return
	}
	if by := strings.TrimSpace(in.By); by != "" {
		agent, room := mergeWho(by, "")
		mine := room == m.RequesterRoom && agent == m.Requester || room == m.LauncherRoom && agent == m.Launcher ||
			m.Merger != "" && strings.EqualFold(agent+"@"+room, m.Merger)
		if !mine {
			crFail(w, http.StatusForbidden, fmt.Sprintf("request %d is %s@%s's to cancel, or its launcher's", n, m.Requester, m.RequesterRoom))
			return
		}
	}
	who := strings.TrimSpace(in.By)
	if who == "" {
		who = "the operator"
	}
	step := hubstore.MergeStep{N: n, From: hubstore.MergeQueued, To: hubstore.MergeCancelled, Reason: "cancelled by " + who}
	switch {
	case m.State == hubstore.MergeConflict:
		step.From, step.To, step.Reason = hubstore.MergeConflict, hubstore.MergeRefused, cutReason(m.Reason+", given up by "+who)
	case m.State == hubstore.MergeTesting && strings.TrimSpace(in.By) == "":
		step.From = hubstore.MergeTesting
	}
	out, err := p.mergeEnd(r.Context(), st, step)
	if err != nil {
		ffail(w, err)
		return
	}
	p.RecordAudit(m.RequesterRoom, "merge-cancel", fmt.Sprintf("#%d by %s", n, who))
	crJSON(w, http.StatusOK, out)
}

// mergeRoomPick is the room that runs the tests now, or "" and why none does: the configured room while it is attached,
// else the room on the hub's own machine. See docs/rnd/merge-queue.md, "Which room runs the tests".
func (p *Proxy) mergeRoomPick(st *hubstore.Store) (string, string) {
	if p.hub == nil {
		return "", "this hub has no rooms"
	}
	configured, err := st.MergeRoom()
	if err != nil {
		return "", "the hub could not read the merge room: " + err.Error()
	}
	rooms := p.hub.Rooms()
	if configured != "" {
		for _, a := range rooms {
			if strings.EqualFold(a.Name, configured) {
				if !a.Git {
					return "", "the merge room " + configured + " takes no git syncs. update it"
				}
				return strings.ToLower(a.Name), ""
			}
		}
		return "", "the merge room " + configured + " is not attached"
	}
	for _, a := range rooms {
		if a.Local && a.Git {
			return strings.ToLower(a.Name), ""
		}
	}
	return "", "no merge room: attach a room on the hub's machine, or name one with atrium merge room <name>"
}

// localConn is whether a room's link came from the hub's own machine.
func localConn(c net.Conn, host string) bool {
	if a := c.RemoteAddr(); a != nil {
		h, _, err := net.SplitHostPort(a.String())
		if err != nil {
			h = a.String()
		}
		if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	me, err := os.Hostname()
	return err == nil && host != "" && strings.EqualFold(strings.TrimSpace(host), me)
}

// mergeKick runs the queue off the caller's path.
func (p *Proxy) mergeKick() {
	go p.mergeSchedule(context.Background())
}

// RunMerges is the queue's tick, every 30 seconds, so a queue held for want of a merge room moves when one attaches,
// and a run whose verdict never came is noticed. What a restart left merging is queued again before anything runs.
func (p *Proxy) RunMerges(ctx context.Context) {
	p.merges.run.Lock()
	p.mergeRecover()
	p.merges.run.Unlock()
	t := time.NewTicker(mergeTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.mergeTick(ctx)
		}
	}
}

func (p *Proxy) mergeTick(ctx context.Context) {
	p.merges.run.Lock()
	defer p.merges.run.Unlock()
	p.mergeRecover()
	p.mergeWatch(ctx)
	p.mergeScheduleLocked(ctx)
}

// mergeRecover queues again, free, each request left merging. A merge is made under merges.run, so one seen merging
// while that lock is held was lost to a restart or a store failure.
func (p *Proxy) mergeRecover() {
	st := p.backlogStore()
	if st == nil || p.git() == nil {
		return
	}
	open, err := st.MergeList(hubstore.MergeFilter{Open: true, Limit: 500})
	if err != nil {
		log.Printf("[hub] merge queue: could not read the open requests: %v", err)
		return
	}
	for _, m := range open {
		if m.State != hubstore.MergeMerging {
			continue
		}
		if _, err := st.MergeMove(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeQueued, Free: true,
			Reason: "the hub restarted while merging"}); err != nil {
			log.Printf("[hub] merge %d: could not queue it again after a restart: %v", m.N, err)
		}
	}
}

// mergeWatch asks the room of each testing request how its run goes. A verdict is applied and a run still going is
// left. A run the room has no record of past mergeGrace, or a room that does not answer past mergeSilent, gets the
// request queued again, free, since neither is the request's fault.
func (p *Proxy) mergeWatch(ctx context.Context) {
	st := p.backlogStore()
	if st == nil || p.git() == nil {
		return
	}
	open, err := st.MergeList(hubstore.MergeFilter{Open: true, Limit: 500})
	if err != nil {
		log.Printf("[hub] merge queue: could not read the open requests: %v", err)
		return
	}
	for _, m := range open {
		if m.State != hubstore.MergeTesting {
			continue
		}
		age := p.merges.age(m.StartedAt)
		var rep gitsync.MergeReport
		err := p.mergeRoomCall(ctx, m.Room, http.MethodGet, "/v1/git/merge/status?n="+strconv.FormatInt(m.N, 10), nil, &rep)
		var ra *mergeRoomAnswer
		mine := err == nil && rep.N == m.N && rep.Candidate == m.Candidate
		why := ""
		switch {
		case mine && rep.Verdict == gitsync.VerdictRunning:
		case mine:
			if _, _, err := p.mergeVerdict(ctx, st, m.Room, rep); err != nil {
				log.Printf("[hub] merge %d: the verdict %s from %s was not taken: %v", m.N, rep.Verdict, m.Room, err)
			}
		case (err == nil || errors.As(err, &ra) && ra.Code == http.StatusNotFound) && age > mergeGrace:
			why = "the merge room " + m.Room + " has no run of it"
		case err != nil && age > mergeSilent:
			why = fmt.Sprintf("the merge room %s did not answer for the run: %v", m.Room, err)
		}
		if why != "" {
			log.Printf("[hub] merge %d: %s, queued again", m.N, why)
			p.mergeAgain(ctx, st, m, why, true)
		}
	}
}

// mergeSchedule starts the next request of every repository with nothing merging or testing. Serial.
func (p *Proxy) mergeSchedule(ctx context.Context) {
	p.merges.run.Lock()
	defer p.merges.run.Unlock()
	p.mergeScheduleLocked(ctx)
}

func (p *Proxy) mergeScheduleLocked(ctx context.Context) {
	st, g := p.backlogStore(), p.git()
	if st == nil || g == nil {
		return
	}
	repos, err := st.MergeRepos()
	if err != nil {
		log.Printf("[hub] merge queue: could not read the queue: %v", err)
		return
	}
	for _, repo := range repos {
		m, ok, err := st.MergeNext(repo)
		if err != nil {
			continue
		}
		if !ok {
			p.merges.hold(repo, "")
			continue
		}
		if why, ok := p.merges.stalled(repo); ok {
			p.merges.hold(repo, why)
			continue
		}
		room, why := p.mergeRoomPick(st)
		p.merges.hold(repo, why)
		if room == "" {
			continue
		}
		p.mergeStart(ctx, st, g.Store(), m, room)
	}
}

// mergeStart merges request m in the hub's store and hands the candidate to room.
func (p *Proxy) mergeStart(ctx context.Context, st *hubstore.Store, gs *gitsync.Store, m hubstore.MergeRequest, room string) {
	m, err := st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: room})
	if err != nil {
		log.Printf("[hub] merge %d: could not start it: %v", m.N, err)
		return
	}
	p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": m.State})
	res, err := gs.MergeCandidate(ctx, m.Repo, m.Branch, m.SHA, m.N, fmt.Sprintf("Merge %s (#%d)", m.Branch, m.N))
	switch {
	case errors.Is(err, gitsync.ErrRefused):
		log.Printf("[hub] merge %d: refused: %v", m.N, err)
		p.mergeEnd(ctx, st, hubstore.MergeStep{N: m.N, From: hubstore.MergeMerging, To: hubstore.MergeRefused, Reason: cutReason(err.Error())})
	case errors.Is(err, gitsync.ErrStopped) || ctx.Err() != nil:
		log.Printf("[hub] merge %d: stopped while merging, queued again at the next start", m.N)
	case err != nil:
		why := cutReason("the hub could not merge it: " + gitsync.MergeClean(err.Error()))
		log.Printf("[hub] merge %d: %s", m.N, why)
		p.merges.stall(m.Repo, why)
		_ = p.mergeAgain(ctx, st, m, why, true)
	case res.Already:
		p.mergeEnd(ctx, st, hubstore.MergeStep{N: m.N, From: hubstore.MergeMerging, To: hubstore.MergeLanded, Base: res.Base,
			Landed: res.Base, Reason: "claude/main has it already"})
	case len(res.Conflicts) > 0 && m.Continues > 0:
		p.mergeEnd(ctx, st, hubstore.MergeStep{N: m.N, From: hubstore.MergeMerging, To: hubstore.MergeRefused, Base: res.Base,
			Reason: "the merger's branch does not merge either, " + mergeConflictWhy(res.Conflicts)})
	case len(res.Conflicts) > 0:
		p.mergeConflict(ctx, st, m, res)
	default:
		m, err = st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeMerging, To: hubstore.MergeTesting, Base: res.Base,
			Candidate: res.Candidate, Room: room, Token: mergeToken()})
		if err != nil {
			log.Printf("[hub] merge %d: could not record the candidate: %v", m.N, err)
			return
		}
		p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": m.State})
		job := gitsync.MergeJob{N: m.N, Repo: m.Repo, Candidate: res.Candidate, Base: res.Base, Token: m.Token}
		if err := p.mergeRoomCall(ctx, room, http.MethodPost, "/v1/git/merge/run", job, nil); err != nil {
			why := cutReason("the merge room " + room + " did not take the run: " + gitsync.MergeClean(err.Error()))
			log.Printf("[hub] merge %d: %s", m.N, why)
			p.merges.stall(m.Repo, why)
			_ = p.mergeAgain(ctx, st, m, why, true)
			return
		}
		log.Printf("[hub] merge %d: %s %s at %s merged onto %s as %s, testing on %s", m.N, m.Repo, m.Branch, shortHex(m.SHA),
			shortHex(res.Base), shortHex(res.Candidate), room)
	}
}

// mergeConflict starts one slim merger for a request that did not merge, on the merge room, under the requester's
// item and launched as the requester, so its ending reaches the card that asked. One merger per request, never
// retried: a merger that cannot start refuses the request back to its requester.
func (p *Proxy) mergeConflict(ctx context.Context, st *hubstore.Store, m hubstore.MergeRequest, res gitsync.MergeResult) {
	why := mergeConflictWhy(res.Conflicts)
	m, err := st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeMerging, To: hubstore.MergeConflict, Base: res.Base, Reason: why})
	if err != nil {
		log.Printf("[hub] merge %d: could not record the conflict: %v", m.N, err)
		return
	}
	p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": m.State})
	merger, err := p.mergeLaunchMerger(ctx, m, res)
	if err != nil {
		p.mergeEnd(ctx, st, hubstore.MergeStep{N: m.N, From: hubstore.MergeConflict, To: hubstore.MergeRefused,
			Reason: cutReason(why + ", and no merger could start: " + err.Error() + ". merge claude/main into it, push, ask again")})
		return
	}
	if m, err = st.MergeSetMerger(m.N, merger); err != nil {
		log.Printf("[hub] merge %d: started %s and could not record it: %v", m.N, merger, err)
	}
	p.RecordAudit(m.Room, "merge-merger", fmt.Sprintf("#%d %s by %s", m.N, m.Branch, merger))
	p.mergeTell(ctx, m)
}

func cutReason(s string) string {
	if len(s) > hubstore.MergeReasonMax {
		return s[:hubstore.MergeReasonMax-4] + " ..."
	}
	return s
}

// MergerBranch is the branch a merger of request n works on and asks to merge.
func MergerBranch(n int64) string { return "claude/merge-" + strconv.FormatInt(n, 10) }

func (p *Proxy) mergeLaunchMerger(ctx context.Context, m hubstore.MergeRequest, res gitsync.MergeResult) (string, error) {
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if c == nil {
		return "", errors.New("the hub has no control server to launch with")
	}
	card := p.mergeCardOf(ctx, m.RequesterRoom, m.Requester)
	if card == nil {
		return "", fmt.Errorf("the card that asked, %s@%s, cannot be read", m.Requester, m.RequesterRoom)
	}
	item := WorkOfTags(card.Tags)
	if item == "" {
		return "", fmt.Errorf("the card that asked, %s@%s, is on no backlog item", m.Requester, m.RequesterRoom)
	}
	spawnedBy, spawnedByID := card.Wire, card.ID
	if m.RequesterRoom != m.Room {
		spawnedBy, spawnedByID = card.Wire+"@"+m.RequesterRoom, tagFor(m.RequesterRoom, card.ID)
	}
	in := launchInput{
		Title: fmt.Sprintf("%s merge #%d %s", item, m.N, m.Branch), Repo: m.Repo, Branch: MergerBranch(m.N),
		Item: item, Superlean: true, Brief: mergerBrief(m, res), Prompt: "Read BRIEF.md and do what it says.",
		Why:  fmt.Sprintf("merge request %d did not merge onto claude/main", m.N),
		Tags: []string{"atrium:merger", "atrium:merge:" + strconv.FormatInt(m.N, 10)},
	}
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	out, err := c.launchOnRoom(lctx, in, "claude", m.Room, m.RequesterRoom, spawnedBy, spawnedByID, "")
	if err != nil {
		return "", err
	}
	handle := out.Handle
	if handle == "" {
		_, handle = splitTag(out.Card)
	}
	return handle + "@" + m.Room, nil
}

// mergerBrief is the smallest brief that works: the request, the paths and the steps.
func mergerBrief(m hubstore.MergeRequest, res gitsync.MergeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Merge conflict, request %d\n\n", m.N)
	fmt.Fprintf(&b, "The merge queue could not merge %s (at %s) onto claude/main (at %s) of %s. The conflicted paths:\n\n",
		m.Branch, shortHex(m.SHA), shortHex(res.Base), m.Repo)
	for _, path := range res.Conflicts {
		fmt.Fprintf(&b, "- %s\n", path)
	}
	nb := MergerBranch(m.N)
	fmt.Fprintf(&b, "\nYou are on %s, made from claude/main. Do this and nothing else:\n\n", nb)
	fmt.Fprintf(&b, "1. `git fetch hub %s` then `git merge --no-edit FETCH_HEAD`.\n", m.Branch)
	b.WriteString("2. Fix each conflict so that what both sides meant still holds. Read both sides' commits when in doubt. " +
		"Commit the merge.\n")
	b.WriteString("3. Run `bash scripts/test-touched.sh` and fix what it finds in the merge.\n")
	fmt.Fprintf(&b, "4. `git push hub %s`, then call atrium_merge with action request, branch %s and of %d.\n", nb, nb, m.N)
	b.WriteString("5. End with atrium_done naming the sha you pushed.\n\n")
	fmt.Fprintf(&b, "If you cannot make it work, call atrium_merge with action cancel and n %d, then atrium_blocked with why. "+
		"Never push claude/main or main.\n", m.N)
	return b.String()
}

func mergeConflictWhy(paths []string) string {
	return cutReason("conflicts in " + strings.Join(paths, ", "))
}

// mergeAgain queues a testing request again once, and refuses it the second time, so nothing makes the queue spin.
// A free one is the hub's or the room's failure and spends no retry.
func (p *Proxy) mergeAgain(ctx context.Context, st *hubstore.Store, m hubstore.MergeRequest, why string, free bool) error {
	if gs := p.git(); gs != nil {
		_ = gs.Store().MergeDrop(ctx, m.Repo, m.N)
	}
	why = cutReason(why)
	if !free && m.Retries >= mergeRetry {
		_, err := p.mergeEnd(ctx, st, hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeRefused, Reason: cutReason(why + ", again")})
		return err
	}
	if _, err := st.MergeMove(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeQueued, Reason: why, Free: free}); err != nil {
		log.Printf("[hub] merge %d: could not queue it again: %v", m.N, err)
		return err
	}
	p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": hubstore.MergeQueued})
	p.mergeKick()
	return nil
}

// mergeRoomAnswer is a room that answered, and not with a yes.
type mergeRoomAnswer struct {
	Code int
	Msg  string
}

func (e *mergeRoomAnswer) Error() string { return e.Msg }

// mergeRoomCall is one request to a room's merge routes, over the link's data pool, as git sync makes them.
func (p *Proxy) mergeRoomCall(ctx context.Context, room, method, path string, in, out any) error {
	if p.hub == nil || room == "" || !p.hub.Has(room) {
		return fmt.Errorf("the room %s is not attached", room)
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, mergeAsk)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+url.PathEscape(room)+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	rt := p.hub.Transport(room)
	if t, ok := rt.(*http.Transport); ok {
		defer t.CloseIdleConnections()
	}
	res, err := (&http.Client{Transport: rt}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return &mergeRoomAnswer{Code: res.StatusCode, Msg: e.Error}
		}
		return &mergeRoomAnswer{Code: res.StatusCode, Msg: fmt.Sprintf("the room answered %d", res.StatusCode)}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// mergeReport takes the merge room's verdict. Its body has its own bound, since a room's output is larger than any
// other write here.
func (p *Proxy) mergeReport(w http.ResponseWriter, r *http.Request, st *hubstore.Store) {
	var in struct {
		gitsync.MergeReport
		Room, By string
	}
	if err := crDecodeMax(r, &in, mergeReportBody); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if by := strings.TrimSpace(in.By); by != "" && !strings.HasPrefix(by, "@") {
		crFail(w, http.StatusForbidden, "a verdict comes from the merge room itself, never from a card")
		return
	}
	code, m, err := p.mergeVerdict(r.Context(), st, strings.ToLower(strings.TrimSpace(in.Room)), in.MergeReport)
	if err != nil {
		crFail(w, code, err.Error())
		return
	}
	crJSON(w, http.StatusOK, m)
}

// mergeVerdict applies a verdict from room. Only the room the request is testing on, for the candidate it was given,
// with the run's token, is heard, so a late verdict of a run the hub gave up on changes nothing. The same verdict
// again, for a request it already ended, is answered as taken, so the room stops sending it. A store that cannot
// record the end is a 503, so the room sends it again.
func (p *Proxy) mergeVerdict(ctx context.Context, st *hubstore.Store, room string, rep gitsync.MergeReport) (int, hubstore.MergeRequest, error) {
	p.merges.verdict.Lock()
	defer p.merges.verdict.Unlock()
	m, err := st.MergeGet(rep.N)
	switch {
	case errors.Is(err, hubstore.ErrMergeNotFound):
		return http.StatusNotFound, m, err
	case err != nil:
		return http.StatusServiceUnavailable, m, err
	case m.Token == "" || subtle.ConstantTimeCompare([]byte(m.Token), []byte(rep.Token)) != 1:
		return http.StatusForbidden, m, fmt.Errorf("that verdict does not carry request %d's run token", m.N)
	case !strings.EqualFold(m.Room, room):
		return http.StatusForbidden, m, fmt.Errorf("request %d is tested on %s, not %s", m.N, m.Room, room)
	case !hubstore.MergeOpen(m.State) && m.Candidate == rep.Candidate:
		return http.StatusOK, m, nil
	case m.State != hubstore.MergeTesting || m.Candidate != rep.Candidate:
		return http.StatusConflict, m, fmt.Errorf("request %d is %s and not testing %s", m.N, m.State, shortHex(rep.Candidate))
	}
	g := p.git()
	if g == nil {
		return http.StatusServiceUnavailable, m, errors.New("this hub keeps no git store")
	}
	end := func(step hubstore.MergeStep) (int, hubstore.MergeRequest, error) {
		out, err := p.mergeEnd(ctx, st, step)
		if err != nil {
			return http.StatusServiceUnavailable, m, fmt.Errorf("the hub could not record request %d's end: %w", m.N, err)
		}
		return http.StatusOK, out, nil
	}
	gs := g.Store()
	log.Printf("[hub] merge %d: %s from %s", m.N, gitsync.MergeClean(rep.Verdict), room)
	if rep.Verdict == gitsync.VerdictCannot {
		why := cutReason("the merge room " + room + " could not run it: " + gitsync.MergeClean(rep.Reason))
		p.merges.stall(m.Repo, why)
		if err := p.mergeAgain(ctx, st, m, why, true); err != nil {
			return http.StatusServiceUnavailable, m, err
		}
		return http.StatusOK, m, nil
	}
	p.merges.unstall(m.Repo)
	switch rep.Verdict {
	case gitsync.VerdictGreen:
		err := gs.MergeLand(ctx, m.Repo, m.N, m.Base, m.Candidate)
		switch {
		case errors.Is(err, gitsync.ErrConflict):
			if err := p.mergeAgain(ctx, st, m, err.Error(), false); err != nil {
				return http.StatusServiceUnavailable, m, err
			}
			return http.StatusOK, m, nil
		case errors.Is(err, gitsync.ErrRefused):
			return end(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeRefused, Reason: cutReason(err.Error()), Output: rep.Output})
		case err != nil:
			return http.StatusServiceUnavailable, m, fmt.Errorf("the hub could not move claude/main: %w", err)
		}
		g.Store().RefreshView()
		p.RecordAudit(room, "merge-land", fmt.Sprintf("#%d %s %s as %s", m.N, m.Repo, m.Branch, shortHex(m.Candidate)))
		return end(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeLanded, Landed: m.Candidate, Output: rep.Output})
	case gitsync.VerdictRed:
		var failing []string
		for _, f := range rep.Failing {
			if f = gitsync.MergeClean(f); f != "" && len(failing) < hubstore.MergeFailingMax {
				if len(f) > gitsync.MergeNameMax {
					f = strings.ToValidUTF8(f[:gitsync.MergeNameMax], "")
				}
				failing = append(failing, f)
			}
		}
		return end(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeRed, Failing: failing, Output: rep.Output,
			Reason: "tests failed on the merge"})
	case gitsync.VerdictError:
		why := cutReason(gitsync.MergeClean(rep.Reason))
		if why == "" {
			why = "the merge room could not run the tests"
		}
		return end(hubstore.MergeStep{N: m.N, From: m.State, To: hubstore.MergeRefused, Reason: why, Output: rep.Output})
	}
	return http.StatusBadRequest, m, fmt.Errorf("a verdict is %s, %s or %s", gitsync.VerdictGreen, gitsync.VerdictRed, gitsync.VerdictError)
}

// mergeEnd moves a request to where it stops waiting on the queue, cleans up after it and tells. A request a merger's
// request continues ends with it. The error is the store's, and nothing else is done when the move was not recorded.
func (p *Proxy) mergeEnd(ctx context.Context, st *hubstore.Store, step hubstore.MergeStep) (hubstore.MergeRequest, error) {
	m, err := st.MergeMove(step)
	if err != nil {
		log.Printf("[hub] merge %d: could not record %s: %v", step.N, step.To, err)
		return m, err
	}
	p.backlogEvent("merge", map[string]string{"n": strconv.FormatInt(m.N, 10), "state": m.State})
	if g := p.git(); g != nil && m.State != hubstore.MergeConflict {
		_ = g.Store().MergeDrop(ctx, m.Repo, m.N)
	}
	if m.State == hubstore.MergeLanded {
		p.mergeTidy(ctx, m.Repo, m.Branch, m.SHA)
	}
	if m.State != hubstore.MergeCancelled || step.From == hubstore.MergeTesting {
		p.mergeTell(ctx, m)
	}
	if m.Continues > 0 && m.State != hubstore.MergeConflict {
		if of, err := st.MergeGet(m.Continues); err == nil && of.State == hubstore.MergeConflict {
			next := hubstore.MergeStep{N: of.N, From: of.State, To: hubstore.MergeRefused,
				Reason: fmt.Sprintf("the merger's request %d ended %s", m.N, m.State)}
			if m.State == hubstore.MergeLanded {
				next.To, next.Landed, next.Reason = hubstore.MergeLanded, m.Landed, fmt.Sprintf("landed through %s (#%d)", m.Branch, m.N)
			}
			if _, err := p.mergeEnd(ctx, st, next); err != nil {
				log.Printf("[hub] merge %d: request %d it carries on did not end with it: %v", m.N, of.N, err)
			}
		}
	}
	p.mergeKick()
	return m, nil
}

// mergeTidy deletes a landed branch in the hub's store and asks every attached room to delete its local branch.
func (p *Proxy) mergeTidy(ctx context.Context, repo, branch, sha string) {
	g := p.git()
	if g == nil {
		return
	}
	deleted, err := g.Store().DeleteLanded(ctx, repo, branch, sha)
	if err != nil {
		log.Printf("[hub] merge: could not delete %s %s: %v", repo, branch, err)
	}
	if deleted {
		if g.PushLog != nil {
			_ = g.PushLog.Release(ctx, repo, "refs/heads/"+branch, "landed by the merge queue")
		}
		g.Store().RefreshView()
		p.RecordAudit("", "merge-branch-deleted", repo+" "+branch)
	}
	if p.hub == nil {
		return
	}
	for _, a := range p.hub.Rooms() {
		if !a.Git {
			continue
		}
		room := strings.ToLower(a.Name)
		go func() {
			var out struct {
				Deleted bool   `json:"deleted"`
				Kept    string `json:"kept"`
			}
			err := p.mergeRoomCall(context.Background(), room, http.MethodPost, "/v1/git/merge/prune",
				gitsync.MergePrune{Repo: repo, Branch: branch, SHA: sha}, &out)
			switch {
			case err != nil:
				log.Printf("[hub] merge: %s did not prune %s: %v", room, branch, err)
			case !out.Deleted && out.Kept != "":
				log.Printf("[hub] merge: %s kept %s: %s", room, branch, out.Kept)
			}
		}()
	}
}

// mergeTell says how a request ended to its requester and its launcher. One that is gone is not an error: the row
// holds the answer.
func (p *Proxy) mergeTell(ctx context.Context, m hubstore.MergeRequest) {
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if c == nil {
		return
	}
	text, kind := mergeLine(m), "needs"
	if m.State == hubstore.MergeLanded {
		kind = "fyi"
	}
	type who struct{ room, card string }
	to := []who{}
	if m.Requester != "" && m.RequesterRoom != "" {
		to = append(to, who{m.RequesterRoom, m.Requester})
	}
	if m.Launcher != "" && m.LauncherRoom != "" && (m.Launcher != m.Requester || m.LauncherRoom != m.RequesterRoom) {
		to = append(to, who{m.LauncherRoom, m.Launcher})
	}
	for _, t := range to {
		go func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if ans := c.deliverAs(ctx, t.room, t.card, mergeFrom, text, "", kind, false, false); !ans.OK {
				log.Printf("[hub] merge %d: could not tell %s@%s: %s", m.N, t.card, t.room, ans.Error)
			}
		}()
	}
}

// mergeLine is the one line a request's end is told in.
func mergeLine(m hubstore.MergeRequest) string {
	switch m.State {
	case hubstore.MergeLanded:
		if m.Landed == m.Base && m.Candidate == "" {
			return fmt.Sprintf("merged %s: claude/main had it already, at %s (#%d)", m.Branch, shortHex(m.Landed), m.N)
		}
		return fmt.Sprintf("merged %s as %s on claude/main (#%d)", m.Branch, shortHex(m.Landed), m.N)
	case hubstore.MergeRed:
		failed := "tests"
		if len(m.Failing) > 0 {
			failed = strings.Join(m.Failing, ", ")
		}
		return fmt.Sprintf("red: %s on claude/main failed %s. atrium merge show %d", m.Branch, failed, m.N)
	case hubstore.MergeConflict:
		if m.Merger != "" {
			return fmt.Sprintf("conflict: %s does not merge onto claude/main, %s. %s is merging it (#%d)", m.Branch, m.Reason, m.Merger, m.N)
		}
		return fmt.Sprintf("conflict: %s does not merge onto claude/main, %s (#%d)", m.Branch, m.Reason, m.N)
	default:
		return fmt.Sprintf("%s: %s, %s (#%d)", m.State, m.Branch, m.Reason, m.N)
	}
}
