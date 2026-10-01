package link

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// A deploy waits for a new context the way it waits for an idle runner. See internal/daemon/newcontext.go.
//
// A restart cuts a clear off, and the card is left half cycled: the capture typed, or `/clear`, and no wake. The
// room now ends such a run at its next start and says so on the card, but a deploy that never cuts one off is
// better than one that explains it. So the hub asks every attached room which cards have a run in flight, and
// the one-click deploy refuses while any does, naming the card. The deploy scripts ask the same question through
// GET /_hub/new-contexts and wait.
//
// A ROOM THAT DOES NOT ANSWER IS NOT COUNTED. A clear is typed into a terminal that room owns, so one that is
// unreachable cannot be advancing one, and a deploy held forever by a room that is down is worse.

// newContextRun is one card with a run in flight, as the board lists it.
type newContextRun struct {
	Room  string `json:"room"`
	Card  string `json:"card"`
	Name  string `json:"name"`
	Step  string `json:"step"`
	N     int    `json:"n"`
	Since string `json:"since,omitempty"`
}

// label is how the run is named in a refusal: the card, the room, and the step it is on.
func (r newContextRun) label() string {
	return r.Name + "@" + r.Room + " (step " + strconv.Itoa(r.N) + " of 3, " + r.Step + ")"
}

// newContextsUnderWay asks each attached room for its cards and keeps the ones with a run that has not failed.
// A failed chip is not under way: nothing is typing and a restart loses nothing.
func (p *Proxy) newContextsUnderWay(ctx context.Context) []newContextRun {
	rooms := p.attachedView()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []newContextRun
	)
	for _, a := range rooms {
		wg.Add(1)
		go func(room string) {
			defer wg.Done()
			var body struct {
				Tasks []struct {
					ID         string `json:"id"`
					Title      string `json:"display_title"`
					Wire       string `json:"wire_name"`
					NewContext *struct {
						Step  string `json:"step"`
						N     int    `json:"n"`
						Since string `json:"since"`
					} `json:"new_context"`
				} `json:"tasks"`
			}
			if !p.roomGet(ctx, room, "/v1/tasks", &body) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, t := range body.Tasks {
				if t.NewContext == nil || t.NewContext.Step == "" || t.NewContext.Step == "failed" {
					continue
				}
				name := strings.TrimSpace(t.Title)
				if name == "" {
					name = t.Wire
				}
				if name == "" {
					name = t.ID
				}
				out = append(out, newContextRun{Room: room, Card: t.ID, Name: name, Step: t.NewContext.Step,
					N: t.NewContext.N, Since: t.NewContext.Since})
			}
		}(a.Name)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Room+out[i].Card < out[j].Room+out[j].Card })
	return out
}

// newContextNames is the runs as one sentence fragment, for a refusal.
func newContextNames(runs []newContextRun) string {
	names := make([]string, len(runs))
	for i, r := range runs {
		names[i] = r.label()
	}
	return strings.Join(names, ", ")
}

// serveNewContexts answers GET /_hub/new-contexts with the runs under way, so a deploy script can wait on them.
func (p *Proxy) serveNewContexts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "that has to be a GET"})
		return
	}
	runs := p.newContextsUnderWay(r.Context())
	if runs == nil {
		runs = []newContextRun{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"under_way": runs, "count": len(runs)})
}
