package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE STEP ENGINE, THE PART THAT GATHERS (item lifecycle L2). Derive (lifecycle.go) is pure, and this is what feeds it and
// keeps what it answers: it notices a designer's finished commit as a `design` fact, reads the design doc's state from the
// hub's git store, derives, and writes the cache (`work_step`). It runs when something it reads changes:
//
//   - an assignment push the hub applied (items.go), off the request's path, since a git read may take seconds;
//   - a board action (work_actions.go);
//   - a read of one item, so a commit to the doc shows as `stale` the next time anyone looks.
//
// A design is only ever NOTICED here, from a finished designer assignment whose note names a commit that exists in the
// hub's store and changes a markdown file. A card never says "this is the design doc": the commit is the evidence.

// designGit is what the design steps read of git. gitsync.Store has it, and a test brings its own.
type designGit interface {
	FindCommit(ctx context.Context, sha string) (repo, full string, ok bool)
	CommitFiles(ctx context.Context, repo, sha string) ([]string, error)
	LastTouch(ctx context.Context, repo, base, path string) (string, error)
	TouchesSince(ctx context.Context, repo, base, path string) (int, error)
}

var _ designGit = (*gitsync.Store)(nil)

// designReader is what to read git with: a test's, else the hub's own store, else nothing. Nothing is not an error: the
// steps that need no git are still derived, and a design waits to be noticed.
func (p *Proxy) designReader() designGit {
	p.mu.Lock()
	o := p.designGit
	p.mu.Unlock()
	if o != nil {
		return o
	}
	if g := p.git(); g != nil {
		if s := g.Store(); s != nil {
			return s
		}
	}
	return nil
}

// workLocks is one lock per item, so two refreshes of one item do not interleave and two items never wait on each other.
var workLocks sync.Map

func lockWork(id string) func() {
	v, _ := workLocks.LoadOrStore(id, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// designPayload is what a design fact says of where the doc is.
type designPayload struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// pickDesignDoc is the design doc among the files a designer's commit changed: a markdown file, the one named for the
// item when there are several, then one under a design or rnd folder, then the first by name. A commit that changes no
// markdown file is not a design.
func pickDesignDoc(files []string, id string) (string, bool) {
	var md []string
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f), ".md") {
			md = append(md, f)
		}
	}
	if len(md) == 0 {
		return "", false
	}
	id = strings.ToLower(id)
	score := func(f string) int {
		l, n := strings.ToLower(f), 0
		if id != "" && strings.Contains(l, id) {
			n += 4
		}
		if strings.Contains(l, "design") || strings.Contains(l, "/rnd/") {
			n += 2
		}
		if strings.HasPrefix(l, "docs/") {
			n++
		}
		return n
	}
	sort.SliceStable(md, func(i, j int) bool {
		if si, sj := score(md[i]), score(md[j]); si != sj {
			return si > sj
		}
		return md[i] < md[j]
	})
	return md[0], true
}

// workRefresh is the result of one run: the derivation, and the doc's latest commit, which an approval pins.
type workRefresh struct {
	Work        hubstore.Work
	Cards       []hubstore.WorkCard
	Assignments []hubstore.Item
	Facts       []hubstore.WorkFact
	Lifecycle   Lifecycle
	Doc         DocView
}

// refreshWork notices designs, derives the steps and caches them. withGit false derives from what is already known, which
// is how a list of many items stays one cheap pass.
func (p *Proxy) refreshWork(ctx context.Context, st *hubstore.Store, id string, withGit bool) (workRefresh, error) {
	defer lockWork(id)()
	var r workRefresh
	var err error
	if r.Work, err = st.WorkByID(id); err != nil {
		return r, err
	}
	if r.Cards, err = st.WorkCards(id); err != nil {
		return r, err
	}
	if r.Assignments, err = st.WorkAssignments(id); err != nil {
		return r, err
	}
	if r.Facts, err = st.WorkFacts(id); err != nil {
		return r, err
	}
	gr := designGit(nil)
	if withGit {
		gr = p.designReader()
	}
	if gr != nil && p.noticeDesigns(ctx, st, gr, &r) {
		if r.Facts, err = st.WorkFacts(id); err != nil {
			return r, err
		}
	}
	r.Doc = docView(ctx, gr, r.Facts)
	stored, err := st.WorkSteps(id)
	if err != nil {
		return r, err
	}
	ev := Evidence{}
	if withGit {
		ev = p.readEvidence(ctx, r.Work, r.Facts)
	} else {
		ev = evidenceMemo(id)
	}
	r.Lifecycle = Derive(DeriveIn{Work: r.Work, Cards: r.Cards, Assignments: r.Assignments, Facts: r.Facts, Doc: r.Doc, Stored: stored, Evidence: ev})
	// ONLY THE ENGINE'S OWN FIVE ARE WRITTEN. landed, deployed and pushed are L3's rows, and this does not touch them.
	for _, s := range r.Lifecycle.Steps {
		if !engineStep(s.Step) {
			continue
		}
		s.BacklogID = id
		if err := st.PutWorkStep(s); err != nil {
			return r, err
		}
	}
	return r, nil
}

// EvidenceGit is what the tested and reviewed steps read of the hub's repository. The same value that is a LandGit may also be
// this one. Nil, or a branch the hub does not hold, is "git could not be read" and the steps are derived from the facts alone.
type EvidenceGit interface {
	ItemEvidence(ctx context.Context, branch string, shas []string) (gitsync.Evidence, error)
}

// evidenceMemos is the last evidence read for each item. A read that fails is not allowed to unsay a test that had gone stale,
// so the last good answer stands in for it.
var evidenceMemos sync.Map

func evidenceMemo(id string) Evidence {
	if v, ok := evidenceMemos.Load(id); ok {
		return v.(Evidence)
	}
	return Evidence{}
}

// readEvidence reads the item's branch for the shas its tests and reviews name.
func (p *Proxy) readEvidence(ctx context.Context, w hubstore.Work, facts []hubstore.WorkFact) Evidence {
	p.mu.Lock()
	lg := p.landGit
	p.mu.Unlock()
	g, ok := lg.(EvidenceGit)
	if !ok || g == nil || w.Branch == "" {
		return evidenceMemo(w.BacklogID)
	}
	seen := map[string]bool{}
	var shas []string
	for _, f := range facts {
		if (f.Kind == hubstore.FactTest || f.Kind == hubstore.FactReview) && f.AtSHA != "" && !seen[f.AtSHA] {
			seen[f.AtSHA] = true
			shas = append(shas, f.AtSHA)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	got, err := g.ItemEvidence(cctx, w.Branch, shas)
	if err != nil || got.Tip == "" {
		return evidenceMemo(w.BacklogID)
	}
	ev := Evidence{Known: true, Tip: got.Tip, Since: got.Since, CoverKnown: got.CoverKnown,
		Cover: CoverView{Total: got.Cover.Total, Covered: got.Cover.Covered, Held: len(got.Cover.Held), TipCovered: got.Cover.TipCovered}}
	evidenceMemos.Store(w.BacklogID, ev)
	return ev
}

// noticeDesigns writes a `design` fact for each finished designer assignment whose commit is a design the hub can see, once.
// True when it wrote one.
func (p *Proxy) noticeDesigns(ctx context.Context, st *hubstore.Store, gr designGit, r *workRefresh) bool {
	have := map[string]bool{}
	for _, f := range r.Facts {
		if f.Kind == hubstore.FactDesign {
			have[f.Ref] = true
		}
	}
	design := map[string]bool{}
	for _, c := range r.Cards {
		if StepOfCard(c) == hubstore.StepDesigned {
			design[cardKey(c.Room, c.Card)] = true
		}
	}
	wrote := false
	for _, a := range r.Assignments {
		sha := NoteSHA(a.Note)
		if a.State != hubstore.ItemStateFinished || sha == "" || have[a.ID] || !design[cardKey(a.Room, a.Card)] {
			continue
		}
		repo, full, ok := gr.FindCommit(ctx, sha)
		if !ok {
			continue
		}
		files, err := gr.CommitFiles(ctx, repo, full)
		if err != nil {
			continue
		}
		path, ok := pickDesignDoc(files, r.Work.BacklogID)
		if !ok {
			continue
		}
		pl, _ := json.Marshal(designPayload{Repo: repo, Path: path})
		if w, err := st.AddWorkFact(hubstore.WorkFact{
			BacklogID: r.Work.BacklogID, Kind: hubstore.FactDesign, Source: hubstore.SourceSeen,
			Card: cardKey(a.Room, a.Card), Ref: a.ID, AtSHA: full, Payload: string(pl),
		}); err == nil && w {
			wrote = true
		}
	}
	return wrote
}

// docView reads the design doc's state from git: its newest commit, and how many changed it since the approved one. An
// unreadable git is not Known, and a done step stays done.
func docView(ctx context.Context, gr designGit, facts []hubstore.WorkFact) DocView {
	design, ok := latest(facts, hubstore.FactDesign)
	if !ok || gr == nil {
		return DocView{}
	}
	var pl designPayload
	if json.Unmarshal([]byte(design.Payload), &pl) != nil || pl.Repo == "" || pl.Path == "" {
		return DocView{}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	v := DocView{Latest: design.AtSHA}
	l, err := gr.LastTouch(cctx, pl.Repo, design.AtSHA, pl.Path)
	if err != nil {
		return DocView{}
	}
	v.Latest, v.Known = l, true
	for _, f := range facts {
		if f.Kind == hubstore.FactApprove && f.Note("design") == design.AtSHA {
			n, err := gr.TouchesSince(cctx, pl.Repo, f.AtSHA, pl.Path)
			if err != nil {
				return DocView{}
			}
			v.Since = n
		}
	}
	return v
}

// refreshWorkSoon runs a refresh off the caller's path, for a push the hub has just applied. Every failure is the log's
// business at most: the next read derives again.
func (p *Proxy) refreshWorkSoon(st *hubstore.Store, id string) {
	if id == "" {
		return
	}
	p.goWork(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := p.refreshWork(ctx, st, id, true); err == nil {
			p.backlogEvent("item", map[string]string{"id": id, "work": "1"})
		}
	})
}

// goWork runs fn on a goroutine WaitWork can wait for.
func (p *Proxy) goWork(fn func()) {
	p.work.Add(1)
	go func() {
		defer p.work.Done()
		fn()
	}()
}

// WaitWork waits up to limit for the goroutines goWork started, and says whether they all finished. The hub's store
// closes after it, and a call still running then would hold the database file.
func (p *Proxy) WaitWork(limit time.Duration) bool {
	done := make(chan struct{})
	go func() {
		p.work.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(limit):
		return false
	}
}

func newFactRef() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── what the human does ─────────────────────────────────────────────────────

// workAct is `POST /_hub/work/<id>`: {do: approve|send_back|build_directly, by, sha?, note?}. Only the operator on the hub's
// machine, as the items tab's own writes are, and the board sends `by: board`.
type workAct struct {
	Do   string `json:"do"`
	By   string `json:"by"`
	SHA  string `json:"sha"`
	Note string `json:"note"`
}

func (p *Proxy) workAction(w http.ResponseWriter, r *http.Request, st *hubstore.Store, id string) {
	var in workAct
	if err := crDecode(r, &in); err != nil {
		crFail(w, http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	by := strings.TrimSpace(in.By)
	if by == "" {
		by = "board"
	}
	cur, err := p.refreshWork(r.Context(), st, id, true)
	if err != nil {
		wfail(w, err)
		return
	}
	step := func(name string) hubstore.WorkStepRow {
		for _, s := range cur.Lifecycle.Steps {
			if s.Step == name {
				return s
			}
		}
		return hubstore.WorkStepRow{}
	}
	design, haveDesign := latest(cur.Facts, hubstore.FactDesign)
	switch strings.ToLower(strings.TrimSpace(in.Do)) {
	case "approve":
		ap := step(hubstore.StepApproved)
		if ap.State != hubstore.StepActive && ap.State != hubstore.StepStale {
			crFail(w, http.StatusConflict, "there is no design waiting for approval: approved is "+ap.State)
			return
		}
		// THE APPROVAL NAMES THE COMMIT IT WAS SHOWN. A design that moved since is a 409, so what is approved is what was read.
		want := firstNonEmpty(cur.Doc.Latest, design.AtSHA)
		if sha := strings.ToLower(strings.TrimSpace(in.SHA)); sha != "" && sha != want {
			crFail(w, http.StatusConflict, "the design is now at "+short(want)+", not "+short(sha)+". read it again")
			return
		}
		pl, _ := json.Marshal(map[string]string{"design": design.AtSHA, "by": by})
		if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: hubstore.FactApprove, Source: hubstore.SourceSeen,
			AtSHA: want, Payload: string(pl)}); err != nil {
			wfail(w, err)
			return
		}
		p.RecordAudit("", "work-approve", id)
	case "send_back":
		note := strings.TrimSpace(in.Note)
		switch {
		case note == "":
			crFail(w, http.StatusBadRequest, "a design is sent back with a note, which is typed to the designer")
			return
		case len(note) > hubstore.ItemNoteMax:
			crFail(w, http.StatusBadRequest, "that note is too long")
			return
		case !haveDesign || step(hubstore.StepDesigned).State != hubstore.StepDone:
			crFail(w, http.StatusConflict, "there is no design to send back: designed is "+step(hubstore.StepDesigned).State)
			return
		}
		a, err := st.ItemByID(design.Ref)
		if err != nil {
			ifail(w, err)
			return
		}
		raw, _ := json.Marshal(map[string]string{"by": by, "answer": note})
		code, resp, err := p.callItemRoom(r.Context(), a.Room, "/v1/items/"+a.ID+"/sendback", raw)
		if err != nil {
			crFail(w, http.StatusBadGateway, "the room that ran the design is not answering: "+err.Error())
			return
		}
		if code >= 300 {
			w.WriteHeader(code)
			_, _ = w.Write(resp)
			return
		}
		pl, _ := json.Marshal(map[string]string{"note": note, "by": by})
		if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: hubstore.FactSendBack, Source: hubstore.SourceSeen,
			Card: cardKey(a.Room, a.Card), Ref: newFactRef(), AtSHA: design.AtSHA, Payload: string(pl)}); err != nil {
			wfail(w, err)
			return
		}
		p.RecordAudit(a.Room, "work-send-back", id)
	case "build_directly":
		if haveDesign || step(hubstore.StepDesigned).State == hubstore.StepActive {
			crFail(w, http.StatusConflict, "a design is already in play for that item, so it is not filed to build directly")
			return
		}
		pl, _ := json.Marshal(map[string]string{"by": by})
		if _, err := st.AddWorkFact(hubstore.WorkFact{BacklogID: id, Kind: hubstore.FactBuildDirectly, Source: hubstore.SourceSeen,
			Payload: string(pl)}); err != nil {
			wfail(w, err)
			return
		}
		p.RecordAudit("", "work-build-directly", id)
	default:
		crFail(w, http.StatusBadRequest, "do is approve, send_back or build_directly")
		return
	}
	// Derived again, now the fact is written, so the answer is what the board shows next.
	if _, err := p.refreshWork(r.Context(), st, id, true); err != nil {
		wfail(w, err)
		return
	}
	p.backlogEvent("item", map[string]string{"id": id, "work": "1"})
	p.writeWorkDetail(w, r, st, id)
}
