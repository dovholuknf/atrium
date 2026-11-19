package link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

const capToolDesc = "What each room can do: reach (ssh aliases, ziti controllers), toolchains, builds of a named project and " +
	"access (gh, hub push, signing). The rooms probe themselves, read-only, and the hub keeps the newest result of each.\n\n" +
	"action is one of:\n" +
	"- `which {need, fresh?}`: the attached rooms where EVERY capability in `need` passes and the result is newer than " +
	"`fresh` (a duration like 6h, default 24h). A stale pass is a candidate, never a match. A capability marked optional on " +
	"a room is not a pass.\n" +
	"- `room {room}`: every capability of one room, newest result each.\n" +
	"- `list {kind?}`: the declared capabilities.\n" +
	"- `run {room, caps?}`: ask that room to run its probes now, all or the named ids. The room decides what runs. " +
	"One run at a time per room."

type capInput struct {
	Action string   `json:"action" jsonschema:"which, room, list or run"`
	Need   []string `json:"need,omitempty" jsonschema:"for which: capability ids that must all pass, like ssh:agentwsl"`
	Fresh  string   `json:"fresh,omitempty" jsonschema:"for which: how recent a pass must be, like 6h. default 24h"`
	Room   string   `json:"room,omitempty" jsonschema:"for room and run: the room's name"`
	Kind   string   `json:"kind,omitempty" jsonschema:"for list: reach, toolchain, build or access"`
	Caps   []string `json:"caps,omitempty" jsonschema:"for run: ids to run. empty runs them all"`
}

// CapabilityMatch is one room and how it measures against a need.
type CapabilityMatch struct {
	Room    string            `json:"room"`
	Online  bool              `json:"online"`
	OK      bool              `json:"ok"`
	Stale   bool              `json:"stale,omitempty"`
	Results map[string]string `json:"results"`
}

type capOutput struct {
	Rooms   []CapabilityMatch `json:"rooms,omitempty"`
	Caps    []any             `json:"caps,omitempty"`
	Results []capCell         `json:"results,omitempty"`
	Note    string            `json:"note,omitempty"`
	Asked   []string          `json:"asked,omitempty"`
}

type capCell struct {
	Cap     string `json:"cap"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
	At      string `json:"at,omitempty"`
}

func capMeta(v CapabilityView) map[string]struct {
	Kind        string   `json:"kind"`
	Optional    bool     `json:"optional"`
	NotExpected []string `json:"not_expected"`
} {
	out := map[string]struct {
		Kind        string   `json:"kind"`
		Optional    bool     `json:"optional"`
		NotExpected []string `json:"not_expected"`
	}{}
	for _, raw := range v.Caps {
		var c struct {
			ID          string   `json:"id"`
			Kind        string   `json:"kind"`
			Optional    bool     `json:"optional"`
			NotExpected []string `json:"not_expected"`
		}
		if json.Unmarshal(raw, &c) == nil {
			out[c.ID] = struct {
				Kind        string   `json:"kind"`
				Optional    bool     `json:"optional"`
				NotExpected []string `json:"not_expected"`
			}{c.Kind, c.Optional, c.NotExpected}
		}
	}
	return out
}

// CapabilityWhich is the rooms that can do everything in need. A room is OK only when it is online and each need passed
// no longer ago than fresh. A room whose passes are all there but older is a candidate with Stale set, and an offline
// room is never OK. Optional changes how a failure is shown, never whether a need was met.
func CapabilityWhich(v CapabilityView, need []string, fresh time.Duration, now time.Time) []CapabilityMatch {
	got := map[string]map[string]capCell{}
	for _, r := range v.Results {
		k := wire.KeyOf(r.Room)
		if got[k] == nil {
			got[k] = map[string]capCell{}
		}
		got[k][r.Cap] = capCell{Cap: r.Cap, State: r.State, Version: r.Version, Reason: r.Reason, At: r.At}
	}
	var out []CapabilityMatch
	for _, rm := range v.Rooms {
		m := CapabilityMatch{Room: rm.Name, Online: rm.Online, Results: map[string]string{}}
		all, stale := true, false
		for _, n := range need {
			c, ok := got[wire.KeyOf(rm.Name)][n]
			switch {
			case !ok:
				m.Results[n] = "unknown"
				all = false
			case c.State != "pass":
				m.Results[n] = c.State
				all = false
			default:
				at, _ := time.Parse(time.RFC3339, c.At)
				if now.Sub(at) > fresh {
					m.Results[n] = "pass, stale"
					stale = true
				} else {
					m.Results[n] = "pass"
				}
			}
		}
		if all {
			m.OK, m.Stale = rm.Online && !stale, stale
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OK && !out[j].OK })
	return out
}

func (c *controlMCP) registerCapabilities(s *mcp.Server, class ctl.Class) {
	addTool(s, class, &mcp.Tool{Name: "atrium_capabilities", Description: capToolDesc},
		func(ctx context.Context, req *mcp.CallToolRequest, in capInput) (*mcp.CallToolResult, capOutput, error) {
			out, err := capCall(ctx, in, func(ctx context.Context, method, path string, out any) error {
				return c.Ask(ctx, method, path, "", nil, out)
			})
			return nil, out, err
		})
}

// AddRoomCapabilityTool is atrium_capabilities on the stdio server, which has no class of its own.
func AddRoomCapabilityTool(s *mcp.Server, call func(ctx context.Context, method, path string, out any) error) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_capabilities", Description: capToolDesc},
		func(ctx context.Context, req *mcp.CallToolRequest, in capInput) (*mcp.CallToolResult, capOutput, error) {
			out, err := capCall(ctx, in, call)
			return nil, out, err
		})
}

// capCall is one code for the hub's control and the stdio server, which differ only in how they reach the hub.
func capCall(ctx context.Context, in capInput, ask func(ctx context.Context, method, path string, out any) error) (capOutput, error) {
	var out capOutput
	var v CapabilityView
	get := func() error { return ask(ctx, http.MethodGet, "/_hub/capabilities", &v) }
	switch strings.TrimSpace(in.Action) {
	case "which":
		if len(in.Need) == 0 {
			return out, fmt.Errorf("name what is needed, like need: [\"ssh:agentwsl\"]")
		}
		fresh := 24 * time.Hour
		if in.Fresh != "" {
			d, err := time.ParseDuration(in.Fresh)
			if err != nil || d <= 0 {
				return out, fmt.Errorf("fresh is a duration like 6h")
			}
			fresh = d
		}
		if err := get(); err != nil {
			return out, err
		}
		out.Rooms = CapabilityWhich(v, in.Need, fresh, time.Now())
		if len(out.Rooms) == 0 {
			out.Note = "no room has all of these passing"
		}
	case "room":
		if err := get(); err != nil {
			return out, err
		}
		for _, r := range v.Results {
			if wire.KeyOf(r.Room) == wire.KeyOf(in.Room) {
				out.Results = append(out.Results, capCell{r.Cap, r.State, r.Version, r.Reason, r.At})
			}
		}
		if len(out.Results) == 0 {
			out.Note = "no results for that room"
		}
	case "list":
		if err := get(); err != nil {
			return out, err
		}
		kind := strings.TrimSpace(in.Kind)
		meta := capMeta(v)
		for _, raw := range v.Caps {
			var id struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(raw, &id)
			if kind == "" || meta[id.ID].Kind == kind {
				out.Caps = append(out.Caps, raw)
			}
		}
	case "run":
		if strings.TrimSpace(in.Room) == "" {
			return out, fmt.Errorf("name the room to run on")
		}
		q := url.Values{"room": {in.Room}}
		if len(in.Caps) > 0 {
			q.Set("cap", strings.Join(in.Caps, ","))
		}
		var res struct {
			Asked []string `json:"asked"`
		}
		if err := ask(ctx, http.MethodPost, "/_hub/capabilities?"+q.Encode(), &res); err != nil {
			return out, err
		}
		out.Asked = res.Asked
	default:
		return out, fmt.Errorf("action is which, room, list or run")
	}
	return out, nil
}
