package shared

// LaunchRequest is what the board sends to start a runner.
type LaunchRequest struct {
	Harness string `json:"harness"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	// Resume picks a conversation back up instead of starting a new one. The
	// value is the runner's own session id, recorded by the session hooks.
	Resume string `json:"resume"`
	// Respawn marks a launch rebuilt from an existing card (a reopen after a restart, an unpark, a runner restart).
	// It is not a first launch even when no conversation id could be passed, so the launch-inactivity notice stays
	// quiet. Never set from the wire.
	Respawn bool `json:"-"`
	// TaskID starts a runner onto a card that already exists, instead of making
	// one. Unshelving uses it: the card, its history and its resume id are the
	// reason to pick the work back up, so a second card would defeat the point.
	TaskID string `json:"task_id,omitempty"`
	// MovedFrom is `room~id` of the card this one is the successor of, on a move
	// between rooms. It is the key that makes a repeat launch for one move return
	// the same card. Pinned and PinOrder put it in the old card's pinned slot.
	MovedFrom string `json:"moved_from,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	PinOrder  int    `json:"pin_order,omitempty"`
	// LaunchKey is a random string the caller makes on its first attempt and sends again on a retry of the same
	// launch. A card already filed under it on this room is returned, marked `launch_replay`, and nothing is started:
	// the answer to the first attempt may have been lost while the card went on being made. Optional, and a launch
	// without one is as it always was. Ignored on a launch onto a card (`task_id`).
	LaunchKey string `json:"launch_key,omitempty"`
	// Tags are what the operator calls this work. A script that starts a
	// session from a ticket knows things the path does not.
	Tags []string `json:"tags,omitempty"`
	// Prompt is the first instruction the runner gets, handed over as the
	// harness's PromptArgs say to. This is how a card raised from an issue
	// starts with the issue in front of it rather than at an empty cursor.
	Prompt string `json:"prompt,omitempty"`
	// Brief is context to hand the new session, written to BRIEF.md in its
	// directory before it starts and read first.
	//
	// A FILE, NOT A LONGER PROMPT, and that is the whole difference. A prompt is
	// said once, is the first thing to fall out of a compaction, and cannot be
	// consulted afterwards. A file in the working directory can be re-read at any
	// point, survives compaction, and is there when a human takes the card over.
	// See writeBriefFile. Written on the room's own machine, which is why this
	// runs here rather than wherever the caller was: the hub has no directory to
	// write to. Ignored on a resume, which continues a conversation and takes no
	// first prompt.
	Brief string `json:"brief,omitempty"`
	// Model names the model this session runs on, handed over as the harness's
	// ModelArgs say to.
	//
	// ONE TIME WITH RESPECT TO THE HARNESS, STICKY WITH RESPECT TO THE CARD.
	// The form's control is unticked every time it opens, so nobody has to
	// remember to turn it back, and nothing is written to the runner's row.
	// The card keeps it, and `reopenSaved` replays it, so a session that
	// started on a model stays on it across a restart.
	//
	// Naming one for a harness with no ModelArgs is REFUSED. See runnerArgs.
	Model string `json:"model,omitempty"`
	// Effort is the thinking effort, handed over as the harness's EffortArgs or
	// EffortEnv say to. Args and Env are extra argv and environment for the
	// runner, used as given. All three are sticky with respect to the card, as
	// Model is, and none is checked against a list. See
	// docs/runtime/launch-options-design.md.
	Effort string            `json:"effort,omitempty"`
	Args   []string          `json:"args,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	// Source, ExternalID and URL record where this work came from. See
	// store.SetOrigin and docs/runtime/intake-design.md.
	Source     string `json:"source,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	URL        string `json:"url,omitempty"`
	// SourceKind and SourceURL are the same two under the names a launcher
	// resolving a URL calls them.
	//
	// TWO NAMES FOR ONE THING, on purpose and only here. `source` and `url`
	// are what the store and the intake path have always called them, and
	// renaming those would touch every source ever configured. A caller that
	// turned `github.com/o/r/pull/9` into a worktree is thinking in the other
	// vocabulary, and refusing it over a word is a round trip for nothing.
	// The explicit pair wins when both are sent.
	SourceKind string `json:"source_kind,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	// What a launcher knows about the work that the directory cannot say.
	//
	// ATRIUM IS NOT LEARNING GIT. None of this is derived here and none of it
	// is checked: whoever resolved the URL and made the worktree already knows
	// the answers, and a daemon that re-derives them is a second implementation
	// to disagree with the first. Every one of them is optional, and a session
	// that joined on its own leaves them all empty.
	Repo   string `json:"repo,omitempty"`
	Org    string `json:"org,omitempty"`
	Host   string `json:"host,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Base is where a new branch starts, and Home asks for a home with no repo. With `repo` as `host/owner/repo` or a
	// path to a clone and no cwd, the room makes the card's home and adds `<home>/work` as a worktree on Branch. See
	// cardhome.go.
	Base string `json:"base,omitempty"`
	Home bool   `json:"home,omitempty"`
	// Interactive marks a launch somebody pressed, as opposed to one that
	// happened on its own.
	//
	// NOT ON THE WIRE. Set by the board's launch handler and by nothing else,
	// so a fixture coming up at boot, a source's queued launch and a peer
	// asking for one are all automated by default. The one thing it decides is
	// whether the runner version check is allowed to hold the launch up: there
	// is somebody in front of an interactive launch to read the answer, and
	// nobody in front of the others. See runnerupdate.go.
	Interactive bool `json:"-"`
	// Window is which pile this card belongs to, and it is the board's
	// grouping key. `active-work`, `pull-requests`, `tangent`, `discourse`, a
	// repo name, or anything else: atrium stores the string and groups by it
	// without knowing what any of them mean.
	Window string `json:"window,omitempty"`
	// Theme is the terminal palette this session comes up in. A launcher that
	// keeps a repo-to-color map sends the answer rather than atrium keeping a
	// second copy of that map. Empty leaves it to the board.
	Theme string `json:"theme,omitempty"`
	// Throwaway requests a temporary directory, with directory, card, and
	// transcript cleanup at exit. Ignore it when a directory or card was supplied.
	Throwaway bool `json:"throwaway,omitempty"`
	// IfRunning is what to do when this directory already has a card.
	//
	// FOR CALLERS THAT ARE NOT A PERSON. A human pressing start in the launch
	// dialog is looking at the board and means it. A script is not: `gwt new`
	// run twice, or run on a worktree that already has a pinned session, would
	// otherwise put a second runner in a directory that has one, and the two
	// write to the same files while neither knows about the other.
	//
	//	""       refuse when a live or parked card holds the directory, naming it
	//	"skip"   hand back the card that is there and start nothing
	//	"adopt"  start onto that card, unless something is already running on it
	//
	// The same question `startFixture` answers, moved to where every caller
	// can ask it. It lived in that one caller, which is why the endpoint under
	// it never asked.
	IfRunning string `json:"if_running,omitempty"`
	// SpawnedBy is the handle of the session asking for this launch, sent by
	// the hub's `atrium_launch` from the caller's own identity header. The
	// board's dialog sends nothing and is recorded as `@human`. See
	// store.SetLineage.
	SpawnedBy string `json:"spawned_by,omitempty"`
	// SpawnedByID is the launcher's card on ANOTHER room, as `room~id`, sent by
	// the hub when it launches here for a session on another room. Taken only
	// in that tagged form, so it can never name a card on this room. See
	// docs/fabric/cross-room-say-design.md.
	SpawnedByID string `json:"spawned_by_id,omitempty"`
	// SpawnedByDir is where that launcher works, read by the hub from its card. Used only to ask git whether it
	// shares a repository with this launch, so a path that is not on this machine means nothing.
	SpawnedByDir string `json:"spawned_by_dir,omitempty"`
	// Lean starts a claude session with only what a worker needs, and MCP names
	// the servers from the runner's MCP config it keeps beside atrium-control.
	// Recorded on the card as tags, so a reopen starts it lean again. See lean.go.
	//
	// A pointer, because absent and false differ. Absent leaves it to the card.
	// False starts a lean card with the full setup and takes the lean tags off
	// it, so its next reopen is not lean either.
	Lean *bool    `json:"lean,omitempty"`
	MCP  []string `json:"mcp,omitempty"`
	// Superlean starts a lean worker with a smaller tool list, no mercurius and a one paragraph prompt. A pointer like
	// Lean: absent leaves it to the card, false brings a superlean card back lean. Implies lean, refused with `lean:
	// false`, claude only, never an interview. Recorded as the tag atrium:superlean beside atrium:lean. See
	// docs/runtime/superlean-design.md.
	Superlean *bool `json:"superlean,omitempty"`
	// LeanAgents keeps the Agent tool on a lean launch and makes just these
	// agents (the operator's ~/.claude/agents/<name>.md) available to it, and
	// LeanSkills the Skill tool and these skills (~/.claude/skills/<name>/). A
	// name with no file refuses the launch. Claude only, and either starts lean.
	// Recorded on the card as `atrium:agent:` and `atrium:skill:` tags. See
	// lean_agents.go.
	LeanAgents []string `json:"lean_agents,omitempty"`
	LeanSkills []string `json:"lean_skills,omitempty"`
	// ReportTo is who this card's reports go to: a handle, alias or card id on
	// this room, resolved as `atrium_say` resolves one. The card's launcher is
	// set to that card, and the name is kept as given and resolved again at each
	// delivery. An unknown name refuses the launch. See reportto.go.
	//
	// THE BOARD AND THE CLI ONLY. A launch carrying the agent marker is refused
	// for sending it, so no session can point another card's reports at a third
	// party. It sets the launcher and nothing else: no tag, room or permission.
	ReportTo string `json:"report_to,omitempty"`

	// OutsideCode says this card runs code that is not the room's own (a PR's tests, `prove`, an outside target). It
	// gets no git token in its environment, so that code cannot read it and push as the card. It can only take
	// something away, so any caller may set it. The tag atrium:outside-code says the same and survives a reopen.
	OutsideCode bool `json:"outside_code,omitempty"`

	// Scratch lets an agent launch start in a directory that is not a git checkout on a claude/* branch. Without it
	// such a launch is refused, so what an agent writes is kept in git. See agentCheckoutRefusal.
	Scratch bool `json:"scratch,omitempty"`

	// Lifetime is `work` for a launch from atrium_launch_worker: atrium exits the runner once the card's branch
	// merges. Empty leaves a relaunched card's own value and gives a new card none. Anything but `work` is refused.
	Lifetime string `json:"lifetime,omitempty"`
	// Item starts the card as an item: the ItemTag, an ending that names no launcher, atrium as the opening prompt's
	// sender, and no say. See item.go. Since item lifecycle L1 this is what `isolated` says, and it stays an alias of it
	// for one release: what makes a card an item is the attachment, WorkItem.
	Item bool `json:"item,omitempty"`
	// Isolated is Item, said plainly: the isolated toolset and endings, and nothing about which item the card is on.
	Isolated bool `json:"isolated,omitempty"`
	// WorkItem is the backlog id of the item this card is attached to. It becomes the tag `atrium:item:<id>`, which
	// travels with the card, and the assignment pushed to the hub names it. Launchers resolve `new:` before they post, so
	// this is always an id. A launch from a script that sends none is attached to nothing and unchanged.
	WorkItem string `json:"work_item,omitempty"`
}
