package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_deploy: one call asks for a room deploy, the deploy owner holds the
// room, waits for it to go quiet, and runs the deploy it runs today. See
// docs/rnd/room-deploy-hold-design.md, section 5.
//
// THE REQUEST IS ON THE HUB, because the hub outlives the room and the owner may
// be on another one. THE HOLD IS ON THE ROOM (`/v1/hold`, internal/daemon/
// roomhold.go), because the room has to find it after the restart and the
// permission chain that enforces it runs there. This file never holds anything
// itself: it records requests and forwards the owner's calls.
//
// FULL CLASS ONLY. A worker does not decide that a room goes down: it asks its
// director. `workerTools` does not name this tool.

// SettingDeployOwner is the hub setting naming the deploy owner, a handle or
// `name@room`. Resolved at each call, the way report_to is, because the owner's
// card id changes when it is relaunched. Unset means no owner: a request is
// refused rather than guessed at.
const SettingDeployOwner = "deploy_owner"

// deployRequestKey is the hub setting holding one room's requests.
func deployRequestKey(room string) string { return "deploy_request:" + room }

const (
	// deployWaitMax bounds one `wait`.
	deployWaitMax = 600 * time.Second
	// deployWaitPoll is how often `wait` asks the room.
	deployWaitPoll = 5 * time.Second
)

// deployAsk is one director's reason for a deploy.
type deployAsk struct {
	By  string `json:"by"`
	Why string `json:"why"`
	SHA string `json:"sha,omitempty"`
	At  string `json:"at"`
}

// deployRequests is a room's record: what is waiting for the next deploy, and
// what the running one was started for.
type deployRequests struct {
	Pending []deployAsk `json:"pending,omitempty"`
	Running []deployAsk `json:"running,omitempty"`
	HoldID  string      `json:"hold_id,omitempty"`
}

type deployInput struct {
	Action     string   `json:"action" jsonschema:"request, start, wait, cancel or status"`
	Room       string   `json:"room,omitempty" jsonschema:"the room to deploy. default: your own"`
	Why        string   `json:"why,omitempty" jsonschema:"request: what the deploy is for. cancel: why it is called off"`
	SHA        string   `json:"sha,omitempty" jsonschema:"request: the commit you need live, if there is one"`
	Exempt     []string `json:"exempt,omitempty" jsonschema:"start: up to three cards the deploy itself needs, never held"`
	MaxSeconds int      `json:"max_seconds,omitempty" jsonschema:"wait: how long to wait for the room to go quiet, at most 600"`
}

type deployOutput struct {
	Room    string          `json:"room"`
	State   string          `json:"state"`
	Owner   string          `json:"owner,omitempty"`
	Note    string          `json:"note,omitempty"`
	Pending []deployAsk     `json:"pending,omitempty"`
	Running []deployAsk     `json:"running,omitempty"`
	Hold    json.RawMessage `json:"hold,omitempty"`
	Busy    []deployBusy    `json:"busy,omitempty"`
	Ungated []string        `json:"ungated,omitempty"`
}

type deployBusy struct {
	Card  string `json:"card"`
	Title string `json:"title"`
}

// roomHoldView is what the room's `GET /v1/hold` answers.
type roomHoldView struct {
	Hold    json.RawMessage `json:"hold"`
	Busy    []deployBusy    `json:"busy"`
	Quiet   bool            `json:"quiet"`
	Ungated []string        `json:"ungated"`
}

func (v roomHoldView) held() bool {
	s := strings.TrimSpace(string(v.Hold))
	return s != "" && s != "null"
}

const deployToolDesc = "Ask for a room deploy, or, as the deploy owner, run one without interrupting anybody.\n\n" +
	"`request` (anyone): records why you need the room redeployed and tells the deploy owner once. Keep " +
	"working: you are held when the deploy starts and woken after it. A second request while one is " +
	"waiting is added to it.\n\n" +
	"The DEPLOY OWNER runs the rest. `start` holds the room: every other card's next tool call is refused " +
	"with \"end your turn and wait\", and messages between agents are held. `wait` blocks until the room " +
	"is quiet, at most 600 seconds, and answers `quiet` or who is still busy. Then run the deploy as you " +
	"do today. The room lifts its own hold when it comes back and wakes every held card with one line. " +
	"`cancel` lifts the hold with no restart and wakes them the same way.\n\n" +
	"`status` (anyone): the waiting requests, the hold, and who is busy."

func (c *controlMCP) registerDeploy(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_deploy", Description: deployToolDesc},
		audited(c, "ctl-deploy", describeDeploy, c.deployHandler))
}

func describeDeploy(req *mcp.CallToolRequest, in deployInput, out deployOutput) (string, string, bool) {
	if in.Action == "status" || in.Action == "wait" {
		return "", "", false
	}
	room := out.Room
	if room == "" {
		room = roomOf(req)
	}
	return room, "deploy " + in.Action, true
}

func (c *controlMCP) deployHandler(ctx context.Context, req *mcp.CallToolRequest, in deployInput) (
	*mcp.CallToolResult, deployOutput, error) {

	room := strings.TrimSpace(in.Room)
	if room == "" {
		room = roomOf(req)
	}
	out := deployOutput{Room: room, Owner: c.deployOwner()}
	if room == "" {
		return nil, out, errors.New("no room named, and this session has none. pass room")
	}
	if c.settings == nil {
		return nil, out, errors.New("this hub keeps no settings, so it cannot hold a deploy request")
	}
	switch strings.TrimSpace(in.Action) {
	case "request":
		return c.deployRequest(ctx, req, in, out)
	case "start":
		return c.deployStart(ctx, req, in, out)
	case "wait":
		return c.deployWait(ctx, req, in, out)
	case "cancel":
		return c.deployCancel(ctx, req, in, out)
	case "status":
		return c.deployStatus(ctx, out)
	}
	return nil, out, errors.New(`action is request, start, wait, cancel or status`)
}

func (c *controlMCP) deployOwner() string {
	if c.settings == nil {
		return ""
	}
	st := c.settings()
	if st == nil {
		return ""
	}
	v, _ := st.HubSetting(SettingDeployOwner)
	return strings.TrimPrefix(strings.TrimSpace(v), "@")
}

// loadRequests reads a room's record, and settles a running deploy whose hold
// has ended: the hold is the room's, and a room without one is a deploy that is
// over whichever way it went.
func (c *controlMCP) loadRequests(ctx context.Context, room string) (deployRequests, roomHoldView, error) {
	var rec deployRequests
	var view roomHoldView
	st := c.settings()
	if st == nil {
		return rec, view, errors.New("this hub keeps no settings")
	}
	if v, err := st.HubSetting(deployRequestKey(room)); err != nil {
		return rec, view, err
	} else if strings.TrimSpace(v) != "" {
		if err := json.Unmarshal([]byte(v), &rec); err != nil {
			rec = deployRequests{}
		}
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/hold", room, nil, &view); err != nil {
		var be *boardError
		if errors.As(err, &be) && be.bare && be.code == http.StatusNotFound {
			return rec, view, fmt.Errorf("%s is older than the room deploy hold. update the room", room)
		}
		return rec, view, err
	}
	if rec.HoldID != "" && !view.held() {
		rec.Running, rec.HoldID = nil, ""
	}
	return rec, view, nil
}

func (c *controlMCP) saveRequests(room string, rec deployRequests) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return c.settings().SetHubSetting(deployRequestKey(room), string(raw))
}

func (c *controlMCP) deployRequest(ctx context.Context, req *mcp.CallToolRequest, in deployInput, out deployOutput) (
	*mcp.CallToolResult, deployOutput, error) {

	if out.Owner == "" {
		return nil, out, &refusedError{"no deploy owner is set: ask the human"}
	}
	why := strings.TrimSpace(in.Why)
	if why == "" {
		return nil, out, errors.New("say what the deploy is for")
	}
	c.deployMu.Lock()
	rec, view, err := c.loadRequests(ctx, out.Room)
	if err != nil {
		c.deployMu.Unlock()
		return nil, out, err
	}
	first := len(rec.Pending) == 0
	rec.Pending = append(rec.Pending, deployAsk{By: orUnnamed(agentOf(req)), Why: why,
		SHA: strings.TrimSpace(in.SHA), At: time.Now().UTC().Format(time.RFC3339)})
	err = c.saveRequests(out.Room, rec)
	c.deployMu.Unlock()
	if err != nil {
		return nil, out, err
	}
	out.Pending, out.Running = rec.Pending, rec.Running
	switch {
	case view.held():
		out.State = "next"
		out.Note = "recorded as the NEXT deploy: " + out.Room + " is being redeployed now, and the build may " +
			"already be made. " + out.Owner + " is told after the wake."
	case first:
		out.State = "requested"
		c.tellOwner(ctx, req, out.Owner, "deploy requested for "+out.Room+" by "+orUnnamed(agentOf(req))+
			": "+why+". when you are ready: atrium_deploy start, then wait, then run the deploy.")
		out.Note = "requested, " + out.Owner + " deploys it. keep working: you will be held when it starts " +
			"and woken after."
	default:
		out.State = "added"
		out.Note = "added to the request " + rec.Pending[0].By + " made at " + rec.Pending[0].At + "."
	}
	return nil, out, nil
}

// tellOwner is the one say to the owner, on the existing say path. Best effort:
// a request is recorded whether or not the owner could be told.
func (c *controlMCP) tellOwner(ctx context.Context, req *mcp.CallToolRequest, owner, text string) {
	if _, _, err := c.sayHandler(ctx, req, sayInput{To: owner, Text: text}); err != nil && c.audit != nil {
		c.audit(roomOf(req), "ctl-deploy", "could not tell the deploy owner "+owner+": "+auditOutcome(err))
	}
}

// isOwner reports whether the caller is the deploy owner.
func (c *controlMCP) isOwner(ctx context.Context, req *mcp.CallToolRequest, owner string) bool {
	me := agentOf(req)
	if me == "" || owner == "" {
		return false
	}
	name, ownerRoom, err := SplitAddress(owner)
	if err != nil {
		return false
	}
	if ownerRoom == "" {
		ownerRoom = roomOf(req)
	}
	if !equalFold(ownerRoom, roomOf(req)) {
		return false
	}
	want, _, err := c.resolvePeer(ctx, ownerRoom, name)
	if err != nil {
		return false
	}
	got, _, err := c.resolvePeer(ctx, roomOf(req), me)
	return err == nil && got == want
}

func (c *controlMCP) deployStart(ctx context.Context, req *mcp.CallToolRequest, in deployInput, out deployOutput) (
	*mcp.CallToolResult, deployOutput, error) {

	if !c.isOwner(ctx, req, out.Owner) {
		return nil, out, &refusedError{"only the deploy owner (" + orUnnamed(out.Owner) + ") starts a deploy"}
	}
	c.deployMu.Lock()
	defer c.deployMu.Unlock()
	rec, _, err := c.loadRequests(ctx, out.Room)
	if err != nil {
		return nil, out, err
	}
	whys := make([]string, 0, len(rec.Pending))
	for _, a := range rec.Pending {
		whys = append(whys, a.Why+" ("+a.By+")")
	}
	if w := strings.TrimSpace(in.Why); w != "" {
		whys = append(whys, w)
	}
	me := agentOf(req)
	var view roomHoldView
	if err := c.ask(ctx, http.MethodPost, "/v1/hold", out.Room, map[string]any{
		"action": "start", "by": me, "by_card": me, "whys": whys, "exempt": in.Exempt,
	}, &view); err != nil {
		return nil, out, err
	}
	var h struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(view.Hold, &h)
	rec.Running, rec.Pending, rec.HoldID = rec.Pending, nil, h.ID
	if err := c.saveRequests(out.Room, rec); err != nil {
		return nil, out, err
	}
	out.State, out.Running, out.Hold, out.Busy, out.Ungated = "held", rec.Running, view.Hold, view.Busy, view.Ungated
	out.Note = "held. call atrium_deploy wait until it answers quiet, then run the deploy."
	if len(view.Ungated) > 0 {
		out.Note += " some cards are not gated and were not held: see ungated."
	}
	return nil, out, nil
}

func (c *controlMCP) deployWait(ctx context.Context, req *mcp.CallToolRequest, in deployInput, out deployOutput) (
	*mcp.CallToolResult, deployOutput, error) {

	if !c.isOwner(ctx, req, out.Owner) {
		return nil, out, &refusedError{"only the deploy owner waits on a deploy"}
	}
	limit := time.Duration(in.MaxSeconds) * time.Second
	if limit <= 0 || limit > deployWaitMax {
		limit = deployWaitMax
	}
	deadline := time.Now().Add(limit)
	for {
		var view roomHoldView
		if err := c.ask(ctx, http.MethodGet, "/v1/hold", out.Room, nil, &view); err != nil {
			return nil, out, err
		}
		out.Hold, out.Busy, out.Ungated = view.Hold, view.Busy, view.Ungated
		switch {
		case !view.held():
			out.State, out.Note = "not-held", "the room is not held. start the deploy first."
			return nil, out, nil
		case view.Quiet:
			out.State, out.Note = "quiet", "quiet. run the deploy now."
			return nil, out, nil
		case !time.Now().Add(deployWaitPoll).Before(deadline):
			out.State = "busy"
			out.Note = "still busy at the limit. wait again, or run the deploy anyway: the restart ends what is " +
				"running and the wake says the call did not run."
			return nil, out, nil
		}
		select {
		case <-ctx.Done():
			return nil, out, ctx.Err()
		case <-time.After(deployWaitPoll):
		}
	}
}

func (c *controlMCP) deployCancel(ctx context.Context, req *mcp.CallToolRequest, in deployInput, out deployOutput) (
	*mcp.CallToolResult, deployOutput, error) {

	if !c.isOwner(ctx, req, out.Owner) {
		return nil, out, &refusedError{"only the deploy owner calls a deploy off"}
	}
	c.deployMu.Lock()
	defer c.deployMu.Unlock()
	// Read before the lift, which would otherwise settle the running deploy as over.
	rec, _, err := c.loadRequests(ctx, out.Room)
	if err != nil {
		return nil, out, err
	}
	var view roomHoldView
	if err := c.ask(ctx, http.MethodPost, "/v1/hold", out.Room, map[string]any{
		"action": "lift", "outcome": "cancelled", "by": agentOf(req), "why": strings.TrimSpace(in.Why),
	}, &view); err != nil {
		return nil, out, err
	}
	// Called off, not done: what it was for is still wanted.
	rec.Pending = append(rec.Running, rec.Pending...)
	rec.Running, rec.HoldID = nil, ""
	if err := c.saveRequests(out.Room, rec); err != nil {
		return nil, out, err
	}
	out.State, out.Pending = "cancelled", rec.Pending
	out.Note = "called off. every held card is woken, and the requests are waiting again."
	return nil, out, nil
}

func (c *controlMCP) deployStatus(ctx context.Context, out deployOutput) (*mcp.CallToolResult, deployOutput, error) {
	c.deployMu.Lock()
	rec, view, err := c.loadRequests(ctx, out.Room)
	if err == nil {
		err = c.saveRequests(out.Room, rec)
	}
	c.deployMu.Unlock()
	if err != nil {
		return nil, out, err
	}
	out.Pending, out.Running, out.Hold, out.Busy, out.Ungated = rec.Pending, rec.Running, view.Hold, view.Busy, view.Ungated
	switch {
	case view.held():
		out.State = "held"
	case len(rec.Pending) > 0:
		out.State = "requested"
	default:
		out.State = "idle"
	}
	return nil, out, nil
}

// serveDeployOwner answers GET and PUT /_hub/deploy-owner, `{"owner": "merge"}`.
// Set from the hub's machine only, like the launch caps: who may take a room down
// is not something a board reached over an overlay decides. Empty clears it.
func (p *Proxy) serveDeployOwner(w http.ResponseWriter, r *http.Request) {
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	p.mu.Lock()
	st := p.capStore
	p.mu.Unlock()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if !loopbackRemote(r.RemoteAddr) {
			fail(http.StatusForbidden, "the deploy owner is set only from the machine the hub runs on")
			return
		}
		var in struct {
			Owner string `json:"owner"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&in); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		owner := strings.TrimPrefix(strings.TrimSpace(in.Owner), "@")
		if owner != "" {
			if _, _, err := SplitAddress(owner); err != nil {
				fail(http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := st.SetHubSetting(SettingDeployOwner, owner); err != nil {
			fail(http.StatusInternalServerError, "could not save the owner: "+err.Error())
			return
		}
		p.RecordAudit("", "deploy-owner-set", orUnnamed(owner))
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		return
	}
	v, _ := st.HubSetting(SettingDeployOwner)
	_ = json.NewEncoder(w).Encode(map[string]string{"owner": v})
}

func orUnnamed(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unnamed"
	}
	return s
}
