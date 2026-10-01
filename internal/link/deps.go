package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/itemgate"
)

// Item gates as the hub serves and checks them. See docs/runtime/item-dependencies-design.md.
//
// ── ONLY THE HUB DECIDES A GATE IS MET ──────────────────
//
// A gate is met by something the hub reads for itself (the integration branch in the
// registered checkout, its own attached rooms, its own build commit) or by a person on the
// board. No tool and no route an agent is handed marks one met. /_hub/deps/clear refuses a
// request carrying X-Atrium-Agent, which is tidiness and not a boundary: an agent can curl
// loopback without the header, the way it could curl a permission decision. What stops that
// is the permission gate seeing the command, and every clear is audited with where it came
// from, so one that did not come from the board shows.
//
// ── A FAILED CHECK LEAVES A GATE OPEN ───────────────────
//
// A git read that fails or times out, a repository not in git_repos, a hub built as `dev`:
// the gate stays open with the reason on the row. Nothing here halts anything, and nothing
// here clears a gate because it could not look.

// DepsStore is what the gates need of the hub's database. hubstore.Store has every method.
type DepsStore interface {
	AddGates(repo, item string, waits []itemgate.Target, why, addedBy string) ([]itemgate.Gate, error)
	Gates(repo, item string, openOnly bool) ([]itemgate.Gate, error)
	Gate(id string) (itemgate.Gate, error)
	HasOpenGates() (bool, error)
	GateMet(id, by, why string) (bool, error)
	GateTold(ids []string) error
	PendingTells() ([]itemgate.Gate, error)
	RenameItem(repo, from, to, by string) (int, error)
	ItemNames(repo, id string) ([]string, error)
}

const (
	// depsCheckBound is how long one pass of checks may spend on git. Every read is local
	// (rev-parse, ls-tree, merge-base, log), so this only bites on a sick disk.
	depsCheckBound = 20 * time.Second
	// depsTickEvery is the one hub ticker that re-checks open gates and tells waiters.
	depsTickEvery = time.Minute
	// depsTellFrom is who a waiter hears the news from.
	depsTellFrom = "atrium-hub"
	// depsTellGiveUp is how long a waiter that cannot be reached is tried, the same day a
	// held say is kept for.
	depsTellGiveUp = 24 * time.Hour
	// depsReadyMax bounds how many backlog files one ready call reads.
	depsReadyMax = 500
)

// deps is the hub's gate state: the store, the hub's own build, and the landed-items cache.
type deps struct {
	store DepsStore
	// build is the commit this hub binary was built from. Empty for a `dev` build, which
	// answers every `live:` gate as unknown.
	build string

	mu     sync.Mutex
	landed map[string]landedCache
}

// landedCache is one repository's landed items at one branch tip.
type landedCache struct {
	tip   string
	items map[string]string
}

// SetDeps wires item gates. `build` is the hub's own build commit. Optional: without it
// /_hub/deps answers 404 and launches are never checked.
func (p *Proxy) SetDeps(st DepsStore, build string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deps = &deps{store: st, build: strings.TrimSpace(build)}
}

func (p *Proxy) depsState() *deps {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deps
}

// depView is a gate with what the hub makes of it right now.
type depView struct {
	ID      string     `json:"id"`
	Repo    string     `json:"repo"`
	Item    string     `json:"item"`
	Kind    string     `json:"kind"`
	Target  string     `json:"target"`
	Why     string     `json:"why,omitempty"`
	AddedBy string     `json:"added_by"`
	AddedAt time.Time  `json:"added_at"`
	MetAt   *time.Time `json:"met_at,omitempty"`
	MetBy   string     `json:"met_by,omitempty"`
	// State is `open` or `met`. Reason says why: what was seen when it was met, or what is
	// missing while it is open.
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

func viewOf(g itemgate.Gate, reason string) depView {
	v := depView{ID: g.ID, Repo: g.Repo, Item: g.Item, Kind: g.Kind, Target: g.Target, Why: g.Why,
		AddedBy: g.AddedBy, AddedAt: g.AddedAt, MetAt: g.MetAt, MetBy: g.MetBy, State: "open", Reason: reason}
	if !g.Open() {
		v.State, v.Reason = "met", g.MetWhy
	}
	return v
}

// ── git reads ───────────────────────────────────────────

func runnerOf(g *gitsync.Hub) *gitsync.Runner {
	if g.Runner != nil {
		return g.Runner
	}
	return gitsync.Default
}

// repoFor is the git_repos entry a call means. Empty means the only one there is.
func (p *Proxy) repoFor(name string) (gitsync.Repo, error) {
	g := p.git()
	if g == nil || g.Repos == nil {
		return gitsync.Repo{}, errors.New("this hub has no git side, so it cannot read the integration branch")
	}
	repos, err := g.Repos()
	if err != nil {
		return gitsync.Repo{}, fmt.Errorf("git_repos is refused: %w", err)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		switch len(repos) {
		case 0:
			return gitsync.Repo{}, errors.New("git_repos is empty, so there is no integration branch to read")
		case 1:
			return repos[0], nil
		}
		names := make([]string, 0, len(repos))
		for _, r := range repos {
			names = append(names, r.Name)
		}
		return gitsync.Repo{}, fmt.Errorf("say which repository: %s", strings.Join(names, ", "))
	}
	for _, r := range repos {
		if r.Name == name {
			return r, nil
		}
	}
	return gitsync.Repo{}, fmt.Errorf("%q is not in git_repos", name)
}

func (p *Proxy) gitIn(ctx context.Context, r gitsync.Repo, args ...string) (string, error) {
	return runnerOf(p.git()).Git(ctx, r.Checkout, args...)
}

// tipOf is the integration branch's commit in the hub's checkout.
func (p *Proxy) tipOf(ctx context.Context, r gitsync.Repo) (string, error) {
	out, err := p.gitIn(ctx, r, "rev-parse", "--verify", "--quiet", "refs/heads/"+r.Branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("could not read %s in %s: %v", r.Branch, r.Checkout, err)
	}
	return strings.TrimSpace(out), nil
}

// landedItems is every item with a changelog entry on the branch tip, id to path. Cached by
// tip, so a read on an unchanged branch costs one rev-parse.
func (p *Proxy) landedItems(ctx context.Context, d *deps, r gitsync.Repo) (map[string]string, string, error) {
	tip, err := p.tipOf(ctx, r)
	if err != nil {
		return nil, "", err
	}
	d.mu.Lock()
	c, ok := d.landed[r.Name]
	d.mu.Unlock()
	if ok && c.tip == tip {
		return c.items, tip, nil
	}
	out, err := p.gitIn(ctx, r, "ls-tree", "-r", "--name-only", tip, "--", "changelog")
	if err != nil {
		return nil, "", fmt.Errorf("could not list changelog on %s: %v", r.Branch, err)
	}
	items := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if id, ok := itemgate.LandedID(strings.TrimSpace(line)); ok {
			items[id] = strings.TrimSpace(line)
		}
	}
	d.mu.Lock()
	if d.landed == nil {
		d.landed = map[string]landedCache{}
	}
	d.landed[r.Name] = landedCache{tip: tip, items: items}
	d.mu.Unlock()
	return items, tip, nil
}

// isAncestor is `merge-base --is-ancestor a b`: true, false, or git could not say.
func (p *Proxy) isAncestor(ctx context.Context, r gitsync.Repo, a, b string) (bool, error) {
	_, err := p.gitIn(ctx, r, "merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	var ge *gitsync.Error
	var ee *exec.ExitError
	if errors.As(err, &ge) && errors.As(ge.Err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ── the check ───────────────────────────────────────────

// landedAs finds an item's changelog entry under any name it has had.
func (p *Proxy) landedAs(ctx context.Context, d *deps, r gitsync.Repo, id string) (string, string, error) {
	items, tip, err := p.landedItems(ctx, d, r)
	if err != nil {
		return "", "", err
	}
	names, err := d.store.ItemNames(r.Name, id)
	if err != nil {
		return "", tip, err
	}
	for _, n := range names {
		if at, ok := items[n]; ok {
			return at, tip, nil
		}
	}
	return "", tip, nil
}

// evaluate is section 3's table: whether a gate's target is met, and in words why or why not.
func (p *Proxy) evaluate(ctx context.Context, d *deps, g itemgate.Gate) (bool, string) {
	if g.Kind == itemgate.KindCond {
		kind, arg := itemgate.Cond(g.Target)
		switch kind {
		case "room":
			if p.hub == nil {
				return false, "this hub has no link, so no room is attached"
			}
			for _, a := range p.hub.Rooms() {
				if equalFold(a.Name, arg) {
					return true, arg + " attached to the hub"
				}
			}
			return false, arg + " is not attached to the hub"
		case "":
			return false, "a named condition, cleared by a human only"
		}
	}
	r, err := p.repoFor(g.Repo)
	if err != nil {
		return false, err.Error()
	}
	switch {
	case g.Kind == itemgate.KindItem:
		at, tip, err := p.landedAs(ctx, d, r, g.Target)
		if err != nil {
			return false, err.Error()
		}
		if at == "" {
			return false, fmt.Sprintf("not landed: no changelog/*/*-%s.md on %s %s", g.Target, r.Branch, short(tip))
		}
		return true, fmt.Sprintf("%s landed (%s on %s %s)", g.Target, at, r.Branch, short(tip))
	}
	kind, arg := itemgate.Cond(g.Target)
	switch kind {
	case "sha":
		if !itemgate.ValidSHA(arg) {
			return false, fmt.Sprintf("%q is not a commit id", arg)
		}
		tip, err := p.tipOf(ctx, r)
		if err != nil {
			return false, err.Error()
		}
		in, err := p.isAncestor(ctx, r, arg, tip)
		switch {
		case err != nil:
			return false, fmt.Sprintf("could not ask git about %s: %v", arg, err)
		case !in:
			return false, fmt.Sprintf("%s is not on %s %s", arg, r.Branch, short(tip))
		}
		return true, fmt.Sprintf("%s is on %s %s", arg, r.Branch, short(tip))
	case "live":
		if !itemgate.ValidItem(arg) {
			return false, fmt.Sprintf("%q is not an item id", arg)
		}
		at, tip, err := p.landedAs(ctx, d, r, arg)
		if err != nil {
			return false, err.Error()
		}
		if at == "" {
			return false, fmt.Sprintf("%s has not landed: no changelog/*/*-%s.md on %s %s", arg, arg, r.Branch, short(tip))
		}
		if d.build == "" {
			return false, "hub build unknown: this hub was built without a commit, so it cannot say what it runs"
		}
		added, err := p.gitIn(ctx, r, "log", "--diff-filter=A", "-1", "--format=%H", tip, "--", at)
		added = strings.TrimSpace(added)
		if err != nil || added == "" {
			return false, fmt.Sprintf("could not find the commit that added %s: %v", at, err)
		}
		in, err := p.isAncestor(ctx, r, added, d.build)
		switch {
		case err != nil:
			return false, fmt.Sprintf("could not compare %s with the hub build %s: %v", short(added), short(d.build), err)
		case !in:
			return false, fmt.Sprintf("%s landed in %s, which is not in the hub build %s", arg, short(added), short(d.build))
		}
		return true, fmt.Sprintf("%s is live: %s is in the hub build %s", arg, short(added), short(d.build))
	}
	return false, "a named condition, cleared by a human only"
}

// refresh checks every open gate and records the ones now met. Met is sticky: a gate
// already met is shown as it was met and never checked again.
func (p *Proxy) refresh(ctx context.Context, d *deps, gates []itemgate.Gate) []depView {
	ctx, cancel := context.WithTimeout(ctx, depsCheckBound)
	defer cancel()
	out := make([]depView, 0, len(gates))
	for _, g := range gates {
		if !g.Open() {
			out = append(out, viewOf(g, ""))
			continue
		}
		met, why := p.evaluate(ctx, d, g)
		if !met {
			out = append(out, viewOf(g, why))
			continue
		}
		changed, err := d.store.GateMet(g.ID, itemgate.MetByAtrium, why)
		if err != nil {
			out = append(out, viewOf(g, "met, but it could not be recorded: "+err.Error()))
			continue
		}
		if fresh, err := d.store.Gate(g.ID); err == nil {
			g = fresh
		}
		if changed {
			p.depsChanged(g.Repo, g.Item, "met")
		}
		out = append(out, viewOf(g, why))
	}
	return out
}

// depsChanged tells watching boards a gate moved. A delta: the board re-fetches.
func (p *Proxy) depsChanged(repo, item, what string) {
	payload, err := json.Marshal(map[string]string{"repo": repo, "item": item, "what": what})
	if err != nil {
		return
	}
	p.feeds.emit(Event{Kind: "deps", Data: payload})
}

// ── the launch refusal ──────────────────────────────────

// launchGate answers whether a launch titled `title` is for an item with an open gate, and
// the sentence that refuses it. A title that names no gated item, or a store that cannot be
// read, is allowed: every existing launch behaves as it did.
func (p *Proxy) launchGate(ctx context.Context, title string) (string, bool) {
	d := p.depsState()
	item := itemgate.ItemOfTitle(title)
	if d == nil || item == "" {
		return "", false
	}
	gates, err := d.store.Gates("", item, true)
	if err != nil {
		log.Printf("[hub] could not read the gates on %s, so its launch is allowed: %v", item, err)
		return "", false
	}
	if len(gates) == 0 {
		return "", false
	}
	var open []string
	for _, v := range p.refresh(ctx, d, gates) {
		if v.State == "open" {
			open = append(open, fmt.Sprintf("%s (%s)", v.Target, v.Reason))
		}
	}
	if len(open) == 0 {
		return "", false
	}
	return fmt.Sprintf("%s waits on: %s. a gate clears when the board sees the work land or a human clears it. "+
		"atrium_deps check %s shows it again.", item, strings.Join(open, ", "), item), true
}

// ── the ticker, and telling the waiter ──────────────────

// RunDeps re-checks open gates every minute and tells each waiter once when its item is
// clear, until ctx ends. An idle hub, with no open gate and nobody owed a word, pays one
// read a minute.
func (p *Proxy) RunDeps(ctx context.Context) {
	t := time.NewTicker(depsTickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.deployReadyTick(ctx)
			p.depsTick(ctx)
		}
	}
}

func (p *Proxy) depsTick(ctx context.Context) {
	d := p.depsState()
	if d == nil {
		return
	}
	if has, err := d.store.HasOpenGates(); err == nil && has {
		if open, err := d.store.Gates("", "", true); err == nil {
			p.refresh(ctx, d, open)
		}
	}
	p.tellWaiters(ctx, d)
}

// tellWaiters sends ONE say to each agent that added a gate on an item that has no open
// gate left, and stamps the gates so it is never sent twice. A waiter whose room is not
// answering is tried again next tick, for a day.
func (p *Proxy) tellWaiters(ctx context.Context, d *deps) {
	pending, err := d.store.PendingTells()
	if err != nil || len(pending) == 0 {
		return
	}
	type key struct{ repo, item string }
	byItem := map[key][]itemgate.Gate{}
	var order []key
	for _, g := range pending {
		k := key{g.Repo, g.Item}
		if _, ok := byItem[k]; !ok {
			order = append(order, k)
		}
		byItem[k] = append(byItem[k], g)
	}
	for _, k := range order {
		open, err := d.store.Gates(k.repo, k.item, true)
		if err != nil || len(open) > 0 {
			continue
		}
		// Only this round's gates: one met and told in an earlier round said so then.
		var said []string
		for _, g := range byItem[k] {
			if g.MetWhy != "" {
				said = append(said, g.MetWhy)
			}
		}
		text := fmt.Sprintf("%s is ready: %s", k.item, strings.Join(said, "; "))
		waiters := map[string][]itemgate.Gate{}
		for _, g := range byItem[k] {
			waiters[g.AddedBy] = append(waiters[g.AddedBy], g)
		}
		for who, gs := range waiters {
			retry := p.tell(ctx, who, text)
			if retry && !givenUp(gs) {
				continue
			}
			ids := make([]string, 0, len(gs))
			for _, g := range gs {
				ids = append(ids, g.ID)
			}
			if err := d.store.GateTold(ids); err != nil {
				log.Printf("[hub] told %s that %s is ready, and could not record it: %v", who, k.item, err)
			}
		}
	}
}

// givenUp is whether every gate in gs was met more than a day ago.
func givenUp(gs []itemgate.Gate) bool {
	for _, g := range gs {
		if g.MetAt == nil || time.Since(*g.MetAt) < depsTellGiveUp {
			return false
		}
	}
	return true
}

// tell says `text` to `who` (`handle@room`, or a bare handle) through the same board path
// atrium_say takes. It answers true only when it is worth trying again: the board or the
// room did not answer. A refusal (no such session, a parked card) is final, and logged.
func (p *Proxy) tell(ctx context.Context, who, text string) (retry bool) {
	p.mu.Lock()
	c := p.ctl
	p.mu.Unlock()
	if c == nil {
		return true
	}
	name, room, err := SplitAddress(who)
	if err != nil {
		log.Printf("[hub] cannot tell %q that a gate cleared: %v", who, err)
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	id, _, err := c.resolvePeer(ctx, room, name)
	if err == nil {
		err = c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message", room,
			map[string]any{"text": text, "from": depsTellFrom, "when": "done"}, nil)
	}
	if err == nil {
		return false
	}
	var be *boardError
	if errors.As(err, &be) && be.code >= 500 {
		return true
	}
	log.Printf("[hub] could not tell %s that a gate cleared, and will not try again: %v", who, err)
	return false
}

// ── the routes ──────────────────────────────────────────

// serveDeps answers /_hub/deps, /_hub/deps/clear, /_hub/deps/rename and /_hub/deps/ready.
// Reads are open like /_hub/launch-caps. Writes are loopback only.
func (p *Proxy) serveDeps(w http.ResponseWriter, r *http.Request, sub string) {
	d := p.depsState()
	if d == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	write := r.Method != http.MethodGet
	if write && r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a POST")
		return
	}
	if write && !edge.LocalOperator(r) {
		fail(http.StatusForbidden, "gates are changed only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	body := func(v any) bool {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return false
		}
		return true
	}
	switch {
	case sub == "deps" && !write:
		q := r.URL.Query()
		gates, err := d.store.Gates(q.Get("repo"), q.Get("item"), q.Get("open") == "1")
		if err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		views := p.refresh(r.Context(), d, gates)
		if q.Get("open") == "1" {
			// Checked just now, so a gate met by this very read is not listed as open.
			kept := views[:0]
			for _, v := range views {
				if v.State == "open" {
					kept = append(kept, v)
				}
			}
			views = kept
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"gates": views})
	case sub == "deps":
		var in struct {
			Repo    string   `json:"repo"`
			Item    string   `json:"item"`
			WaitsOn []string `json:"waits_on"`
			Why     string   `json:"why"`
			AddedBy string   `json:"added_by"`
		}
		if !body(&in) {
			return
		}
		repo, err := p.repoFor(in.Repo)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		if len(in.WaitsOn) > itemgate.MaxTargets {
			fail(http.StatusBadRequest, fmt.Sprintf("at most %d targets at once", itemgate.MaxTargets))
			return
		}
		var waits []itemgate.Target
		for _, s := range in.WaitsOn {
			t, err := itemgate.ParseTarget(s)
			if err != nil {
				fail(http.StatusBadRequest, err.Error())
				return
			}
			waits = append(waits, t)
		}
		gates, err := d.store.AddGates(repo.Name, in.Item, waits, in.Why, in.AddedBy)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.depsChanged(repo.Name, strings.TrimSpace(in.Item), "added")
		_ = json.NewEncoder(w).Encode(map[string]any{"gates": p.refresh(r.Context(), d, gates)})
	case sub == "deps/clear" && write:
		// FROM THE BOARD ONLY. See the note at the top of this file.
		if strings.TrimSpace(r.Header.Get(AgentHeader)) != "" {
			fail(http.StatusForbidden, "a gate is cleared by a human on the board, never by an agent. "+
				"if a gate is wrong, ask the human")
			return
		}
		var in struct {
			ID  string `json:"id"`
			Why string `json:"why"`
		}
		if !body(&in) {
			return
		}
		if strings.TrimSpace(in.Why) == "" {
			fail(http.StatusBadRequest, "say why it is cleared. the reason is kept on the gate")
			return
		}
		g, err := d.store.Gate(strings.TrimSpace(in.ID))
		if err != nil {
			fail(http.StatusNotFound, "no gate with that id")
			return
		}
		why := "cleared by a human: " + strings.TrimSpace(in.Why)
		changed, err := d.store.GateMet(g.ID, itemgate.MetByHuman, why)
		if err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
		if !changed {
			fail(http.StatusConflict, "that gate is already met")
			return
		}
		p.RecordAudit("", "deps-clear", fmt.Sprintf("%s: %s waits on %s. %s. from %s, origin %q, user agent %q",
			g.ID, g.Item, g.Target, itemgate.Bound(in.Why, itemgate.MaxWhy), r.RemoteAddr,
			r.Header.Get("Origin"), itemgate.Bound(r.Header.Get("User-Agent"), 120)))
		p.depsChanged(g.Repo, g.Item, "cleared")
		if fresh, err := d.store.Gate(g.ID); err == nil {
			g = fresh
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"gate": viewOf(g, "")})
	case sub == "deps/rename" && write:
		var in struct {
			Repo string `json:"repo"`
			From string `json:"from"`
			To   string `json:"to"`
			By   string `json:"by"`
		}
		if !body(&in) {
			return
		}
		repo, err := p.repoFor(in.Repo)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		by := strings.TrimSpace(in.By)
		if by == "" {
			by = itemgate.AddedByHuman
		}
		n, err := d.store.RenameItem(repo.Name, in.From, in.To, by)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.depsChanged(repo.Name, strings.TrimSpace(in.To), "renamed")
		_ = json.NewEncoder(w).Encode(map[string]any{"moved": n})
	case sub == "deps/ready" && !write:
		items, err := p.readyItems(r.Context(), d, r.URL.Query().Get("repo"), r.URL.Query().Get("dept"))
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	default:
		http.NotFound(w, r)
	}
}

// ── ready ───────────────────────────────────────────────

// readyItem is one backlog item a department could start.
type readyItem struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
}

var deptName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// readyItems is a department's backlog files on the integration branch, less the ones that
// landed and the ones with an open gate. It reads claude/main, not anybody's worktree,
// which is how it never hands out an item already landed. Status lines are shown as
// written and trusted for nothing.
func (p *Proxy) readyItems(ctx context.Context, d *deps, repoName, dept string) ([]readyItem, error) {
	dept = strings.TrimSpace(dept)
	if !deptName.MatchString(dept) {
		return nil, fmt.Errorf("%q is not a department", dept)
	}
	r, err := p.repoFor(repoName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, depsCheckBound)
	defer cancel()
	landed, tip, err := p.landedItems(ctx, d, r)
	if err != nil {
		return nil, err
	}
	listing, err := p.gitIn(ctx, r, "ls-tree", "--name-only", tip, "--", "docs/backlog/"+dept+"/")
	if err != nil {
		return nil, fmt.Errorf("could not list docs/backlog/%s on %s: %v", dept, r.Branch, err)
	}
	var files []string
	for _, f := range strings.Split(listing, "\n") {
		f = strings.TrimSpace(f)
		if strings.HasSuffix(f, ".md") && itemgate.ValidItem(strings.TrimSuffix(path.Base(f), ".md")) {
			files = append(files, f)
		}
		if len(files) == depsReadyMax {
			break
		}
	}
	if len(files) == 0 {
		return []readyItem{}, nil
	}

	// Open gates, checked once for the whole repository.
	blocked := map[string]bool{}
	if open, err := d.store.Gates(r.Name, "", true); err == nil {
		for _, v := range p.refresh(ctx, d, open) {
			if v.State == "open" {
				blocked[v.Item] = true
			}
		}
	} else {
		return nil, err
	}

	var stdin bytes.Buffer
	for _, f := range files {
		stdin.WriteString(tip + ":" + f + "\n")
	}
	raw, err := runnerOf(p.git()).GitInput(ctx, r.Checkout, stdin.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("could not read the backlog files: %v", err)
	}
	heads := batchHeads(raw, len(files))

	out := []readyItem{}
	for i, f := range files {
		id := strings.TrimSuffix(path.Base(f), ".md")
		if blocked[id] {
			continue
		}
		names, err := d.store.ItemNames(r.Name, id)
		if err != nil {
			return nil, err
		}
		done := false
		for _, n := range names {
			if _, ok := landed[n]; ok {
				done = true
			}
			if blocked[n] {
				done = true
			}
		}
		if done {
			continue
		}
		it := readyItem{ID: id}
		if i < len(heads) {
			it.Title, it.Status = backlogHead(id, heads[i])
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// batchHeads splits `cat-file --batch` output into each object's text, in order. A missing
// object is an empty entry.
func batchHeads(raw string, n int) []string {
	out := make([]string, 0, n)
	for len(raw) > 0 && len(out) < n {
		nl := strings.IndexByte(raw, '\n')
		if nl < 0 {
			break
		}
		head := strings.Fields(raw[:nl])
		raw = raw[nl+1:]
		if len(head) != 3 {
			out = append(out, "")
			continue
		}
		size, err := strconv.Atoi(head[2])
		if err != nil || size > len(raw) {
			break
		}
		out = append(out, raw[:size])
		raw = strings.TrimPrefix(raw[size:], "\n")
	}
	return out
}

// backlogHead reads a backlog file's title and Status line from its first twelve lines,
// the lines backlog-index.ps1 reads.
func backlogHead(id, text string) (title, status string) {
	lines := strings.SplitN(text, "\n", 13)
	if len(lines) > 12 {
		lines = lines[:12]
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		switch {
		case title == "" && strings.HasPrefix(l, "# "):
			t := strings.TrimSpace(strings.TrimPrefix(l, "# "))
			for _, sep := range []string{".", ":"} {
				if rest, ok := strings.CutPrefix(t, id+sep); ok {
					t = strings.TrimSpace(rest)
					break
				}
			}
			title = itemgate.Bound(t, 200)
		case status == "" && strings.HasPrefix(l, "Status:"):
			status = itemgate.Bound(strings.TrimSpace(strings.TrimPrefix(l, "Status:")), 300)
		}
	}
	return title, status
}
