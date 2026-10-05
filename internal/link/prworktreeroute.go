package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// PlacedRoomHeader is the room the hub chose for a request that named none. The board reads it off the answer so the
// launch that follows goes to the same room, since the worktree is only there.
const PlacedRoomHeader = "X-Atrium-Placed-Room"

// placePRWorktree places `POST /v1/providers/<name>/pr-worktree` when two or more rooms are attached and none is
// named. A PR already claimed goes to its owner, so a second paste of the same PR lands where the first one did, and
// a new one goes to the least busy room and is claimed there. Nothing here runs for a named room or a single room,
// which are routed as they always were. The body's `host` is the claim key's host, and the room ignores it.
func (p *Proxy) placePRWorktree(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.Method != http.MethodPost || r.Body == nil || !strings.HasPrefix(r.URL.Path, "/v1/providers/") ||
		!strings.HasSuffix(r.URL.Path, "/pr-worktree") {
		return r, true
	}
	if room, named := p.roomFor(r); named || room != "" {
		return r, true
	}
	if len(p.hub.Rooms()) < 2 {
		return r, true
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	var in struct {
		Host   string `json:"host"`
		Org    string `json:"org"`
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	_ = json.Unmarshal(raw, &in)

	st := p.prClaims()
	key := ""
	if st != nil && in.Host != "" && in.Org != "" && in.Repo != "" && in.Number > 0 {
		key = strings.ToLower(in.Host + "/" + in.Org + "/" + in.Repo + "/" + strconv.Itoa(in.Number))
		if c, err := st.PRClaimOf(key); err == nil {
			return p.placedOn(w, r, c.Room), true
		}
	}
	room := p.placePRRoom(r.Context(), "")
	if room == "" {
		return r, true
	}
	if key != "" {
		// A claim lost to a paste that raced this one is the owner's, wherever it was placed.
		if c, made, err := st.ClaimPR(key, room, "paste"); err == nil {
			room = c.Room
			if made {
				r = r.WithContext(context.WithValue(r.Context(), prMadeKey{}, prMade{key: key, room: room}))
			}
		}
	}
	return p.placedOn(w, r, room), true
}

// prMade is a claim this request made, so a refusal by the room it was placed on can let it go.
type prMadeKey struct{}

type prMade struct{ key, room string }

// releaseOnRefusal wraps w when the request made a claim. When the placed room answers 4xx or 5xx (or the proxy could
// not reach it), nothing was made there, so the claim is released and a retry places the key again. Without that a
// refused clone or fetch would leave the key owned by a room that has no worktree for it.
func (p *Proxy) releaseOnRefusal(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	m, ok := r.Context().Value(prMadeKey{}).(prMade)
	if !ok {
		return w
	}
	return &refusalWriter{ResponseWriter: w, refused: func() { p.releasePRClaim(m.key, m.room) }}
}

// releasePRClaim lets go of a claim the room it was placed on refused, and ends its offline warning.
func (p *Proxy) releasePRClaim(key, room string) {
	st := p.prClaims()
	if st == nil {
		return
	}
	c, err := st.ReleasePRClaim(key, room)
	if err != nil {
		return
	}
	if c.Warned {
		if g := p.growler(); g != nil {
			g.endAbout(prWaitID(key, c.WarnN))
		}
	}
	p.RecordAudit(room, "pr-claim-released", key+": the room refused the worktree")
}

// refusalWriter calls refused once, when the status written is 4xx or 5xx.
type refusalWriter struct {
	http.ResponseWriter
	refused func()
	once    bool
}

func (rw *refusalWriter) WriteHeader(code int) {
	if !rw.once {
		rw.once = true
		if code >= 400 {
			rw.refused()
		}
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *refusalWriter) Write(b []byte) (int, error) {
	if !rw.once {
		rw.once = true
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *refusalWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *refusalWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

func (p *Proxy) placedOn(w http.ResponseWriter, r *http.Request, room string) *http.Request {
	w.Header().Set(PlacedRoomHeader, room)
	return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, room))
}
