package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/store"
)

// `atrium-open:` is how a browser on the room's machine opens a card's file with the DESKTOP USER's default app.
//
// `files/launch` opens it as the account the room runs as, and a room running as a service account in the console
// session of another user cannot start a Store app for it. So the board asks the room for a link instead and the
// browser hands the link to a handler registered under the desktop user's own HKCU (`atrium open-handler install`).
//
// A LINK IS A URL ANY WEB PAGE CAN FIRE, so it carries no path. It carries a token the room issued for one file it
// had already judged launchable, good once and for a minute. The handler redeems it with the room, gets the path
// the room checked again, and opens only that. A page that fires its own link has no token, and a token for a
// file the card may not open was never issued.

// OpenLinkScheme is the URL scheme the handler is registered for.
const OpenLinkScheme = "atrium-open"

const (
	openLinkTTL  = time.Minute
	openLinkKeep = 256
)

type openGrant struct {
	card, rel string
	exp       time.Time
}

var openGrants = struct {
	sync.Mutex
	m map[string]openGrant
}{m: map[string]openGrant{}}

// OpenLink builds the link for a card's file: the room's loopback address, the card and the token.
func OpenLink(addr, card, token string) string {
	q := url.Values{"addr": {addr}, "id": {card}, "t": {token}}
	return OpenLinkScheme + "://open?" + q.Encode()
}

// ParseOpenLink reads a link back. The address must be this machine's loopback, so a link cannot aim the handler
// at another host.
func ParseOpenLink(raw string) (addr, card, token string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != OpenLinkScheme {
		return "", "", "", errors.New("not an " + OpenLinkScheme + " link")
	}
	q := u.Query()
	addr, card, token = q.Get("addr"), q.Get("id"), q.Get("t")
	if card == "" || token == "" || !edge.LoopbackHost(addr) {
		return "", "", "", errors.New("that link does not name a room on this machine")
	}
	if _, p, e := net.SplitHostPort(addr); e != nil {
		return "", "", "", errors.New("that link has no port")
	} else if n, e := strconv.Atoi(p); e != nil || n < 1 || n > 65535 {
		return "", "", "", errors.New("that link has no port")
	}
	return addr, card, token, nil
}

// launchLink issues a link for a file the card may open. Refused with 409 when the board is not on this machine,
// which is the board's cue to use `files/launch` instead.
func (s *Server) launchLink(w http.ResponseWriter, r *http.Request) {
	if !edge.LocalOperator(r) {
		writeErr(w, http.StatusConflict, errors.New("the board is not on the room's machine"))
		return
	}
	task, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(task.Worktree) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("this card has no directory"))
		return
	}
	var body openFileRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	if _, ok := s.launchTarget(w, task, body.Path); !ok {
		return
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tok := hex.EncodeToString(raw[:])
	now := time.Now()
	openGrants.Lock()
	for k, g := range openGrants.m {
		if now.After(g.exp) {
			delete(openGrants.m, k)
		}
	}
	if len(openGrants.m) >= openLinkKeep {
		openGrants.Unlock()
		writeErr(w, http.StatusTooManyRequests, errors.New("too many links waiting"))
		return
	}
	openGrants.m[tok] = openGrant{card: task.ID, rel: body.Path, exp: now.Add(openLinkTTL)}
	openGrants.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"url": OpenLink(r.Host, task.ID, tok)})
}

// redeemLink gives the handler the path a token was issued for, once. The file is judged again, so a path that
// became a link to a program in the minute between is refused.
func (s *Server) redeemLink(w http.ResponseWriter, r *http.Request) {
	if !edge.LocalOperator(r) {
		writeErr(w, http.StatusForbidden, errors.New("redeem a link from the room's machine"))
		return
	}
	var body struct {
		T string `json:"t"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.T == "" {
		writeErr(w, http.StatusBadRequest, errors.New("could not read that request"))
		return
	}
	openGrants.Lock()
	g, ok := openGrants.m[body.T]
	delete(openGrants.m, body.T)
	openGrants.Unlock()
	if !ok || time.Now().After(g.exp) || g.card != r.PathValue("id") {
		writeErr(w, http.StatusForbidden, errors.New("that link is used up or expired. open it again from the board"))
		return
	}
	task, err := s.st.Get(g.card)
	if err != nil {
		s.fail(w, err)
		return
	}
	full, ok := s.launchTarget(w, task, g.rel)
	if !ok {
		return
	}
	if err := s.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"what": "opened a file with the default app, by link", "path": g.rel,
	}); err != nil {
		log.Printf("[atrium api] could not record the open of %s: %v", g.rel, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": full})
}

// OpenWithDefault hands a file to the default app of the account this process runs as. The caller has already
// checked the extension with Launchable.
func OpenWithDefault(path string) error {
	if Executable(filepath.Ext(path)) || !Launchable(filepath.Ext(path)) {
		return fmt.Errorf("%s is not a document or a picture", filepath.Base(path))
	}
	return osLaunch(path, filepath.Dir(path))
}
