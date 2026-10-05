package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE REVIEW MOVES WITH THE CLAIM. A PR's review lives on the room that owns the PR: a row and a run folder. The hub
// holds only the claim, so a move that changed only the claim would leave the findings and their walk on the old room.
// MovePRClaimWith therefore copies first and moves second:
//
//  1. both rooms must be online, else it refuses with a sentence and the claim stays
//  2. the old room exports the review (`GET /v1/prs/export`), and the hub pipes that straight into the new room's
//     `POST /v1/prs/import`. The hub is a router and keeps no copy: nothing is written to its disk or held whole in
//     memory. An old room with no row for the key (a claim with no review) answers 404 and only the claim moves.
//  3. an import that failed leaves the claim and the old row as they were
//  4. the claim moves
//  5. the old room archives its row (`POST /v1/prs/{id}/archive`), which also stops a run that is going. The old
//     folder stays. An archive that fails is logged and audited, and does not undo the move: the claim and the new
//     row are right, and the old room's row is a stale copy somebody can archive by hand.
//  6. when the caller knows the card that moved with the PR, it is recorded as the new row's walker.

// prMoveWait bounds the whole copy. A review is at most the room's archive cap, so this is generous.
const prMoveWait = 10 * time.Minute

// moveError is a refusal of a move with the sentence for the operator and the status to answer with.
type moveError struct {
	status int
	msg    string
}

func (e *moveError) Error() string { return e.msg }

// PRClaimMove is the answer to a move. Review says what happened to the review: `moved`, or `none` when the old room
// had none and only the claim moved. PRID is the new room's row id when it moved.
type PRClaimMove struct {
	hubstore.PRClaim
	Review string `json:"review"`
	PRID   string `json:"pr_id,omitempty"`
}

// MovePRClaim gives a key to another room and ends its offline warning, taking the review along. The call the room
// handoff makes when a PR's card moves, so a key keeps one owner.
func (p *Proxy) MovePRClaim(key, to string) (hubstore.PRClaim, error) {
	m, err := p.MovePRClaimWith(context.Background(), key, to, "")
	return m.PRClaim, err
}

// MovePRClaimWith is MovePRClaim that also says the card that walks the review on the new room, when the caller
// knows it, and takes the context of the request that asked.
func (p *Proxy) MovePRClaimWith(ctx context.Context, key, to, walker string) (PRClaimMove, error) {
	st := p.prClaims()
	if st == nil {
		return PRClaimMove{}, hubstore.ErrNoPRClaim
	}
	old, err := st.PRClaimOf(key)
	if err != nil {
		return PRClaimMove{}, err
	}
	out := PRClaimMove{PRClaim: old, Review: "none"}
	if keyOf(old.Room) == keyOf(to) {
		return out, nil
	}
	switch {
	case !p.hub.Has(old.Room):
		return out, &moveError{http.StatusConflict, fmt.Sprintf(
			"the review is on %s, which is offline. bring it back or move it later", old.Room)}
	case !p.hub.Has(to):
		return out, &moveError{http.StatusConflict, fmt.Sprintf("%s is offline, so the review cannot go there. "+
			"bring it back or pick another room", to)}
	}
	ctx, cancel := context.WithTimeout(ctx, prMoveWait)
	defer cancel()
	oldID, newID, err := p.copyReview(ctx, key, old.Room, to)
	if err != nil {
		return out, err
	}
	c, err := st.MovePRClaim(key, to)
	if err != nil {
		return out, err
	}
	out.PRClaim = c
	if old.Warned {
		if g := p.growler(); g != nil {
			g.endAbout(prWaitID(key, old.WarnN))
		}
	}
	p.RecordAudit(to, "pr-claim-moved", key+" from "+old.Room)
	if newID == "" {
		return out, nil
	}
	out.Review, out.PRID = "moved", newID
	if err := p.roomJSON(ctx, old.Room, http.MethodPost, "/v1/prs/"+oldID+"/archive", nil, nil); err != nil {
		log.Printf("[hub] pr %s moved to %s but %s did not archive its row %s: %v", key, to, old.Room, oldID, err)
		p.RecordAudit(old.Room, "pr-review-archive-failed", key+" row "+oldID+": "+err.Error())
	}
	if walker != "" {
		body := map[string]string{"action": "set", "task": walker}
		if err := p.roomJSON(ctx, to, http.MethodPost, "/v1/prs/"+newID+"/walker", body, nil); err != nil {
			log.Printf("[hub] pr %s: %s did not take walker %s: %v", key, to, walker, err)
		}
	}
	return out, nil
}

// copyReview pipes the old room's export into the new room's import. newID is empty, and nothing was made, when the
// old room has no review of the key.
func (p *Proxy) copyReview(ctx context.Context, key, from, to string) (oldID, newID string, err error) {
	exp, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+hostFor(from)+"/v1/prs/export?key="+url.QueryEscape(key), nil)
	if err != nil {
		return "", "", err
	}
	res, err := p.roomClient(from).Do(exp)
	if err != nil {
		return "", "", &moveError{http.StatusBadGateway, fmt.Sprintf(
			"the review is on %s, which did not answer (%v). bring it back or move it later", from, err)}
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return "", "", nil
	case res.StatusCode != http.StatusOK:
		return "", "", &moveError{http.StatusBadGateway, fmt.Sprintf("%s would not export the review: %s", from,
			roomSentence(res))}
	}
	oldID = res.Header.Get("X-Atrium-PR-ID")
	if oldID == "" {
		return "", "", &moveError{http.StatusBadGateway, from + " exported a review and did not say which row it is"}
	}
	imp, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+hostFor(to)+"/v1/prs/import", res.Body)
	if err != nil {
		return "", "", err
	}
	imp.Header.Set("Content-Type", "application/gzip")
	ires, err := p.roomClient(to).Do(imp)
	if err != nil {
		return "", "", &moveError{http.StatusBadGateway, fmt.Sprintf(
			"%s did not take the review (%v). the claim and the review stay on %s", to, err, from)}
	}
	defer ires.Body.Close()
	if ires.StatusCode != http.StatusOK && ires.StatusCode != http.StatusCreated {
		return "", "", &moveError{http.StatusBadGateway, fmt.Sprintf(
			"%s refused the review: %s. the claim and the review stay on %s", to, roomSentence(ires), from)}
	}
	var got struct {
		PR struct {
			ID string `json:"id"`
		} `json:"pr"`
	}
	if json.NewDecoder(io.LimitReader(ires.Body, 4<<20)).Decode(&got) != nil || got.PR.ID == "" {
		return "", "", &moveError{http.StatusBadGateway, fmt.Sprintf(
			"%s took the review and did not say which row it made. the claim and the review stay on %s", to, from)}
	}
	return oldID, got.PR.ID, nil
}

// roomSentence is the error a room answered with, or its status.
func roomSentence(res *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && strings.TrimSpace(e.Error) != "" {
		return strings.TrimSpace(e.Error)
	}
	return fmt.Sprintf("status %d", res.StatusCode)
}

// roomJSON is one JSON call to a room's human listener. A non-2xx is an error carrying the room's sentence.
func (p *Proxy) roomJSON(ctx context.Context, room, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		body = strings.NewReader(string(raw))
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+hostFor(room)+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return errors.New(roomSentence(res))
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<20))
	return nil
}
