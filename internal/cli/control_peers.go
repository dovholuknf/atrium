package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Working with the other agents on the board, as tools rather than as curl.
//
// Everything here already existed on the HTTP API and could be reached with a
// shell. That is not the same as being usable: an agent driving another agent
// through `curl` spends its attention on JSON quoting, on a shell that refuses
// redirection, and on remembering which port. What it should be spending
// attention on is what to say.
//
// THESE ARE ON THE CONTROL SERVER, not the daemon, and not because of the
// restart problem that put `restart_atrium` here. It is that a stdio MCP
// server is reachable from a session whether or not that session is on the
// board, whether or not it is supervised, and whether or not `atrium` is on
// the PATH. The last one is not hypothetical: the first time an agent was
// asked to answer a peer, it could not, because `atrium tell` was a command
// not found and nothing said so.
//
// WHAT THESE TOOLS ARE NOT. They are not a way to type into somebody else's
// terminal on a whim. `atrium_say` goes through the message endpoint, which
// types only where atrium owns the terminal and queues everywhere else, and
// `docs/architecture-v2.md` records at length why a peer never gets to type.
// The tool inherits that and must keep inheriting it.

// peerTimeout bounds every call these tools make to the board.
//
// Short. All of them are local HTTP against a daemon on loopback, and the
// failure worth reporting quickly is "no daemon", not "a slow one".
const peerToolTimeout = 8 * time.Second

// addPeerTools registers the agent-to-agent surface on the control server.
func addPeerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_peers",
		Description: "The other sessions on this board: what each is called, what it is doing, " +
			"where it is working, and how long it has been waiting.\n\n" +
			"Call this before saying anything to anybody. The handle is what `atrium_say` " +
			"takes, and a handle read off a card title rather than from here is usually wrong.\n\n" +
			"`rooms: true` adds the sessions on other rooms, asked of this room's hub. Their " +
			"handle is `name@room`, which `atrium_say` takes. A bare name always means this room.",
	}, peersHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_say",
		Description: "Say something to another session on the board.\n\n" +
			"IMMEDIATE BY DEFAULT. Where atrium owns the terminal the text is typed in as soon " +
			"as that session's input line is empty and no dialog is open, EVEN MID-TURN, and " +
			"the answer says `terminal`. Where it does not, or the line is not clear yet, the " +
			"message is QUEUED and the answer says `queued`: it reaches that session at its " +
			"next tool call, at the end of its turn, or when the line clears. Neither is a " +
			"reply: if you want one, ask for it and then look, or wait to be told.\n\n" +
			"`when: \"done\"` waits for that session's turn to end instead, for something that " +
			"should not disturb it mid-thought.\n\n" +
			"What arrives is framed as a person speaking, not as a refusal, so write it as one " +
			"agent talking to another. The receiving session is told who you are " +
			"automatically, so do not announce yourself.\n\n" +
			"Ask for a reply explicitly, and say how. The other session answers by calling " +
			"`atrium_say` back at your own handle, which is in `atrium_peers` under `me`.\n\n" +
			"ANOTHER ROOM is `name@room` (or `alias@room`), carried by this room's hub. The " +
			"recipient sees you as `you@thisroom` and answers to that. `held` means the hub or " +
			"that room is not answering: it is kept here and sent when they are, for up to a " +
			"day. `unconfirmed` means it may or may not have arrived, so ask before sending it " +
			"again.\n\n" +
			"`kind` is `fyi` for news the receiver need not act on, or `needs` (the default) for " +
			"anything that wants an answer or an action. A receiver that holds its notices keeps an " +
			"`fyi` on its card and is not interrupted, so say `needs` for anything you want read now. " +
			"On the same room only.",
	}, sayHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_report",
		Description: "Report on your work to the session that launched you, on this room or another.\n\n" +
			"IF ANOTHER SESSION LAUNCHED YOU, END EVERY TURN WITH THIS, or with an `atrium_say` " +
			"to your launcher. A turn that ends with neither is a silent stop: your launcher is " +
			"told you went quiet, and the human's board is told after that.\n\n" +
			"status is one of:\n" +
			"- `done`: the work is finished. Give `sha`, the commit it landed as, or `no_commit` " +
			"saying why there is none.\n" +
			"- `blocked`: you cannot go on. Give `ask`: what you need, and from whom.\n" +
			"- `question`: you need an answer to go on. Give `ask`.\n" +
			"- `progress`: you are stopping on purpose while something runs.\n\n" +
			"`summary` is what happened, in your words. It reaches your launcher verbatim. An " +
			"incomplete report is refused with what is missing, so fix it and call again.\n\n" +
			"`kind` is `fyi` for news your launcher need not act on, or `needs` (the default) for " +
			"anything that wants an answer or an action. A launcher that holds its notices keeps " +
			"an `fyi` on its card and is not interrupted. Only `progress` with no `ask` can be an `fyi`: `done`, `blocked`, `question` and anything with an `ask` are always `needs`.",
	}, reportHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_launch",
		Description: "Start a new agent in a directory, on its own card, supervised by atrium.\n\n" +
			"A REAL SESSION, not a subagent. It has its own conversation, its own permission " +
			"gate and its own card, it outlives the session that started it, and the human can " +
			"watch it, type into it or take it over. It is a colleague rather than a call.\n\n" +
			"Atrium does not create the directory. Make it first.\n\n" +
			"IT STARTS EMPTY. Unlike a subagent it inherits nothing of your conversation, " +
			"which is the point as often as it is the cost: hand it what it needs and it is " +
			"not carrying three hours of unrelated debugging. Use `brief` for that. It is " +
			"written to a file the session reads first and can re-read, so it survives " +
			"compaction and is still there when a human takes the card over. Put in it what " +
			"you would tell a colleague joining: what the job is, what has been tried, what " +
			"the constraints are, and what NOT to do.\n\n" +
			"Its permission requests go to the HUMAN, on their board, so an agent started here " +
			"and left alone stops at the first gated command. Say who asked for it and why, " +
			"because whoever finds the card later will want to know.\n\n" +
			"`lean_agents` and `lean_skills` start a LEAN claude worker (no user CLAUDE.md, memory or " +
			"other agents and skills) that keeps the Agent or Skill tool and the named ones only: files " +
			"in ~/.claude/agents (no .md) and directories in ~/.claude/skills on the room. They are " +
			"namespaced, so start `atrium:<name>`. A name with no file, or a runner that is not " +
			"claude, refuses the launch. The card keeps the lists.\n\n" +
			"`room` launches on ANOTHER ROOM, through this room's hub: `cwd` is then a path on that " +
			"room's machine, `brief` is written there and not here, and the worker's reports and " +
			"notices still reach you. An unknown room is refused with the rooms the hub knows, and one " +
			"that is not answering is refused and not held, so launch again when it is. A launch that " +
			"may have started is `unconfirmed`: look at `atrium_peers` with `rooms` before launching " +
			"again. Needs a hub that knows launching on another room.\n\n" +
			"Returns the card id. Use it with `atrium_task`, `atrium_say` and `atrium_exit`.",
	}, launchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_task",
		Description: "One card: its status, what its runner is doing right now, and its recent " +
			"events.\n\n" +
			"This does NOT return what the session printed. Atrium records that a session ran " +
			"and every status it moved through, never its output, so `needs-input` here means " +
			"it stopped and not what it said. To learn what it thinks, ask it, and have it " +
			"answer with `atrium_say`.\n\n" +
			"A card on ANOTHER ROOM is `name@room`, `alias@room` or `room~id`, as `atrium_say` " +
			"takes it, asked of this room's hub.",
	}, taskHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_exit",
		Description: "Ask a session to finish and leave.\n\n" +
			"ASKED, not killed. Atrium sends the exit keys its harness is configured with, so " +
			"the runner shuts itself down and writes whatever it writes on the way out. Its " +
			"card and its whole history stay on the board.\n\n" +
			"Say something first if the work is not finished. A session asked to leave mid-task " +
			"leaves mid-task.\n\n" +
			"A card on ANOTHER ROOM is `name@room`, `alias@room` or `room~id`, as `atrium_say` " +
			"takes it, asked of this room's hub.",
	}, exitHandler)
}

// ── talking to the board ────────────────────────────────────────────────────

// board finds the running daemon, or says why it cannot.
//
// The address comes from the file the daemon writes rather than from a
// constant, because a daemon on another port is a real arrangement and the
// tools should follow it rather than guess the default and fail obscurely.
func board() (string, error) {
	loc, err := readLocation()
	if err != nil || strings.TrimSpace(loc.Board) == "" {
		return "", fmt.Errorf("no daemon is running, or none has recorded an address. " +
			"call atrium_status")
	}
	return strings.TrimRight(loc.Board, "/"), nil
}

// ask does one request against the board and decodes the answer.
//
// The BODY of a failure is returned, not just the status. Every refusal in
// this API is a sentence written to be read by whoever caused it, and
// flattening those into "400 Bad Request" throws away the only useful part.
func ask(ctx context.Context, method, path string, body any, out any) error {
	return askFor(ctx, peerToolTimeout, method, path, body, out)
}

// askFor is ask with its own bound, for a call that waits on more than this room.
func askFor(ctx context.Context, wait time.Duration, method, path string, body any, out any) error {
	base, err := board()
	if err != nil {
		return err
	}
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
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := (&http.Client{Timeout: wait}).Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the board at %s: %w", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if strings.TrimSpace(e.Error) != "" {
			return &peerToolErr{code: res.StatusCode, msg: e.Error}
		}
		return &peerToolErr{code: res.StatusCode, msg: "the board answered " + res.Status, bare: true}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// peerToolErr is a refusal from the board with its status kept. `bare` is an
// answer with no sentence, which is how a room older than an endpoint says it
// does not have one.
type peerToolErr struct {
	code int
	msg  string
	bare bool
}

func (e *peerToolErr) Error() string { return e.msg }

// olderRoom is whether an error is the room not having the endpoint at all.
func olderRoom(err error) bool {
	var pe *peerToolErr
	return errors.As(err, &pe) && pe.code == http.StatusNotFound && pe.bare
}

// card is the part of a task these tools report. The board's own shape is much
// larger and most of it is for drawing.
type card struct {
	ID       string `json:"id"`
	Title    string `json:"display_title"`
	Wire     string `json:"wire_name"`
	Alias    string `json:"alias"`
	Created  string `json:"created_at"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
	Runner   string `json:"runner"`
	Why      string `json:"why"`
	Idle     int    `json:"idle_seconds"`
	Wait     int    `json:"wait_seconds"`
	Superv   bool   `json:"supervised"`
	// What the room says the launch applied. Empty from a room older than
	// launch options, which is how launchHandler notices.
	Model         string   `json:"model"`
	Effort        string   `json:"effort"`
	LaunchArgs    []string `json:"launch_args"`
	LaunchEnvKeys []string `json:"launch_env_keys"`
	Tags          []string `json:"tags"`
	Activity      struct {
		What string `json:"what"`
	} `json:"activity"`
}

// ── peers ───────────────────────────────────────────────────────────────────

type PeersInput struct {
	// All includes cards with no session on them. Off by default: the question
	// this tool answers is who can be spoken to.
	All bool `json:"all,omitempty" jsonschema:"include cards that have no running session"`
	// Rooms adds the sessions on other rooms, through this room's hub.
	Rooms bool `json:"rooms,omitempty" jsonschema:"also list sessions on other rooms. their handles are name@room, which atrium_say takes"`
}

type Peer struct {
	Handle string `json:"handle"`
	// Room is set on a peer from another room, whose handle is `name@room`.
	Room string `json:"room,omitempty"`
	// Alias is the short name the operator gave it, accepted in place of the
	// handle.
	Alias  string `json:"alias,omitempty"`
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
	// Everywhere marks a card that is listed because it carries atrium:everywhere.
	Everywhere bool `json:"everywhere,omitempty"`
}

type PeersOutput struct {
	// Me is this session's own handle, when it has one. It is what a peer has
	// to be told in order to answer, and an agent has no other way to learn it.
	Me    string `json:"me,omitempty"`
	Peers []Peer `json:"peers"`
	Note  string `json:"note,omitempty"`
}

func peersHandler(ctx context.Context, _ *mcp.CallToolRequest, in PeersInput) (
	*mcp.CallToolResult, PeersOutput, error) {

	out := PeersOutput{Peers: []Peer{}}
	// The environment names this session, which is how a runner learns its own
	// handle. Absent when the caller is not one of atrium's runners, which is a
	// perfectly ordinary way to use these tools and not an error.
	out.Me = strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))

	var body struct {
		Tasks []card `json:"tasks"`
	}
	if err := ask(ctx, http.MethodGet, "/v1/tasks", nil, &body); err != nil {
		return nil, out, err
	}
	for _, t := range body.Tasks {
		if t.Wire == out.Me && out.Me != "" {
			continue
		}
		// A card with no session is a place, not somebody to talk to. Kept out
		// unless asked for, because the list is long and the useful part of it
		// is short.
		live := t.Status != "done" && t.Status != "dead" && t.Status != "shelved"
		if !in.All && !live {
			continue
		}
		out.Peers = append(out.Peers, Peer{
			Handle: t.Wire, Alias: t.Alias, Card: t.ID, Title: t.Title, Status: t.Status,
			Doing: t.Activity.What, Where: t.Worktree,
			Waiting: t.Wait, Owned: t.Superv,
		})
	}
	if in.Rooms {
		var more struct {
			Peers []Peer `json:"peers"`
			Note  string `json:"note"`
		}
		switch err := ask(ctx, http.MethodGet, peersRoomsPath(in.All), nil, &more); {
		case olderRoom(err):
			out.Note = "this room is older than cross-room say, so it cannot list other rooms."
		case err != nil:
			out.Note = "other rooms could not be listed: " + err.Error()
		default:
			out.Peers = append(out.Peers, more.Peers...)
			if more.Note != "" {
				out.Note = more.Note
			}
		}
	}
	if !in.Rooms {
		// THE CARDS TAGGED atrium:everywhere on other rooms, after this room's own.
		// A room or a hub that predates them leaves the list as it is, and a hub
		// that is not answering says so.
		var more struct {
			Peers []Peer `json:"peers"`
		}
		switch err := ask(ctx, http.MethodGet, "/v1/peers/rooms?everywhere=1", nil, &more); {
		case olderRoom(err):
		case err != nil:
			out.Note = "cards on other rooms were not listed: " + err.Error()
		default:
			out.Peers = append(out.Peers, more.Peers...)
		}
	}
	if out.Me == "" {
		out.Note = "this session is not on the board, so it has no handle. a peer cannot " +
			"answer you: ask it to leave its reply somewhere you can read instead."
	}
	return nil, out, nil
}

// ── say ─────────────────────────────────────────────────────────────────────

type SayInput struct {
	// To is a handle from atrium_peers, or a card id. Both are accepted
	// because both are things the caller has in hand, and refusing the one it
	// happens to be holding is a puzzle rather than a rule.
	To string `json:"to" jsonschema:"the handle, alias or card id to say it to"`
	// Text is what to say, as one agent to another.
	Text string `json:"text" jsonschema:"what to say, as one agent to another. the recipient is told who you are automatically, so do not announce yourself"`
	// When is `immediate` (the default) or `done`. See internal/daemon/saywhen.go.
	When string `json:"when,omitempty" jsonschema:"immediate (the default): typed as soon as the line is empty, even mid-turn. done: wait for that session's turn to end"`
	// Reply marks the say as needing an answer, so it shows as owed on the receiving card.
	Reply bool `json:"reply,omitempty" jsonschema:"true when you need an answer, not just a delivery. it shows as owed on that session until it says something back"`
	// Wake resumes a parked card so this reaches it. Without it a say to a parked card is refused.
	Wake bool `json:"wake,omitempty" jsonschema:"true to resume a PARKED session (idle, no process) and deliver this. it costs a cold start, so leave it off unless the message is worth it. without it a say to a parked session is refused and nothing is queued. a card on another room, named as name@room or reached by a bare name on every room, is resumed there too. a room that is not attached refuses it, and nothing is held"`
	// Kind is `fyi` or `needs`. See internal/daemon/fyi.go.
	Kind string `json:"kind,omitempty" jsonschema:"needs (the default): wants an answer or an action. fyi: news the receiver need not act on, which a receiver that holds its notices keeps on its card instead of being interrupted"`
}

type SayOutput struct {
	// Say is this say's id, for looking it up afterwards through atrium_task with says.
	Say string `json:"say,omitempty"`
	// Via is how the name resolved: handle, alias or card.
	Via string `json:"via,omitempty"`
	// Delivered is `terminal` or `queued`. Two different promises, and the
	// caller has to know which one was made: typed has already landed, queued
	// has not and will not until that session next reaches a hook.
	Delivered string `json:"delivered"`
	To        string `json:"to"`
	Card      string `json:"card"`
	When      string `json:"when,omitempty"`
	Note      string `json:"note,omitempty"`
}

func sayHandler(ctx context.Context, _ *mcp.CallToolRequest, in SayInput) (
	*mcp.CallToolResult, SayOutput, error) {

	out := SayOutput{}
	if strings.TrimSpace(in.Text) == "" {
		return nil, out, fmt.Errorf("nothing to say")
	}
	// WHO IS SAYING IT, from the environment the room launched this session
	// with. It used to send nobody, which the room reads as the operator, so a
	// peer's words were typed as though the human had typed them.
	me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
	body := map[string]any{"text": in.Text, "from": me, "to": in.To}
	if w := strings.TrimSpace(in.When); w != "" {
		body["when"] = w
	}
	if in.Reply {
		body["reply"] = true
	}
	if in.Wake {
		body["wake"] = true
	}
	if k := strings.TrimSpace(in.Kind); k != "" {
		body["kind"] = k
	}

	// BY ADDRESS, so `name@room` reaches another room through this room's link
	// to its hub. See internal/daemon/relay.go.
	var res struct {
		Delivered string `json:"delivered"`
		To        string `json:"to"`
		Card      string `json:"card"`
		When      string `json:"when"`
		Warning   string `json:"warning"`
		Note      string `json:"note"`
		Say       string `json:"say"`
		Via       string `json:"via"`
	}
	err := ask(ctx, http.MethodPost, "/v1/say", body, &res)
	if olderRoom(err) {
		// A ROOM OLDER THAN THIS BINARY. A bare name takes the old way, now
		// with the sender. Another room cannot be reached from it at all.
		name, room, perr := daemon.SplitAddress(in.To)
		if perr != nil {
			return nil, out, perr
		}
		if room != "" && !strings.EqualFold(room, strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))) {
			return nil, out, fmt.Errorf("this room is older than cross-room say, so it cannot reach %s", in.To)
		}
		return sayByCard(ctx, name, me, in.Text, in.When)
	}
	if err != nil {
		return nil, out, err
	}
	out.Delivered, out.To, out.Card, out.When = res.Delivered, res.To, res.Card, res.When
	out.Say, out.Via = res.Say, res.Via
	if res.Note != "" {
		out.Note = res.Note
	}
	if res.Warning != "" {
		out.Note = res.Warning
	}
	if out.Note != "" {
		return nil, out, nil
	}
	return nil, sayNote(out), nil
}

// sayByCard is the old way: the card's own message endpoint.
func sayByCard(ctx context.Context, to, from, text, when string) (*mcp.CallToolResult, SayOutput, error) {
	out := SayOutput{}
	id, handle, err := resolvePeer(ctx, to)
	if err != nil {
		return nil, out, err
	}
	out.To, out.Card = handle, id
	var res struct {
		Delivered string `json:"delivered"`
		When      string `json:"when"`
	}
	body := map[string]string{"text": text, "from": from}
	if w := strings.TrimSpace(when); w != "" {
		body["when"] = w
	}
	if err := ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message",
		body, &res); err != nil {
		return nil, out, err
	}
	out.Delivered, out.When = res.Delivered, res.When
	return nil, sayNote(out), nil
}

// sayNote is what a sender reads next, by what was promised.
func sayNote(out SayOutput) SayOutput {
	res := out
	switch {
	case res.Delivered == "held":
		out.Note = "the hub or that room is not answering. held on this room and sent when it answers, " +
			"for up to 24 hours."
	case res.Delivered == "unconfirmed":
		out.Note = "it may or may not have arrived. ask before sending it again."
	case res.Delivered == "queued" && res.When == "done":
		out.Note = "queued until that session's turn ends. it is typed in then, or carried by " +
			"its Stop hook."
	case res.Delivered == "queued":
		out.Note = "queued, not typed yet. it is typed in as soon as that session's line clears, " +
			"or arrives at its next tool call or the end of its turn."
	}
	return out
}

// resolvePeer turns a handle or a card id into both.
//
// Handles are matched first and exactly. A card id is a ULID and a handle is a
// name somebody chose, so the two cannot collide, and trying the name first
// means a caller that pasted a handle never gets an obscure 404 from an
// endpoint that wanted an id.
func resolvePeer(ctx context.Context, who string) (id, handle string, err error) {
	who = strings.TrimSpace(who)
	if who == "" {
		return "", "", fmt.Errorf("say who to")
	}
	var body struct {
		Tasks []card `json:"tasks"`
	}
	if err := ask(ctx, http.MethodGet, "/v1/tasks", nil, &body); err != nil {
		return "", "", err
	}
	for _, t := range body.Tasks {
		if t.Wire == who || t.ID == who {
			return t.ID, t.Wire, nil
		}
	}
	// Then an alias, a live card before a done one and the newest first, the way
	// the room resolves one. A dead card no longer answers. See
	// internal/store/alias.go.
	if a := strings.ToLower(strings.TrimPrefix(who, "@")); a != "" {
		var best *card
		for i := range body.Tasks {
			t := &body.Tasks[i]
			if t.Alias != a || t.Status == "dead" {
				continue
			}
			// Live before done, then newest: the room's own order.
			if best == nil || aliasBeats(t.Status, t.Created, best.Status, best.Created) {
				best = t
			}
		}
		if best != nil {
			return best.ID, best.Wire, nil
		}
	}
	// The list of ones that would have worked, which is the whole of the fix
	// for a wrong handle and is otherwise another tool call away.
	names := make([]string, 0, len(body.Tasks))
	for _, t := range body.Tasks {
		if t.Status == "done" || t.Status == "dead" {
			continue
		}
		if t.Alias != "" {
			names = append(names, t.Wire+" (@"+t.Alias+")")
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

// ── launch ──────────────────────────────────────────────────────────────────

type LaunchInput struct {
	Cwd   string `json:"cwd" jsonschema:"the directory to run in. it has to exist already"`
	Title string `json:"title,omitempty" jsonschema:"what to call the card"`
	Why   string `json:"why,omitempty" jsonschema:"what this is for, read back later"`
	// Prompt is the first thing the new session is told.
	Prompt string `json:"prompt,omitempty" jsonschema:"the first instruction it gets"`
	// Brief is everything the new session needs to know before that.
	//
	// WRITTEN TO A FILE, not passed on the command line, and that is the whole
	// difference between this and a long prompt. A prompt is said once: it is
	// at the top of a conversation, it is the first thing to fall out of a
	// compaction, and it cannot be consulted afterwards. A file in the working
	// directory can be re-read at any point, survives compaction, is there when
	// somebody takes the session over, and is still there tomorrow when the
	// card is picked back up.
	//
	// It goes into `BRIEF.md` and the runner is told to read it. Not
	// `CLAUDE.md`, deliberately: that file is loaded into every session in that
	// directory forever, including ones nobody meant to brief, and writing one
	// on somebody's behalf is a decision with no expiry.
	Brief string `json:"brief,omitempty" jsonschema:"context to hand the new session. written to BRIEF.md in its directory and read before it starts, so it survives compaction and can be re-read"`
	// Runner names a configured harness. Empty means claude.
	Runner string   `json:"runner,omitempty" jsonschema:"which configured runner to start. default claude"`
	Tags   []string `json:"tags,omitempty" jsonschema:"free text labels, used for grouping and filtering"`
	// The launch options, as the hub's atrium_launch takes them. See
	// docs/runtime/launch-options-design.md.
	Model  string            `json:"model,omitempty" jsonschema:"which model the runner starts on, in the shape its runner row declares. not checked against any list. empty is the runner's default"`
	Effort string            `json:"effort,omitempty" jsonschema:"thinking effort, in the shape its runner row declares. not checked. empty is the runner's default"`
	Args   []string          `json:"args,omitempty" jsonschema:"extra command-line arguments for the runner, used as given. shown on the card, so keep secrets out"`
	Env    map[string]string `json:"env,omitempty" jsonschema:"extra environment for the runner, used as given. the card shows the names only"`
	// LeanAgents, as the hub's atrium_launch takes it. The stdio server has no
	// `lean` field, so this is the only way to a lean launch from here.
	LeanAgents []string `json:"lean_agents,omitempty" jsonschema:"agents a lean claude worker can start, by name: the files in the operator's ~/.claude/agents on the room, without .md. it keeps the Agent tool and ONLY these, namespaced as atrium:<name>. a name with no file refuses the launch. implies lean. claude only. the card keeps the list"`
	LeanSkills []string `json:"lean_skills,omitempty" jsonschema:"skills a lean claude worker can use, by name: the directories in the operator's ~/.claude/skills on the room. it keeps the Skill tool and ONLY these. a name with no directory refuses the launch. implies lean. claude only. the card keeps the list"`
	// Room launches on another room, as the hub's atrium_launch does.
	Room string `json:"room,omitempty" jsonschema:"launch on this room instead of your own. cwd is then a path on that room's machine and brief is written there. its reports and notices still reach you, as your-handle@your-room"`
}

type LaunchOutput struct {
	Card   string `json:"card"`
	Handle string `json:"handle"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Watch  string `json:"watch,omitempty"`
	// Brief is where the briefing was written, when there was one. Returned so
	// the caller can add to it later, which is the ordinary case: a peer that
	// turns out to need one more fact should be given it in the file it
	// already reads rather than only in a message it will forget.
	Brief string `json:"brief,omitempty"`
	Note  string `json:"note,omitempty"`
	// What the room ran it with, so a caller sees that the model it asked for took.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// briefFile is what a briefing is called in the new session's directory.
//
// One fixed name so that a second launch into the same directory replaces the
// briefing rather than littering it with dated copies nobody reads. A stale
// brief is worse than a missing one: the session believes it.
const briefFile = "BRIEF.md"

// writeBrief puts the briefing where the new session will find it.
//
// Overwrites. See `briefFile`. The path is returned rather than assumed by the
// caller, because the directory is the caller's and this is the only thing
// that knows what was written into it.
func writeBrief(cwd, brief string) (string, error) {
	dir := strings.TrimSpace(cwd)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("%s is not there. atrium does not create the directory: "+
			"make it first, then launch into it", dir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is a file, not a directory", dir)
	}
	path := filepath.Join(dir, briefFile)
	body := brief
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("could not write the briefing to %s: %w", path, err)
	}
	return filepath.ToSlash(path), nil
}

// briefPrompt puts the instruction to read the briefing ahead of the task.
//
// AHEAD, because the order is what makes it work: a session that reads the
// task first starts answering it, and the briefing arrives as correction. The
// task still has to be in the prompt rather than only in the file, or the
// session reads a briefing and sits there waiting to be told what to do with
// it.
func briefPrompt(prompt string) string {
	read := "Read " + briefFile + " in this directory first. It is your briefing, written " +
		"for you by another agent, and it holds everything you are expected to know. " +
		"Re-read it whenever you lose the thread rather than guessing."
	if prompt == "" {
		return read + " Then do what it asks."
	}
	return read + "\n\nThen: " + prompt
}

func launchHandler(ctx context.Context, _ *mcp.CallToolRequest, in LaunchInput) (
	*mcp.CallToolResult, LaunchOutput, error) {

	out := LaunchOutput{}
	if strings.TrimSpace(in.Cwd) == "" {
		return nil, out, fmt.Errorf("say where to run it. atrium does not create the directory")
	}
	harness := strings.TrimSpace(in.Runner)
	if harness == "" {
		harness = "claude"
	}
	// ANOTHER ROOM goes through this room's hub and touches nothing on this disk: no
	// briefing written here and no directory looked at. This room's own name is the
	// path below, untouched.
	if r := strings.TrimSpace(in.Room); r != "" {
		if done, o, err := launchAcrossRoom(ctx, in, harness, r); done {
			return nil, o, err
		}
	}

	// The briefing lands BEFORE the runner starts, or it is not a briefing.
	// A session told to read a file that is not there yet reads nothing and
	// reports that it read nothing, which is worse than no briefing at all
	// because it looks like the file was empty.
	prompt := strings.TrimSpace(in.Prompt)
	if brief := strings.TrimSpace(in.Brief); brief != "" {
		path, err := writeBrief(in.Cwd, brief)
		if err != nil {
			return nil, out, err
		}
		prompt = briefPrompt(prompt)
		out.Brief = path
	}
	req := map[string]any{
		"harness": harness, "cwd": in.Cwd, "title": in.Title,
		"why": in.Why, "prompt": prompt, "tags": in.Tags,
		"model": in.Model, "effort": in.Effort, "args": in.Args, "env": in.Env,
		"lean_agents": in.LeanAgents, "lean_skills": in.LeanSkills,
	}
	var t card
	if err := ask(ctx, http.MethodPost, "/v1/launch", req, &t); err != nil {
		return nil, out, err
	}
	out.Card, out.Handle, out.Title, out.Status = t.ID, t.Wire, t.Title, t.Status
	if base, err := board(); err == nil {
		// Where the human looks. Worth returning rather than leaving them to
		// assemble it, because the fragment form is not guessable.
		out.Watch = base + "/#term=" + url.PathEscape(t.ID)
	}
	out.Note = "started. its permission requests go to the human on their board, so it will " +
		"stop at the first gated command unless somebody is watching."
	out.Model, out.Effort = t.Model, t.Effort
	// A room older than launch options drops the fields without a word.
	missed := link.LaunchOptionsDropped(in.Model, in.Effort, in.Args, in.Env,
		t.Model, t.Effort, t.LaunchArgs, t.LaunchEnvKeys)
	missed = append(missed, link.LeanAgentsDropped(in.LeanAgents, in.LeanSkills, t.Tags)...)
	out.Note = link.LaunchDroppedWarning(missed) + out.Note
	return nil, out, nil
}

// ── one card ────────────────────────────────────────────────────────────────

type TaskInput struct {
	Card string `json:"card" jsonschema:"a card id, handle or alias. name@room or room~id for a card on another room"`
	// Events includes the recent history, which is what a card DID rather than
	// where it is now.
	Events bool `json:"events,omitempty" jsonschema:"include recent events"`
	// Says includes the says this card sent and received, and what became of each.
	Says bool `json:"says,omitempty" jsonschema:"include the says this card sent and received, with their state, channel and whether a reply is owed"`
}

// TaskSay is one say on a card, from its own side.
type TaskSay struct {
	ID          string `json:"id"`
	Direction   string `json:"direction"`
	Other       string `json:"other"`
	Via         string `json:"via,omitempty"`
	State       string `json:"state"`
	Channel     string `json:"channel,omitempty"`
	SentAt      string `json:"sent_at"`
	DeliveredAt string `json:"delivered_at,omitempty"`
	Preview     string `json:"preview,omitempty"`
	ReplyWanted bool   `json:"reply_wanted,omitempty"`
	RepliedAt   string `json:"replied_at,omitempty"`
	ResetKind   string `json:"reset_kind,omitempty"`
}

type TaskEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

type TaskOutput struct {
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
	Events  []TaskEvent `json:"events,omitempty"`
	Says    []TaskSay   `json:"says,omitempty"`
	Note    string      `json:"note,omitempty"`
}

func taskHandler(ctx context.Context, _ *mcp.CallToolRequest, in TaskInput) (
	*mcp.CallToolResult, TaskOutput, error) {

	out := TaskOutput{}
	who := strings.TrimSpace(in.Card)
	if isAcross(who) {
		// ANOTHER ROOM, by way of this room's hub. See item 68 in
		// docs/backlog-2.md.
		path := "/v1/peers/card?to=" + url.QueryEscape(who)
		if in.Events {
			path += "&events=1"
		}
		var res struct {
			Local string      `json:"local"`
			Task  *TaskOutput `json:"task"`
		}
		err := ask(ctx, http.MethodGet, path, nil, &res)
		if who, err = localAfterAll(who, res.Local, err); err != nil {
			return nil, out, err
		}
		if who == "" && res.Task != nil {
			out = *res.Task
			out.Note = taskNote
			return nil, out, nil
		}
	}
	id, _, err := resolvePeer(ctx, who)
	if err != nil {
		return nil, out, err
	}
	var t card
	if err := ask(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, &t); err != nil {
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
		if err := ask(ctx, http.MethodGet,
			"/v1/tasks/"+url.PathEscape(id)+"/events", nil, &body); err == nil {
			// The tail, because the useful end of a history is the recent one
			// and a card that has been up for days has hundreds.
			from := 0
			if len(body.Events) > 20 {
				from = len(body.Events) - 20
			}
			for _, e := range body.Events[from:] {
				out.Events = append(out.Events, TaskEvent{At: e.At, Kind: e.Kind})
			}
		}
	}
	if in.Says {
		var body struct {
			Says []struct {
				ID          string `json:"id"`
				FromTask    string `json:"from_task"`
				FromWire    string `json:"from"`
				ToWire      string `json:"to"`
				ToInput     string `json:"to_input"`
				Via         string `json:"via"`
				State       string `json:"state"`
				Channel     string `json:"channel"`
				SentAt      string `json:"sent_at"`
				DeliveredAt string `json:"delivered_at"`
				Preview     string `json:"preview"`
				ReplyWant   bool   `json:"reply_wanted"`
				RepliedAt   string `json:"replied_at"`
				ResetKind   string `json:"reset_kind"`
			} `json:"says"`
		}
		if err := ask(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id)+"/says", nil, &body); err == nil {
			for _, s := range body.Says {
				ts := TaskSay{ID: s.ID, Via: s.Via, State: s.State, Channel: s.Channel, SentAt: s.SentAt,
					DeliveredAt: s.DeliveredAt, Preview: s.Preview, ReplyWanted: s.ReplyWant,
					RepliedAt: s.RepliedAt, ResetKind: s.ResetKind}
				if s.FromTask == id {
					ts.Direction, ts.Other = "sent", s.ToWire
					if ts.Other == "" {
						ts.Other = s.ToInput
					}
				} else {
					ts.Direction, ts.Other = "received", s.FromWire
				}
				out.Says = append(out.Says, ts)
			}
		}
	}
	out.Note = taskNote
	return nil, out, nil
}

const taskNote = "status and events only. atrium does not record what a session printed, so this " +
	"cannot tell you what it said or thinks."

// isAcross is whether an address names a room at all, `name@room` or
// `room~id`. The room decides whether that room is itself.
func isAcross(who string) bool {
	_, room, err := daemon.SplitAddress(who)
	return err == nil && room != ""
}

// localAfterAll reads the room's answer to an address that named a room. It
// answers the name to find on this room when the address named this room
// after all, and empty when the room reached the card elsewhere.
//
// A room older than this has no such endpoint. Its own room name is still
// this room, and any other it cannot reach.
func localAfterAll(who, local string, err error) (string, error) {
	if olderRoom(err) {
		name, room, perr := daemon.SplitAddress(who)
		if perr != nil {
			return "", perr
		}
		if strings.EqualFold(room, strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))) {
			return name, nil
		}
		return "", fmt.Errorf("this room is older than reaching a card on another room, so it cannot reach %s", who)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(local), nil
}

// ── exit ────────────────────────────────────────────────────────────────────

type ExitInput struct {
	Card string `json:"card" jsonschema:"a card id, handle or alias. name@room or room~id for a card on another room"`
}

type ExitOutput struct {
	Card   string `json:"card"`
	Handle string `json:"handle,omitempty"`
	Asked  bool   `json:"asked"`
	Note   string `json:"note,omitempty"`
}

func exitHandler(ctx context.Context, _ *mcp.CallToolRequest, in ExitInput) (
	*mcp.CallToolResult, ExitOutput, error) {

	out := ExitOutput{}
	who := strings.TrimSpace(in.Card)
	if isAcross(who) {
		// ANOTHER ROOM, by way of this room's hub. See item 68 in
		// docs/backlog-2.md.
		var res struct {
			Local  string `json:"local"`
			Card   string `json:"card"`
			Handle string `json:"handle"`
			Asked  bool   `json:"asked"`
		}
		err := ask(ctx, http.MethodPost, "/v1/peers/exit", map[string]string{"to": who}, &res)
		if who, err = localAfterAll(who, res.Local, err); err != nil {
			return nil, out, err
		}
		if who == "" {
			out.Card, out.Handle, out.Asked = res.Card, res.Handle, res.Asked
			out.Note = exitNote
			return nil, out, nil
		}
	}
	id, handle, err := resolvePeer(ctx, who)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = id, handle
	if err := ask(ctx, http.MethodPost,
		"/v1/tasks/"+url.PathEscape(id)+"/exit", nil, nil); err != nil {
		return nil, out, err
	}
	out.Asked = true
	out.Note = exitNote
	return nil, out, nil
}

// aliasBeats says whether a card of status `s` created at `c` is the better
// answer for an alias than the one already chosen (`bs`, `bc`): a live card
// before a done one, then the newest.
func aliasBeats(s, c, bs, bc string) bool {
	if (s == "done") != (bs == "done") {
		return s != "done"
	}
	return c > bc
}

const exitNote = "asked to leave with its harness's exit keys. the card and its history stay."
