package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/dovholuknf/atrium/internal/link/notify"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// WHICH ROOM RUNS A PR, AND ONE ROW PER PR ACROSS ROOMS. Design: docs/rnd/scm-forge-design.md section 6 and the answers
// at its end, which overrule it where they differ. Item f-new-pr-room-and-dedupe.
//
//	POST /_claim/pr (a room, on the git kind)  {key, url, why, head, source}   ask for the key
//	GET  /_hub/pr-claims                       every claim
//	POST /_hub/pr-claims/move                  {key, to, walker?}   the operator's hand move, the review with it. See prmove.go
//
// ── placement ───────────────────────────────────────────
//
// THE LEAST BUSY ONLINE ROOM: the fewest running sessions, a session being a card that is running, or waiting on an
// answer or a permission (the statuses a card's token stays live for). The count is each room's own `/v1/tasks`,
// asked when a claim is placed, so there is no new standing index on the hub. A tie goes to the lower room name, a
// stable order: the hub is told no CPU figure by a room today, and the room's reports are not made bigger for it.
// Whether a room has the checkout does not rank it, since a room without one is given the code by the hub. A room
// marked for deletion starts no new work, and a room whose count could not be read is passed over unless nothing else
// answered.
//
// ── the claim ───────────────────────────────────────────
//
// A room that is about to make a PR row asks first. The key's first claim wins and every later ask is told the owner.
// If the hub placed the key on another room than the asker, the asker makes nothing and the hub forwards the paste to
// the owner, which makes the row. NOTHING IS EVER RE-PLACED BY ITSELF: a claim whose room is offline stays, and the
// board's growler says so. See prWarnSweep. Only the operator's move changes an owner.

// SetPRClaims wires the claim table. Without it the claim route answers 404 and a PR is placed by nobody.
func (p *Service) SetPRClaims(st *hubstore.Store) {
	p.mu.Lock()
	p.prc = st
	p.mu.Unlock()
	if p.HubUp() {
		p.MountPRClaim(http.HandlerFunc(p.servePRClaim))
	}
}

func (p *Service) PRClaims() *hubstore.Store {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prc
}

// liveStatuses are the card statuses that count as a running session.
var liveStatuses = map[string]bool{"running": true, "needs-input": true, "needs-permission": true}

// roomSessions is how many sessions a room is running, from its own task list. ok is false when it did not answer.
func (p *Service) roomSessions(ctx context.Context, room string) (n int, ok bool) {
	var body struct {
		Tasks []struct {
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if !p.RoomGet(ctx, room, "/v1/tasks", &body) {
		return 0, false
	}
	for _, t := range body.Tasks {
		if liveStatuses[t.Status] {
			n++
		}
	}
	return n, true
}

// placeLoadWait is how long placement waits for the rooms' session counts.
const PlaceLoadWait = 1500 * time.Millisecond

// roomLoad is what placement knows of one room. n is running sessions, negative for a room not to be used, ok whether
// it answered, idle the idle CPU percent it last reported and nil when it never has.
type RoomLoad struct {
	Room string
	N    int
	ok   bool
	// has is whether the room already holds the checkout the paste lands in. Only asked of a paste that names a repo.
	has  bool
	Idle *float64
}

// lessLoaded is whether l is a better home than o: fewer sessions, then more idle CPU with a room that reported a
// figure ahead of one that did not, then the lower name.
func (l RoomLoad) LessLoaded(o RoomLoad) bool {
	if l.N != o.N {
		return l.N < o.N
	}
	if (l.Idle == nil) != (o.Idle == nil) {
		return l.Idle != nil
	}
	if l.Idle != nil && *l.Idle != *o.Idle {
		return *l.Idle > *o.Idle
	}
	return wire.KeyOf(l.Room) < wire.KeyOf(o.Room)
}

// placePRRoom picks the room for a new PR: the least busy online room, a tie to the one with more idle CPU, then the
// lower name.
// `fallback` is the answer when no room could be asked, which is the asker for a claim and nothing for a paste.
func (p *Service) PlacePRRoom(ctx context.Context, fallback string) string {
	return p.placePRRoomExcept(ctx, fallback, nil)
}

// placePRRoomExcept is placePRRoom with rooms passed over: the ones that already refused this paste, by key.
func (p *Service) placePRRoomExcept(ctx context.Context, fallback string, skip map[string]string) string {
	return p.PlaceRoomHolding(ctx, fallback, skip, "")
}

// roomHolds is whether a room already has a checkout of repo ("host/org/repo"). A room that does not answer, or is on
// a build with no such question, reads as false: it is no worse than before.
func (p *Service) roomHolds(ctx context.Context, room, repo string) bool {
	var body struct {
		Has bool `json:"has"`
	}
	return p.RoomGet(ctx, room, "/v1/scm/has?repo="+url.QueryEscape(repo), &body) && body.Has
}

// placeRoomHolding is placePRRoomExcept that prefers, among the rooms it would take, one that already holds a checkout
// of repo. A room with the clone opens the link in seconds, and any other clones the repo first, which is the slow part
// of a pasted link. repo "" asks nothing and places by load alone. The holders are asked beside the session counts, so
// placement waits no longer than it did.
func (p *Service) PlaceRoomHolding(ctx context.Context, fallback string, skip map[string]string, repo string) string {
	rooms := p.Rooms()
	marked := p.MarkedRooms()
	got := make([]RoomLoad, len(rooms))
	// A ROOM THAT DOES NOT ANSWER IN TIME IS NOT WAITED FOR. The count is read from every room at once, and one that
	// is slow or gone held the whole placement for roomGet's ten seconds, which a pasted link then spent in silence.
	// A room that missed the wait reads as one that did not answer, and the second pass below still takes it when no
	// other room can be had.
	ctx, cancel := context.WithTimeout(ctx, PlaceLoadWait)
	defer cancel()
	var wg sync.WaitGroup
	for i, a := range rooms {
		got[i].Room, got[i].Idle = a.Name, a.IdleCPU
		if _, passed := skip[wire.KeyOf(a.Name)]; passed || marked[wire.KeyOf(a.Name)] || p.isDeaf(a) {
			got[i].N = -1
			continue
		}
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			got[i].N, got[i].ok = p.roomSessions(ctx, name)
		}(i, a.Name)
		if repo != "" {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				got[i].has = p.roomHolds(ctx, name, repo)
			}(i, a.Name)
		}
	}
	wg.Wait()
	var best *RoomLoad
	for pass := 0; pass < 2 && best == nil; pass++ {
		for i := range got {
			l := &got[i]
			if l.N < 0 || (pass == 0 && !l.ok) {
				continue
			}
			if best != nil && l.has != best.has {
				if l.has {
					best = l
				}
				continue
			}
			if best == nil || l.LessLoaded(*best) {
				best = l
			}
		}
	}
	if best == nil {
		return fallback
	}
	return best.Room
}

// ── a paste with no room named ──────────────────────────

// placeNewPR sends `POST /v1/prs` with two or more rooms and none named to the least busy one. With one room, or a
// room named by header, query or tag, nothing here runs. That room then asks for the claim itself and may be told the
// key is someone else's, so this chooses only where the paste is recognised.
//
// `POST /v1/recognise` with no room goes the same way only for a row whose fetch is a command: the hub answers every
// other one itself (recogniseroute.go). The room it went to is in X-Atrium-Placed-Room, set on the answer, and a room
// too old to read the hub's rows hands the paste on to the next. See retryDeaf.
func (p *Service) PlaceNewPR(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.Method != http.MethodPost || (r.URL.Path != "/v1/prs" && r.URL.Path != "/v1/recognise") {
		return r, true
	}
	if room, named := p.RoomFor(r); named || room != "" {
		return r, true
	}
	if len(p.Rooms()) < 2 {
		return r, true
	}
	room := p.PlacePRRoom(r.Context(), "")
	if room == "" {
		return r, true
	}
	return p.PlacePaste(r, room), true
}

// ── a room's claim ──────────────────────────────────────

type prClaimAsk struct {
	Key    string `json:"key"`
	URL    string `json:"url"`
	Why    string `json:"why"`
	Head   string `json:"head"`
	Source string `json:"source"`
	// Held says the asker already has a live row for the key, so it is the owner when nothing else is.
	Held bool `json:"held"`
	// Take says the operator picked the asking room for this link by name. The claim moves to the asker, unless the
	// room that holds it has a live card for the key.
	Take bool `json:"take"`
}

// roomLive is the live card a room has for a claim key, or "". A room on a build with no such question reads as having
// none: the claim then moves, as the operator picked.
func (p *Service) roomLive(ctx context.Context, room, key string) string {
	var body struct {
		Card string `json:"card"`
	}
	if !p.RoomGet(ctx, room, "/v1/open/live?key="+url.QueryEscape(key), &body) {
		return ""
	}
	return body.Card
}

func (p *Service) servePRClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	st := p.PRClaims()
	if st == nil || r.Method != http.MethodPost || r.URL.Path != PRClaimPrefix {
		http.NotFound(w, r)
		return
	}
	asker := strings.TrimSpace(r.Header.Get(PRClaimRoomHeader))
	var ask prClaimAsk
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<17)).Decode(&ask); err != nil || strings.TrimSpace(ask.Key) == "" {
		crFail(w, http.StatusBadRequest, "a claim needs a key")
		return
	}
	ask.Key = strings.ToLower(strings.TrimSpace(ask.Key))
	ans, err := p.claimPR(r.Context(), st, asker, ask)
	if err != nil {
		crFail(w, http.StatusInternalServerError, err.Error())
		return
	}
	crJSON(w, http.StatusOK, ans)
}

// claimPR is the rule: an existing claim is the answer, otherwise the key is placed and recorded. When it was placed
// on another room and the ask carried the paste, the paste is forwarded to that room. A forward that cannot reach the
// owner gives the key to the asker, who is plainly online, since nothing was made on the owner.
func (p *Service) claimPR(ctx context.Context, st *hubstore.Store, asker string, ask prClaimAsk) (PRClaimAnswer, error) {
	ans := PRClaimAnswer{Key: ask.Key}
	if c, err := st.PRClaimOf(ask.Key); err == nil {
		ans.Owner = c.Room
		if ask.Take && wire.KeyOf(c.Room) != wire.KeyOf(asker) {
			// THE ROOM THE OPERATOR PICKED WINS, unless the owner has a card on the link that is live: that card is the
			// answer, and nothing is made beside it.
			if card := p.roomLive(ctx, c.Room, ask.Key); card != "" {
				ans.Live = card
				return ans, nil
			}
			if _, err := p.MovePRClaimWith(ctx, ask.Key, asker, ""); err != nil {
				// An owner that is offline cannot hand its review over, and the pick still stands: the claim moves alone.
				if mv, err2 := st.MovePRClaim(ask.Key, asker); err2 == nil {
					ans.Owner = mv.Room
				} else {
					return ans, err2
				}
			} else {
				ans.Owner = asker
			}
			p.RecordAudit(asker, "link-claim-taken", ask.Key+" from "+c.Room)
		} else if wire.KeyOf(c.Room) != wire.KeyOf(asker) {
			ans.Holder = p.roomLive(ctx, c.Room, ask.Key)
		}
		return ans, nil
	} else if !errors.Is(err, hubstore.ErrNoPRClaim) {
		return ans, err
	}
	want := asker
	if !ask.Held {
		want = p.PlacePRRoom(ctx, asker)
	}
	if want == "" {
		want = asker
	}
	c, made, err := st.ClaimPR(ask.Key, want, ask.Source)
	if err != nil {
		return ans, err
	}
	ans.Owner, ans.Made = c.Room, made
	if !made || wire.KeyOf(c.Room) == wire.KeyOf(asker) || strings.TrimSpace(ask.URL) == "" {
		return ans, nil
	}
	status, body, ferr := p.forwardPR(ctx, c.Room, ask)
	if ferr != nil {
		log.Printf("[hub] pr %s placed on %s but it did not take it (%v), so %s keeps it", ask.Key, c.Room, ferr, asker)
		if mv, err := st.MovePRClaim(ask.Key, asker); err == nil {
			ans.Owner = mv.Room
		}
		return ans, nil
	}
	ans.Forwarded, ans.ForwardStatus, ans.Forward = true, status, body
	return ans, nil
}

// forwardPR hands a paste to the owner's `POST /v1/prs`. The owner claims the key itself, is told it is the owner, and
// makes the row. A 5xx or a transport error is a refusal to take it.
func (p *Service) forwardPR(ctx context.Context, room string, ask prClaimAsk) (int, json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, _ := json.Marshal(map[string]string{"url": ask.URL, "why": ask.Why, "head": ask.Head})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+wire.HostFor(room)+"/v1/prs", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.RoomClient(room).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 500 {
		return res.StatusCode, nil, wire.ErrStatus(res.StatusCode)
	}
	if !json.Valid(body) {
		body = nil
	}
	return res.StatusCode, body, nil
}

// ── the operator's routes ───────────────────────────────

// servePRClaims lists the claims and takes the operator's move. THE MOVE IS THE ONE WAY AN OWNER CHANGES. The room
// handoff that moves a PR's card calls it, and so does an operator by hand.
func (p *Service) ServePRClaims(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.PRClaims()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "pr-claims"), "/")
	switch {
	case rest == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		list, err := st.PRClaims()
		if err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []hubstore.PRClaim{}
		}
		crJSON(w, http.StatusOK, map[string]any{"claims": list})
	case rest == "holder" && r.Method == http.MethodGet:
		// WHO HOLDS A LINK, for the launch dialog to say before "go": the room, and the card it has live on it if any.
		key, _ := p.OpenTargetOf(r.URL.Query().Get("url"), r.URL.Query().Get("repo"))
		out := map[string]string{"key": key, "owner": "", "card": ""}
		if key != "" {
			if c, err := st.PRClaimOf(strings.ToLower(key)); err == nil {
				out["owner"] = c.Room
				ctx, cancel := context.WithTimeout(r.Context(), PlaceLoadWait)
				out["card"] = p.roomLive(ctx, c.Room, strings.ToLower(key))
				cancel()
			}
		}
		crJSON(w, http.StatusOK, out)
	case rest == "move" && r.Method == http.MethodPost:
		if err := wire.DocsCrossOrigin.Check(r); err != nil {
			crFail(w, http.StatusForbidden, "a page on another origin cannot move a pr here")
			return
		}
		var in struct {
			Key string `json:"key"`
			To  string `json:"to"`
			// Walker is the card that walks the review on the new room, when the caller knows it.
			Walker string `json:"walker"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil ||
			strings.TrimSpace(in.Key) == "" || strings.TrimSpace(in.To) == "" {
			crFail(w, http.StatusBadRequest, "say the key and the room to move it to")
			return
		}
		c, err := p.MovePRClaimWith(r.Context(), strings.ToLower(strings.TrimSpace(in.Key)), strings.TrimSpace(in.To),
			strings.TrimSpace(in.Walker))
		var me *moveError
		switch {
		case errors.Is(err, hubstore.ErrNoPRClaim):
			crFail(w, http.StatusNotFound, "no room has claimed that pr")
		case errors.As(err, &me):
			crFail(w, me.status, me.msg)
		case err != nil:
			crFail(w, http.StatusInternalServerError, err.Error())
		default:
			crJSON(w, http.StatusOK, c)
		}
	default:
		crFail(w, http.StatusMethodNotAllowed, "that has to be a GET, or a POST to /move")
	}
}

// ── the warning ─────────────────────────────────────────

// PRWarnGrace is how long a claim's room must stay offline before the board is warned. A test sets `warnGrace` on its
// Proxy instead of waiting.
const PRWarnGrace = 2 * time.Minute

func prWaitID(key string, n int) string { return "prwait|" + key + "|" + ItoaN(n) }

func ItoaN(n int) string { return strconv.Itoa(n) }

// prWarnSweep raises a WARNING growler for every claim whose room is offline and ends it when the room is back or the
// claim moved. It is the growler's own ticker doing it, so nothing new runs. The growler is the board's alert and it
// reaches every screen, the bell and the phone. The PR stays where it is: this only says so.
//
// A ROOM IS OFFLINE FOR PRWarnGrace BEFORE IT WARNS, counted from when this sweep first saw it gone, so a blink of
// the network and a hub restart (when no room has dialled back yet) raise nothing.
func (p *Service) PRWarnSweep(g *notify.Growler) bool {
	st := p.PRClaims()
	if st == nil {
		return false
	}
	claims, err := st.PRClaims()
	if err != nil {
		return false
	}
	rs, ok := g.Raiser()
	if !ok {
		return false
	}
	changed := false
	at := time.Now()
	p.mu.Lock()
	if p.prGone == nil {
		p.prGone = map[string]time.Time{}
	}
	gone := p.prGone
	p.mu.Unlock()
	sort.Slice(claims, func(i, j int) bool { return claims[i].Key < claims[j].Key })
	for _, c := range claims {
		offline := !p.Has(c.Room)
		p.mu.Lock()
		if !offline {
			delete(gone, c.Key)
		} else if _, seen := gone[c.Key]; !seen {
			gone[c.Key] = at
		}
		since := gone[c.Key]
		p.mu.Unlock()
		offline = offline && at.Sub(since) >= p.WarnGrace
		switch {
		case offline && !c.Warned:
			n, flipped, err := st.PRClaimWarn(c.Key, true)
			if err != nil || !flipped {
				continue
			}
			raised, err := rs.Raise(c.Room, notify.GrowlRow{ID: prWaitID(c.Key, n), Reason: notify.ReasonQuestion, Subject: c.Key,
				Title: "WARNING: PR " + c.Key + " is on " + c.Room + ", which is offline",
				Body:  "It stays claimed and waits for " + c.Room + " to return. Move it by hand to run it elsewhere."})
			if err != nil {
				log.Printf("[hub] growlers could not raise the offline warning for pr %s: %v", c.Key, err)
				continue
			}
			changed = changed || raised
		case !offline && c.Warned:
			n, flipped, err := st.PRClaimWarn(c.Key, false)
			if err != nil || !flipped {
				continue
			}
			if ch, err := rs.End(prWaitID(c.Key, n)); err == nil {
				changed = changed || ch
			}
		}
	}
	return changed
}

// ── the room's side ─────────────────────────────────────
