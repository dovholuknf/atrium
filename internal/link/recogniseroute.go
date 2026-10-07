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
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/linkfetch"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE HUB RECOGNISES A PASTE ITSELF. Item r-recognisers-gwt.
//
// `POST /v1/recognise` with no room named used to be placed on the least busy room, and the room matched the link
// against the hub's rows. A room on a build older than the hub's table matched only its own, empty, table and answered
// 404, so ctrl-alt-r said "nothing here knows what that is" for a link the hub had a row for, depending on which room
// was least busy. The rows are the hub's, so the hub answers: it matches, runs the built-in fetch through its own forge
// and fills the dialog. No room's build is involved, and where the launch goes is chosen when it is launched.
//
// A row whose fetch is a command of the operator's (`gh ...`, a script) is still placed on a room, as before, because
// the hub runs no command a recogniser names. A room named by header or query still answers for itself: the directory
// is looked for on that room's disk.
//
// AN OPEN OR A PASTE PLACED ON A ROOM THAT CANNOT READ THE HUB'S ROWS IS TRIED ON ANOTHER. `/v1/open` and `/v1/prs`
// make a card, so a room does the work and the room must recognise the link. A room that answers no_recogniser for a
// link the hub's rows match is on an old build: it is remembered with the commit it runs and placement passes it over
// until it runs another, and the paste goes to the next least busy room. Nothing was made on the first room, since
// recognising is the first step of both.

// hubFetches are the fetches the hub runs itself. Anything else is a command, which a room runs.
var hubFetches = map[string]bool{"": true, forge.FactsFetch: true, linkfetch.Redirect: true}

// hubRecognise answers `POST /v1/recognise` with no room named from the hub's table. False leaves the request to the
// rest of the proxy: a named room, a hub with no forge wired, or a row whose fetch is a command.
func (p *Proxy) hubRecognise(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/recognise" {
		return false
	}
	if _, named := p.roomFor(r); named {
		return false
	}
	f := p.forgeSide()
	if f == nil {
		return false
	}
	raw := peekBody(r, 1<<16)
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		w.Header().Set("Content-Type", "application/json")
		crFail(w, http.StatusBadRequest, err.Error())
		return true
	}
	rows, err := hubRecogniserRows(f.st)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		crFail(w, http.StatusInternalServerError, "the hub's recognisers do not read: "+err.Error())
		return true
	}
	row, vars, err := store.MatchRecogniserIn(rows, in.URL)
	if err == nil && !hubFetches[strings.TrimSpace(row.Fetch)] {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, store.ErrNoRecogniser):
		crFail(w, http.StatusNotFound, err.Error())
		return true
	case err != nil:
		crFail(w, http.StatusBadRequest, err.Error())
		return true
	}
	crJSON(w, http.StatusOK, f.resolve(r.Context(), row, vars))
	return true
}

// resolve fills a matched row in on the hub: the built-in fetch, the templates, and a look at the directory on the
// hub's own disk, which is the board's machine.
func (f *hubForge) resolve(ctx context.Context, row *store.Recogniser, vars map[string]string) *store.Resolved {
	var facts map[string]string
	var fetchErr error
	name, args := store.FillArgv(strings.TrimSpace(row.Fetch), row.FetchArgs, vars)
	switch name {
	case "":
	case forge.FactsFetch:
		ctx, cancel := context.WithTimeout(ctx, linkfetch.Timeout*2)
		facts, fetchErr = forge.Facts(ctx, args, vars, f.forgeFor)
		cancel()
	case linkfetch.Redirect:
		facts, fetchErr = linkfetch.Follow(ctx, vars["url"], args)
	}
	if fetchErr != nil {
		facts = nil
	}
	out := row.Resolve(vars, facts, fetchErr)
	store.DescribeCwd(out, os.Stat)
	return out
}

// peekBody reads up to max bytes of a request's body and puts them back, for the room the request may still go to.
func peekBody(r *http.Request, max int64) []byte {
	if r.Body == nil {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, max))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	return raw
}

// ── a room that cannot read the hub's rows ─────────────

// placedKey carries a paste the hub placed on the least busy room, for a retry on another. See retryDeaf.
type placedKey struct{}

type placedPaste struct {
	room string
	body []byte
}

// placePaste sends r to room as a placed paste. The placed room header is set on the answer, by rewrite, so a retry
// can change it.
func (p *Proxy) placePaste(r *http.Request, room string) *http.Request {
	pp := &placedPaste{room: room, body: peekBody(r, 1<<16)}
	ctx := context.WithValue(r.Context(), cardRoomKey{}, room)
	return r.WithContext(context.WithValue(ctx, placedKey{}, pp))
}

// isDeaf is whether a room is remembered as not reading the hub's rows, on the build it runs now.
func (p *Proxy) isDeaf(a Attached) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	commit, ok := p.deaf[keyOf(a.Name)]
	return ok && commit == a.Commit
}

func (p *Proxy) markDeaf(room string) {
	commit := ""
	for _, a := range p.hub.Rooms() {
		if keyOf(a.Name) == keyOf(room) {
			commit = a.Commit
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deaf == nil {
		p.deaf = map[string]string{}
	}
	p.deaf[keyOf(room)] = commit
}

// deafAnswer is whether a room's answer is "no recogniser" for a link the hub's own rows match.
func (p *Proxy) deafAnswer(status int, body, paste []byte) bool {
	if status != http.StatusUnprocessableEntity {
		return false
	}
	var ans struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &ans) != nil || ans.Code != "no_recogniser" {
		return false
	}
	var in struct {
		URL string `json:"url"`
	}
	f := p.forgeSide()
	if f == nil || json.Unmarshal(paste, &in) != nil {
		return false
	}
	rows, err := hubRecogniserRows(f.st)
	if err != nil {
		return false
	}
	_, _, err = store.MatchRecogniserIn(rows, in.URL)
	return err == nil
}

// retryDeaf takes a placed paste a room could not recognise to the next least busy room, until one recognises it or
// none is left. It answers the room that has the answer now, "" for a request the hub did not place.
func (p *Proxy) retryDeaf(res *http.Response) (string, error) {
	if res.Request == nil {
		return "", nil
	}
	pp, _ := res.Request.Context().Value(placedKey{}).(*placedPaste)
	if pp == nil {
		return "", nil
	}
	room := pp.room
	for tries := len(p.hub.Rooms()); tries > 0 && res.StatusCode == http.StatusUnprocessableEntity; tries-- {
		raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		res.Body = io.NopCloser(bytes.NewReader(raw))
		if err != nil || !p.deafAnswer(res.StatusCode, raw, pp.body) {
			return room, nil
		}
		p.markDeaf(room)
		next := p.placePRRoom(res.Request.Context(), "")
		if next == "" || p.isDeafName(next) {
			return room, nil
		}
		log.Printf("[hub] %s could not recognise a paste the hub's rows match, so it went to %s", room, next)
		if err := p.answerFrom(res, next, pp.body); err != nil {
			log.Printf("[hub] %s did not take the paste either: %v", next, err)
			res.Body = io.NopCloser(bytes.NewReader(raw))
			return room, nil
		}
		room = next
	}
	return room, nil
}

func (p *Proxy) isDeafName(room string) bool {
	for _, a := range p.hub.Rooms() {
		if keyOf(a.Name) == keyOf(room) {
			return p.isDeaf(a)
		}
	}
	return false
}

// answerFrom puts room's answer to the same request in place of res's.
func (p *Proxy) answerFrom(res *http.Response, room string, body []byte) error {
	ctx, cancel := context.WithTimeout(res.Request.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+hostFor(room)+res.Request.URL.Path,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	got, err := p.roomClient(room).Do(req)
	if err != nil {
		return err
	}
	defer got.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(got.Body, 4<<20))
	if err != nil {
		return err
	}
	if got.StatusCode >= 500 {
		return errStatus(got.StatusCode)
	}
	res.StatusCode, res.Status = got.StatusCode, got.Status
	res.Header.Set("Content-Type", got.Header.Get("Content-Type"))
	res.Header.Del("Content-Encoding")
	res.Body = io.NopCloser(bytes.NewReader(raw))
	res.ContentLength = int64(len(raw))
	res.Header.Set("Content-Length", fmt.Sprint(len(raw)))
	return nil
}
