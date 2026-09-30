package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card by its handle on the room's own port. See
// docs/rnd/handle-addressed-http-design.md section 6.
//
// ONE WRAPPER IN FRONT OF THE MUX, so every `/v1/tasks/{id}...` route takes a
// name and no handler changes. The segment is looked up the way the room
// already resolves a name for a message (peers.go, relay.go): the wire name,
// qualified with this atrium's, then the alias, live before done. Found, the
// path is rewritten to the id and the request goes on. Not found, a 404 lists
// the live handles that would have worked.
//
// AN ID GOES STRAIGHT THROUGH, untouched and unread. That is every request the
// board makes, and the hub forwards ids too, having resolved the name itself.
//
// A ROOM DOES NOT FORWARD. `name@room` naming another room is a 404 saying to
// ask the hub, which is the one thing that sees every room.

// cardRoutesNotCards are the `/v1/tasks/<segment>` routes whose segment is not
// a card. The hub keeps the same set (`notCards` in internal/link/cardroute.go).
var cardRoutesNotCards = map[string]bool{"prune": true, "pin-order": true, "archive-workers": true}

// looksLikeID is the shape of an id this room mints, a UUID.
func looksLikeID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// cardSegment is the `<segment>` of `/v1/tasks/<segment>...`, or empty.
func cardSegment(path string) string {
	const pre = "/v1/tasks/"
	if !strings.HasPrefix(path, pre) {
		return ""
	}
	rest := path[len(pre):]
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// byName is the wrapper.
func (s *Server) byName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// READ ESCAPED, so a qualified wire name, `sparta%2Frnd`, is one segment
		// and not two.
		esc := r.URL.EscapedPath()
		segEsc := cardSegment(esc)
		seg, err := url.PathUnescape(segEsc)
		if err != nil {
			seg = segEsc
		}
		if seg == "" || cardRoutesNotCards[seg] || looksLikeID(seg) {
			next.ServeHTTP(w, r)
			return
		}
		// An id of another shape, which a test or an old card may carry.
		if _, err := s.st.Get(seg); !errors.Is(err, sql.ErrNoRows) {
			next.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(seg, "@")
		if i := strings.LastIndex(name, "@"); i >= 0 {
			room := name[i+1:]
			if !strings.EqualFold(room, s.st.Tenant()) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "\"" + seg + "\" names room " + room +
					". a room answers for its own cards only: ask the hub"})
				return
			}
			name = strings.TrimPrefix(name[:i], "@")
		}
		t, err := s.st.GetByWireName(s.st.Qualify(name))
		if errors.Is(err, sql.ErrNoRows) {
			t, err = s.st.GetByAlias(name)
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			s.noCardCalled(w, seg)
			return
		case err != nil:
			// The store is the handler's to report, halt and all.
			next.ServeHTTP(w, r)
			return
		}
		rest := esc[len("/v1/tasks/")+len(segEsc):]
		r.URL.RawPath = "/v1/tasks/" + t.ID + rest
		if p, err := url.PathUnescape(r.URL.RawPath); err == nil {
			r.URL.Path = p
		}
		w.Header().Set("X-Atrium-Card", t.ID)
		handle := t.WireName
		if tenant := s.st.Tenant(); tenant != "" && handle != "" {
			handle += "@" + tenant
		}
		w.Header().Set("X-Atrium-Handle", handle)
		next.ServeHTTP(w, r)
	})
}

// noCardCalled is the 404 for a name nothing here answers to, with the live
// handles that would have worked, spelled the way the hub spells them.
func (s *Server) noCardCalled(w http.ResponseWriter, seg string) {
	var work []string
	if tasks, err := s.st.List(); err == nil {
		for _, t := range tasks {
			if t.Status == store.StatusDone || t.Status == store.StatusDead {
				continue
			}
			name, alias := t.WireName, t.Alias
			if name == "" {
				name, alias = alias, ""
			}
			if name == "" {
				continue
			}
			if alias != "" {
				name += " (@" + alias + ")"
			}
			work = append(work, name)
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": "no card called \"" + seg + "\" on this room", "would_work": work,
	})
}
