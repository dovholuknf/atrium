package link

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/link/notify"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// A ROOM'S NIGHTLY CRUFT REPORT: what its pass freed (count, bytes) and kept and why. The hub keeps the latest per room.
//
//	POST /_hub/cruft   a room's report. The room is stamped from the link, so a room can only report for itself.
//	GET  /_hub/cruft   {reports: {room: report}}
//
// The board hears of it only when a kept item needs the operator's call: one question growler per room, under an id
// made from the items it names, so the same list is raised once and a changed list raises again. A report with none
// ends it. Everything else is on the GET and nowhere louder.

const cruftBodyMax = 1 << 20

type cruftIn struct {
	Room string `json:"room"`
	Kept []struct {
		Kind  string `json:"kind"`
		Ref   string `json:"ref"`
		Why   string `json:"why"`
		Needs bool   `json:"needs"`
	} `json:"kept"`
}

// cruftNeeds is the growler id and body for a report's items that want a decision, "" when none do.
func cruftNeeds(in cruftIn) (id, body string, n int) {
	var refs []string
	for _, k := range in.Kept {
		if k.Needs {
			refs = append(refs, k.Kind+" "+k.Ref)
			if body == "" {
				body = k.Kind + " " + k.Ref + ": " + k.Why
			}
		}
	}
	if len(refs) == 0 {
		return "", "", 0
	}
	sort.Strings(refs)
	sum := sha256.Sum256([]byte(strings.Join(refs, "\n")))
	return "cruft|" + in.Room + "|" + hex.EncodeToString(sum[:6]), body, len(refs)
}

func (p *Proxy) serveCruft(w http.ResponseWriter, r *http.Request) {
	st := p.backlogStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		reports, err := st.CruftReports()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		out := map[string]json.RawMessage{}
		for room, raw := range reports {
			out[room] = json.RawMessage(raw)
		}
		remindReply(w, map[string]any{"reports": out})
	case http.MethodPost:
		switch {
		case !edge.LocalOperator(r):
			remindFail(w, http.StatusForbidden, "a cruft report is filed from the machine the hub runs on"+edge.ProxyNote(r))
			return
		case wire.DocsCrossOrigin.Check(r) != nil:
			remindFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, cruftBodyMax))
		var in cruftIn
		if err != nil || json.Unmarshal(raw, &in) != nil || strings.TrimSpace(in.Room) == "" {
			remindFail(w, http.StatusBadRequest, "that is not a report from a room")
			return
		}
		prev, _ := st.CruftReport(in.Room)
		if err := st.PutCruftReport(in.Room, string(raw)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		p.askCruft(in, prev)
		remindReply(w, map[string]any{"ok": true})
	default:
		remindFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
	}
}

// askCruft raises the question for a report's items that need a decision, and ends the one the last report raised.
func (p *Proxy) askCruft(in cruftIn, prev string) {
	g := p.growler()
	if g == nil {
		return
	}
	id, body, n := cruftNeeds(in)
	var before cruftIn
	if json.Unmarshal([]byte(prev), &before) == nil {
		if old, _, _ := cruftNeeds(before); old != "" && old != id {
			g.EndAbout(old)
		}
	}
	if id != "" {
		g.Ask(in.Room, GrowlRow{ID: id, Reason: ReasonQuestion, Subject: in.Room,
			Title: fmt.Sprintf("Cruft on %s: %s kept need your call", in.Room, plural(n, "item")),
			Body:  notify.Clip(body)})
	}
}
