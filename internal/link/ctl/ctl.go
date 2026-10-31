// Package ctl holds the vocabulary the control tools share, below the feature packages that use it.
package ctl

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// AgentHeader names the calling session. The room header is `RoomHeader`, shared
// with the board, so a control call scopes exactly the way a board request does.
//
// LANDMINE THAT PICKED THESE NAMES: Claude Code blanks any header whose name
// contains TOKEN, SECRET, KEY, PASSWORD or AUTH. `X-Atrium-Agent` and
// `X-Atrium-Room` avoid all five.
const AgentHeader = "X-Atrium-Agent"

// BoardError is a refusal from the board, with its status kept. The sentence
// is the whole of what a tool shows. The code is what the relay reads to tell
// a room that is not answering from a refusal that will never succeed.
type BoardError struct {
	Code int
	Msg  string
	// NoSentence is an answer with no sentence in it, which is what a route the room
	// does not have looks like. A room older than an endpoint answers a bare 404.
	NoSentence bool
}

func (e *BoardError) Error() string { return e.Msg }

// HTTPStatus is what the board answered, for a caller that reads a 404 as "no such thing".
func (e *BoardError) HTTPStatus() int { return e.Code }

// Bare is whether the answer carried no sentence, which is a route that is not there rather than a thing that is not.
func (e *BoardError) Bare() bool { return e.NoSentence }

// RefusedError is a call turned away by a rule rather than one that failed, such
// as the launch cap. The audit line says `refused:` for it, which is the line
// somebody looks for, and a plain error is written as it came.
type RefusedError struct{ Msg string }

func (e *RefusedError) Error() string { return e.Msg }

// AgentOf and RoomOf read the per-request identity headers the go-sdk hands a
// tool on `Extra`. Absent when the caller is not one of atrium's sessions,
// which is an ordinary way to use these tools and not an error.
func AgentOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(AgentHeader))
}

func RoomOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(wire.RoomHeader))
}

// Class is which set of control tools a caller gets.
type Class int

const (
	// ClassFull is every tool. The default, and what any doubt resolves to.
	ClassFull Class = iota
	// ClassWorker is the tools a worker uses to talk and to report.
	ClassWorker
	// ClassWorkerResident and ClassFullResident are those two with the done and blocked words of a card that stays open.
	ClassWorkerResident
	ClassFullResident
	// ClassWorkerItem is the worker set of an item: no say, no peers, no task and no report, and the done and blocked
	// words that name no launcher. See ItemTag.
	ClassWorkerItem
	// ClassWorkerReview is the worker set and atrium_review, for a card tagged ReviewTag.
	ClassWorkerReview
)

// LaunchInput is the parameters of atrium_launch.
type LaunchInput struct {
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
	LeanAgents []string `json:"lean_agents,omitempty" jsonschema:"agents a lean claude worker can start, by name: the files in the operator's ~/.claude/agents on the room, without .md. it keeps the Agent tool and ONLY these. Explore and Plan are Claude Code's own built-in agents and need no file, started by their bare name. any other name is namespaced as atrium:<name>. a name with no file refuses the launch. implies lean. claude only. the card keeps the list"`
	LeanSkills []string `json:"lean_skills,omitempty" jsonschema:"skills a lean claude worker can use, by name: the directories in the operator's ~/.claude/skills on the room. it keeps the Skill tool and ONLY these. a name with no directory refuses the launch. implies lean. claude only. the card keeps the list"`
	// Superlean cuts a lean worker further. Absent leaves it to superleanLaunch.
	Superlean *bool `json:"superlean,omitempty" jsonschema:"start a claude worker SUPERLEAN: lean, and also only the tools Read, Edit, Write, Grep, Glob, Bash, PowerShell and ToolSearch, no mercurius unless mcp names it, and a one paragraph worker prompt. for a bounded change: edit files, run a command, commit, report. implies lean, refused with lean: false. claude only, never an interview. default true for an isolated item worker other than a review, false for anything else: false brings an item worker back lean"`
	// Model and Effort are mapped by the runner's harness row, Args and Env are
	// passed as given. See docs/runtime/launch-options-design.md.
	Model  string            `json:"model,omitempty" jsonschema:"which model the runner starts on, passed in the shape its runner row declares (claude and codex: --model). not checked against any list. empty is the runner's default. a runner with no way to take a model refuses"`
	Effort string            `json:"effort,omitempty" jsonschema:"thinking effort, passed in the shape its runner row declares (claude: --effort, codex: -c model_reasoning_effort=). not checked: whatever the runner accepts, such as low, medium or high for claude. empty is the runner's default. a runner with no way to take one refuses"`
	Args   []string          `json:"args,omitempty" jsonschema:"extra command-line arguments for the runner, one per element, added after the model and effort and before the prompt. used as given. shown on the card, so keep secrets out"`
	Env    map[string]string `json:"env,omitempty" jsonschema:"extra environment for the runner, used as given. ATRIUM_ names are refused. the values stay on the room and the card shows the names only"`
	// Room launches on another room. The worker's reports come back across.
	Room string `json:"room,omitempty" jsonschema:"launch on this room instead of your own. its reports and notices still reach you, as your-handle@your-room"`
	// Kind is what sort of session this is. Empty is an ordinary worker.
	Kind string `json:"kind,omitempty" jsonschema:"what sort of session to start. empty is an ordinary worker. interview starts an interviewer: atrium writes the interviewer rules into BRIEF.md ahead of your brief (put the topic and context in brief), tags the card atrium:interview, and does not nudge it for stopping on a question. its questions reach the human in its terminal, so tell the human to watch its card. it runs on opus unless model says otherwise. review starts a slop review of the branch checked out in cwd (or repo and branch): atrium writes the review rules into BRIEF.md, tags the card atrium:review, serves it atrium_review and does not nudge it. base names the branch it measures against, default claude/main. it is still a worker and still names an item"`
	// Scratch lets the launch start outside a git checkout.
	Scratch bool `json:"scratch,omitempty" jsonschema:"start in a directory that is not a git checkout on a claude/* branch. without this such a launch is refused, because what an agent writes there is not kept in git. say it only for throwaway work"`
	// Item is the backlog item the worker is on, REQUIRED. See WorkTagPrefix and work_launch.go.
	Item string `json:"item,omitempty" jsonschema:"REQUIRED: the backlog item this worker is on, by its id (find it with atrium_backlog list), or new:<a title> to file one as you launch, under your own department. every card launched on one id is attached to the same item. a launch with no item is refused"`
	// Isolated starts the worker with the isolated toolset. See ItemTag.
	Isolated  bool   `json:"isolated,omitempty" jsonschema:"start the worker ISOLATED: its ending lines never mention a launcher, its opening prompt comes from atrium, it has no atrium_say, atrium_peers, atrium_task or atrium_report, and atrium refuses any say from it. it ends with atrium_done or atrium_blocked and nothing else. leave it out for an ordinary worker, which is unchanged. this is no longer what makes a card an item: item is"`
	ItemOwner string `json:"item_owner,omitempty" jsonschema:"who owns the item and hears when it is blocked or finished. empty is you, the creating session. orchestrator or director is whoever holds that role when it ends. human is the person at the board, and no session is told. tester is refused, it has several holders"`
	// Lifetime is not a parameter: atrium_launch_worker sets it to `work`, atrium_launch leaves it empty.
	Lifetime string `json:"-"`
	// Task is a task of an item: the room makes the folder and writes the brief. Never a parameter. See tasks_launch.go.
	Task *TaskBlock `json:"-"`
}

// LaunchOutput is what atrium_launch answers.
type LaunchOutput struct {
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

// TaskBlock is a task of an item, as a launch carries it.
type TaskBlock struct {
	BacklogID string   `json:"backlog_id"`
	TaskID    string   `json:"task_id"`
	Attempt   int      `json:"attempt"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Brief     string   `json:"brief"`
	Outputs   []string `json:"outputs"`
	ItemTitle string   `json:"item_title"`
	Context   string   `json:"context,omitempty"`
	Learnings []string `json:"learnings,omitempty"`
}

// Caller is what a control tool asks of the hub's control server: a call to the board, a name resolved to a card, a
// message carried or a launch started on a room.
type Caller interface {
	Ask(ctx context.Context, method, path, room string, body, out any) error
	ResolvePeer(ctx context.Context, room, who string) (id, handle string, err error)
	ResolveCard(ctx context.Context, room, who string) (scope, id, handle string, err error)
	Relay(ctx context.Context, from string, req wire.RelayRequest) wire.RelayAnswer
	DeliverAs(ctx context.Context, room, to, fromWire, text, when, kind string, wake, force bool) wire.RelayAnswer
	LaunchOnRoom(ctx context.Context, in LaunchInput, harness, room, callerRoom,
		spawnedBy, spawnedByID, spawnedByDir string) (LaunchOutput, error)
}
