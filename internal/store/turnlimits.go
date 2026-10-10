package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A DAILY LIMIT ON WHAT ONE KIND OF ATRIUM TEXT CAUSES. A delivery kind (delivery.go) that costs more than this many
// turns or tokens in one UTC day is a finding on the Terminals tab. The judgment lives here and in api/turnlimits.go,
// and never in the usage tab's report, which only says what happened.

// SettingTurnCauseLimits is the daemon-wide limit, as JSON:
//
//	{"turns": 100, "tokens": 5000000, "kinds": {"say": {"turns": 40}}}
//
// turns and tokens apply to every kind. kinds overrides one kind, and an override that leaves a field out keeps the
// general one. A field of 0 is no limit on that axis. Empty means the default, and `off` means no limit at all.
// The operator's own typing is never counted: it is not a cause atrium can remove.
const SettingTurnCauseLimits = "turn_cause_limits"

// The defaults, picked from GET /v1/usage/turns on a live room: over three days its busiest atrium cause (not the
// operator) ran 6 turns and 180k tokens in a day. These are well past that, so a normal day is silent.
const (
	DefaultTurnLimit  = 100
	DefaultTokenLimit = 5_000_000
)

// TurnLimit is one cause kind's daily limit. Zero is no limit on that axis.
type TurnLimit struct {
	Turns  int   `json:"turns"`
	Tokens int64 `json:"tokens"`
}

// TurnLimits is the setting, parsed.
type TurnLimits struct {
	Off bool
	TurnLimit
	Kinds map[string]TurnLimit
}

type turnLimitsJSON struct {
	Turns  *int                 `json:"turns"`
	Tokens *int64               `json:"tokens"`
	Kinds  map[string]turnLimit `json:"kinds,omitempty"`
}

type turnLimit struct {
	Turns  *int   `json:"turns"`
	Tokens *int64 `json:"tokens"`
}

// ParseTurnLimits reads the setting. An empty value is the default and a value that does not parse is an error.
func ParseTurnLimits(v string) (TurnLimits, error) {
	v = strings.TrimSpace(v)
	out := TurnLimits{TurnLimit: TurnLimit{Turns: DefaultTurnLimit, Tokens: DefaultTokenLimit}}
	switch v {
	case "":
		return out, nil
	case "off":
		return TurnLimits{Off: true}, nil
	}
	var j turnLimitsJSON
	if err := json.Unmarshal([]byte(v), &j); err != nil {
		return out, fmt.Errorf("turn_cause_limits is JSON like {\"turns\":100,\"tokens\":5000000}, empty or off: %v", err)
	}
	if j.Turns != nil {
		out.Turns = *j.Turns
	}
	if j.Tokens != nil {
		out.Tokens = *j.Tokens
	}
	if out.Turns < 0 || out.Tokens < 0 {
		return out, fmt.Errorf("turn_cause_limits cannot be negative. 0 is no limit")
	}
	for k, o := range j.Kinds {
		l := out.TurnLimit
		if o.Turns != nil {
			l.Turns = *o.Turns
		}
		if o.Tokens != nil {
			l.Tokens = *o.Tokens
		}
		if l.Turns < 0 || l.Tokens < 0 {
			return out, fmt.Errorf("turn_cause_limits for %s cannot be negative. 0 is no limit", k)
		}
		if out.Kinds == nil {
			out.Kinds = map[string]TurnLimit{}
		}
		out.Kinds[k] = l
	}
	return out, nil
}

// For is the limit of one kind.
func (t TurnLimits) For(kind string) TurnLimit {
	if l, ok := t.Kinds[kind]; ok {
		return l
	}
	return t.TurnLimit
}

// TurnLimits reads the setting, the default when it is unset or unreadable.
func (s *Store) TurnLimits() TurnLimits {
	v, _ := s.Setting(SettingTurnCauseLimits)
	l, err := ParseTurnLimits(v)
	if err != nil {
		l, _ = ParseTurnLimits("")
	}
	return l
}
