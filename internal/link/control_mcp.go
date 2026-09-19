package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The control tools, served by the HUB over HTTP rather than by a stdio child
// per session.
//
// WHY THE HUB AND NOT THE ROOM. `internal/cli/control.go` exists because the
// thing that restarts the daemon has to outlive it: a session supervised by the
// daemon cannot restart the daemon, since closing its terminal takes the caller
// with it. The room IS the daemon. The hub already outlives rooms by design, so
// the hub is the surviving third party that control needs. Moving the server
// here also ends the one-process-per-session cost: a session opens an HTTP
// connection to this endpoint from inside its own claude process, so the dozen
// ~24MB `atrium-control.exe` children drop to zero.
//
// IDENTITY ARRIVES IN HEADERS, PER REQUEST. Each session's MCP config carries
// `X-Atrium-Agent` (which session is calling, the old `ATRIUM_AGENT_NAME`) and
// `X-Atrium-Room` (which room a command targets). Claude Code evaluates the
// `${ENV}` in those headers per session at connect, so two sessions send two
// values to this one URL. The go-sdk hands them to a tool on
// `req.Extra.Header`, and the server runs stateless so no session id is pinned
// and every POST is read on its own.
//
// HOW A TOOL REACHES THE BOARD. Rather than read a local daemon location file
// the way the CLI did, these call the hub's OWN board over loopback and set
// `X-Atrium-Room` from the caller's header. That inherits the proxy's scoping,
// aggregation and offline-room behaviour whole, instead of a second copy of it.

// AgentHeader names the calling session. The room header is `RoomHeader`, shared
// with the scoped-board path in proxy.go, so a control call scopes exactly the
// way a board request does.
//
// LANDMINE THAT PICKED THESE NAMES: Claude Code blanks any header whose name
// contains TOKEN, SECRET, KEY, PASSWORD or AUTH. `X-Atrium-Agent` and
// `X-Atrium-Room` avoid all five.
const AgentHeader = "X-Atrium-Agent"

// controlTimeout bounds every call these tools make to the hub's own board.
//
// Short. All of them are local HTTP against a listener in this process, and the
// failure worth reporting quickly is "the board is down", not "a slow one".
const controlTimeout = 8 * time.Second

// controlMCP holds what the hub-side tools need: where to reach the hub's own
// board, a client to do it with, and the hub to forward a restart over.
type controlMCP struct {
	// board is the loopback base for this hub's own API, e.g.
	// http://127.0.0.1:7778. See loopbackBase.
	board  string
	client *http.Client
	// hub forwards a restart_atrium down the link to a room. Nil in a test that
	// only exercises the read tools.
	hub *Hub
}

// newControlHandler builds the hub-side control MCP server as an http.Handler,
// ready to mount at /_hub/mcp.
//
// ONE SERVER FOR EVERY REQUEST, and that is correct rather than a shortcut: the
// tools read who is calling from the per-request header, never from the server,
// so there is nothing per session to build. `getServer` returns the same one.
func newControlHandler(board string, hub *Hub) http.Handler {
	c := &controlMCP{board: board, client: &http.Client{Timeout: controlTimeout}, hub: hub}
	srv := c.server()
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		// STATELESS, so no Mcp-Session-Id is validated and a temporary session is
		// used per request. Every session POSTs to the same URL carrying its own
		// identity headers, which is the whole point: one endpoint, many callers,
		// each read on its own.
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// server wires the tools onto one MCP server.
func (c *controlMCP) server() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "atrium-control", Version: "v0.0.0-dev"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_status",
		Description: "Is atrium running, is its store healthy, and what is on the board.\n\n" +
			"Answered from the hub, which serves the board, rather than from a local daemon. " +
			"Scopes to your room when your session has one, so the counts are yours and not " +
			"every room's.",
	}, c.statusHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_peers",
		Description: "The other sessions on this board: what each is called, what it is doing, " +
			"where it is working, and how long it has been waiting.\n\n" +
			"Call this before saying anything to anybody. The handle is what `atrium_say` " +
			"takes, and a handle read off a card title rather than from here is usually wrong.",
	}, c.peersHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_say",
		Description: "Say something to another session on the board.\n\n" +
			"DELIVERY IS NOT TYPING. Where atrium owns the terminal the text is typed in and " +
			"the answer says `terminal`. Where it does not, the message is QUEUED and reaches " +
			"that session at its next tool call or at the end of its turn, and the answer says " +
			"`queued`. Neither is instant and neither is a reply: if you want one, ask for it " +
			"and then look, or wait to be told.\n\n" +
			"What arrives is framed as a person speaking, not as a refusal, so write it as one " +
			"agent talking to another. The receiving session is told who you are " +
				"automatically, so do not announce yourself.\n\n" +
			"Ask for a reply explicitly, and say how. The other session answers by calling " +
			"`atrium_say` back at your own handle, which is in `atrium_peers` under `me`.",
	}, c.sayHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_task",
		Description: "One card: its status, what its runner is doing right now, and its recent " +
			"events.\n\n" +
			"This does NOT return what the session printed. Atrium records that a session ran " +
			"and every status it moved through, never its output, so `needs-input` here means " +
			"it stopped and not what it said. To learn what it thinks, ask it, and have it " +
			"answer with `atrium_say`.",
	}, c.taskHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_launch",
		Description: "Start a new agent in a directory, on its own card, supervised by atrium.\n\n" +
			"A REAL SESSION, not a subagent. It has its own conversation, its own permission " +
			"gate and its own card, it outlives the session that started it, and the human can " +
			"watch it, type into it or take it over. It is a colleague rather than a call.\n\n" +
			"Atrium does not create the directory. Make it first.\n\n" +
			"IT STARTS EMPTY. Unlike a subagent it inherits nothing of your conversation, " +
			"which is the point as often as it is the cost: hand it what it needs and it is " +
			"not carrying three hours of unrelated debugging. Use `prompt` for that.\n\n" +
			"Its permission requests go to the HUMAN, on their board, so an agent started here " +
			"and left alone stops at the first gated command. Say who asked for it and why, " +
			"because whoever finds the card later will want to know.\n\n" +
			"Use `brief` for context it needs before the task. It is written to BRIEF.md in the " +
			"new session's directory ON THE ROOM and read first, so it survives compaction and is " +
			"still there when a human takes the card over. Put in it what you would tell a " +
			"colleague joining: what the job is, what has been tried, what the constraints are, " +
			"and what NOT to do.\n\n" +
			"Returns the card id. Use it with `atrium_task` and `atrium_say`.",
	}, c.launchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_exit",
		Description: "Ask a session to finish and leave.\n\n" +
			"ASKED, not killed. Atrium sends the exit keys its harness is configured with, so " +
			"the runner shuts itself down and writes whatever it writes on the way out. Its " +
			"card and its whole history stay on the board.\n\n" +
			"Say something first if the work is not finished. A session asked to leave mid-task " +
			"leaves mid-task.",
	}, c.exitHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "restart_atrium",
		Description: "Wind a room's daemon down and bring it straight back on the same database.\n\n" +
			"FORWARDED TO THE ROOM. The hub spawns nothing on the room's machine: it sends the " +
			"instruction over the link and the room parks its other agents, spawns a detached " +
			"restarter that outlives it, and winds down. Scoped to your room via X-Atrium-Room.\n\n" +
			"SCHEDULED, not immediate. It returns at once and the restart happens a moment later, " +
			"because it takes down every terminal the room owns, this session included if it is " +
			"one of them. Say what you are doing before you call this, and expect to be resumed " +
			"rather than answered.\n\n" +
			"OTHER AGENTS ARE PARKED FIRST. Any supervised session that is working is told what is " +
			"coming and given time to stop. Pass `force` to restart even if some are still busy.",
	}, c.restartHandler)

	return s
}

// ── reaching the hub's own board ──────────────────────────────────────────────

// ask does one request against this hub's board and decodes the answer.
//
// The BODY of a failure is returned, not just the status. Every refusal in the
// board API is a sentence written to be read by whoever caused it, and
// flattening those into "400 Bad Request" throws away the only useful part.
//
// `room` scopes the call to one room by setting `X-Atrium-Room`, exactly the
// way a scoped board does. Empty leaves it off, which is the aggregate view.
func (c *controlMCP) ask(ctx context.Context, method, path, room string, body, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.board+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the board at %s: %w", c.board, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if strings.TrimSpace(e.Error) != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("the board answered %s", res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// agentOf and roomOf read the per-request identity headers the go-sdk hands a
// tool on `Extra`. Absent when the caller is not one of atrium's sessions,
// which is an ordinary way to use these tools and not an error.
func agentOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(AgentHeader))
}

func roomOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(RoomHeader))
}

// ctlCard is the part of a task these tools report. The board's own shape is
// much larger and most of it is for drawing.
type ctlCard struct {
	ID       string `json:"id"`
	Title    string `json:"display_title"`
	Wire     string `json:"wire_name"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
	Runner   string `json:"runner"`
	Why      string `json:"why"`
	Idle     int    `json:"idle_seconds"`
	Wait     int    `json:"wait_seconds"`
	Superv   bool   `json:"supervised"`
	Activity struct {
		What string `json:"what"`
	} `json:"activity"`
}

// ── status ────────────────────────────────────────────────────────────────────

type statusInput struct{}

type statusOutput struct {
	Running bool   `json:"running"`
	Board   string `json:"board,omitempty"`
	DB      string `json:"db,omitempty"`
	Room    string `json:"room,omitempty"`
	// Halted is the store having failed. The daemon is up and the agent listener
	// is closed, which is a state worth reporting as its own thing rather than as
	// "running".
	Halted bool   `json:"halted,omitempty"`
	Cause  string `json:"cause,omitempty"`
	Cards  int    `json:"cards,omitempty"`
	// Waiting is how many want you right now, which is the only number worth
	// reading without opening the board.
	Waiting int    `json:"waiting,omitempty"`
	Note    string `json:"note,omitempty"`
}

func (c *controlMCP) statusHandler(ctx context.Context, req *mcp.CallToolRequest, _ statusInput) (
	*mcp.CallToolResult, statusOutput, error) {

	out := statusOutput{Board: c.board, Room: roomOf(req)}
	room := out.Room

	var health map[string]any
	if err := c.ask(ctx, http.MethodGet, "/v1/health", room, nil, &health); err != nil {
		out.Note = "the hub board did not answer: " + err.Error()
		return nil, out, nil
	}
	out.Running = true
	if v, ok := health["halted"].(bool); ok {
		out.Halted = v
	}
	if v, ok := health["cause"].(string); ok {
		out.Cause = v
	}
	if v, ok := health["db"].(string); ok {
		out.DB = v
	}

	var body struct {
		Tasks []struct {
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err == nil {
		out.Cards = len(body.Tasks)
		for _, t := range body.Tasks {
			if t.Status == "needs-input" || t.Status == "needs-permission" {
				out.Waiting++
			}
		}
	}
	if room == "" {
		out.Note = "no room named, so this is every room the hub can see. set X-Atrium-Room, " +
			"or launch from a session that has one, to scope it."
	}
	return nil, out, nil
}

// ── peers ──────────────────────────────────────────────────────────────────────

type peersInput struct {
	// All includes cards with no session on them. Off by default: the question
	// this tool answers is who can be spoken to.
	All bool `json:"all,omitempty" jsonschema:"include cards that have no running session"`
}

type peer struct {
	Handle string `json:"handle"`
	Card   string `json:"card"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Doing  string `json:"doing,omitempty"`
	Where  string `json:"where,omitempty"`
	// Waiting is how long it has wanted somebody, in seconds. The one number
	// worth acting on: a peer that has been waiting an hour is a peer nobody
	// answered.
	Waiting int `json:"waiting_seconds,omitempty"`
	// Owned is whether atrium holds this session's terminal, which decides
	// whether a message is typed or queued.
	Owned bool `json:"atrium_owns_terminal"`
}

type peersOutput struct {
	// Me is this session's own handle, when it has one. It is what a peer has to
	// be told in order to answer, and an agent has no other way to learn it.
	Me    string `json:"me,omitempty"`
	Peers []peer `json:"peers"`
	Note  string `json:"note,omitempty"`
}

func (c *controlMCP) peersHandler(ctx context.Context, req *mcp.CallToolRequest, in peersInput) (
	*mcp.CallToolResult, peersOutput, error) {

	out := peersOutput{Peers: []peer{}}
	// The header names this session, which is how a runner learns its own handle.
	out.Me = agentOf(req)
	room := roomOf(req)

	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return nil, out, err
	}
	for _, t := range body.Tasks {
		if t.Wire == out.Me && out.Me != "" {
			continue
		}
		// A card with no session is a place, not somebody to talk to. Kept out
		// unless asked for, because the list is long and the useful part of it is
		// short.
		live := t.Status != "done" && t.Status != "dead" && t.Status != "shelved"
		if !in.All && !live {
			continue
		}
		out.Peers = append(out.Peers, peer{
			Handle: t.Wire, Card: t.ID, Title: t.Title, Status: t.Status,
			Doing: t.Activity.What, Where: t.Worktree,
			Waiting: t.Wait, Owned: t.Superv,
		})
	}
	if out.Me == "" {
		out.Note = "this session is not on the board, so it has no handle. a peer cannot " +
			"answer you: ask it to leave its reply somewhere you can read instead."
	}
	return nil, out, nil
}

// resolvePeer turns a handle or a card id into both, within a room's scope.
//
// Handles are matched first and exactly. A card id is a ULID and a handle is a
// name somebody chose, so the two cannot collide, and trying the name first
// means a caller that pasted a handle never gets an obscure 404 from an endpoint
// that wanted an id.
func (c *controlMCP) resolvePeer(ctx context.Context, room, who string) (id, handle string, err error) {
	who = strings.TrimSpace(who)
	if who == "" {
		return "", "", fmt.Errorf("say who to")
	}
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return "", "", err
	}
	for _, t := range body.Tasks {
		if t.Wire == who || t.ID == who {
			return t.ID, t.Wire, nil
		}
	}
	// The list of ones that would have worked, which is the whole of the fix for
	// a wrong handle and is otherwise another tool call away.
	names := make([]string, 0, len(body.Tasks))
	for _, t := range body.Tasks {
		if t.Status == "done" || t.Status == "dead" {
			continue
		}
		names = append(names, t.Wire)
	}
	if len(names) == 0 {
		return "", "", fmt.Errorf("no session called %q, and nothing else is running either", who)
	}
	return "", "", fmt.Errorf("no session called %q. these would have worked: %s",
		who, strings.Join(names, ", "))
}

// ── say ────────────────────────────────────────────────────────────────────────

type sayInput struct {
	// To is a handle from atrium_peers, or a card id. Both are accepted because
	// both are things the caller has in hand, and refusing the one it happens to
	// be holding is a puzzle rather than a rule.
	To string `json:"to" jsonschema:"the handle or card id to say it to"`
	// Text is what to say, as one agent to another.
	Text string `json:"text" jsonschema:"what to say, as one agent to another. the recipient is told who you are automatically, so do not announce yourself"`
}

type sayOutput struct {
	// Delivered is `terminal` or `queued`. Two different promises, and the caller
	// has to know which one was made: typed has already landed, queued has not
	// and will not until that session next reaches a hook.
	Delivered string `json:"delivered"`
	To        string `json:"to"`
	Card      string `json:"card"`
	Note      string `json:"note,omitempty"`
}

func (c *controlMCP) sayHandler(ctx context.Context, req *mcp.CallToolRequest, in sayInput) (
	*mcp.CallToolResult, sayOutput, error) {

	out := sayOutput{}
	if strings.TrimSpace(in.Text) == "" {
		return nil, out, fmt.Errorf("nothing to say")
	}
	room := roomOf(req)
	id, handle, err := c.resolvePeer(ctx, room, in.To)
	if err != nil {
		return nil, out, err
	}
	out.To, out.Card = handle, id

	// The caller's own handle, read off the per-request header the same way
	// atrium_peers reads `me`, so the attribution the recipient sees and the
	// handle it replies to both come from one source. Empty when a human or a
	// non-atrium caller sent this: passed through as empty, which the room reads
	// as the operator's own message channel rather than a peer, so no broken
	// attribution is ever rendered. The room frames it, not the hub, so a peer
	// message is not double-framed as coming from the operator.
	from := agentOf(req)

	var res struct {
		Delivered string `json:"delivered"`
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message", room,
		map[string]string{"text": in.Text, "from": from}, &res); err != nil {
		return nil, out, err
	}
	out.Delivered = res.Delivered
	if res.Delivered == "queued" {
		out.Note = "queued, not typed. it arrives at that session's next tool call or at the " +
			"end of its turn, which may be a while if it is idle."
	}
	return nil, out, nil
}

// ── one card ────────────────────────────────────────────────────────────────────

type taskInput struct {
	Card string `json:"card" jsonschema:"a card id or a handle"`
	// Events includes the recent history, which is what a card DID rather than
	// where it is now.
	Events bool `json:"events,omitempty" jsonschema:"include recent events"`
}

type taskEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

type taskOutput struct {
	Card    string      `json:"card"`
	Handle  string      `json:"handle,omitempty"`
	Title   string      `json:"title,omitempty"`
	Status  string      `json:"status"`
	Doing   string      `json:"doing,omitempty"`
	Where   string      `json:"where,omitempty"`
	Why     string      `json:"why,omitempty"`
	Idle    int         `json:"idle_seconds,omitempty"`
	Waiting int         `json:"waiting_seconds,omitempty"`
	Owned   bool        `json:"atrium_owns_terminal"`
	Events  []taskEvent `json:"events,omitempty"`
	Note    string      `json:"note,omitempty"`
}

func (c *controlMCP) taskHandler(ctx context.Context, req *mcp.CallToolRequest, in taskInput) (
	*mcp.CallToolResult, taskOutput, error) {

	out := taskOutput{}
	room := roomOf(req)
	id, _, err := c.resolvePeer(ctx, room, in.Card)
	if err != nil {
		return nil, out, err
	}
	var t ctlCard
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), room, nil, &t); err != nil {
		return nil, out, err
	}
	out.Card, out.Handle, out.Title = t.ID, t.Wire, t.Title
	out.Status, out.Doing, out.Where, out.Why = t.Status, t.Activity.What, t.Worktree, t.Why
	out.Idle, out.Waiting, out.Owned = t.Idle, t.Wait, t.Superv

	if in.Events {
		var body struct {
			Events []struct {
				At   string `json:"at"`
				Kind string `json:"kind"`
			} `json:"events"`
		}
		if err := c.ask(ctx, http.MethodGet,
			"/v1/tasks/"+url.PathEscape(id)+"/events", room, nil, &body); err == nil {
			// The tail, because the useful end of a history is the recent one and a
			// card that has been up for days has hundreds.
			from := 0
			if len(body.Events) > 20 {
				from = len(body.Events) - 20
			}
			for _, e := range body.Events[from:] {
				out.Events = append(out.Events, taskEvent{At: e.At, Kind: e.Kind})
			}
		}
	}
	out.Note = "status and events only. atrium does not record what a session printed, so this " +
		"cannot tell you what it said or thinks."
	return nil, out, nil
}

// ── launch ──────────────────────────────────────────────────────────────────────

type launchInput struct {
	Cwd    string `json:"cwd" jsonschema:"the directory to run in. it has to exist already"`
	Title  string `json:"title,omitempty" jsonschema:"what to call the card"`
	Why    string `json:"why,omitempty" jsonschema:"what this is for, read back later"`
	Prompt string `json:"prompt,omitempty" jsonschema:"the first instruction it gets"`
	// Brief is written to BRIEF.md in the new session's directory on the room and
	// read first, so it survives compaction and can be re-read.
	Brief  string   `json:"brief,omitempty" jsonschema:"context to hand the new session. written to BRIEF.md in its directory on the room and read before it starts, so it survives compaction and can be re-read"`
	Runner string   `json:"runner,omitempty" jsonschema:"which configured runner to start. default claude"`
	Tags   []string `json:"tags,omitempty" jsonschema:"free text labels, used for grouping and filtering"`
	// Theme names the terminal palette the new session comes up in. Empty leaves
	// it to the board, which colours a themeless card from its repo, so an
	// agent-driven launch looks the same as a board-dialog one without this.
	Theme string `json:"theme,omitempty" jsonschema:"the terminal palette to come up in. empty lets the board colour it from the repo"`
}

type launchOutput struct {
	Card   string `json:"card"`
	Handle string `json:"handle"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Watch  string `json:"watch,omitempty"`
	// Brief is where the briefing was written on the room, when there was one.
	// Returned so the caller can add to it later: a peer that turns out to need
	// one more fact should be given it in the file it already reads.
	Brief string `json:"brief,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (c *controlMCP) launchHandler(ctx context.Context, req *mcp.CallToolRequest, in launchInput) (
	*mcp.CallToolResult, launchOutput, error) {

	out := launchOutput{}
	if strings.TrimSpace(in.Cwd) == "" {
		return nil, out, fmt.Errorf("say where to run it. atrium does not create the directory")
	}
	harness := strings.TrimSpace(in.Runner)
	if harness == "" {
		harness = "claude"
	}
	room := roomOf(req)

	// The briefing is written ON THE ROOM: /v1/launch carries the text and the
	// room's own daemon writes BRIEF.md into the new session's directory before
	// it starts. The hub has no such directory to write to, which is why this is
	// a field on the request rather than a file this side writes.
	reqBody := map[string]any{
		"harness": harness, "cwd": in.Cwd, "title": in.Title,
		"why": in.Why, "prompt": strings.TrimSpace(in.Prompt),
		"brief": strings.TrimSpace(in.Brief), "tags": in.Tags,
		"theme": strings.TrimSpace(in.Theme),
	}
	var t ctlCard
	if err := c.ask(ctx, http.MethodPost, "/v1/launch", room, reqBody, &t); err != nil {
		return nil, out, err
	}
	out.Card, out.Handle, out.Title, out.Status = t.ID, t.Wire, t.Title, t.Status
	if strings.TrimSpace(in.Brief) != "" {
		// The room wrote it; name it back in the same slash form the rest of
		// atrium carries, so the caller can add to the file it already reads.
		out.Brief = strings.TrimRight(strings.ReplaceAll(in.Cwd, "\\", "/"), "/") + "/" + "BRIEF.md"
	}
	// Where the human looks. Worth returning rather than leaving them to assemble
	// it, because the fragment form is not guessable.
	out.Watch = c.board + "/#term=" + url.PathEscape(t.ID)
	out.Note = "started. its permission requests go to the human on their board, so it will " +
		"stop at the first gated command unless somebody is watching."
	return nil, out, nil
}

// ── exit ────────────────────────────────────────────────────────────────────────

type exitInput struct {
	Card string `json:"card" jsonschema:"a card id or a handle"`
}

type exitOutput struct {
	Card   string `json:"card"`
	Handle string `json:"handle,omitempty"`
	Asked  bool   `json:"asked"`
	Note   string `json:"note,omitempty"`
}

func (c *controlMCP) exitHandler(ctx context.Context, req *mcp.CallToolRequest, in exitInput) (
	*mcp.CallToolResult, exitOutput, error) {

	out := exitOutput{}
	room := roomOf(req)
	id, handle, err := c.resolvePeer(ctx, room, in.Card)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = id, handle
	if err := c.ask(ctx, http.MethodPost,
		"/v1/tasks/"+url.PathEscape(id)+"/exit", room, nil, nil); err != nil {
		return nil, out, err
	}
	out.Asked = true
	out.Note = "asked to leave with its harness's exit keys. the card and its history stay."
	return nil, out, nil
}

// ── restart ──────────────────────────────────────────────────────────────────────

type restartInput struct {
	Why         string `json:"why,omitempty" jsonschema:"what this restart is for"`
	Force       bool   `json:"force,omitempty" jsonschema:"restart even if other agents are still working"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for other agents, in seconds"`
}

type restartOutput struct {
	Scheduled bool   `json:"scheduled"`
	Room      string `json:"room,omitempty"`
	Note      string `json:"note"`
}

// restartHandler forwards a restart instruction to the caller's room.
//
// It names a room or it does nothing: a restart with no room is a restart of
// "whichever", and that is exactly the mistake this must not make. The hub only
// forwards; the room parks, spawns the detached restarter and winds down, so the
// answer here is "instructed", not "done". See Hub.AskRestart and the room's
// OnRestart.
func (c *controlMCP) restartHandler(_ context.Context, req *mcp.CallToolRequest, in restartInput) (
	*mcp.CallToolResult, restartOutput, error) {

	room := roomOf(req)
	out := restartOutput{Room: room}
	if room == "" {
		return nil, out, fmt.Errorf("no room named. a restart has to name the room to restart, " +
			"which comes from X-Atrium-Room. call from a session that has one")
	}
	if c.hub == nil {
		return nil, out, fmt.Errorf("this hub cannot forward a restart")
	}
	if err := c.hub.AskRestart(room, RestartAsk{
		Why: strings.TrimSpace(in.Why), Force: in.Force, WaitSeconds: in.WaitSeconds,
	}); err != nil {
		return nil, out, fmt.Errorf("could not ask %s to restart: %w", room, err)
	}
	out.Scheduled = true
	out.Note = "asked " + room + " to restart. it parks its other agents, then winds down and " +
		"comes back on the same database. if this session is on that room it goes down too: say " +
		"nothing further this turn and expect to be resumed."
	return nil, out, nil
}

// ── mounting ──────────────────────────────────────────────────────────────────────

// loopbackBase turns the board's listen address into the base URL the control
// tools use to reach this hub's own API over loopback.
//
// The listen address is often `:7778`, meaning every interface, and a tool
// cannot dial an empty host. Loopback is the right target regardless: the tools
// run inside the hub process and the board listens on this machine.
func loopbackBase(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "http://127.0.0.1:7778"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// loopbackRemote reports whether a request came from this machine.
func loopbackRemote(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
