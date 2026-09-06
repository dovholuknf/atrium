package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
			"takes, and a handle read off a card title rather than from here is usually wrong.",
	}, peersHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_say",
		Description: "Say something to another session on the board.\n\n" +
			"DELIVERY IS NOT TYPING. Where atrium owns the terminal the text is typed in and " +
			"the answer says `terminal`. Where it does not, the message is QUEUED and reaches " +
			"that session at its next tool call or at the end of its turn, and the answer says " +
			"`queued`. Neither is instant and neither is a reply: if you want one, ask for it " +
			"and then look, or wait to be told.\n\n" +
			"What arrives is framed as a person speaking, not as a refusal, so write it as one " +
			"agent talking to another. Say who you are: the receiving session is not told.\n\n" +
			"Ask for a reply explicitly, and say how. The other session answers by calling " +
			"`atrium_say` back at your own handle, which is in `atrium_peers` under `me`.",
	}, sayHandler)

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
			"Returns the card id. Use it with `atrium_task` and `atrium_say`.",
	}, launchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_task",
		Description: "One card: its status, what its runner is doing right now, and its recent " +
			"events.\n\n" +
			"This does NOT return what the session printed. Atrium records that a session ran " +
			"and every status it moved through, never its output, so `needs-input` here means " +
			"it stopped and not what it said. To learn what it thinks, ask it, and have it " +
			"answer with `atrium_say`.",
	}, taskHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name: "atrium_exit",
		Description: "Ask a session to finish and leave.\n\n" +
			"ASKED, not killed. Atrium sends the exit keys its harness is configured with, so " +
			"the runner shuts itself down and writes whatever it writes on the way out. Its " +
			"card and its whole history stay on the board.\n\n" +
			"Say something first if the work is not finished. A session asked to leave mid-task " +
			"leaves mid-task.",
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

	res, err := (&http.Client{Timeout: peerToolTimeout}).Do(req)
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
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("the board answered %s", res.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// card is the part of a task these tools report. The board's own shape is much
// larger and most of it is for drawing.
type card struct {
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

// ── peers ───────────────────────────────────────────────────────────────────

type PeersInput struct {
	// All includes cards with no session on them. Off by default: the question
	// this tool answers is who can be spoken to.
	All bool `json:"all,omitempty" jsonschema:"include cards that have no running session"`
}

type Peer struct {
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

// ── say ─────────────────────────────────────────────────────────────────────

type SayInput struct {
	// To is a handle from atrium_peers, or a card id. Both are accepted
	// because both are things the caller has in hand, and refusing the one it
	// happens to be holding is a puzzle rather than a rule.
	To string `json:"to" jsonschema:"the handle or card id to say it to"`
	// Text is what to say, as one agent to another.
	Text string `json:"text" jsonschema:"what to say. say who you are: the other session is not told"`
}

type SayOutput struct {
	// Delivered is `terminal` or `queued`. Two different promises, and the
	// caller has to know which one was made: typed has already landed, queued
	// has not and will not until that session next reaches a hook.
	Delivered string `json:"delivered"`
	To        string `json:"to"`
	Card      string `json:"card"`
	Note      string `json:"note,omitempty"`
}

func sayHandler(ctx context.Context, _ *mcp.CallToolRequest, in SayInput) (
	*mcp.CallToolResult, SayOutput, error) {

	out := SayOutput{}
	if strings.TrimSpace(in.Text) == "" {
		return nil, out, fmt.Errorf("nothing to say")
	}
	id, handle, err := resolvePeer(ctx, in.To)
	if err != nil {
		return nil, out, err
	}
	out.To, out.Card = handle, id

	var res struct {
		Delivered string `json:"delivered"`
	}
	if err := ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message",
		map[string]string{"text": in.Text}, &res); err != nil {
		return nil, out, err
	}
	out.Delivered = res.Delivered
	if res.Delivered == "queued" {
		out.Note = "queued, not typed. it arrives at that session's next tool call or at the " +
			"end of its turn, which may be a while if it is idle."
	}
	return nil, out, nil
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
	// The list of ones that would have worked, which is the whole of the fix
	// for a wrong handle and is otherwise another tool call away.
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
	return nil, out, nil
}

// ── one card ────────────────────────────────────────────────────────────────

type TaskInput struct {
	Card string `json:"card" jsonschema:"a card id or a handle"`
	// Events includes the recent history, which is what a card DID rather than
	// where it is now.
	Events bool `json:"events,omitempty" jsonschema:"include recent events"`
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
	Note    string      `json:"note,omitempty"`
}

func taskHandler(ctx context.Context, _ *mcp.CallToolRequest, in TaskInput) (
	*mcp.CallToolResult, TaskOutput, error) {

	out := TaskOutput{}
	id, _, err := resolvePeer(ctx, in.Card)
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
	out.Note = "status and events only. atrium does not record what a session printed, so this " +
		"cannot tell you what it said or thinks."
	return nil, out, nil
}

// ── exit ────────────────────────────────────────────────────────────────────

type ExitInput struct {
	Card string `json:"card" jsonschema:"a card id or a handle"`
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
	id, handle, err := resolvePeer(ctx, in.Card)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = id, handle
	if err := ask(ctx, http.MethodPost,
		"/v1/tasks/"+url.PathEscape(id)+"/exit", nil, nil); err != nil {
		return nil, out, err
	}
	out.Asked = true
	out.Note = "asked to leave with its harness's exit keys. the card and its history stay."
	return nil, out, nil
}
