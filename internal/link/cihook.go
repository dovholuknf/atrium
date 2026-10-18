package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The hub hears CI. See docs/fabric/ci-hook-design.md.
//
// One handler, two doors: the ziti-hook service (CIHook.ServeHTTP, POST / and nothing else) and the hub board's loopback
// route (Proxy.serveCIHook, LocalOperator only). Both end in Receive, which authenticates, bounds, records and answers
// before any delivery is tried. Delivery is the worker's, fed by the pending list in the store.

const (
	ciSecretHeader  = "X-Ziti-Hook-Secret"
	ciMaxBody       = 512 << 10
	ciRatePerMinute = 60
	ciRateBurst     = 20
	ciPendingFor    = 24 * time.Hour
	ciTick          = 30 * time.Second
	ciSweepEvery    = time.Hour
	ciFrom          = "ci@hub"
	ciSignoff       = "(from CI, no reply goes anywhere)"
)

// CITarget is a card a route's `to` resolved to.
type CITarget struct {
	Card   string
	Room   string
	Handle string
	ID     string
}

// CIAnswer is what one try at a delivery came to. State is a hubstore.CI* state, or empty from Resolve when the name
// resolved.
type CIAnswer struct {
	State  string
	Detail string
}

// CIDeliverer is the say path, as the hook needs it. A fake answers each state in the tests.
type CIDeliverer interface {
	Resolve(ctx context.Context, to string) (CITarget, CIAnswer)
	Send(ctx context.Context, t CITarget, from, text, kind string, wake bool) CIAnswer
}

// CIHook is the receiver and its delivery worker.
type CIHook struct {
	Store   *hubstore.Store
	Deliver CIDeliverer
	// Audit writes one hub-level line. Nil records nothing.
	Audit func(kind, detail string)
	// Now is the clock, injected by tests.
	Now func() time.Time
	// Tell raises one operator notification for a post: a title naming what came in and a body saying what the hub did.
	// Nil tells nobody. Proxy.SetCIHook fills it in.
	Tell func(title, body string)

	bucket ciBucket
	qmu    sync.Mutex
	quiet  map[string]time.Time
	kick   chan struct{}
	mu     sync.Mutex
	swept  time.Time
}

// NewCIHook builds the receiver.
func NewCIHook(st *hubstore.Store, d CIDeliverer, audit func(kind, detail string)) *CIHook {
	return &CIHook{Store: st, Deliver: d, Audit: audit, kick: make(chan struct{}, 1)}
}

func (h *CIHook) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *CIHook) audit(kind, detail string) {
	if h.Audit != nil {
		h.Audit(kind, detail)
	}
}

// tell raises the operator notification for one post.
func (h *CIHook) tell(title, body string) {
	if h.Tell != nil {
		h.Tell(title, body)
	}
}

// tellThrottled is tell for a post nothing vouches for: at most one a minute per kind, so a stranger or a runaway loop
// cannot fill the board with notices.
func (h *CIHook) tellThrottled(kind, title, body string) {
	h.qmu.Lock()
	now := h.now()
	if h.quiet == nil {
		h.quiet = map[string]time.Time{}
	}
	if at, ok := h.quiet[kind]; ok && now.Sub(at) < time.Minute {
		h.qmu.Unlock()
		return
	}
	h.quiet[kind] = now
	h.qmu.Unlock()
	h.tell(title, body)
}

const ciDropped = "Dropped: "

// ServeHTTP is the listener on the ziti-hook service. POST / and nothing else, so a runner holding the dial identity
// reaches this and not the board, the API or the control MCP.
func (h *CIHook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.Receive(w, r)
}

type ciBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func (b *ciBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last.IsZero() {
		b.tokens = ciRateBurst
	} else if d := now.Sub(b.last).Seconds(); d > 0 {
		b.tokens += d * ciRatePerMinute / 60
		if b.tokens > ciRateBurst {
			b.tokens = ciRateBurst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ciEvent is the part of a `workflow_run` event the hub reads.
type ciEvent struct {
	WorkflowRun struct {
		ID         int64  `json:"id"`
		RunAttempt int    `json:"run_attempt"`
		Name       string `json:"name"`
		Path       string `json:"path"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
		Event      string `json:"event"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"workflow_run"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func ciReply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Receive is the one handler, behind both doors.
func (h *CIHook) Receive(w http.ResponseWriter, r *http.Request) {
	ok, why, err := h.Store.CIHookSecretOK(r.Header.Get(ciSecretHeader))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		h.audit("ci-hook-refused", why)
		h.tellThrottled("secret", "CI hook refused a post", ciDropped+why+".")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !h.bucket.allow(h.now()) {
		h.tellThrottled("rate", "CI hook is being posted to too fast", ciDropped+"too many notices, over 60 a minute.")
		w.Header().Set("Retry-After", "1")
		ciReply(w, http.StatusTooManyRequests, map[string]string{"error": "too many notices, slow down"})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, ciMaxBody))
	if err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			h.tell("CI hook got a post it could not use", ciDropped+"the body is over 512 KiB.")
			ciReply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the body is over 512 KiB"})
			return
		}
		h.tell("CI hook got a post it could not use", ciDropped+"the body could not be read.")
		ciReply(w, http.StatusBadRequest, map[string]string{"error": "the body could not be read"})
		return
	}
	var ev ciEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		h.tell("CI hook got a post it could not use", ciDropped+"malformed, the body is not a workflow_run event.")
		ciReply(w, http.StatusUnprocessableEntity, map[string]string{"error": "the body is not a workflow_run event"})
		return
	}
	run := ev.WorkflowRun
	if run.ID == 0 || run.RunAttempt == 0 || strings.TrimSpace(run.Conclusion) == "" ||
		strings.TrimSpace(ev.Repository.FullName) == "" {
		h.tell("CI hook got a post it could not use", ciDropped+"malformed, a workflow_run event needs "+
			"workflow_run.id, run_attempt and conclusion, and repository.full_name.")
		ciReply(w, http.StatusUnprocessableEntity, map[string]string{"error": "a workflow_run event needs " +
			"workflow_run.id, run_attempt and conclusion, and repository.full_name"})
		return
	}
	repo := ciClean(ev.Repository.FullName, 120)
	if repos, err := h.Store.CIHookRepos(); err == nil && len(repos) > 0 && !ciListed(repos, repo) {
		h.audit("ci-hook-refused", "repository not listed")
		h.tell("CI hook refused a post from "+repo, ciDropped+"that repository is not in the hook's list.")
		ciReply(w, http.StatusForbidden, map[string]string{"error": "that repository is not listed"})
		return
	}
	rec := hubstore.CIRun{
		Repo: repo, Workflow: ciClean(run.Name, 100), Branch: ciClean(run.HeadBranch, 100),
		SHA: ciClean(run.HeadSHA, 64), RunID: run.ID, Attempt: run.RunAttempt,
		Conclusion: ciClean(run.Conclusion, 30), URL: ciGitHubURL(run.HTMLURL), Auth: "secret",
	}
	last, hadLast, err := h.Store.CILastConclusion(rec.Repo, rec.Workflow, rec.Branch)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rec, dup, err := h.Store.CIRunAdd(rec)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if dup {
		h.tell(ciTitle(rec), ciDropped+"a repeat of a run already recorded."+ciLink(rec))
		ciReply(w, http.StatusOK, map[string]bool{"duplicate": true})
		return
	}
	var routes []CIRoute
	if stored, err := h.Store.CIHookRoutes(); err == nil {
		if routes, err = ParseCIRoutes(stored); err != nil {
			h.audit("ci-hook-routes-bad", err.Error())
			routes = nil
		}
	}
	targets := ciTargets(routes, rec, ciClean(run.Path, 200), last, hadLast)
	text := ciLine(rec)
	told := make([]string, 0, len(targets))
	kinds := make([]string, 0, len(targets))
	for _, t := range targets {
		if _, err := h.Store.CIDeliveryAdd(hubstore.CIDelivery{
			RunRef: rec.ID, Target: t.To, Kind: t.Kind, Wake: t.Wake, Text: text}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		told = append(told, t.To)
		kinds = append(kinds, t.To+" ("+t.Kind+")")
	}
	h.audit("ci-hook", fmt.Sprintf("%s %s on %s: %s. told: %s", rec.Repo, rec.Workflow, rec.Branch, rec.Conclusion,
		ciOrNobody(told)))
	if len(targets) > 0 {
		h.tell(ciTitle(rec), "Delivering to "+strings.Join(kinds, ", ")+"."+ciLink(rec))
	} else {
		h.tell(ciTitle(rec), ciDropped+ciWhyNot(routes, rec, ciClean(run.Path, 200), last, hadLast)+ciLink(rec))
	}
	if len(targets) > 0 {
		select {
		case h.kick <- struct{}{}:
		default:
		}
	}
	ciReply(w, http.StatusAccepted, map[string]any{"run": rec.ID, "targets": len(targets)})
}

func ciOrNobody(l []string) string {
	if len(l) == 0 {
		return "nobody"
	}
	return strings.Join(l, ", ")
}

func ciListed(repos []string, repo string) bool {
	for _, r := range repos {
		if strings.EqualFold(strings.TrimSpace(r), repo) {
			return true
		}
	}
	return false
}

// ciClean makes a body field safe to put in a line: no control characters, bounded.
func ciClean(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		s = string(rs[:n])
	}
	return s
}

// ciGitHubURL keeps a run's link only when it is on github.com.
func ciGitHubURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return ""
	}
	return ciClean(u.String(), 300)
}

// ciLine is the one line the hub says, built from parsed fields and never from free text in the body.
func ciLine(r hubstore.CIRun) string {
	sha := r.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	line := fmt.Sprintf("CI %s: %s %s on %s at %s, attempt %d.", r.Conclusion, r.Repo, r.Workflow, r.Branch, sha, r.Attempt)
	if r.URL != "" {
		line += " " + r.URL
	}
	return line + " " + ciSignoff
}

// ciTitle is what came in, from parsed fields only.
func ciTitle(r hubstore.CIRun) string {
	sha := r.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return fmt.Sprintf("CI %s: %s %s on %s at %s, attempt %d", r.Conclusion, r.Repo, r.Workflow, r.Branch, sha, r.Attempt)
}

func ciLink(r hubstore.CIRun) string {
	if r.URL == "" {
		return ""
	}
	return " " + r.URL
}

// ciWhyNot says why no rule told anybody: none names the repo, or the rules that do were filtered out by the first
// field of theirs that did not match.
func ciWhyNot(routes []CIRoute, run hubstore.CIRun, path, last string, hadLast bool) string {
	for _, r := range routes {
		if !strings.EqualFold(r.Repo, run.Repo) {
			continue
		}
		switch {
		case r.Workflow != "" && r.Workflow != run.Workflow && r.Workflow != path:
			return "filtered, the rule for this repository takes another workflow."
		case r.Branch != "" && r.Branch != run.Branch:
			return "filtered, the rule for this repository takes another branch."
		case len(r.Conclusion) > 0 && !slices.Contains(r.Conclusion, run.Conclusion):
			return "filtered, the rule for this repository does not take the conclusion " + run.Conclusion + "."
		default:
			return "filtered, the conclusion is the same as the last run's and the rule asks for a change."
		}
	}
	return "no route names this repository."
}

// CIRoute is one rule from a run to a handle.
type CIRoute struct {
	Repo       string   `json:"repo"`
	Workflow   string   `json:"workflow,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Conclusion []string `json:"conclusion,omitempty"`
	Change     bool     `json:"change,omitempty"`
	To         string   `json:"to"`
	Kind       string   `json:"kind,omitempty"`
	Wake       bool     `json:"wake,omitempty"`
}

const (
	ciMaxRoutes = 200
	ciMaxToLen  = 200
)

var (
	ciRepoRe      = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	ciConclusions = map[string]bool{"success": true, "failure": true, "cancelled": true, "timed_out": true, "action_required": true}

	errCINoRoutes   = errors.New("routes is not a list of rules")
	ciRouteFieldErr = "rule %d: %s"
)

// ParseCIRoutes reads and validates the routes setting. One bad rule refuses the whole list.
func ParseCIRoutes(raw string) ([]CIRoute, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var routes []CIRoute
	if err := dec.Decode(&routes); err != nil {
		return nil, fmt.Errorf("%w: %v", errCINoRoutes, err)
	}
	if len(routes) > ciMaxRoutes {
		return nil, fmt.Errorf("at most %d rules", ciMaxRoutes)
	}
	for i := range routes {
		r := &routes[i]
		r.Repo, r.Workflow, r.Branch, r.To, r.Kind = strings.TrimSpace(r.Repo), strings.TrimSpace(r.Workflow),
			strings.TrimSpace(r.Branch), strings.TrimSpace(r.To), strings.TrimSpace(r.Kind)
		switch {
		case !ciRepoRe.MatchString(r.Repo):
			return nil, fmt.Errorf(ciRouteFieldErr, i+1, "repo is required, as owner/name")
		case r.To == "" || len(r.To) > ciMaxToLen || strings.ContainsAny(r.To, " \t\r\n"):
			return nil, fmt.Errorf(ciRouteFieldErr, i+1, "to is required: a handle, alias or name@room")
		case r.Kind != "" && r.Kind != "fyi" && r.Kind != "needs":
			return nil, fmt.Errorf(ciRouteFieldErr, i+1, "kind is fyi or needs")
		}
		for _, c := range r.Conclusion {
			if !ciConclusions[c] {
				return nil, fmt.Errorf(ciRouteFieldErr, i+1,
					"conclusion is a list from success, failure, cancelled, timed_out, action_required")
			}
		}
	}
	return routes, nil
}

// kindFor is the rule's kind, or by default needs for a red and fyi for the rest.
func (r CIRoute) kindFor(conclusion string) string {
	if r.Kind != "" {
		return r.Kind
	}
	if conclusion == "failure" || conclusion == "timed_out" {
		return "needs"
	}
	return "fyi"
}

func (r CIRoute) matches(run hubstore.CIRun, path, last string, hadLast bool) bool {
	if !strings.EqualFold(r.Repo, run.Repo) {
		return false
	}
	if r.Workflow != "" && r.Workflow != run.Workflow && r.Workflow != path {
		return false
	}
	if r.Branch != "" && r.Branch != run.Branch {
		return false
	}
	if len(r.Conclusion) > 0 {
		hit := false
		for _, c := range r.Conclusion {
			hit = hit || c == run.Conclusion
		}
		if !hit {
			return false
		}
	}
	if r.Change && hadLast && last == run.Conclusion {
		return false
	}
	return true
}

// ciTarget is one thing to tell: every matching rule adds one, and two rules naming the same string send one.
type ciTarget struct {
	To   string
	Kind string
	Wake bool
}

func ciTargets(routes []CIRoute, run hubstore.CIRun, path, last string, hadLast bool) []ciTarget {
	var out []ciTarget
	for _, r := range routes {
		if !r.matches(run, path, last, hadLast) {
			continue
		}
		kind := r.kindFor(run.Conclusion)
		merged := false
		for i := range out {
			if strings.EqualFold(out[i].To, r.To) {
				if kind == "needs" {
					out[i].Kind = "needs"
				}
				out[i].Wake = out[i].Wake || r.Wake
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, ciTarget{To: r.To, Kind: kind, Wake: r.Wake})
		}
	}
	return out
}

// Run is the delivery worker: one pass when a notice arrives and one every 30 seconds, until the context ends.
func (h *CIHook) Run(ctx context.Context) {
	t := time.NewTicker(ciTick)
	defer t.Stop()
	for {
		h.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.kick:
		}
	}
}

// Pass tries every pending delivery once. The name is resolved again on every try, so a new session that took the alias
// gets what the last holder missed.
func (h *CIHook) Pass(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if now.Sub(h.swept) >= ciSweepEvery {
		h.swept = now
		_, _ = h.Store.CISweep()
	}
	pending, err := h.Store.CIDeliveriesPending()
	if err != nil {
		return
	}
	live := pending[:0:0]
	for _, d := range pending {
		if !d.Created.Add(ciPendingFor).After(now) {
			h.finish(d, hubstore.CIExpired, "no session took it in 24 hours", d.Card, "ci-delivery-expired")
			continue
		}
		live = append(live, d)
	}
	newest := map[string]string{}
	for _, d := range live {
		newest[ciStream(d)] = d.ID
	}
	for _, d := range live {
		if newest[ciStream(d)] != d.ID {
			h.finish(d, hubstore.CIExpired, "superseded", d.Card, "ci-delivery-expired")
			continue
		}
		h.try(ctx, d)
	}
}

func ciStream(d hubstore.CIDelivery) string {
	return strings.ToLower(d.Target) + "\x00" + strings.ToLower(d.Repo) + "\x00" + d.Workflow + "\x00" + d.Branch
}

func (h *CIHook) finish(d hubstore.CIDelivery, state, detail, card, auditKind string) {
	if err := h.Store.CIDeliverySet(d.ID, state, detail, card); err != nil {
		return
	}
	if auditKind != "" {
		h.audit(auditKind, fmt.Sprintf("%s %s on %s run %d to %s: %s", d.Repo, d.Workflow, d.Branch, d.RunID, d.Target, detail))
	}
}

func (h *CIHook) try(ctx context.Context, d hubstore.CIDelivery) {
	if h.Deliver == nil {
		return
	}
	t, ans := h.Deliver.Resolve(ctx, d.Target)
	if ans.State != "" {
		h.settle(d, ans, "")
		return
	}
	if sibs, err := h.Store.CIDeliveriesOf(d.RunRef); err == nil {
		for _, s := range sibs {
			if s.ID != d.ID && s.Card == t.Card && (s.State == hubstore.CIDelivered || s.State == hubstore.CIUnconfirmed) {
				h.finish(d, hubstore.CIFolded, "the same session as "+s.Target, t.Card, "")
				return
			}
		}
	}
	kind := d.Kind
	h.settle(d, h.Deliver.Send(ctx, t, ciFrom, d.Text, kind, d.Wake), t.Card)
}

func (h *CIHook) settle(d hubstore.CIDelivery, a CIAnswer, card string) {
	switch a.State {
	case hubstore.CIRefused:
		h.finish(d, a.State, a.Detail, card, "ci-delivery-refused")
	case hubstore.CIDelivered, hubstore.CIUnconfirmed:
		h.finish(d, a.State, a.Detail, card, "")
	default:
		h.finish(d, hubstore.CIPending, a.Detail, card, "")
	}
}

// ── the say path ───────────────────────────────────────

type proxyCIDeliverer struct{ p *Proxy }

// CIDeliverer is the hub's own say path, through the control server's resolution and post.
func (p *Proxy) CIDeliverer() CIDeliverer { return proxyCIDeliverer{p} }

func (d proxyCIDeliverer) ctl() *controlMCP {
	d.p.mu.Lock()
	defer d.p.mu.Unlock()
	return d.p.ctl
}

func (d proxyCIDeliverer) Resolve(ctx context.Context, to string) (CITarget, CIAnswer) {
	c := d.ctl()
	if c == nil {
		return CITarget{}, CIAnswer{State: hubstore.CIPending, Detail: "the hub's control server is not up"}
	}
	scope, id, handle, err := c.resolveCard(ctx, "", to)
	if err != nil {
		var amb *errAmbiguous
		var be *boardError
		switch {
		case errors.As(err, &amb):
			return CITarget{}, CIAnswer{State: hubstore.CIRefused, Detail: err.Error()}
		case errors.As(err, &be) && be.code != http.StatusBadGateway && be.code != http.StatusServiceUnavailable &&
			be.code != http.StatusGatewayTimeout && be.code != http.StatusNotFound:
			return CITarget{}, CIAnswer{State: hubstore.CIRefused, Detail: be.msg}
		}
		return CITarget{}, CIAnswer{State: hubstore.CIPending, Detail: err.Error()}
	}
	return CITarget{Card: tagFor(scope, id), Room: scope, Handle: handle, ID: id}, CIAnswer{}
}

func (d proxyCIDeliverer) Send(ctx context.Context, t CITarget, from, text, kind string, wake bool) CIAnswer {
	c := d.ctl()
	if c == nil {
		return CIAnswer{State: hubstore.CIPending, Detail: "the hub's control server is not up"}
	}
	ans := c.postSay(ctx, t.Room, t.ID, t.Handle, from, text, "", kind, wake)
	switch {
	case ans.OK && ans.Delivered == "parked":
		return CIAnswer{State: hubstore.CIPending, Detail: "the session is parked and the route does not wake it"}
	case ans.OK:
		return CIAnswer{State: hubstore.CIDelivered, Detail: ans.Delivered}
	case ans.Unconfirmed:
		return CIAnswer{State: hubstore.CIUnconfirmed, Detail: ans.Error}
	case ans.Unreachable:
		return CIAnswer{State: hubstore.CIPending, Detail: ans.Error}
	}
	return CIAnswer{State: hubstore.CIRefused, Detail: ans.Error}
}
