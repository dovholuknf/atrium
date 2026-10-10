package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// TURN CAUSES OVER THEIR DAILY LIMIT, for the Terminals tab. The usage tab's report (turncost.go) says what each
// kind of atrium text cost and decides nothing. This is the decision: one finding per kind past the limit today.

// turnCauseItems is the backlog item that would remove a cause, when one is known. A kind with no entry has no item,
// and its finding says how to raise the limit. Keyed by delivery kind. Add an entry only while the item is open.
var turnCauseItems = map[string]string{}

// TurnFinding is one cause kind over a limit today.
type TurnFinding struct {
	Kind        string `json:"kind"`
	Turns       int    `json:"turns"`
	Tokens      int64  `json:"tokens"`
	LimitTurns  int    `json:"limit_turns"`
	LimitTokens int64  `json:"limit_tokens"`
	// Over is which limit was passed: turns, tokens or both.
	Over string `json:"over"`
	// Item is the backlog item that would remove this cause, "" when none is known.
	Item string `json:"item,omitempty"`
}

// TurnLimitReport is the answer of GET /v1/usage/turn-limits.
type TurnLimitReport struct {
	// Day is the UTC day counted.
	Day      string        `json:"day"`
	Off      bool          `json:"off"`
	Findings []TurnFinding `json:"findings"`
}

// CheckTurnLimits is the findings for one day's rows. The operator's own turns are not a cause to flag. Tokens are
// counted as the usage tab counts them, cache reads apart. Findings come out most turns first.
func CheckTurnLimits(rows []store.CostRow, day string, lim store.TurnLimits) []TurnFinding {
	out := []TurnFinding{}
	if lim.Off {
		return out
	}
	type sum struct {
		turns  int
		tokens int64
	}
	by := map[string]*sum{}
	for _, r := range rows {
		if r.Day != day || r.Kind == store.DeliveryOperator {
			continue
		}
		s := by[r.Kind]
		if s == nil {
			s = &sum{}
			by[r.Kind] = s
		}
		s.turns += r.Turns
		s.tokens += r.Input + r.Output + r.CacheWrite
	}
	for kind, s := range by {
		l := lim.For(kind)
		overTurns := l.Turns > 0 && s.turns > l.Turns
		overTokens := l.Tokens > 0 && s.tokens > l.Tokens
		if !overTurns && !overTokens {
			continue
		}
		over := "turns"
		switch {
		case overTurns && overTokens:
			over = "both"
		case overTokens:
			over = "tokens"
		}
		out = append(out, TurnFinding{Kind: kind, Turns: s.turns, Tokens: s.tokens, LimitTurns: l.Turns,
			LimitTokens: l.Tokens, Over: over, Item: turnCauseItems[kind]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Turns != out[j].Turns {
			return out[i].Turns > out[j].Turns
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// roomTurnLimits answers which kinds of atrium text are past their daily limit today.
func (s *Server) roomTurnLimits(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	rep, err := s.st.DeliveryCosts(start, now, 1)
	if err != nil {
		s.fail(w, err)
		return
	}
	lim := s.st.TurnLimits()
	day := start.Format("2006-01-02")
	writeJSON(w, http.StatusOK, TurnLimitReport{Day: day, Off: lim.Off, Findings: CheckTurnLimits(rep.Rows, day, lim)})
}
