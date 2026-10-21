package link

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
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
	// audit records an operational event, e.g. a launch refused by the cap. Nil
	// on a hub that keeps no log, and best effort: it never blocks a launch.
	audit func(room, kind, detail string)

	// mu guards reservations. The control server is one instance shared by every
	// request (see newControlHandler), so the launch cap's book-keeping lives here
	// and is serialised across concurrent launches.
	mu sync.Mutex
	// reservations are launches counted against the cap that have no running card
	// yet: taken the instant a launch is admitted and self-expiring after
	// reservationTTL, by which point the new session shows up as a live card and
	// the reservation is no longer needed. See reserveSlot.
	reservations []reservation
	// resSeq numbers reservations so each has a distinct id.
	resSeq int

	// capFor is the launch cap for one room. Nil means launchCap() for every
	// room, which is what a test that builds this directly gets. See
	// launchcaps.go.
	capFor func(room string) int

	// gate answers whether a launch with this title is for an item with an open gate,
	// and the sentence refusing it. Nil checks nothing. See deps.go.
	gate func(ctx context.Context, title string) (string, bool)

	// settings is the hub's own settings, where deploy requests and the deploy
	// owner are kept. Nil on a hub with no store. deployMu serialises a room's
	// requests, apart from mu so a launch never waits on a deploy call. See
	// deploy_mcp.go.
	settings func() HubSettings
	// docs is the hub's document store, read at each call so SetDocs may come later. Nil on a
	// hub without one. See docs_mcp.go.
	docs func() *hubstore.Store
	// resourcesDir and rooms feed atrium_resources. Nil in a test that does not read it. See resources_mcp.go.
	resourcesDir func() string
	rooms        func() []resourceRoom
	deployMu     sync.Mutex

	// classMu guards classes, the per-caller class cache. See ctlclass.go.
	classMu sync.Mutex
	classes map[string]classEntry
	// now is the clock the class cache reads. Nil means time.Now, and a test sets it.
	now func() time.Time
}

// reservation is one in-flight launch holding a slot against its room's cap
// until its card appears in the live count or it expires.
type reservation struct {
	id   string
	room string
	at   time.Time
}

// newControlHandler builds the hub-side control MCP server as an http.Handler,
// ready to mount at /_hub/mcp.
//
// ONE SERVER FOR EVERY REQUEST, and that is correct rather than a shortcut: the
// tools read who is calling from the per-request header, never from the server,
// so there is nothing per session to build. `getServer` returns the same one.
func newControlHandler(board string, hub *Hub, audit func(room, kind, detail string)) http.Handler {
	return newControl(board, hub, audit).handler()
}

// newControl builds the tools' state, which the relay shares. See control_relay.go.
func newControl(board string, hub *Hub, audit func(room, kind, detail string)) *controlMCP {
	return &controlMCP{board: board, client: &http.Client{Timeout: controlTimeout}, hub: hub, audit: audit}
}

// handler is the MCP server over HTTP.
//
// FOUR SERVERS, BUILT ONCE, and `getServer` picks one per request by the caller's
// class. See ctlclass.go, which says why this is tidiness and not a boundary.
func (c *controlMCP) handler() http.Handler {
	servers := map[ctlClass]*mcp.Server{}
	for _, class := range []ctlClass{classFull, classWorker, classFullResident, classWorkerResident, classWorkerItem} {
		servers[class] = c.server(class)
	}
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return servers[c.classOf(r)] },
		// STATELESS, so no Mcp-Session-Id is validated and a temporary session is
		// used per request. Every session POSTs to the same URL carrying its own
		// identity headers, which is the whole point: one endpoint, many callers,
		// each read on its own.
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// server wires the tools of one class onto one MCP server.
//
// ONE LIST, FILTERED. Every tool is registered here through `addTool`, which
// skips it when the class does not include it, so a tool's description and its
// `audited` wrapping are written once whichever servers it lands on.
func (c *controlMCP) server(class ctlClass) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "atrium-control", Version: "v0.0.0-dev"}, nil)

	addTool(s, class, &mcp.Tool{
		Name: "atrium_status",
		Description: "Is atrium running, is its store healthy, and what is on the board.\n\n" +
			"Answered from the hub, which serves the board, rather than from a local daemon. " +
			"Scopes to your room when your session has one, so the counts are yours and not " +
			"every room's.",
	}, c.statusHandler)

	addTool(s, class, &mcp.Tool{
		Name: "atrium_peers",
		Description: "The other sessions on this board: what each is called, what it is doing, " +
			"where it is working, and how long it has been waiting.\n\n" +
			"Call this before saying anything to anybody. The handle is what `atrium_say` " +
			"takes, and a handle read off a card title rather than from here is usually wrong. " +
			"A peer's `alias`, when it has one, is a short name the operator gave it (`sa89`, " +
			"`dotfiles`), and `atrium_say` takes that too, with or without the `@`.\n\n" +
			"`rooms: true` adds the sessions on other rooms. Their handle is `name@room`, and " +
			"`atrium_say` takes that, as it takes `alias@room`. A bare name always means your own room.",
	}, c.peersHandler)

	addTool(s, class, &mcp.Tool{
		Name: "atrium_say",
		Description: "Say something to another session on the board.\n\n" +
			"IMMEDIATE BY DEFAULT. Where atrium owns the terminal and the session is idle, the text is " +
			"typed in as soon as its input line is empty and no dialog is open, and the answer says " +
			"`terminal`. A session in the MIDDLE OF A TURN is not typed into if its runner queues a " +
			"line typed mid-turn (Claude Code does: it keeps the line in its own queued-message box " +
			"and reads it later). The message is QUEUED and the answer says `queued`: its hook hands " +
			"it to the model at the next tool call or at the end of its turn. A session inside ONE LONG " +
			"TOOL CALL makes no tool call, so it reads nothing until that call returns. Nothing is " +
			"interrupted, so a say is not how to stop a tool. A runner that reads typed input as it " +
			"arrives (codex) is still typed into mid-turn and answers `terminal`. `typed-queued` means " +
			"a line was typed in but sits unread in the runner's queued box. Where atrium does not own " +
			"the terminal, or the line is not clear yet, the answer is `queued` and it arrives when " +
			"the line clears. None of these is a reply: if you want one, ask for it and then look, or " +
			"wait to be told.\n\n" +
			"`when: \"done\"` waits for that session's turn to end instead, for something that " +
			"should not disturb it mid-thought. A runner set not to take input mid-turn always " +
			"waits, and the answer's `when` says which one happened. A message that waited more than " +
			"15 minutes is carried by the next tool call with a line saying how long it waited.\n\n" +
			"`undeliverable` means that session has no way to receive a queued message (its " +
			"runner has no atrium hook and atrium does not own its terminal), and " +
			"`queued-unconfirmed` means it has never shown one. The note says what to do instead.\n\n" +
			"Write one or two sentences. The receiver is told who you are and already knows how to " +
			"answer, so never add \"reply with atrium_say to X\". Ask outright if you want an answer.\n\n" +
			"Answer with an atrium tool if an answer is owed. Write nothing else: the human sees the tool call.\n\n" +
			"ANOTHER ROOM is `name@room` (or `alias@room`). It goes by way of the hub and is " +
			"delivered the same way. The recipient sees you as `you@yourroom` and answers to that. " +
			"`held` means the hub or that room is not answering: it is kept on your room and sent " +
			"when they are, for up to a day. `unconfirmed` means it may or may not have arrived, " +
			"so ask before sending it again.\n\n" +
			"`kind` is `fyi` for news the receiver need not act on, or `needs` (the default) for " +
			"anything that wants an answer or an action. An `fyi` to a session that holds its notices, such as " +
			"the orchestrator, is typed when its turn ends, or at once if it is idle, and the answer says " +
			"`queued` with `when: \"done\"`. `queued` is not `held`: `held` is only for a hub or room that is not answering. " +
			"A launcher's fyi, such as 'stop, nothing else to do', asks for no reply and the worker owes none.\n\n" +
			"IF ANOTHER SESSION LAUNCHED YOU, END WITH `atrium_done` when the work is finished and " +
			"`atrium_blocked` when something stops you. That is the one message your launcher gets, so do " +
			"not say the same thing with this as well. A turn that ends without either is a silent stop: " +
			"nothing is typed into you, and your launcher is told you stopped without reporting.",
	}, audited(c, "ctl-wake-say", describeSay, c.sayHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_report",
		Description: "Report on your work to the session that launched you.\n\n" +
			"A launched session ends with `atrium_done` or `atrium_blocked`, " +
			"and that is what its launch prompt asks for. Use this only when your launcher " +
			"asks for a report by this tool. Either one counts as telling your launcher.\n\n" +
			"status is one of:\n" +
			"- `done`: the work is finished. Give `sha`, the commit it landed as, or `no_commit` " +
			"saying why there is none.\n" +
			"- `blocked`: you cannot go on. Give `ask`: what you need, and from whom.\n" +
			"- `question`: you need an answer to go on. Give `ask`.\n" +
			"- `progress`: you are stopping on purpose while something runs.\n\n" +
			"`summary` is what happened, in your words. It reaches your launcher verbatim. An " +
			"incomplete report is refused with what is missing, so fix it and call again.\n\n" +
			"`kind` is `fyi` for news your launcher need not act on, or `needs` (the default) for " +
			"anything that wants an answer or an action. An `fyi` to a launcher that holds its notices is " +
			"typed when its turn ends, so it is read without being interrupted. Only `progress` with no `ask` can be an `fyi`: `done`, `blocked`, `question` and anything with an `ask` are always `needs`.",
	}, c.reportHandler)

	end := EndDoor{Report: c.endReport}
	doneDesc, blockedDesc := doneToolDesc, blockedToolDesc
	if residentClass(class) {
		doneDesc, blockedDesc = residentDoneToolDesc, residentBlockedToolDesc
	}
	if class == classWorkerItem {
		doneDesc, blockedDesc = itemDoneToolDesc, itemBlockedToolDesc
	}
	addTool(s, class, &mcp.Tool{Name: "atrium_done", Description: doneDesc}, end.Done)
	addTool(s, class, &mcp.Tool{Name: "atrium_blocked", Description: blockedDesc}, end.Blocked)

	addTool(s, class, &mcp.Tool{Name: "atrium_record", Description: recordToolDesc}, c.recordHandler)

	addTool(s, class, &mcp.Tool{
		Name: "atrium_task",
		Description: "One card: its status, what its runner is doing right now, and its recent " +
			"events.\n\n" +
			"This does NOT return what the session printed. Atrium records that a session ran " +
			"and every status it moved through, never its output, so `needs-input` here means " +
			"it stopped and not what it said. To learn what it thinks, ask it, and have it " +
			"answer with `atrium_say`.\n\n" +
			"`seen` says whether the HUMAN has seen the card's last turn (`unseen`, which a report " +
			"to the launcher of an agent-launched card also clears, `seen_via` `launcher`) and which of " +
			"that turn's Open Questions they have not answered yet (`open_questions`, `answered`, and " +
			"`answered_via` is `dismissed` when the operator dismissed the questions without replying). " +
			"Leave `card` empty to ask about your own card. Before telling the human your " +
			"questions are still open, check this: if `unseen` is true they never read them, so " +
			"repeat them in full rather than referring back.\n\n" +
			"`notices: true` with `card` empty is how a card tagged `atrium:orchestrator` or " +
			"`atrium:hold-notices` hears about its workers. Atrium never types their automatic notices " +
			"(a silent stop, a context size, a session that ended without a report) into its terminal, " +
			"and keeps them on its card instead. Its workers' reports and `fyi` says are " +
			"typed into it at the end of its turn, not kept. A say or report is never parked unseen. Reading your own notices marks " +
			"them read, which clears the `held_notices` count on your card's row.\n\n" +
			"A card on ANOTHER ROOM is `name@room`, `alias@room` or `room~id`, as `atrium_say` " +
			"takes it and as `atrium_launch` with `room` hands it back.",
	}, c.taskHandler)

	addTool(s, class, &mcp.Tool{Name: "atrium_alias", Description: aliasToolDesc},
		audited(c, "ctl-alias", describeAlias, c.aliasHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_launch",
		Description: "Start a new agent in a directory, on its own card, supervised by atrium.\n\n" +
			"A REAL SESSION, not a subagent. It has its own conversation, its own permission " +
			"gate and its own card, it outlives the session that started it, and the human can " +
			"watch it, type into it or take it over. It is a colleague rather than a call.\n\n" +
			"LAUNCH WITH `repo` AND `branch`, NOT WITH A WORKTREE YOU MADE. Give `repo` (host/owner/repo, or an absolute path to a clone on the room) and optionally `branch` and `base`, and atrium makes the card's home under the room's card root and adds the worktree there, so the room's work is in one place and its space is taken back when the card is culled. `home: true` alone makes an empty home for scratch work. `cwd` is for a directory that already exists and is not together with `repo`. A room can be set to refuse an agent launch whose `cwd` is outside its card root.\n\n" +
			"IT STARTS EMPTY. Unlike a subagent it inherits nothing of your conversation, " +
			"which is the point as often as it is the cost: hand it what it needs and it is " +
			"not carrying three hours of unrelated debugging. Use `prompt` for that.\n\n" +
			"Its permission requests go to the HUMAN, on their board, so an agent started here " +
			"and left alone stops at the first gated command. Say who asked for it and why, " +
			"because whoever finds the card later will want to know.\n\n" +
			"Do not write how to finish into the brief: atrium appends the ending to every launch.\n\n" +
			"Use `brief` for context it needs before the task. It is written to BRIEF.md in the " +
			"new session's directory ON THE ROOM and read first, so it survives compaction and is " +
			"still there when a human takes the card over. Put in it what you would tell a " +
			"colleague joining: what the job is, what has been tried, what the constraints are, " +
			"and what NOT to do.\n\n" +
			"WHAT IT RUNS ON. `model` and `effort` pick the model and the thinking effort, for " +
			"a cheap agent such as an interviewer: a small model at low effort. Each runner's " +
			"row says how it takes them (claude: `--model`, `--effort`. codex: `--model`, " +
			"`-c model_reasoning_effort=`), and atrium checks neither against any list, so use " +
			"whatever names and levels that runner accepts. A runner with no way to take one " +
			"refuses the launch rather than starting on its default. `args` is extra argv and " +
			"`env` extra environment, passed as given for anything the two fields do not cover. " +
			"Empty means the runner's default. The card keeps all four, so a restart comes back " +
			"the same, and shows them in its details (env by name only).\n\n" +
			"AGENTS AND SKILLS A LEAN WORKER KEEPS. A lean claude worker has no Agent or Skill tool. " +
			"`lean_agents` (names of files in the operator's ~/.claude/agents on the room, without .md) " +
			"keeps the Agent tool and exposes ONLY those, and `lean_skills` (directories in " +
			"~/.claude/skills) keeps the Skill tool and ONLY those, so a review manager can run its " +
			"reviewers without the rest of the operator's setup. They ride in a session-only plugin " +
			"named `atrium`, so they are NAMESPACED: start `atrium:go-security-reviewer`, not " +
			"`go-security-reviewer`. Either implies lean. Claude only, and a name with no file refuses " +
			"the launch. The card keeps the lists.\n\n" +
			"`superlean: true` starts a worker below lean: only Read, Edit, Write, Grep, Glob, Bash and " +
			"ToolSearch, no mercurius unless `mcp` names it, and a one paragraph worker prompt. For a bounded " +
			"change. It implies lean and is refused with `lean: false`. Lean stays the default.\n\n" +
			"`room` launches on ANOTHER ROOM: `cwd` is then a path on that room's machine, `brief` is " +
			"written there, the cap is that room's, and the worker's reports and notices still reach you. " +
			"An unknown room is refused with the rooms the hub knows. A session on a room says `room` " +
			"to its own hub the same way, and a launch that may have started is `unconfirmed` and never " +
			"retried: look at `atrium_peers` with `rooms` before launching again.\n\n" +
			"`kind: \"interview\"` STARTS AN INTERVIEWER. Atrium writes the interviewer rules into BRIEF.md " +
			"ahead of your `brief`, so put only the topic and context in `brief`. The card is tagged " +
			"`atrium:interview`: its reply IS the question, it asks one per reply and calls no atrium tool " +
			"between questions, and atrium never nudges it or calls it stuck for stopping on a question. " +
			"The human answers in its terminal, so point them at the card. At the end it commits, pushes " +
			"to the hub, publishes and calls atrium_done. It runs on opus unless `model` or the " +
			"worker_policy `interview_model` says otherwise.\n\n" +
			"WHAT IT WRITES GOES IN GIT. A `cwd` that is not a git checkout on a `claude/*` branch is " +
			"REFUSED, with the reason, so a writeup is not lost in a scratch folder. Use `repo` and `branch`, or " +
			"pass `scratch: true` for work that is throwaway.\n\n" +
			"Returns the card id. Use it with `atrium_task`, `atrium_say` and `atrium_exit`, on another room too.",
	}, audited(c, "ctl-launch", describeLaunch, c.launchHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_launch_worker",
		Description: "Start a new agent for PARALLEL WORK that ends when its branch merges. Same parameters as " +
			"`atrium_launch`, and the same session, but the card is marked `lifetime: work`: once its branch is merged " +
			"to claude/main atrium EXITS its runner, and it also exits it a few seconds after the worker's own final " +
			"`atrium_done`, scratch or not, so a finished worker never holds a launch slot. Pass `resident: true` to " +
			"keep one running. A blocked worker keeps running. The card and the worktree stay, and `atrium_say` with " +
			"`wake: true` resumes the worker for another round. Use it for a worker with a job that ends in a " +
			"merge or a report.\n\n" +
			"Use `atrium_launch` instead for an investigation, a long conversation, or anything the human said to " +
			"leave open: atrium never exits that card on a merge. Which one you call is the choice, so make it.",
	}, audited(c, "ctl-launch", describeLaunch, c.launchWorkerHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_exit",
		Description: "Ask a session to finish and leave.\n\n" +
			"ASKED, not killed. Atrium sends the exit keys its harness is configured with, so " +
			"the runner shuts itself down and writes whatever it writes on the way out. Its " +
			"card and its whole history stay on the board.\n\n" +
			"Say something first if the work is not finished. A session asked to leave mid-task " +
			"leaves mid-task.\n\n" +
			"A card on ANOTHER ROOM is `name@room`, `alias@room` or `room~id`, as `atrium_say` " +
			"takes it and as `atrium_launch` with `room` hands it back.",
	}, audited(c, "ctl-exit", describeExit, c.exitHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_model",
		Description: "Switch a live session's model, now.\n\n" +
			"Atrium types `/model <model>` into the card's terminal when its input line is clear, the way " +
			"an immediate `atrium_say` is typed. No new context and no relaunch: the session keeps its " +
			"conversation. The choice is also recorded on the card, so a later resume or relaunch starts " +
			"on it.\n\n" +
			"`model` is `sonnet`, `opus`, `haiku`, `fable`, or a full id starting `claude-`. Only a claude " +
			"session whose terminal atrium owns takes it, and anything else is refused with the reason. " +
			"`typed` says whether it went in now. When it did not, `delivered` is `waiting` and atrium " +
			"keeps trying until the line clears.\n\n" +
			"A card on ANOTHER ROOM is `name@room`, `alias@room` or `room~id`, as `atrium_say` takes it.",
	}, audited(c, "ctl-model", describeModel, c.modelHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_cull",
		Description: "Reclaim a finished worker once its work is MERGED AND DEPLOYED: ask it to leave, then " +
			"remove its worktree and branch, its BRIEF.md, its scratch directory and what it owns.\n\n" +
			"CALLING THIS IS YOUR WORD THAT THE WORK IS MERGED AND DEPLOYED. Call it then, as the launcher, " +
			"or the merger acting for it. Not before: a reclaimed worker cannot be sent back to fix " +
			"anything. A worker cannot cull itself. Its done report closed its card already, so " +
			"this is the second step, and it is yours.\n\n" +
			"NEVER RECLAIMED, and refused: a card that is not done (it stays in Terminals), a card " +
			"you did not launch, and a card tagged atrium:keep-open or atrium:investigation.\n\n" +
			"THE ROOM CHECKS, and refuses the whole cull when the card is not tagged " +
			"atrium:subagent or its branch is not merged. A worktree with uncommitted changes is " +
			"kept, and so is its branch, and the answer says why. The worker is still asked to " +
			"leave in that case, which frees its launch-cap slot. Nothing is forced: git removes " +
			"the worktree only when it agrees it is clean. The card and its history stay.\n\n" +
			"A room reclaims a landed worker on its own: merged_cull_grace (a day unless set, `off` opts out) " +
			"marks a merged worker, and one idle that long is culled unless it has a new turn or is held. `hold=true` " +
			"keeps a worker for good: the mark is dropped and nothing marks it again, only an " +
			"explicit cull removes it.",
	}, audited(c, "ctl-cull", describeCull, c.cullHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_close",
		Description: "Close a card the way the operator does: stop its session, archive its review and remove the " +
			"worktree, branch and fetched refs its inventory lists. The card goes to done and stays in history.\n\n" +
			"WITHOUT confirm it changes nothing and answers the preview: `items`, `warnings`, and `asks`, the " +
			"worktrees holding work that is nowhere else. Each ask needs an answer in `answers` by its seq: keep " +
			"(stays on disk), stash (pushed to the hub as stash/<card>/<branch>, then removed) or delete (the work " +
			"is lost). Never pick delete unless whoever you work for said so. Do not close a card you were not " +
			"asked to.\n\n" +
			"Use this rather than a script against the API. A card on ANOTHER ROOM is `name@room`, `alias@room` " +
			"or `room~id`, as `atrium_say` takes it.",
	}, audited(c, "ctl-close", describeClose, c.closeHandler))

	addTool(s, class, &mcp.Tool{
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
			"coming and given time to stop. Pass `force` to restart even if some are still busy.\n\n" +
			"To be prompted when you come back, call `atrium_wake_after_restart` first.",
	}, audited(c, "ctl-restart", describeRestart, c.restartHandler))

	addTool(s, class, &mcp.Tool{
		Name: "atrium_wake_after_restart",
		Description: "Queue one prompt for YOUR OWN card, typed into your terminal once after a restart " +
			"brings your session back.\n\n" +
			"A restart ends your terminal, and the resumed session sits idle until somebody types. Call " +
			"this before a restart that takes you down, with the line you want to receive, for example " +
			"what to check next. The room types it once your runner is back, the input line is empty " +
			"and the turn is over. It is not typed into the session running now.\n\n" +
			"ONE PER CARD. A second call replaces the first. It waits however long your runner takes " +
			"to come back, and is typed behind a grey `[atrium] restart wake:` label. Pass `clear` to " +
			"cancel it.",
	}, audited(c, "ctl-wake", describeWake, c.wakeHandler))

	c.registerGit(s, class)
	c.registerDeps(s, class)
	c.registerBacklog(s, class)
	c.registerItems(s, class)
	c.registerFlow(s, class)
	c.registerDocs(s, class)
	c.registerFiles(s, class)
	c.registerDeploy(s, class)
	c.registerWorktrees(s, class)
	c.registerTransport(s, class)
	c.registerResources(s, class)

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
		// A 502 in all but name, kept as one so the relay reads it as a hub
		// that is not answering rather than a refusal.
		return &boardError{code: http.StatusBadGateway,
			msg: fmt.Sprintf("could not reach the board at %s: %v", c.board, err)}
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if strings.TrimSpace(e.Error) != "" {
			return &boardError{code: res.StatusCode, msg: e.Error}
		}
		return &boardError{code: res.StatusCode, msg: "the board answered " + res.Status, bare: true}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// boardError is a refusal from the board, with its status kept. The sentence
// is the whole of what a tool shows. The code is what the relay reads to tell
// a room that is not answering from a refusal that will never succeed.
type boardError struct {
	code int
	msg  string
	// bare is an answer with no sentence in it, which is what a route the room
	// does not have looks like. A room older than an endpoint answers a bare 404.
	bare bool
}

func (e *boardError) Error() string { return e.msg }

// HTTPStatus is what the board answered, for a caller that reads a 404 as "no such thing".
func (e *boardError) HTTPStatus() int { return e.code }

// Bare is whether the answer carried no sentence, which is a route that is not there rather than a thing that is not.
func (e *boardError) Bare() bool { return e.bare }

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
	ID string `json:"id"`
	// LaunchReplay: the room answered with the card an earlier attempt filed under the same launch key.
	LaunchReplay bool     `json:"launch_replay"`
	Title        string   `json:"display_title"`
	Wire         string   `json:"wire_name"`
	Alias        string   `json:"alias"`
	Created      string   `json:"created_at"`
	Status       string   `json:"status"`
	Worktree     string   `json:"worktree"`
	Runner       string   `json:"runner"`
	Why          string   `json:"why"`
	Idle         int      `json:"idle_seconds"`
	Wait         int      `json:"wait_seconds"`
	Superv       bool     `json:"supervised"`
	Tags         []string `json:"tags"`
	// What the card was launched with. See docs/runtime/launch-options-design.md.
	Model         string   `json:"model"`
	Effort        string   `json:"effort"`
	LaunchArgs    []string `json:"launch_args"`
	LaunchEnvKeys []string `json:"launch_env_keys"`
	Activity      struct {
		What string `json:"what"`
	} `json:"activity"`
	Seen *ctlSeen `json:"seen,omitempty"`
}

// ctlSeen is whether the operator has seen a card's latest turn and answered
// its Open Questions. The room's `store.SeenView`, mirrored so internal/link
// learns nothing of the store. See docs/runtime/seen-design.md.
type ctlSeen struct {
	TurnEndedAt string `json:"turn_ended_at,omitempty"`
	SeenAt      string `json:"seen_at,omitempty"`
	SeenVia     string `json:"seen_via,omitempty"`
	// Unseen is the latest turn having ended with nobody looking at it since.
	Unseen        bool     `json:"unseen"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	// QuestionsUnparsed is a turn that had an Open Questions heading whose
	// items could not be read. They exist, and their text is not known.
	QuestionsUnparsed bool   `json:"questions_unparsed,omitempty"`
	QuestionsAt       string `json:"questions_at,omitempty"`
	AnsweredAt        string `json:"answered_at,omitempty"`
	AnsweredVia       string `json:"answered_via,omitempty"`
	// Answered is absent when no turn ever asked anything.
	Answered *bool `json:"answered,omitempty"`
}

// openCount is how many questions are owed, counting an unreadable block as
// one so it is never reported as none.
func (s *ctlSeen) openCount() int {
	if s == nil || s.Answered == nil || *s.Answered {
		return 0
	}
	if len(s.OpenQuestions) == 0 && s.QuestionsUnparsed {
		return 1
	}
	return len(s.OpenQuestions)
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
	// Rooms adds the sessions on every other attached room. See control_relay.go.
	Rooms bool `json:"rooms,omitempty" jsonschema:"also list sessions on other rooms. their handles are name@room, which atrium_say takes"`
}

type peer struct {
	Handle string `json:"handle"`
	// Room is set on a peer from another room, whose handle is `name@room`.
	Room string `json:"room,omitempty"`
	// Alias is the short name the operator gave it, `sa89` or `dotfiles`.
	// Accepted anywhere the handle is, with or without the `@`.
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
	// Unseen is this session's latest turn having ended with the operator not
	// looking, and OpenQuestions is how many questions it asked that they have
	// not answered. `atrium_task` has the questions themselves.
	Unseen        bool `json:"unseen,omitempty"`
	OpenQuestions int  `json:"open_questions,omitempty"`
	// Everywhere marks a card that is listed because it carries atrium:everywhere.
	Everywhere bool `json:"everywhere,omitempty"`
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
		live := t.Status != "done" && t.Status != "dead" && t.Status != "paused"
		if !in.All && !live {
			continue
		}
		out.Peers = append(out.Peers, peer{
			Handle: t.Wire, Alias: t.Alias, Card: t.ID, Title: t.Title, Status: t.Status,
			Doing: t.Activity.What, Where: t.Worktree,
			Waiting: t.Wait, Owned: t.Superv,
			Unseen: t.Seen != nil && t.Seen.Unseen, OpenQuestions: t.Seen.openCount(),
		})
	}
	// OTHER ROOMS, marked, when asked. Only for a caller with a room: without one
	// the list above is already every room's.
	if in.Rooms && room != "" {
		elsewhere, quiet := c.peersElsewhere(ctx, room, in.All)
		for _, p := range elsewhere {
			out.Peers = append(out.Peers, peer{
				Handle: p.Handle, Room: p.Room, Alias: p.Alias, Card: p.Card, Title: p.Title,
				Status: p.Status, Doing: p.Doing, Where: p.Where, Waiting: p.Waiting, Owned: p.Owned,
			})
		}
		if len(quiet) > 0 {
			out.Note = "not answering, so not listed: " + strings.Join(quiet, ", ")
		}
	} else if room != "" && c.hub != nil {
		// WITHOUT `rooms`, the cards tagged atrium:everywhere on other rooms,
		// after the local ones, so they can be told to by name.
		for _, e := range c.hub.every.all(room) {
			out.Peers = append(out.Peers, e.asPeer())
		}
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
	return c.resolvePeerOpt(ctx, room, who, false)
}

// resolvePeerOpt is resolvePeer, and with strict a name that more than one card
// answers to is refused with each candidate rather than resolved to the newest.
// A close cannot be undone, so the destructive verbs ask for strict.
func (c *controlMCP) resolvePeerOpt(ctx context.Context, room, who string, strict bool) (id, handle string, err error) {
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
	if strict {
		if err := ambiguousAlias(body.Tasks, who, time.Now()); err != nil {
			return "", "", err
		}
	}
	// The rule is matchCard's (resolve.go), shared with the hub's HTTP routing.
	// A caller with no room reads the aggregate list, where two rooms can each
	// hold a live card with one alias. That is ambiguous, never the first found.
	if room == "" {
		byRoom := map[string][]ctlCard{}
		for _, t := range body.Tasks {
			r, _ := splitTag(t.ID)
			byRoom[r] = append(byRoom[r], t)
		}
		if len(byRoom) > 1 {
			found, err := resolveAcross(byRoom, nil, who)
			var amb *errAmbiguous
			if errors.As(err, &amb) {
				return "", "", err
			}
			if err == nil {
				return found.Card.ID, found.Card.Wire, nil
			}
		}
	}
	if t, ok := matchCard(body.Tasks, who); ok {
		return t.ID, t.Wire, nil
	}
	// The list of ones that would have worked, which is the whole of the fix for
	// a wrong handle and is otherwise another tool call away.
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

// resolveCard finds a card the way atrium_say finds one, on this room or on
// another: `name`, `alias`, `name@room`, `alias@room` or `room~id`. `scope` is
// the room to ask about that card, which is the caller's own unless the address
// named another. A card on another room comes back named across, `room~id` and
// `handle@room`, so what one tool hands back another takes. See item 68 in
// docs/backlog-2.md.
//
// A caller with no room is the aggregate view, where a bare name still means
// the aggregate list and a named room is asked directly.
func (c *controlMCP) resolveCard(ctx context.Context, room, who string) (scope, id, handle string, err error) {
	return c.resolveCardOpt(ctx, room, who, false)
}

// resolveCardStrict is resolveCard for a verb that cannot be undone: a name two
// cards answer to is refused, listing them. An id or a wire name always works.
func (c *controlMCP) resolveCardStrict(ctx context.Context, room, who string) (scope, id, handle string, err error) {
	return c.resolveCardOpt(ctx, room, who, true)
}

func (c *controlMCP) resolveCardOpt(ctx context.Context, room, who string, strict bool) (scope, id, handle string, err error) {
	name, target, err := SplitAddress(who)
	if err != nil {
		return "", "", "", err
	}
	other := otherRoom(target, room)
	if other == "" {
		id, handle, err = c.resolvePeerOpt(ctx, room, name, strict)
		return room, id, handle, err
	}
	if ans, ok := c.reachable(ctx, other); !ok {
		return "", "", "", errors.New(ans.Error)
	}
	if id, handle, err = c.resolvePeerOpt(ctx, other, name, strict); err != nil {
		return "", "", "", err
	}
	return other, id, handle, nil
}

// namedFrom is a card's id and handle as a caller on `room` names it: bare on
// its own room, `room~id` and `handle@room` from anywhere else.
func namedFrom(room, scope, id, handle string) (string, string) {
	if scope == "" || equalFold(scope, room) {
		return id, handle
	}
	if handle != "" {
		handle += "@" + scope
	}
	return tagFor(scope, id), handle
}

// ── say ────────────────────────────────────────────────────────────────────────

type sayInput struct {
	// To is a handle from atrium_peers, or a card id. Both are accepted because
	// both are things the caller has in hand, and refusing the one it happens to
	// be holding is a puzzle rather than a rule.
	To string `json:"to" jsonschema:"the handle, alias or card id to say it to"`
	// Text is what to say, as one agent to another.
	Text string `json:"text" jsonschema:"what to say, as one agent to another. the recipient is told who you are automatically, so do not announce yourself"`
	// When is `immediate` (the default) or `done`. See internal/daemon/saywhen.go.
	When string `json:"when,omitempty" jsonschema:"immediate (the default): typed as soon as the line is empty, or carried by a hook at its next tool call when it is mid-turn. done: wait for that session's turn to end"`
	// Wake resumes a parked card so this reaches it.
	Wake bool `json:"wake,omitempty" jsonschema:"true to resume a PARKED session (idle, no process) and deliver this. it costs a cold start, so leave it off unless the message is worth it. a launcher need not pass it for its own worker, parked or finished, unless kind is fyi. without it a say to a parked session is refused and nothing is queued. a card on another room, named as name@room or reached by a bare name on every room, is resumed there too. a room that is not attached refuses it, and nothing is held"`
	// Force types now when the typing gate cannot read the line. See internal/daemon/forcesay.go.
	Force bool `json:"force,omitempty" jsonschema:"true to type it NOW when the gate holds it only because it cannot READ that session's line, as after a room restart. allowed to the operator and to the target's launcher, on this room or another, refused for any other session. never typed past a line atrium knows is part written, or a key pressed in the last two seconds, and refused with the reason (never queued) while a dialog or a new-context cycle owns the terminal"`
	// Kind is `fyi` or `needs`. See internal/daemon/fyi.go.
	Kind string `json:"kind,omitempty" jsonschema:"needs (the default): wants an answer or an action. fyi: news the receiver need not act on, which a receiver that holds its notices is typed at the end of its turn instead of mid-turn"`
}

type sayOutput struct {
	// Delivered is `terminal` or `queued`. Two different promises, and the caller
	// has to know which one was made: typed has already landed, queued has not
	// and will not until that session next reaches a hook.
	Delivered string `json:"delivered"`
	To        string `json:"to"`
	// ToCard is the RECIPIENT's card id, never yours. It was `card`, which a
	// worker read as its own and exited its director with. Nothing in the repo
	// read the old field: it is only ever shown to the model.
	ToCard string `json:"to_card"`
	// When is what it waits for: `immediate`, or `done` when it was asked for
	// or the runner does not take input mid-turn.
	When string `json:"when,omitempty"`
	Note string `json:"note,omitempty"`
}

func (c *controlMCP) sayHandler(ctx context.Context, req *mcp.CallToolRequest, in sayInput) (
	*mcp.CallToolResult, sayOutput, error) {

	out := sayOutput{}
	if strings.TrimSpace(in.Text) == "" {
		return nil, out, fmt.Errorf("nothing to say")
	}
	room := roomOf(req)
	// ANOTHER ROOM, named as `name@room`. Only for a caller with a room: one
	// without keeps the aggregate list, where `room~id` already reaches
	// anywhere. See docs/fabric/cross-room-say-design.md.
	if room != "" {
		name, target, err := SplitAddress(in.To)
		if err != nil {
			return nil, out, err
		}
		if other := otherRoom(target, room); other != "" {
			return c.sayAcross(ctx, req, room, name, other, in)
		}
		in.To = name
	}
	id, handle, err := c.resolvePeer(ctx, room, in.To)
	if err != nil {
		// A BARE NAME THAT MISSED ON THE CALLER'S OWN ROOM, looked for among the
		// cards tagged atrium:everywhere on the others. One match goes on as if
		// `handle@room` had been typed. See everywhere.go.
		card, ferr := c.everywhereFallthrough(room, in.To, err)
		if ferr != nil {
			return nil, out, ferr
		}
		return c.sayAcross(ctx, req, room, card.sendName(), card.Room, in)
	}
	out.To, out.ToCard = handle, id

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
		Warning   string `json:"warning"`
		When      string `json:"when"`
	}
	body := map[string]any{"text": in.Text, "from": from}
	if w := strings.TrimSpace(in.When); w != "" {
		body["when"] = w
	}
	if in.Wake {
		body["wake"] = true
	}
	if in.Force {
		body["force"] = true
	}
	if k := strings.TrimSpace(in.Kind); k != "" {
		body["kind"] = k
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/message", room,
		body, &res); err != nil {
		return nil, out, err
	}
	out.Delivered, out.When = res.Delivered, res.When
	switch {
	case res.Warning != "":
		// The room knows whether that card can drain its queue, and says so
		// when it cannot. Passed through, so the sender hears it now rather
		// than finding out an hour later that nothing arrived.
		out.Note = res.Warning
	case res.Delivered == "queued" && res.When == "done":
		out.Note = "queued until that session's turn ends. it is typed in then, or carried by " +
			"its Stop hook."
	case res.Delivered == "typed-queued":
		out.Note = "typed in, and not read yet: it is in that session's queued-message box. it reads the " +
			"box at its next step, which for a long tool call is when the call returns."
	case res.Delivered == "queued":
		out.Note = "queued, not typed. it arrives at that session's next tool call or the end of its " +
			"turn, or is typed in when it is idle and its line is clear. a long tool call delays it."
	}
	return nil, out, nil
}

// ── report ──────────────────────────────────────────────────────────────────────

type reportInput struct {
	Status   string `json:"status" jsonschema:"done, blocked, question or progress"`
	Summary  string `json:"summary" jsonschema:"what happened, in your words. your launcher reads it verbatim"`
	SHA      string `json:"sha,omitempty" jsonschema:"for done: the commit the work landed as"`
	NoCommit string `json:"no_commit,omitempty" jsonschema:"for done with no commit: why there is none"`
	Ask      string `json:"ask,omitempty" jsonschema:"for blocked or question: what you need, and from whom"`
	Kind     string `json:"kind,omitempty" jsonschema:"needs (the default): wants an answer or an action. fyi: news your launcher need not act on, typed at the end of its turn instead of mid-turn"`
}

type reportOutput struct {
	Recorded     bool   `json:"recorded"`
	Status       string `json:"status"`
	Unverified   bool   `json:"unverified,omitempty"`
	LauncherTold any    `json:"launcher_told"`
	Note         string `json:"note,omitempty"`
}

// reportHandler files the caller's report on its own card. The room validates
// it and queues it to the launcher, so this only finds the card. See
// internal/daemon/finish.go.
func (c *controlMCP) reportHandler(ctx context.Context, req *mcp.CallToolRequest, in reportInput) (
	*mcp.CallToolResult, reportOutput, error) {

	out := reportOutput{}
	me := agentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("atrium_report is for a session atrium knows. this call did not say which one it is")
	}
	room := roomOf(req)
	id, _, err := c.resolvePeer(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	var res struct {
		Recorded     bool   `json:"recorded"`
		Status       string `json:"status"`
		Unverified   bool   `json:"unverified"`
		LauncherTold Told   `json:"launcher_told"`
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/report", room,
		map[string]string{
			"status": strings.TrimSpace(in.Status), "recap": in.Summary, "sha": strings.TrimSpace(in.SHA),
			"no_commit": in.NoCommit, "ask": in.Ask, "kind": strings.TrimSpace(in.Kind),
		}, &res); err != nil {
		return nil, out, err
	}
	out.Recorded, out.Status, out.Unverified, out.LauncherTold = res.Recorded, res.Status, res.Unverified, res.LauncherTold.Value()
	if res.Recorded {
		c.followItem(ctx, room, id, in.Status, me)
	}
	switch {
	case res.Unverified:
		out.Note = "recorded, but that commit is not in your worktree, so the card is flagged. if it " +
			"landed somewhere else, say where in REPORT.md."
	case res.LauncherTold == ToldHeld:
		out.Note = HeldNote
	case !res.LauncherTold.Any():
		out.Note = "recorded on your card. nobody launched you, so there was nobody else to tell."
	}
	return nil, out, nil
}

// endReport files atrium_done and atrium_blocked as the caller's report, by the path reportHandler takes.
func (c *controlMCP) endReport(ctx context.Context, req *mcp.CallToolRequest, body map[string]any) (EndOutput, error) {
	out := EndOutput{}
	me := agentOf(req)
	if me == "" {
		return out, fmt.Errorf("this is for a session atrium knows. this call did not say which one it is")
	}
	room := roomOf(req)
	id, _, err := c.resolvePeer(ctx, room, me)
	if err != nil {
		return out, err
	}
	var res struct {
		Recorded     bool   `json:"recorded"`
		Status       string `json:"status"`
		LauncherTold Told   `json:"launcher_told"`
		RunnerExits  bool   `json:"runner_exits"`
		RunnerNote   string `json:"runner_note"`
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/report", room, body, &res); err != nil {
		return out, err
	}
	out.Recorded, out.Status, out.LauncherTold = res.Recorded, res.Status, res.LauncherTold.Value()
	if res.Recorded {
		c.followItem(ctx, room, id, fmt.Sprint(body["status"]), me)
	}
	switch {
	case res.LauncherTold == ToldHeld:
		out.Note = HeldNote
	case res.LauncherTold == ToldQueued:
		out.Note = QueuedNote
	case !res.LauncherTold.Any():
		out.Note = "recorded on your card. nobody launched you, so there was nobody else to tell."
	}
	if res.RunnerExits {
		out.RunnerExits = true
		out.Note = strings.TrimSpace(out.Note + " " + res.RunnerNote)
	}
	return out, nil
}

// ── one card ────────────────────────────────────────────────────────────────────

type taskInput struct {
	// Card is optional so a session can ask about itself, which is the
	// orchestrator's question: has the operator read my last turn.
	Card string `json:"card,omitempty" jsonschema:"a card id, handle or alias, name@room or room~id for a card on another room. empty means your own card"`
	// Events includes the recent history, which is what a card DID rather than
	// where it is now.
	Events bool `json:"events,omitempty" jsonschema:"include recent events"`
	// Notices is what a launcher that holds its notices reads instead of having
	// them typed. See holdsNotices in internal/daemon/a2a.go.
	Notices bool `json:"notices,omitempty" jsonschema:"include the automatic notices held on the card, newest last"`
	// Dismiss closes the owed item a worker left on your card, by the worker's handle. Reading
	// the notices does not close an item. See internal/daemon/owed.go.
	Dismiss string `json:"dismiss,omitempty" jsonschema:"a worker whose owed item on your card you are closing, by handle. an item also closes when you message, exit or relaunch the worker"`
}

type taskEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

// heldNotice is one automatic notice atrium recorded on a card instead of typing
// it: a worker's silent stop, context size, or end without a report.
type heldNotice struct {
	At     string `json:"at"`
	Source string `json:"source"`
	// Kind is `fyi` for news a worker or peer sent you with kind fyi, and absent for
	// an automatic notice. About says who sent it.
	Kind  string `json:"kind,omitempty"`
	About string `json:"about,omitempty"`
	Card  string `json:"about_card,omitempty"`
	Text  string `json:"text"`
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
	// Notices are the held notices, when asked for. The last 20.
	Notices []heldNotice `json:"notices,omitempty"`
	// Seen is whether the operator has seen this card's latest turn, and the
	// Open Questions it asked that they have not answered. Absent when no turn has
	// ended on it.
	Seen *ctlSeen `json:"seen,omitempty"`
	Note string   `json:"note,omitempty"`
}

func (c *controlMCP) taskHandler(ctx context.Context, req *mcp.CallToolRequest, in taskInput) (
	*mcp.CallToolResult, taskOutput, error) {

	out := taskOutput{}
	room := roomOf(req)
	who := strings.TrimSpace(in.Card)
	scope, id := room, ""
	var err error
	if who == "" {
		if who = agentOf(req); who == "" {
			return nil, out, fmt.Errorf("say which card. this session is not on the board, " +
				"so it has no card of its own to default to")
		}
		// Your own handle is on your own room, whatever it looks like.
		id, _, err = c.resolvePeer(ctx, room, who)
	} else {
		scope, id, _, err = c.resolveCard(ctx, room, who)
		if err != nil && !strings.Contains(who, "@") && !strings.Contains(who, idJoin) {
			// A READ, SO IT FALLS THROUGH like a say does, to the one card on
			// another room that answers to this bare name. Continues as the
			// read of that card, named across, exactly as `room~id` would.
			card, ferr := c.everywhereFallthrough(room, who, err)
			if ferr != nil {
				return nil, out, ferr
			}
			scope, id, err = card.Room, card.ID, nil
		}
	}
	if err != nil {
		return nil, out, err
	}
	if dismiss := strings.TrimSpace(in.Dismiss); dismiss != "" {
		if strings.TrimSpace(in.Card) != "" {
			return nil, out, fmt.Errorf("dismiss closes an item on your own card, so leave card empty")
		}
		wscope, wid, _, derr := c.resolveCard(ctx, room, dismiss)
		if derr != nil || wscope != scope {
			return nil, out, fmt.Errorf("no worker of yours called %q on this room to dismiss", dismiss)
		}
		if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/owed-dismiss", scope,
			map[string]string{"worker": wid}, nil); err != nil {
			return nil, out, err
		}
	}
	t, events, notices, err := c.readCard(ctx, scope, id, in.Events, in.Notices)
	if err != nil {
		return nil, out, err
	}
	out.Notices = notices
	if in.Notices && strings.TrimSpace(in.Card) == "" && len(notices) > 0 {
		// A READ, so the held count on this card's row drops. Stamped with the newest
		// notice handed back and never with now, so one held after the read stays
		// unread. Best effort: a notice that stays counted is a nag, and a failed read
		// must not fail the answer.
		_ = c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/notices-read", scope,
			map[string]string{"through": notices[len(notices)-1].At}, nil)
	}
	out.Card, out.Handle = namedFrom(room, scope, t.ID, t.Wire)
	out.Title = t.Title
	out.Status, out.Doing, out.Where, out.Why = t.Status, t.Activity.What, t.Worktree, t.Why
	out.Idle, out.Waiting, out.Owned = t.Idle, t.Wait, t.Superv
	out.Seen = t.Seen
	out.Events = events
	out.Note = "status and events only. atrium does not record what a session printed, so this " +
		"cannot tell you what it said or thinks. `seen` says whether the operator has seen its " +
		"last turn and answered that turn's Open Questions."
	return nil, out, nil
}

// readCard reads one card on `scope`, and its recent events and held notices
// when asked.
func (c *controlMCP) readCard(ctx context.Context, scope, id string, withEvents, withNotices bool) (
	ctlCard, []taskEvent, []heldNotice, error) {

	var t ctlCard
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), scope, nil, &t); err != nil {
		return t, nil, nil, err
	}
	if !withEvents && !withNotices {
		return t, nil, nil, nil
	}
	var body struct {
		Events []struct {
			At      string          `json:"at"`
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	// Decoded one event at a time: payloads differ by kind, and one that does not
	// fit must not lose the rest.
	type heldPayload struct {
		Held   bool   `json:"held"`
		Source string `json:"source"`
		About  string `json:"about"`
		Card   string `json:"about_card"`
		Text   string `json:"text"`
	}
	var (
		events  []taskEvent
		notices []heldNotice
	)
	if err := c.ask(ctx, http.MethodGet,
		"/v1/tasks/"+url.PathEscape(id)+"/events", scope, nil, &body); err == nil {
		for _, e := range body.Events {
			if withEvents {
				events = append(events, taskEvent{At: e.At, Kind: e.Kind})
			}
			if !withNotices || e.Kind != "notified" {
				continue
			}
			var p heldPayload
			if json.Unmarshal(e.Payload, &p) == nil && p.Held {
				n := heldNotice{At: e.At, Source: p.Source, About: p.About, Card: p.Card, Text: p.Text}
				if p.Source == "fyi" {
					n.Kind = "fyi"
					n.Text = "fyi from " + p.About + ": " + p.Text
				}
				notices = append(notices, n)
			}
		}
		// The tail, because the useful end of a history is the recent one and a
		// card that has been up for days has hundreds.
		events, notices = lastN(events, 20), lastN(notices, 20)
	}
	return t, events, notices, nil
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// ── launch ──────────────────────────────────────────────────────────────────────

// OriginTag marks a card that atrium_launch created, as opposed to a session a
// human started at a terminal or through the board's launch dialog. It is the
// doer signal the board hides agent-launched cards by. The launch cap does NOT
// count it: every atrium_launch stamps it, orchestrators and the resident merger
// included, so counting it charged them all against one cap. See SubagentTag.
//
// HUB-SIDE, SO IT STAYS HUB-ONLY. launchHandler adds it to the tags it forwards
// on /v1/launch, and the room persists it as an ordinary tag (see
// store.SetTags), so nothing in internal/daemon has to learn the marker. A tag
// rather than a new field because tags already flow end to end, and one already
// lowercase and free of commas and spaces survives NormalizeTags unchanged.
const OriginTag = "origin:agent"

// reportLine is appended to every agent launch's prompt. See launchHandler. How a worker ends: atrium_done or
// atrium_blocked. The same text as daemon.LaunchEndingLine, which a test keeps equal.
const reportLine = "Before you end your turn, tell your launcher either:\n" +
	"- atrium_done <sha or artifact> [one line] when the work is finished\n" +
	"- atrium_blocked <up to 50 words> when something stops you, such as a question you need answered, a tool you need installed, or a permission you need.\n" +
	"Then stop. Say nothing more.\n" +
	"Run tests and builds in the foreground, with a longer command timeout if needed. If a command must run in the background, wait for it in the same turn. Never end a turn while a command you started is still running.\n" +
	"Report to your launcher, never to the operator, unless your launcher is the operator. Your commit is read from your worktree or any hub branch, so never ask where to push and never offer a push to origin. A launcher's \"stop\" is a valid end: answer it with atrium_done and your last sha."

// residentReportLine is how a resident card is told to end a task: the same tools, with the words that leave the card
// open. The same text as daemon.ResidentEndingLine, which a test keeps equal.
const residentReportLine = "You are resident: your card stays open between tasks. When a task is finished, call atrium_done <sha or artifact> [one line]. When something stops you, call atrium_blocked <up to 50 words>. Either one tells your launcher and leaves your card open. Then wait for the next message. Never call atrium_exit on yourself.\n" +
	"Run tests and builds in the foreground, with a longer command timeout if needed. If a command must run in the background, wait for it in the same turn. Never end a turn while a command you started is still running.\n" +
	"Report to your launcher, never to the operator, unless your launcher is the operator. Never ask where to push."

// ItemTag marks a card launched with `item`. See daemon.ItemTag.
const ItemTag = "atrium:item"

// ItemOwnerTag carries the owner an item launch asked for. See daemon.ItemOwnerTag.
const ItemOwnerTag = "atrium:item-owner:"

// itemReportLine is how an item worker is told to end: the same text as daemon.ItemEndingLine, which a test keeps
// equal. No launcher is named in it.
const itemReportLine = "Before you end your turn, call either:\n" +
	"- atrium_done <sha or artifact> [one line] when the work is finished\n" +
	"- atrium_blocked <up to 50 words> when something stops you, such as a question you need answered, a tool you need installed, or a permission you need.\n" +
	"Then stop. Say nothing more.\n" +
	"Run tests and builds in the foreground, with a longer command timeout if needed. If a command must run in the background, wait for it in the same turn. Never end a turn while a command you started is still running.\n" +
	"Your commit is read from your worktree or any hub branch, so never ask where to push and never offer a push to origin. If you are asked to stop, answer with atrium_done and your last sha."

// ItemReportLine is itemReportLine for the stdio control server and the tests.
const ItemReportLine = itemReportLine

// WithItemReportLine is WithReportLine for an item.
func WithItemReportLine(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt != "" && !strings.HasSuffix(prompt, itemReportLine) {
		prompt += "\n\n" + itemReportLine
	}
	return prompt
}

// AgentLaunchTags is what an agent launch's tags become: the caller's own, the origin marker,
// and a WORKER marker unless the caller says it is a director or already a subagent. The
// merged-cull only ever touches workers, so the default has to be the one that can be culled
// and the exception has to be asked for. Shared by the hub's atrium_launch and a room's stdio
// one (internal/cli), so the two cannot drift.
func AgentLaunchTags(callerTags []string) []string {
	tags := append(append([]string{}, callerTags...), OriginTag)
	if !hasTag(callerTags, DirectorTag) && !hasTag(callerTags, ResidentTag) && !hasTag(callerTags, SubagentTag) {
		tags = append(tags, SubagentTag)
	}
	return tags
}

// DeptTagPrefix is the tag that files a card under a department (daemon's deptTagPrefix).
const DeptTagPrefix = "dept:"

// WithLauncherDept passes the launching card's department on: the first `dept:*` tag of the
// launcher is added to the launch's tags unless the caller's own already carry one. A launcher
// with no dept tag, or none known, stamps nothing. Shared by the hub's atrium_launch and a
// room's stdio one, like AgentLaunchTags, so a director's workers file under its department.
func WithLauncherDept(tags, launcherTags []string) []string {
	if hasTagPrefix(tags, DeptTagPrefix) {
		return tags
	}
	for _, t := range launcherTags {
		if t = strings.TrimSpace(t); len(t) > len(DeptTagPrefix) && strings.EqualFold(t[:len(DeptTagPrefix)], DeptTagPrefix) {
			return append(append([]string{}, tags...), t)
		}
	}
	return tags
}

// hasTagPrefix reports whether any tag starts with prefix, case-insensitively, and has more after it.
func hasTagPrefix(tags []string, prefix string) bool {
	for _, t := range tags {
		if t = strings.TrimSpace(t); len(t) > len(prefix) && strings.EqualFold(t[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// launcherTags is the tags of the card that is launching, on the room its cards are named from,
// or nil when it cannot be found. `who` may be `name@room`: the room part is dropped, the card
// is looked up on callerRoom. A failed lookup stamps no dept, it never fails the launch.
func (c *controlMCP) launcherTags(ctx context.Context, callerRoom, who string) []string {
	if i := strings.Index(who, "@"); i >= 0 {
		who = who[:i]
	}
	if strings.TrimSpace(who) == "" {
		return nil
	}
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	// The one card, not the board (a room that predates ?name= answers the whole list, matched below).
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks?name="+url.QueryEscape(who), callerRoom, nil, &body); err != nil {
		return nil
	}
	if t, ok := matchCard(body.Tasks, who); ok {
		return t.Tags
	}
	return nil
}

// ReportLine is how a launch prompt ends, for a test outside the package.
const ReportLine = reportLine

// ResidentReportLine is how a resident launch prompt ends, for a test outside the package.
const ResidentReportLine = residentReportLine

// WithReportLine ends a launch prompt with the one line of the worker contract a prompt most
// often leaves out. An empty prompt stays empty: there is nothing to scope.
func WithReportLine(prompt string, resident bool) string {
	line := reportLine
	if resident {
		line = residentReportLine
	}
	prompt = strings.TrimSpace(prompt)
	if prompt != "" && !strings.HasSuffix(prompt, line) {
		prompt += "\n\n" + line
	}
	return prompt
}

// SubagentTag marks a worker an orchestrator launched to do one piece of work.
// The launch cap counts only running cards carrying it. The launcher puts it on
// through `tags`; the hub does not stamp it, so an orchestrator, the resident
// merger or any other long-lived agent launched without it does not consume the
// cap its workers share.
const SubagentTag = "atrium:subagent"

// DirectorTag is what a launch asks for to be left alone by the merged-cull and
// not to count against the launch cap: an orchestrator, not a worker.
const DirectorTag = "atrium:director"

// ResidentTag marks a long-lived card: atrium_done and atrium_blocked report a task and leave it open.
const ResidentTag = "atrium:resident"

// hasOriginTag reports whether a card carries the agent-launch marker.
func hasOriginTag(tags []string) bool { return hasTag(tags, OriginTag) }

// hasTag reports whether tags holds want, trimmed and case-insensitively.
func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

// DefaultLaunchCap is how many concurrent live sessions atrium_launch allows
// before it refuses. It is the HARD backstop under the advisory soft nudge in the
// dotfiles redirect hook, so no amount of over-eager agents can flood the box.
// LaunchCapEnv overrides it for a machine that can take more or fewer.
const DefaultLaunchCap = 10

// LaunchCapEnv overrides DefaultLaunchCap when set to a non-negative integer.
const LaunchCapEnv = "ATRIUM_LAUNCH_CAP"

// reservationTTL is how long an admitted-but-not-yet-running launch holds a slot
// against the cap. Long enough for the new card to surface in the live count,
// after which the reservation lapses and the live card takes over the slot, so
// nothing has to wire an explicit release into session lifecycle. A launch that
// dies before its card appears simply frees its slot when the TTL passes.
const reservationTTL = 60 * time.Second

// launchCap resolves the cap: the env override when it parses as a non-negative
// integer, otherwise the default. A junk or negative value is ignored rather
// than obeyed, so a fat-fingered override cannot silently disable the backstop.
func launchCap() int {
	if v := strings.TrimSpace(os.Getenv(LaunchCapEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return DefaultLaunchCap
}

// capOf is the launch cap for one room.
func (c *controlMCP) capOf(room string) int {
	if c.capFor != nil {
		return c.capFor(room)
	}
	return launchCap()
}

// runningForCap counts the sessions that count against a room's launch cap:
// live supervised runners tagged SubagentTag, on THAT ROOM ONLY.
//
// PER ROOM, because a room is a machine and the cap is about the load on it.
// This used to count the aggregate over every room the hub could see, so five
// workers on sg3 and five on sg4 refused a launch onto either. An empty room is
// a caller the hub cannot place, and gets the aggregate, which can only ever be
// more cautious.
//
// ONLY SUBAGENTS. OriginTag is on every atrium_launch card, orchestrators and
// the resident merger as much as their workers, so it is not what the cap
// counts. A card without SubagentTag never consumes the cap, whoever started
// it. A done/dead/paused card has no running runner and a backlog card has not
// started one, so none of them count, and the launch being attempted is not
// present yet so it is never counted.
func (c *controlMCP) runningForCap(ctx context.Context, room string) (int, error) {
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return 0, err
	}
	n := 0
	for _, t := range body.Tasks {
		if !t.Superv {
			continue
		}
		switch t.Status {
		case "done", "dead", "paused", "shelved", "backlog":
			continue
		}
		if !hasTag(t.Tags, SubagentTag) {
			continue
		}
		n++
	}
	return n, nil
}

// reserveSlot atomically admits or refuses one launch against the cap. `live` is
// the live supervised count just read from the board; the total charged against
// the cap is that plus the outstanding (non-expired) reservations, so two
// launches that both read the same live count cannot both slip through: the
// first records a reservation the second then sees. On admission it records a
// reservation and returns true; at or over the cap it records nothing and
// returns false.
//
// The count is done here under the lock rather than at the call site so the
// check and the record are one indivisible step. Expired reservations are swept
// on the way in, which is the only place they need collecting.
//
// Reservations are per room, like the count: a launch in flight to sg3 holds
// nothing against sg4.
func (c *controlMCP) reserveSlot(room string, live, limit int) (id string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	kept := c.reservations[:0]
	pending := 0
	for _, r := range c.reservations {
		if now.Sub(r.at) < reservationTTL {
			kept = append(kept, r)
			if equalFold(r.room, room) {
				pending++
			}
		}
	}
	c.reservations = kept
	if live+pending >= limit {
		return "", false
	}
	c.resSeq++
	id = strconv.Itoa(c.resSeq) + "@" + now.Format(time.RFC3339Nano)
	c.reservations = append(c.reservations, reservation{id: id, room: room, at: now})
	return id, true
}

type launchInput struct {
	Cwd    string `json:"cwd,omitempty" jsonschema:"a directory you made to run in. it has to exist already. leave it out and name repo (or home) so atrium makes the card's own folder under the room's card root. not together with repo"`
	Repo   string `json:"repo,omitempty" jsonschema:"the repository to work in, as host/owner/repo (cloned from the hub when the room has no clone yet) or an absolute path to a clone on the room. atrium makes the card's home under its card root and adds a git worktree there as the card's directory. use this instead of making a worktree yourself. not together with cwd"`
	Branch string `json:"branch,omitempty" jsonschema:"the branch for the worktree that repo makes. made from base when it does not exist. default claude/<title>"`
	Base   string `json:"base,omitempty" jsonschema:"where a new branch starts. default the clone's claude/main, else its default branch"`
	Home   bool   `json:"home,omitempty" jsonschema:"true asks for a home with no repo: an empty folder under the room's card root for scratch work. the card is kept open"`
	Title  string `json:"title,omitempty" jsonschema:"what to call the card"`
	Why    string `json:"why,omitempty" jsonschema:"what this is for, read back later"`
	Prompt string `json:"prompt,omitempty" jsonschema:"the first instruction it gets"`
	// Brief is written to BRIEF.md in the new session's directory on the room and
	// read first, so it survives compaction and can be re-read.
	Brief  string   `json:"brief,omitempty" jsonschema:"context to hand the new session. written to BRIEF.md in its directory on the room and read before it starts, so it survives compaction and can be re-read"`
	Runner string   `json:"runner,omitempty" jsonschema:"which configured runner to start. default claude"`
	Tags   []string `json:"tags,omitempty" jsonschema:"free text labels, used for grouping and filtering"`
	// Resident marks a long-lived card. See ResidentTag.
	Resident bool `json:"resident,omitempty" jsonschema:"a long-lived card: atrium_done and atrium_blocked report a task and leave it open, and it is never exited or culled for a done"`
	// Theme names the terminal palette the new session comes up in. Empty leaves
	// it to the board, which colours a themeless card from its repo, so an
	// agent-driven launch looks the same as a board-dialog one without this.
	Theme string `json:"theme,omitempty" jsonschema:"the terminal palette to come up in. empty lets the board colour it from the repo"`
	// Lean is on unless the caller turns it off. See leanLaunch.
	Lean *bool    `json:"lean,omitempty" jsonschema:"start a claude worker lean: no user CLAUDE.md, memory, skills or agents, only its brief, the repo, atrium's hooks and the atrium-control and mercurius MCP servers. default true. false starts it with the operator's whole setup"`
	MCP  []string `json:"mcp,omitempty" jsonschema:"extra MCP servers a lean worker keeps beside atrium-control and mercurius, by name from the runner's MCP config"`
	// LeanAgents starts the worker lean and keeps its Agent tool plus these agents.
	LeanAgents []string `json:"lean_agents,omitempty" jsonschema:"agents a lean claude worker can start, by name: the files in the operator's ~/.claude/agents on the room, without .md. it keeps the Agent tool and ONLY these, namespaced as atrium:<name>. a name with no file refuses the launch. implies lean. claude only. the card keeps the list"`
	LeanSkills []string `json:"lean_skills,omitempty" jsonschema:"skills a lean claude worker can use, by name: the directories in the operator's ~/.claude/skills on the room. it keeps the Skill tool and ONLY these. a name with no directory refuses the launch. implies lean. claude only. the card keeps the list"`
	// Superlean cuts a lean worker further. See superleanLaunch.
	Superlean bool `json:"superlean,omitempty" jsonschema:"start a claude worker SUPERLEAN: lean, and also only the tools Read, Edit, Write, Grep, Glob, Bash and ToolSearch, no mercurius unless mcp names it, and a one paragraph worker prompt. for a bounded change: edit files, run a command, commit, report. implies lean, refused with lean: false. claude only, never an interview. default false, lean stays the default"`
	// Model and Effort are mapped by the runner's harness row, Args and Env are
	// passed as given. See docs/runtime/launch-options-design.md.
	Model  string            `json:"model,omitempty" jsonschema:"which model the runner starts on, passed in the shape its runner row declares (claude and codex: --model). not checked against any list. empty is the runner's default. a runner with no way to take a model refuses"`
	Effort string            `json:"effort,omitempty" jsonschema:"thinking effort, passed in the shape its runner row declares (claude: --effort, codex: -c model_reasoning_effort=). not checked: whatever the runner accepts, such as low, medium or high for claude. empty is the runner's default. a runner with no way to take one refuses"`
	Args   []string          `json:"args,omitempty" jsonschema:"extra command-line arguments for the runner, one per element, added after the model and effort and before the prompt. used as given. shown on the card, so keep secrets out"`
	Env    map[string]string `json:"env,omitempty" jsonschema:"extra environment for the runner, used as given. ATRIUM_ names are refused. the values stay on the room and the card shows the names only"`
	// Room launches on another room. The worker's reports come back across.
	Room string `json:"room,omitempty" jsonschema:"launch on this room instead of your own. its reports and notices still reach you, as your-handle@your-room"`
	// Kind is what sort of session this is. Empty is an ordinary worker.
	Kind string `json:"kind,omitempty" jsonschema:"what sort of session to start. empty is an ordinary worker. interview starts an interviewer: atrium writes the interviewer rules into BRIEF.md ahead of your brief (put the topic and context in brief), tags the card atrium:interview, and does not nudge it for stopping on a question. its questions reach the human in its terminal, so tell the human to watch its card. it runs on opus unless model says otherwise"`
	// Scratch lets the launch start outside a git checkout.
	Scratch bool `json:"scratch,omitempty" jsonschema:"start in a directory that is not a git checkout on a claude/* branch. without this such a launch is refused, because what an agent writes there is not kept in git. say it only for throwaway work"`
	// Item is the backlog item the worker is on, REQUIRED. See WorkTagPrefix and work_launch.go.
	Item string `json:"item,omitempty" jsonschema:"REQUIRED: the backlog item this worker is on, by its id (find it with atrium_backlog list), or new:<a title> to file one as you launch, under your own department. every card launched on one id is attached to the same item. a launch with no item is refused"`
	// Isolated starts the worker with the isolated toolset. See ItemTag.
	Isolated  bool   `json:"isolated,omitempty" jsonschema:"start the worker ISOLATED: its ending lines never mention a launcher, its opening prompt comes from atrium, it has no atrium_say, atrium_peers, atrium_task or atrium_report, and atrium refuses any say from it. it ends with atrium_done or atrium_blocked and nothing else. leave it out for an ordinary worker, which is unchanged. this is no longer what makes a card an item: item is"`
	ItemOwner string `json:"item_owner,omitempty" jsonschema:"who owns the item and hears when it is blocked or finished. empty is you, the creating session. orchestrator, merger or director is whoever holds that role when it ends. human is the person at the board, and no session is told. tester is refused, it has several holders"`
	// Lifetime is not a parameter: atrium_launch_worker sets it to `work`, atrium_launch leaves it empty.
	Lifetime string `json:"-"`
	// Task is a task of an item: the room makes the folder and writes the brief. Never a parameter. See tasks_launch.go.
	Task *taskBlock `json:"-"`
}

// leanLaunch is whether an atrium_launch starts lean. On by default for the
// claude runner, because a launched worker is handed its context in the brief
// and pays ~27k tokens a start for the operator's setup otherwise. Another
// runner has no lean mode, so it is only on when asked, and the room refuses.
func leanLaunch(in launchInput, harness string) bool {
	if in.Lean != nil {
		return *in.Lean
	}
	// lean_agents keeps agents on a lean launch, so it is lean whatever the runner.
	// A runner with no lean mode refuses it.
	return harness == "claude" || len(in.LeanAgents) > 0 || len(in.LeanSkills) > 0 || in.Superlean
}

// superleanRefusal is the sentence for `superlean` beside `lean: false`, said before anything is posted.
// Superlean is a lean launch, so the two cannot both be asked for.
func superleanRefusal(in launchInput) error {
	if in.Superlean && in.Lean != nil && !*in.Lean {
		return fmt.Errorf("superlean is a lean launch, and this one says lean: false")
	}
	return nil
}

// SuperleanDropped is true when superlean was asked for and the card the room handed back carries no
// `atrium:superlean` tag, which is how a room older than the field shows: it ignored it and started the worker
// lean. Shared with the stdio control MCP.
func SuperleanDropped(asked bool, gotTags []string) bool {
	if !asked {
		return false
	}
	for _, t := range gotTags {
		if strings.TrimSpace(t) == "atrium:superlean" {
			return false
		}
	}
	return true
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
	// Model and Effort are what the card says it was started with, read back
	// from the room rather than echoed from the request.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Note   string `json:"note,omitempty"`
	// Recovered is a card found on the room after the launch's answer was lost, not one the answer named.
	Recovered bool `json:"-"`
}

// launchTimeout bounds the post to a room's /v1/launch. Under the 45 s the launching room waits on the hub, so
// the answer reaches it, and long enough for a room that trusts the worktree and starts the runner first.
const launchTimeout = 30 * time.Second

// launchKeys holds, per room and directory, the key of a launch whose answer was lost and whose card had not shown
// yet. Memory only, like the snapshot it backs: it is the hub's own memory of a retry, and a restart drops it.
var launchKeys = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func launchAttemptKey(room, cwd string) string {
	return strings.ToLower(room + "|" + strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(cwd), "\\", "/"), "/"))
}

// launchKeyFor is the key a launch for this attempt sends: the one an earlier attempt made, else a new random one.
func launchKeyFor(attempt string) string {
	launchKeys.Lock()
	defer launchKeys.Unlock()
	if k := launchKeys.m[attempt]; k != "" {
		return k
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	k := hex.EncodeToString(b)
	launchKeys.m[attempt] = k
	return k
}

func forgetLaunchKey(attempt string) {
	launchKeys.Lock()
	delete(launchKeys.m, attempt)
	launchKeys.Unlock()
}

// LaunchRecoveredNote is what a launch says when its answer was lost but the card was found on the room.
const LaunchRecoveredNote = "NOTE: the room's answer was slow, so the card was found by looking. it is running: do NOT launch it again. " +
	LaunchStartedNote

// listCards is the cards on a room, or false when it cannot be read.
func (c *controlMCP) listCards(ctx context.Context, room string) ([]ctlCard, bool) {
	var body struct {
		Tasks []ctlCard `json:"tasks"`
	}
	if err := c.ask(ctx, http.MethodGet, "/v1/tasks", room, nil, &body); err != nil {
		return nil, false
	}
	return body.Tasks, true
}

func launchRows(cards []ctlCard) []LaunchRow {
	rows := make([]LaunchRow, len(cards))
	for i, t := range cards {
		rows[i] = LaunchRow{ID: t.ID, Worktree: t.Worktree, Created: t.Created}
	}
	return rows
}

// findLaunched is the card a launch made in `cwd` that was not in `known`, or nil. It is how a launch whose
// answer was lost is told from one that never started.
func (c *controlMCP) findLaunched(ctx context.Context, room, cwd string, known map[string]bool) *ctlCard {
	cards, ok := c.listCards(ctx, room)
	if !ok {
		return nil
	}
	id := PickLaunched(known, cwd, launchRows(cards))
	for i := range cards {
		if id != "" && cards[i].ID == id {
			return &cards[i]
		}
	}
	return nil
}

// LifetimeWork is the lifetime atrium_launch_worker stamps. It is store.LifetimeWork, which this package does not
// import.
const LifetimeWork = "work"

// launchWorkerHandler is atrium_launch_worker: atrium_launch with the card marked lifetime work.
func (c *controlMCP) launchWorkerHandler(ctx context.Context, req *mcp.CallToolRequest, in launchInput) (
	*mcp.CallToolResult, launchOutput, error) {

	in.Lifetime = LifetimeWork
	return c.launchHandler(ctx, req, in)
}

func (c *controlMCP) launchHandler(ctx context.Context, req *mcp.CallToolRequest, in launchInput) (
	*mcp.CallToolResult, launchOutput, error) {

	out := launchOutput{}
	if strings.TrimSpace(in.Cwd) == "" && strings.TrimSpace(in.Repo) == "" && !in.Home {
		return nil, out, &refusedError{"say where to run it: cwd for a directory you made, or repo (with branch) so atrium " +
			"makes the card's own folder, or home for an empty one"}
	}
	if strings.TrimSpace(in.Cwd) != "" && strings.TrimSpace(in.Repo) != "" {
		return nil, out, &refusedError{"pass cwd for a directory you made, or repo for one atrium makes, not both"}
	}
	harness := strings.TrimSpace(in.Runner)
	if harness == "" {
		harness = "claude"
	}
	room := roomOf(req)
	callerRoom := room
	// ANOTHER ROOM. The lineage then names the launcher as `me@myroom`, and its
	// card as `myroom~id`, so the worker's reports and notices come back across
	// to exactly this card. See docs/fabric/cross-room-say-design.md.
	spawnedBy, spawnedByID, spawnedByDir := agentOf(req), "", ""
	if r := strings.TrimSpace(in.Room); r != "" && !equalFold(r, room) {
		if room != "" && spawnedBy != "" {
			if id, _, err := c.resolvePeer(ctx, room, spawnedBy); err == nil {
				spawnedByID = tagFor(room, id)
				spawnedByDir = c.launcherDir(ctx, room, id)
			}
			spawnedBy += "@" + room
		}
		room = r
	}
	out, err := c.launchOnRoom(ctx, in, harness, room, callerRoom, spawnedBy, spawnedByID, spawnedByDir)
	return nil, out, err
}

// launcherDir is the directory of the launcher's card on its own room, "" when it cannot be read. Best effort: the
// room it launches on only asks git about it.
func (c *controlMCP) launcherDir(ctx context.Context, room, id string) string {
	t, _, _, err := c.readCard(ctx, room, id, false, false)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(t.Worktree)
}

// launchOnRoom is the part of a launch that does not care who asked: the gate, the
// cap on `room`, the tags, the lineage and the post to that room's /v1/launch. Shared by
// the hub's own atrium_launch and by a room's launch relayed here (launchAcross), so
// the two cannot drift. `callerRoom` is the room the caller's cards are named from, and
// `spawnedBy`, `spawnedByID` and `spawnedByDir` are the lineage already worked out.
func (c *controlMCP) launchOnRoom(ctx context.Context, in launchInput, harness, room, callerRoom,
	spawnedBy, spawnedByID, spawnedByDir string) (launchOutput, error) {

	out := launchOutput{}
	if err := checkKind(in.Kind); err != nil {
		return out, err
	}
	if err := superleanRefusal(in); err != nil {
		return out, err
	}

	// EVERY LAUNCH NAMES AN ITEM, before a slot is reserved or anything is posted. The argument is the one the caller gave,
	// and a launch relayed from a room's stdio server has already resolved it into the tag. An interviewer is not a
	// worker: it talks to the human, and no work is attached to it. See work_launch.go.
	launcherTags := c.launcherTags(ctx, callerRoom, spawnedBy)
	workID, filedNote := "", ""
	if !isInterview(in.Kind) {
		spec := strings.TrimSpace(in.Item)
		if spec == "" {
			spec = WorkOfTags(in.Tags)
		}
		hubCall := func(ctx context.Context, method, path string, body, out any) error {
			return c.ask(ctx, method, path, "", body, out)
		}
		id, filed, err := ResolveItem(ctx, hubCall, spec, DeptOfTags(launcherTags), spawnedBy, callerRoom)
		if err != nil {
			return out, err
		}
		workID = id
		if filed {
			filedNote = "filed backlog item " + id + " for --item new. "
		}
	}

	// AN ITEM THAT WAITS ON OTHER WORK does not start, before any slot is reserved. A
	// worker's title starts with its item id, so a director cannot start blocked work by
	// accident. There is no override: a human clears the gate on the board first.
	if c.gate != nil {
		if msg, blocked := c.gate(ctx, in.Title); blocked {
			return out, &refusedError{msg}
		}
	}

	// THE HARD CAP. Count the live sessions already running plus the launches
	// already admitted but not yet showing as cards, and refuse before forwarding
	// once the machine is at or over the cap. reserveSlot does the check and the
	// reservation as one locked step, so two near-simultaneous launches that both
	// see the same live count cannot both overshoot it.
	//
	// Fail sane: a count lookup that errored is an infrastructure hiccup (a quiet
	// room, the board mid-restart), not a reason to brick launching, so allow the
	// launch rather than wrongly refuse. The soft nudge in the redirect hook is
	// the first line of defence and a stuck count must not become a launch outage.
	limit := c.capOf(room)
	if n, err := c.runningForCap(ctx, room); err == nil {
		if _, ok := c.reserveSlot(room, n, limit); !ok {
			where := "room " + room
			if room == "" {
				where = "every room together"
			}
			// A refusal, so `audited` writes it as `refused: ...` on ctl-launch.
			return out, &refusedError{fmt.Sprintf("at the launch cap of %d running workers on %s. "+
				"wait for one to finish, exit one, or launch on another room", limit, where)}
		}
	}

	// The briefing is written ON THE ROOM: /v1/launch carries the text and the
	// room's own daemon writes BRIEF.md into the new session's directory before
	// it starts. The hub has no such directory to write to, which is why this is
	// a field on the request rather than a file this side writes.
	// The origin marker rides along as a tag, added here on the hub so the room
	// stores it without knowing what it is (see OriginTag). It is how an agent
	// launch is told apart from a human's hand-started session. The cap counts
	// SubagentTag instead, which the caller supplies in its own tags.
	callerTags := in.Tags
	if in.Resident {
		callerTags = append(append([]string{}, in.Tags...), ResidentTag)
	}
	tags := WithLauncherDept(AgentLaunchTags(callerTags), launcherTags)
	tags = WithWorkTag(tags, workID)
	// WHO IS LAUNCHING, from the caller's own identity header, so the room can
	// record the lineage and route the worker's reports back. See
	// docs/runtime/a2a-reliability-design.md.
	//
	// The prompt ends with the one line of the worker contract a launch prompt
	// most often leaves out. A launch prompt that lists steps scopes the turn to
	// those steps, and the worker stops without a word.
	prompt := WithReportLine(in.Prompt, hasTag(tags, ResidentTag) || hasTag(tags, DirectorTag))
	if o := strings.TrimSpace(in.ItemOwner); o != "" && in.Isolated {
		tags = append(tags, ItemOwnerTag+o)
	}
	if in.Isolated {
		// ISOLATED: tagged, and told to end without a launcher in the words. See ItemTag.
		tags = append(tags, ItemTag)
		prompt = WithItemReportLine(in.Prompt)
	}
	brief := strings.TrimSpace(in.Brief)
	if isInterview(in.Kind) {
		// AN INTERVIEWER'S TURNS END ON A QUESTION, so it is not told to end each one with a report. The rules go into
		// BRIEF.md from the binary, ahead of the caller's topic, and the tag is what the room reads.
		prompt = strings.TrimSpace(in.Prompt)
		brief = interviewBrief(in.Brief)
		// Never a worker: the board files a subagent under its launcher and raises no toasts for it, and an interview
		// belongs in the operator's interview group. It does not count against the launch cap either.
		tags = slices.DeleteFunc(tags, func(t string) bool { return strings.TrimSpace(t) == SubagentTag })
		tags = append(tags, InterviewTag)
	}
	if wid := WorkOfTags(tags); wid != "" && brief != "" && !isInterview(in.Kind) && in.Task == nil {
		brief = WithItemBrief(brief, ItemBrief(ctx, func(ctx context.Context, method, path string, body, out any) error {
			return c.ask(ctx, method, path, "", body, out)
		}, wid))
	}
	reqBody := map[string]any{
		"harness": harness, "cwd": in.Cwd, "title": in.Title,
		"why": in.Why, "prompt": prompt, "scratch": in.Scratch,
		"brief": brief, "tags": tags,
		"theme": strings.TrimSpace(in.Theme), "spawned_by": spawnedBy,
		"lean": leanLaunch(in, harness), "mcp": in.MCP, "lean_agents": in.LeanAgents, "lean_skills": in.LeanSkills,
		"model": strings.TrimSpace(in.Model), "effort": strings.TrimSpace(in.Effort),
		"args": in.Args, "env": in.Env, "lifetime": in.Lifetime,
	}
	if in.Isolated {
		reqBody["item"] = true
	}
	if in.Repo != "" {
		reqBody["repo"] = strings.TrimSpace(in.Repo)
	}
	if in.Branch != "" {
		reqBody["branch"] = strings.TrimSpace(in.Branch)
	}
	if in.Base != "" {
		reqBody["base"] = strings.TrimSpace(in.Base)
	}
	if in.Home {
		reqBody["home"] = true
	}
	if in.Superlean {
		reqBody["superlean"] = true
	}
	if spawnedByID != "" {
		reqBody["spawned_by_id"] = spawnedByID
	}
	if spawnedByDir != "" {
		reqBody["spawned_by_dir"] = spawnedByDir
	}
	// THE LAUNCH KEY: made on the first attempt for this room and directory and sent again by the retry, so a room
	// that has the card filed under it answers with that card and starts nothing. A room older than the key ignores
	// it, and the snapshot below is what recovers.
	keyDir := in.Cwd
	if keyDir == "" {
		keyDir = in.Repo + "|" + in.Branch + "|" + in.Title
	}
	if in.Task != nil {
		keyDir = in.Task.dir()
		reqBody["task"] = in.Task
	}
	attempt := launchAttemptKey(room, keyDir)
	reqBody["launch_key"] = launchKeyFor(attempt)
	var t ctlCard
	// THE CARDS BEFORE THE POST, so a card that was not there is the launch's. Unreadable means no recovery
	// is possible: nothing can be told apart from what was already running.
	before, canTell := c.listCards(ctx, room)
	canTell = canTell && in.Task == nil && strings.TrimSpace(in.Cwd) != ""
	known := IDSet(launchRows(before))
	// A launch waits on the room trusting the worktree and starting the runner, which is longer
	// than the 8 s every other call here is bounded by.
	long := &controlMCP{board: c.board, client: &http.Client{Timeout: launchTimeout, Transport: c.client.Transport}}
	launchPath := "/v1/launch"
	if in.Task != nil {
		launchPath = "/v1/launch/task"
	}
	if err := long.ask(ctx, http.MethodPost, launchPath, room, reqBody, &t); err != nil {
		// THE ANSWER MAY BE LOST WHILE THE CARD EXISTS. Look for it before reporting a failure, so
		// a retry is not a duplicate worker in the same directory.
		var be *boardError
		if !errors.As(err, &be) || be.code != http.StatusBadGateway {
			// The room said no, so nothing was started and the key is not worth keeping. Any other failure, a
			// timeout among them, leaves the outcome unknown and the key for the retry.
			if be != nil {
				forgetLaunchKey(attempt)
			}
			return out, err
		}
		if !canTell {
			return out, err
		}
		found := c.findLaunched(ctx, room, in.Cwd, known)
		if found == nil {
			// The key stays: the retry sends it, and the room answers with the card if it turns up.
			return out, err
		}
		t = *found
		out.Recovered = true
	} else if t.LaunchReplay {
		out.Recovered = true
	}
	forgetLaunchKey(attempt)
	out.Model, out.Effort = t.Model, t.Effort
	out.Card, out.Handle, out.Title, out.Status = t.ID, t.Wire, t.Title, t.Status
	// A tag `item:<id>` links the card to a backlog item, which goes in progress with it. The link is the card as the
	// hub names it, room~id, the same name a report is followed from.
	itemNote := c.linkItemToCard(ctx, in.Tags, room, t.ID, spawnedBy)
	if room != callerRoom && room != "" {
		// Named the way atrium_say takes it from here.
		out.Card, out.Handle = tagFor(room, t.ID), t.Wire+"@"+room
	}
	if brief != "" {
		// The room wrote it; name it back in the same slash form the rest of
		// atrium carries, so the caller can add to the file it already reads.
		dir := in.Cwd
		if strings.TrimSpace(dir) == "" {
			dir = path.Dir(strings.TrimRight(strings.ReplaceAll(t.Worktree, "\\", "/"), "/"))
		}
		out.Brief = strings.TrimRight(strings.ReplaceAll(dir, "\\", "/"), "/") + "/" + "BRIEF.md"
	}
	// Where the human looks. Worth returning rather than leaving them to assemble
	// it, because the fragment form is not guessable.
	out.Watch = c.board + "/#term=" + url.PathEscape(t.ID)
	out.Note = LaunchStartedNote
	if out.Recovered {
		out.Note = LaunchRecoveredNote
	}
	// A room older than launch options drops the fields without a word.
	missed := LaunchOptionsDropped(in.Model, in.Effort, in.Args, in.Env,
		t.Model, t.Effort, t.LaunchArgs, t.LaunchEnvKeys)
	missed = append(missed, LeanAgentsDropped(in.LeanAgents, in.LeanSkills, t.Tags)...)
	if SuperleanDropped(in.Superlean, t.Tags) {
		missed = append(missed, "superlean")
	}
	out.Note = filedNote + itemNote + LaunchDroppedWarning(missed) + out.Note
	return out, nil
}

// LaunchStartedNote is what a launch that took says, after any warning.
const LaunchStartedNote = "started. its permission requests go to the human on their board, so it will " +
	"stop at the first gated command unless somebody is watching."

// LeanAgentsDropped names "lean_agents" and "lean_skills" when they were asked
// for and the card the room handed back carries no `atrium:agent:` or
// `atrium:skill:` tag, which is how a room that ignored the fields shows. Shared
// with the stdio control MCP.
func LeanAgentsDropped(agents, skills, gotTags []string) []string {
	has := func(prefix string) bool {
		for _, t := range gotTags {
			if strings.HasPrefix(strings.TrimSpace(t), prefix) {
				return true
			}
		}
		return false
	}
	var missed []string
	if len(agents) > 0 && !has("atrium:agent:") {
		missed = append(missed, "lean_agents")
	}
	if len(skills) > 0 && !has("atrium:skill:") {
		missed = append(missed, "lean_skills")
	}
	return missed
}

// LaunchOptionsDropped names the launch options that were asked for and that
// the card the room handed back does not carry, which is how an older room
// that ignored them shows. See docs/runtime/launch-options-design.md "Version skew".
// Shared with the stdio control MCP in internal/cli, so the two cannot drift.
func LaunchOptionsDropped(model, effort string, args []string, env map[string]string,
	gotModel, gotEffort string, gotArgs, gotEnvKeys []string) []string {

	var missed []string
	// Model has been on /v1/launch since 0047, so only a very old room drops
	// it, and it is checked the same way for completeness.
	if strings.TrimSpace(model) != "" && gotModel == "" {
		missed = append(missed, "model")
	}
	if strings.TrimSpace(effort) != "" && gotEffort == "" {
		missed = append(missed, "effort")
	}
	if len(args) > 0 && len(gotArgs) == 0 {
		missed = append(missed, "args")
	}
	if len(env) > 0 && len(gotEnvKeys) == 0 {
		missed = append(missed, "env")
	}
	return missed
}

// LaunchDroppedWarning is the sentence put ahead of the note when a room
// dropped launch options, empty when it dropped none. The session is running,
// and exiting it is the caller's call.
func LaunchDroppedWarning(missed []string) string {
	if len(missed) == 0 {
		return ""
	}
	verb := " were"
	if len(missed) == 1 {
		verb = " was"
	}
	return "WARNING: the room is older than launch options, so " + strings.Join(missed, ", ") +
		verb + " NOT applied and the session is running on the runner's defaults. "
}

// ── exit ────────────────────────────────────────────────────────────────────────

type exitInput struct {
	Card  string `json:"card,omitempty" jsonschema:"the card to exit: an id, handle or alias, name@room or room~id on another room. LEAVE IT OUT TO EXIT YOURSELF. never copy it from an atrium_say reply: that names who you spoke to"`
	Force bool   `json:"force,omitempty" jsonschema:"exit a card that is neither you nor one you launched. recorded on that card. a director cannot be exited by an agent at all"`
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
	me := agentOf(req)
	// NO CARD IS YOU. The id an atrium_say reply gives is the recipient's, and a
	// worker once took it for its own and exited the director that launched it.
	who := strings.TrimSpace(in.Card)
	if who == "" {
		if me == "" {
			return nil, out, fmt.Errorf("say which card to exit. there is no session calling this to exit")
		}
		who = me
	}
	scope, id, handle, err := c.resolveCardStrict(ctx, room, who)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = namedFrom(room, scope, id, handle)
	// The room that owns the card decides. See api.guardExit. Another room's
	// card is asked about by a name that is foreign there.
	var body map[string]any
	if me != "" {
		body = map[string]any{"from": me, "force": in.Force}
		if !equalFold(scope, room) {
			body["from"], body["foreign"] = me+"@"+room, true
		}
	}
	if err := c.ask(ctx, http.MethodPost,
		"/v1/tasks/"+url.PathEscape(id)+"/exit", scope, body, nil); err != nil {
		return nil, out, err
	}
	out.Asked = true
	out.Note = "asked to leave with its harness's exit keys. the card and its history stay."
	return nil, out, nil
}

// ── model ───────────────────────────────────────────────────────────────────────

type modelInput struct {
	Card  string `json:"card" jsonschema:"a card id, handle or alias. name@room or room~id for a card on another room"`
	Model string `json:"model" jsonschema:"sonnet, opus, haiku, fable, or a full id such as claude-opus-4-7"`
}

type modelOutput struct {
	Card      string `json:"card"`
	Handle    string `json:"handle,omitempty"`
	Model     string `json:"model"`
	Was       string `json:"was,omitempty"`
	Typed     bool   `json:"typed"`
	Delivered string `json:"delivered,omitempty"`
	When      string `json:"when,omitempty"`
	Note      string `json:"note,omitempty"`
}

// modelHandler forwards a model switch to the room holding the card, which
// checks everything: that atrium owns a claude terminal for it, and the shape of
// the model. See internal/daemon/modelswitch.go.
func (c *controlMCP) modelHandler(ctx context.Context, req *mcp.CallToolRequest, in modelInput) (
	*mcp.CallToolResult, modelOutput, error) {

	out := modelOutput{}
	room := roomOf(req)
	scope, id, handle, err := c.resolveCard(ctx, room, in.Card)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = namedFrom(room, scope, id, handle)
	body := map[string]string{"model": strings.TrimSpace(in.Model)}
	if me := agentOf(req); me != "" {
		body["from"] = me
	}
	var res struct {
		Model     string `json:"model"`
		From      string `json:"from"`
		Typed     bool   `json:"typed"`
		Delivered string `json:"delivered"`
		When      string `json:"when"`
		Note      string `json:"note"`
	}
	if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/model", scope, body, &res); err != nil {
		return nil, out, err
	}
	out.Model, out.Was, out.Typed, out.Delivered, out.When, out.Note =
		res.Model, res.From, res.Typed, res.Delivered, res.When, res.Note
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

// ── cull ────────────────────────────────────────────────────────────────────────

// cullTimeout bounds atrium_cull's one call to the room. Longer than
// controlTimeout, because the room waits out the worker's exit and then runs git
// before it answers.
const cullTimeout = 60 * time.Second

type cullInput struct {
	Card string `json:"card" jsonschema:"the worker to cull: a card id, handle or alias. name@room or room~id for a card on another room"`
	Into string `json:"into,omitempty" jsonschema:"the branch its branch must be merged into. default claude/main"`
	Hold bool   `json:"hold,omitempty" jsonschema:"keep the worker instead of culling it: cancels the automatic cull for good"`
	Tip  string `json:"tip,omitempty" jsonschema:"a merge proof made where the area branch lives: the commit it was checked to contain. the room culls only if its worktree is on it"`
}

type cullOutput struct {
	Card            string `json:"card"`
	Handle          string `json:"handle,omitempty"`
	Exited          bool   `json:"exited"`
	Branch          string `json:"branch,omitempty"`
	Into            string `json:"into,omitempty"`
	Worktree        string `json:"worktree,omitempty"`
	WorktreeRemoved bool   `json:"worktree_removed"`
	BranchDeleted   bool   `json:"branch_deleted"`
	BriefRemoved    bool   `json:"brief_removed"`
	ScratchRemoved  bool   `json:"scratch_removed"`
	BytesFreed      int64  `json:"bytes_freed"`
	Kept            string `json:"kept,omitempty"`
	Note            string `json:"note,omitempty"`
}

// cullHandler forwards a cull to the room, which makes every check. The one
// check made here is the caller not being the card, because only the hub reads
// who is calling: a worker's own done is not the acceptance.
func (c *controlMCP) cullHandler(ctx context.Context, req *mcp.CallToolRequest, in cullInput) (
	*mcp.CallToolResult, cullOutput, error) {

	out := cullOutput{}
	room := roomOf(req)
	scope, id, handle, err := c.resolveCardStrict(ctx, room, in.Card)
	if err != nil {
		return nil, out, err
	}
	out.Card, out.Handle = namedFrom(room, scope, id, handle)
	// Only a card on the caller's own room can be the caller.
	if me := agentOf(req); me != "" && equalFold(scope, room) {
		if myID, _, err := c.resolvePeer(ctx, room, me); err == nil && myID == id {
			return nil, out, fmt.Errorf("a worker cannot cull itself. whoever accepts the work culls it")
		}
	}
	if in.Hold {
		// A route of its own, so a room that predates the hold answers 404
		// rather than reading the flag as an ordinary cull.
		by := agentOf(req)
		if by == "" {
			by = "atrium_cull"
		}
		// To the card's own room, which is not the caller's for name@room (f-010).
		if err := c.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/cull/hold", scope,
			map[string]string{"by": by}, nil); err != nil {
			var be *boardError
			if errors.As(err, &be) && be.bare && be.code == http.StatusNotFound {
				return nil, out, fmt.Errorf("that room is older than the merged-cull hold. nothing was culled " +
					"and nothing was held. update the room")
			}
			return nil, out, err
		}
		out.Note = "held. it will not be culled automatically, only by an explicit atrium_cull."
		return nil, out, nil
	}
	long := &controlMCP{board: c.board, client: &http.Client{Timeout: cullTimeout, Transport: c.client.Transport}}
	var res cullOutput
	// `tip` is a merge proof made on the machine the area branch is on. It goes to
	// the room as it came, flat beside `into`: the hub never makes or checks one.
	body := map[string]string{"into": strings.TrimSpace(in.Into)}
	if tip := strings.TrimSpace(in.Tip); tip != "" {
		body["tip"] = tip
	}
	err = long.ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/cull", scope, body, &res)
	if err != nil {
		var be *boardError
		if errors.As(err, &be) && be.bare && be.code == http.StatusNotFound {
			return nil, out, errors.New(predatesCull(scope, c.hub.buildOf(scope)))
		}
		return nil, out, err
	}
	res.Card, res.Handle = out.Card, out.Handle
	switch {
	case res.ScratchRemoved && res.BriefRemoved:
		res.Note = "reclaimed: asked to leave, BRIEF.md and its scratch directory removed. the card and its history stay."
	case res.WorktreeRemoved && res.BranchDeleted:
		res.Note = fmt.Sprintf("culled: asked to leave, worktree removed (%d bytes freed, %.1f MB), branch deleted. "+
			"the card and its history stay.", res.BytesFreed, float64(res.BytesFreed)/(1<<20))
	default:
		res.Note = "not everything was removed. see kept, which says why."
	}
	return nil, res, nil
}

// ── close ───────────────────────────────────────────────────────────────────────

// closeTimeout bounds a confirmed atrium_close. The room stops the session, then
// runs git on each worktree before it answers. A preview is a read and gets
// cullTimeout.
const closeTimeout = 5 * time.Minute

type closeInput struct {
	Card    string            `json:"card" jsonschema:"the card to close: a card id, handle or alias. name@room or room~id for a card on another room"`
	Confirm bool              `json:"confirm,omitempty" jsonschema:"false (the default) only answers what a close would do. true closes"`
	Answers map[string]string `json:"answers,omitempty" jsonschema:"keep, stash or delete for each asked worktree, by its seq"`
}

// closeHandler forwards a close to the card's room, which makes every check and
// does the work. It is the same route the CLI's atrium_close and the board use.
func (c *controlMCP) closeHandler(ctx context.Context, req *mcp.CallToolRequest, in closeInput) (
	*mcp.CallToolResult, map[string]any, error) {

	out := map[string]any{}
	room := roomOf(req)
	scope, id, handle, err := c.resolveCardStrict(ctx, room, in.Card)
	if err != nil {
		return nil, out, err
	}
	path := "/v1/tasks/" + url.PathEscape(id) + "/close"
	if !in.Confirm {
		long := &controlMCP{board: c.board, client: &http.Client{Timeout: cullTimeout, Transport: c.client.Transport}}
		err = long.ask(ctx, http.MethodGet, path, scope, nil, &out)
	} else {
		long := &controlMCP{board: c.board, client: &http.Client{Timeout: closeTimeout, Transport: c.client.Transport}}
		err = long.ask(ctx, http.MethodPost, path, scope, map[string]any{"confirm": true, "answers": in.Answers}, &out)
	}
	if err != nil {
		var be *boardError
		if errors.As(err, &be) && be.bare && be.code == http.StatusNotFound {
			return nil, out, fmt.Errorf("that room cannot close cards yet. it needs a newer build")
		}
		return nil, out, err
	}
	out["card"], out["handle"] = namedFrom(room, scope, id, handle)
	return nil, out, nil
}

// ── restart ──────────────────────────────────────────────────────────────────────

type restartInput struct {
	Why         string `json:"why,omitempty" jsonschema:"what this restart is for"`
	Force       bool   `json:"force,omitempty" jsonschema:"restart even if other agents are still working"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for other agents, in seconds"`
	Immediate   bool   `json:"immediate,omitempty" jsonschema:"skip the wrap-up: do not ask working sessions to finish their step and be woken after"`
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
		Why: strings.TrimSpace(in.Why), Force: in.Force, WaitSeconds: in.WaitSeconds, Immediate: in.Immediate,
	}); err != nil {
		return nil, out, fmt.Errorf("could not ask %s to restart: %w", room, err)
	}
	out.Scheduled = true
	out.Note = "asked " + room + " to restart. it asks its working agents to wrap up and queues a wake on each, then winds down and " +
		"comes back on the same database. if this session is on that room it goes down too: say " +
		"nothing further this turn and expect to be resumed."
	return nil, out, nil
}

// ── wake after restart ────────────────────────────────────────────────────────────

type wakeInput struct {
	Text  string `json:"text,omitempty" jsonschema:"the prompt to receive once you are back. at most 2000 characters"`
	Clear bool   `json:"clear,omitempty" jsonschema:"cancel the wake queued for your card instead"`
}

type wakeOutput struct {
	Card     string `json:"card"`
	Queued   bool   `json:"queued"`
	Cleared  bool   `json:"cleared,omitempty"`
	Replaced string `json:"replaced,omitempty"`
	Note     string `json:"note,omitempty"`
}

// wakeHandler queues or clears the caller's own after-restart wake. Only its own
// card: a wake on somebody else's card would be a forced turn, which this is
// not. See docs/runtime/restart-wake.md. The room keeps the queue and does the typing.
func (c *controlMCP) wakeHandler(ctx context.Context, req *mcp.CallToolRequest, in wakeInput) (
	*mcp.CallToolResult, wakeOutput, error) {

	out := wakeOutput{}
	me := agentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("atrium_wake_after_restart is for a session atrium knows. this call did " +
			"not say which one it is")
	}
	room := roomOf(req)
	id, _, err := c.resolvePeer(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	out.Card = id
	path := "/v1/tasks/" + url.PathEscape(id) + "/restart-wake"
	if in.Clear {
		var res struct {
			Cleared bool `json:"cleared"`
		}
		if err := c.ask(ctx, http.MethodDelete, path+"?by="+url.QueryEscape(me), room, nil, &res); err != nil {
			return nil, out, err
		}
		out.Cleared = res.Cleared
		if !res.Cleared {
			out.Note = "there was no wake on your card."
		}
		return nil, out, nil
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, out, fmt.Errorf("say what to type in when you are back, or pass clear")
	}
	var res struct {
		Queued   bool   `json:"queued"`
		Replaced string `json:"replaced"`
		Note     string `json:"note"`
	}
	if err := c.ask(ctx, http.MethodPost, path, room,
		map[string]string{"text": in.Text, "by": me}, &res); err != nil {
		return nil, out, err
	}
	out.Queued, out.Replaced, out.Note = res.Queued, res.Replaced, res.Note
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
