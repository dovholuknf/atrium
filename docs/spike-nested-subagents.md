# Spike: child runners, spawned by an agent, grouped under it

A spike, not an implementation. Nothing here is built and no other file in the repository was touched.

## What is being asked for

An agent running under atrium can start another runner. The child is a runner of any configured kind, codex or
opencode or ollama or claude, not necessarily the same kind as its parent. The board and the strip then show the
child UNDER the parent that started it, so the operator can see which agent is responsible for which work.

This is not Claude Code's in-process `Task` subagent. That is a different thing, atrium already tracks it, and
the difference is the whole design, so it is separated out at the end under "The other kind of subagent".

The distinction that matters: a child runner is a real process, in a directory, under a pty atrium owns, with a
card, a wire name, a permission gate and a terminal you can attach to. It is a first class session that happens
to have a parent.

## Recommendation

**Add `parent_id` to the card. Detect it from the environment at launch. Draw the child as an indented row under
its parent in the strip and on the board.**

Do NOT use a naming convention. It fails here for a mechanical reason given below, and it fails harder for child
runners than it would for in-process helpers.

Most of this already exists:

- **Spawning a child of any runner type already works.** `atrium launch --runner codex --cwd ... --prompt ...`
  is a shipped command (`internal/cli/launch.go:44`). Runners are rows in the `harness` table, not code
  (`internal/store/schema.go:141`), so codex and ollama are seeded and opencode is a row somebody adds. An agent
  has a shell, so an agent can already do this today. What it cannot do is say who it was.
- **The parent is already in the child's environment.** `internal/daemon/launch.go:659` puts `ATRIUM_AGENT_NAME`
  and `ATRIUM_TASK_ID` into every runner it starts. An agent that runs `atrium launch` from its own terminal is
  running it with `ATRIUM_TASK_ID` set to its own card. The parent link needs no flag, no hook and no protocol.
  It is sitting in the environment of the process making the call.
- **The child is already a strip row.** The strip draws `all.filter(t => t.supervised || t.pinned)`
  (`internal/api/web/js/terminal-list.js:774`). A launched child is supervised, so it already appears. It appears
  in the wrong place, which is the entire bug.

So the feature is: record the parent, and draw the tree by it. The spawning, the runner-agnosticism, the
terminal, the gate and the peer bus are all already there.

## How atrium finds out

For child runners this question mostly dissolves: **atrium is the one doing the spawning.** The launch is the
event. There is no observation problem, no pty scraping, no hook to wait on.

Three ways in, in the order they should be trusted:

1. **`ATRIUM_TASK_ID` in the caller's environment.** The default, and free. `atrium launch` reads it and sends it
   as the parent. An agent gets the right answer by doing nothing.
2. **`--parent <card-id-or-wire-name>`, an explicit flag.** For a script, for a launch from outside any session,
   and to override the environment. Follows `--tags`, `--prompt` and `--external`, which exist for exactly this
   reason per `CLAUDE.md`.
3. **The board's own "new agent here".** `terminal-list.js:260` already offers this from a card's context menu.
   It knows the card it was opened on, so it can pass a parent without asking.

Two hazards, both already half-handled in the tree:

**A shell is not the agent.** `internal/daemon/shell.go:238` gives a card's shell `ATRIUM_TASK_ID` and
DELIBERATELY not `ATRIUM_AGENT_NAME`, so that anything started from it does not file activity as though the
agent had done it. For parenting, the task id is the right thing to inherit: a launch typed by a human into a
card's shell is still a child of that card. But the reasoning in that comment should be read before wiring this,
because it is the same environment variable being used for a second purpose and the two could drift.

**The daemon's own card.** `internal/daemon/shell_test.go:220` records that a daemon launched from inside a
supervised session inherits `ATRIUM_TASK_ID`. If the daemon then launches anything itself, that stale value must
not become a parent. `launch.go:857` already treats `ATRIUM_TASK_ID` as atrium-owned and overrides it per launch,
so the mechanism to get this right exists. It has to be used deliberately rather than inherited by accident.

**What atrium does NOT get for free:** whether the child's runner reports anything after it starts.
`internal/claudeconf/target.go` knows how to install hooks for claude and codex. A runner with no hook support
gets a card, a terminal and a parent, and a blank activity badge, because nothing is posting to `/activity`. The
tree is correct and the liveness works, since the reaper asks the operating system about a pid rather than asking
the agent. The card just says less. That is the honest cost of runner-agnosticism and it should be stated on the
board rather than hidden, the same way `board.js:537` already says "this runner does not say which".

## Why the naming convention does not work

The suggestion was to name a child `github/dovholuknf/atrium:main-sub-whatever` and let the strip's existing tree
grouping do the work. It is a reasonable instinct and the strip really does build a tree from a label. It fails
on two specifics.

**The strip does not read a name. It derives one.** `terminalLabel` (`internal/api/web/js/solo.js:66`) does not
read `wire_name` or `display_title`. It computes the label from three card FIELDS: it splits `task.worktree` on
the separator, finds the last segment matching `task.repo`, takes that segment and the two before it as the
place, and joins that to `task.branch` with a colon. `termPathOf` (`terminal-list.js:340`) takes the halves back
from the same fields, NOT by splitting the label, and there is a comment saying why: a branch recorded as
`main:desktop-edge-win` made the split invent a directory, and the card grew a heading next to the repo it
belonged under. There is no name to apply a convention to.

**And the tree nests on the path only.** `termTree` (`terminal-list.js:367`) nests on `segs`, which is the place.
The branch is the leaf and is never a node, so `main` cannot become a heading. A child named
`...:main-sub-whatever` draws as a SIBLING leaf under the `atrium` heading, directly below its parent and looking
exactly like it. That is today's behaviour with a longer name.

**For child runners it is worse than that.** A child runner can be in a different directory, a different repo,
even on a different forge. Its derived label puts it under a completely different heading, and no naming
convention applied to a string can pull it back under a parent that the tree is placing by path. The whole point
of this feature is a child doing a different piece of work somewhere else, which is precisely the case the
derivation separates.

**Collisions are not cosmetic.** `wire_name` is `UNIQUE` (`internal/store/schema.go:38`) and it is the ADDRESS
that `atrium tell`, `atrium finish`, the peer bus and `/activity` all resolve a session by, through
`GetByWireName`. Two children of the same parent minted from the same runner type collide, the second insert
fails, and the second child silently never appears. Mint from an id instead and the name is unreadable, which
defeats the point.

**The parent exiting does nothing, which is the problem.** A convention is a string. Nothing in the store knows
the two cards are related, so when the parent dies the child keeps drawing under a heading for something that is
gone, and cleaning it up means parsing names in the sweep.

**A rename severs it silently.** Renaming is offered (`terminal-list.js:254`). Per `docs/architecture-v2.md` a
human override is never overwritten by an observed value, which is right and is exactly what breaks a convention:
rename the parent and every child is stale, with no way to notice.

A convention is a coincidence that looks like a relationship. The test is whether anything other than the
renderer can ask the question. Here the store cannot, the sweep cannot, and `atrium peers` cannot, which matters
because `internal/cli/control_peers.go:73` already tells the model that a peer is "A REAL SESSION, not a
subagent". For a child RUNNER that distinction goes the other way: a child is a real session and SHOULD be
addressable as a peer. A convention gets that accidentally right for the wrong reason and would break the moment
anybody tried to make it deliberate.

## Cost: the parent field

**Schema.** One `ALTER TABLE task ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`, appended to the END of the
migration slice per the rules in `CLAUDE.md`. An added column, not a changed constraint, so none of the table
rebuild pain of `0010` and `0014` applies. This is the cheap part.

**The foreign key is the trap, and the answer is not to cascade.** `PRAGMA foreign_keys = ON`
(`internal/store/store.go:464`), and `task` is already the parent of four `ON DELETE CASCADE` relationships.
`schema.go:506` records what that cost once: a `DROP TABLE` on `task` fires the cascades and takes every event,
permission, message and launch spec with it. A self-referencing cascade makes the card table recursive.

More importantly it would be WRONG here. A child runner is a live process with its own pty. Deleting the parent's
card must not delete a card whose process is still running, because the card is the only handle on it: delete the
row and the pty is orphaned, the reaper has nothing to check, and `atrium stop` cannot reach it. Per
`docs/supervision-design.md` a kill is not a stop, and this would be a kill nobody asked for, or worse, a
surviving process with no card at all.

**So the orphan rule is: clear the parent and promote the child to the root.** The child keeps its terminal, its
card and its id, and loses only its indent. `ON DELETE SET DEFAULT` expresses it, and the tree builder should ALSO
treat an unresolvable parent as no parent, so a dangling id from any other path renders rather than vanishes.

**The tree builder is the real work.** `termTree` today is a path walk over `segs`. A parent field makes it a
graph laid over that path tree, and two things follow:

- **Cycles.** A launch cannot create one, since a new card is created fresh and cannot be its own ancestor. A
  manual re-parent can. The renderer runs on every poll, so a cycle is a hung board. A visited set and a depth
  cap, easy to write and usually only written after it has hung once.
- **Two hierarchies disagreeing.** A child in a DIFFERENT repo belongs under its parent by lineage and under its
  own path by place, and something must decide. This is the case that actually happens here, unlike with
  in-process helpers where the paths are always identical. The recommendation is that **lineage wins and the
  child's own place is drawn on the row**, since the question being answered is "which agent is responsible for
  this", and the row already knows how to draw its full label when it is not under a matching heading
  (`termRow(t, deep)`, `terminal-list.js:429`).

**Folding a parent.** `termCount` already counts rows at any depth, so a folded parent can say what is inside it.
But the fold set is keyed by path (`at`, built in `termNodeHTML`), and a parent is a ROW, not a heading. Either
rows become foldable, which makes a row a control and adds a second kind of disclosure next to the heading's, or
children fold with the heading above them, which is not folding the parent. `terminal-list.js:492` states that a
strip heading is deliberately NOT a control, pointing at an open complaint in `docs/dispatch-queue.md` group F
about clicking the white space of a board heading. A disclosure triangle on the row, distinct from the row's own
click target, is the shape that does not walk into that complaint. It needs deciding before it is built.

**The rest.** `termOrder` sorts a flat list and has to sort within parents or children scatter. The pinned
grouping, the `uncategorized` bucket and the group headings are all written against a flat list. The board's own
card rendering needs the same treatment or the two views disagree about who owns what.

**What comes free once the field exists.** The child is a full session, so `atrium tell` already lets parent and
child talk, `atrium peers` already lists them, and `atrium finish [recap]` is already the channel for a child to
say its work is over and what it did. A parent that spawns three children and waits for three recaps needs no new
protocol. That is worth knowing before anybody designs one.

## The other kind of subagent

Claude Code's in-process `Task` helper is a different thing and is already handled, so it should not be folded
into this.

`SubagentStart` and `SubagentStop` are wired (`internal/claudeconf/hooks.go:82`), carry `agent_id` and
`agent_type`, and `internal/daemon/activity.go` keeps a named, ordered, deduplicated list of the live ones per
card. `subagentList` (`internal/api/web/js/board.js:552`) already draws them indented under the card that spawned
them, with a rule for the stack view at `board.css:1641`.

It has to stay separate because such a helper has **no pid, no pty, no card and no wire name.** It runs inside
the parent's process. It can never be attached to, addressed, stopped or resumed. Drawing it as a row that looks
like a child runner would promise a click that cannot be honoured. It belongs as a line INSIDE its parent's row,
which is what the board already does, and the strip could reuse `subagentList` to match.

One small gap there: `SubagentStart` carries `user_input` (`docs/hook-coverage-spike.md:154`) and `hookInput` in
`internal/cli/hook.go` does not read it. Carrying it turns a line reading `explore` into one reading what the
explore was asked to find. A field on a struct, a field on the wire, and a truncation bound.

## What could not be determined

- **Whether opencode exists as a harness row and what its command line, prompt argument and resume argument
  are.** `harness.go:129` seeds claude, codex and ollama. Nothing in the tree mentions opencode. Adding it is
  configuration, but the three per-runner shapes (`PromptArgs`, `ResumeArgs`, `ModelArgs`) each have to be filled
  in from that tool's actual flags, and `harness.go:36` is explicit that an empty `PromptArgs` means a launch
  carrying a prompt is REFUSED rather than started on a session that will never read it. A child spawned with an
  instruction is the main case here, so this has to be right before opencode is usable as a child.
- **Whether opencode has any hook mechanism at all.** Decides whether a child of that kind ever shows activity,
  answers the permission gate, or can be reached by a queued message. Not answerable from this repository.
- **How deep this should be allowed to go.** A child can launch a grandchild by the same mechanism, since it has
  the same environment and the same command. Nothing here proposes a limit and nothing prevents one agent
  producing a tree of forty sessions. A depth cap, or a launch budget per card, is a policy decision nobody has
  made.
- **Whether the child should inherit the parent's auto-mode setting.** Auto mode is per session and board-wide
  (`docs/auto-mode.md`). A child spawned by an agent running unattended overnight that comes up asking for
  permission is a stall nobody sees. Inheriting silently is worse. Not resolved.
