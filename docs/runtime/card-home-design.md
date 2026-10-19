# Card homes: every launched card works in one folder under one root per room

Backlog item `f-card-home-under-room-root`, high. Status: designed, not built.

clint, 2026-10-19 09:48: "new minions/cards get their own folder where they work. it needs to be located in some
'root atrium' folder per room so that i can find all the files/work that agents are doing and so atrium can reclaim
that space on disk... instead of all worktrees going into one 'worktree place' each card should be going into 'the one
atrium folder'. right now agents are littering the disk with files all over the place."

## What is there today

Nothing owns the space a card works in. The pieces that touch it:

- **The launch** (`internal/daemon/launch.go`, `LaunchRequest`). `cwd` must already exist and is used as given.
  `repo`, `org`, `host` and `branch` are labels atrium stores and never checks. `throwaway` makes a directory with
  `os.MkdirTemp("", "atrium-throwaway-")` (`throwaway.go`), which is the OS temp folder, `C:` on Windows, and removes it
  when the runner exits unless the card was promoted. `scratch` lets an agent launch run outside a `claude/*` checkout,
  and `agentCheckoutRefusal` refuses an agent launch anywhere else.
- **The launcher makes the worktree.** Atrium does not, for a launch. Worktrees end up in `D:\worktrees\...`,
  `atrium-worktrees`, `D:\tmp\<name>`, `C:\temp\...` and per-repo spots, wherever the launcher chose.
- **The board makes some.** A provider's `worktree_root` (`worktreeDest`, `<root>/<org>/<repo>/<branch>`), a link that
  opens a branch (`openkinds.go`), and a scratch folder at `<git.scm_root>/scratch/<host>/<name>` (`scratchDir`).
  `inScratch` lets a close remove a folder under that scratch root.
- **The room's clones** live under `git.scm_root` at `<host>/<owner>/<repo>` (`gitsync.ClonePath`), fetched from the
  hub.
- **Atrium's own litter**: `os.TempDir()/atrium-says` (`launchersay.go`) and `os.TempDir()/atrium/handoffs`
  (`newcontext.go`), both per card.
- **Reclaim** (`reclaim.go`, `docs/changes/r-card-reclaim-on-done.md`). `atrium_cull` from the launcher takes back the
  session, the worktree and its branch, BRIEF.md, and the directory only when that leaves it empty. It never calls
  `RemoveAll`, because a launcher made the directory and atrium cannot know what else is in it. Never a card clint
  started, never one whose work is not done (H4), never one tagged to stay open, never the main checkout.
- **The sweep** (`sweep.go`). A dead card is ARCHIVED after `SweepDeadAfter` (a minute): it leaves the board and keeps
  its row. A finished card is DELETED with its history after `prune_after` (offered as 30 days), and only when pruning
  is on.
- **The item-tasks design** (`docs/runtime/item-tasks-design.md`, accepted, not built) already has a room setting
  `task_root`, a room ledger table `task_dir`, folders atrium makes and so may remove whole, and removal by ledger row
  with links removed and never followed. This design generalises that to every card, and replaces both names (9).

The reclaim rule is the reason the disk fills. Atrium may only remove what it made, and today it makes almost nothing,
so it removes almost nothing.

## The decision in one paragraph

Each room has one **card root**. Every card atrium launches gets a **home**, one folder directly under that root, which
ATRIUM creates and records in a ledger row. The home holds the card's working directory (a git worktree, or an empty
folder for scratch work), its BRIEF.md and REPORT.md, and its temp folder. The runner's `TMP`, `TEMP` and `TMPDIR` point
into the home. Because the ledger row proves atrium made the folder, atrium may remove it whole when the card's work is
taken back. A launch that names a directory of its own still works, gets no home, and is reclaimed as it is today.

## 1. The card root

New room setting `card_root`, an absolute path.

| Set? | Then |
| --- | --- |
| set | that path |
| unset, `WORKTREE_ROOT` set | `$WORKTREE_ROOT/atrium-cards` |
| unset, no `WORKTREE_ROOT`, not Windows | `~/atrium/cards` |
| unset, no `WORKTREE_ROOT`, Windows | none. A launch that wants a home is refused with a sentence naming the setting |

Windows never falls back to the home folder, because that is `C:`, and keeping agent work off `C:` is half the point.
The value is derived on each read and never written back, the rule `EffectiveSCMRoot` keeps.

A value is refused, when it is set and again when it is read, if it is a drive root, the home folder or an ancestor of
it, inside the room's state directory (`daemon.StateDir()`), or inside `git.scm_root`. The last one keeps homes and
clones apart, so removing a home can never reach a clone and a clone walk never finds a home.

The board's room settings show the root, the drive's free space, and a sentence when it is unset on Windows.

## 2. The home

**Name.** `<card_root>/<slug>-<id6>`, flat, one level.

- `slug` is the card's title at launch, else its wire name, lowercased, every run of characters outside `[a-z0-9]`
  turned into one `-`, trimmed, cut at 40.
- `id6` is the last six characters of the card id. It makes the name unique without a counter, and it is what ties a
  folder found on disk back to a card.
- A task folder (item tasks) is `<backlog id>-<task id>-a<attempt>-<id6>`.

Card id alone would be unique and unreadable. A title slug alone would collide and say nothing about which card. The
pair is what a human scanning the root can use. The folder keeps its name when the card is renamed: the ledger row is
the link, not the name. Flat, because "find everything agents are doing" is then one listing, and because nesting
leaves empty parents behind (the reason `flattenBranch` exists).

**Inside it.**

```
<card_root>/fix-say-race-7q2k9x/
  BRIEF.md        written by the launch
  REPORT.md       written by the worker, if it writes one
  work/           the runner's cwd: a git worktree, or an empty folder for scratch work
  tmp/            TMP, TEMP and TMPDIR for the runner and every process under it
  .atrium/        what atrium keeps for this card: handoffs, says, nothing else
```

BRIEF.md and REPORT.md move OUT of the working directory to the home's top. Two reasons: a git worktree stays clean, so
the clean check before removal (5) and item-task capture mean what they say, and BRIEF.md stops showing as untracked in
every worktree. The launch text names the brief by its absolute path instead of "BRIEF.md in your directory".

`.atrium/` replaces `os.TempDir()/atrium-says/<card>` and `os.TempDir()/atrium/handoffs/<card>.md` for a card that has a
home. A card without one keeps the old places.

**The ledger.** New room table `card_home`, at the end of the room's migration slice, `CREATE TABLE IF NOT EXISTS`:

| Column | Meaning |
| --- | --- |
| `id` | ulid, primary key |
| `card` | the card id. Not a foreign key: the row outlives a pruned card (6) |
| `path` | the home, forward slashes, resolved at creation |
| `kind` | `worktree`, `scratch`, `task` or `throwaway`, in a `CHECK` |
| `clone`, `branch`, `base` | for `worktree` and code tasks: the clone the worktree was added from, its branch, its start |
| `state` | `live`, `kept` or `removed`, in a `CHECK` |
| `reason` | why it is `kept`, one sentence |
| `bytes`, `files`, `measured_at` | the last size walk (7) |
| `created_at`, `removed_at` | |

Index on `state`, and on `card`.

**Creation.** The room makes the home and refuses one that exists (it would belong to another card). It writes the
ledger row first with state `live`, then makes the folders, so a crash between the two leaves a row pointing at nothing,
which the next sweep marks `removed`, and never a folder with no row.

## 3. The launch: callers name a repo, not a directory

`atrium_launch` and `atrium_launch_worker` gain fields, and `LaunchRequest` with them:

| Field | Means |
| --- | --- |
| `repo` | the repository to work in: `host/owner/repo` (resolved through `gitsync.ClonePath`, cloned from the hub when the room has no clone yet), or an absolute path to a clone on the room |
| `branch` | the branch for the worktree. Made from `base` when it does not exist. Default `claude/<slug>` |
| `base` | where a new branch starts. Default the clone's `claude/main`, else its default branch |
| `home` | `true` asks for a home with no repo: an empty `work/` (scratch work) |

`repo`, `org`, `host` and `branch` exist today as labels. They keep that meaning when `cwd` is given, so no caller
changes behaviour by accident.

**What the room does**, by what the caller sent:

| Sent | Result |
| --- | --- |
| `repo` (with or without `branch`), no `cwd` | a home of kind `worktree`: `git worktree add <home>/work -b <branch> <base>` from the clone, as an argv, as `makeProviderWorktree` does |
| `home: true`, no `cwd` | a home of kind `scratch`. The card wears `atrium:keep-open`, as a scratch launch does now |
| `throwaway: true`, no `cwd` | a home of kind `throwaway` instead of `os.MkdirTemp`. Removed on exit (5) |
| `cwd` | no home. The directory is used as given, `agentCheckoutRefusal` applies, and reclaim is today's |
| `cwd` and `repo` | refused: "pass cwd for a directory you made, or repo for one atrium makes, not both" |

**Git worktree placement when the repo lives elsewhere.** The clone stays where it is: under `git.scm_root`, or wherever
the given path is. Only the worktree goes in the home. Git keeps a worktree's admin data in the clone's
`.git/worktrees/<name>` and a `.git` file in the worktree pointing back, so a worktree on another drive works. Removal
is `git worktree remove` run in the clone, then `git worktree prune`, so the clone's record goes with the folder. The
worktree's admin name is the home's name, so two cards on one clone never clash. The main checkout is never a home and
never touched: a worktree is added FROM it at most, and `inspectCull` keeps refusing it.

**The board** launches the same way. Its new-card form gains "new home" beside picking a directory, with repo and
branch fields. A link opened on a branch (`openkinds.go`) and a board scratch folder (`scratchDir`) become homes of kind
`worktree` and `scratch`. A provider's `worktree_root` stays for the board's own worktree list and is no longer where a
launch puts one.

**Agents after the change.** The worker brief in `AGENTS.md` and the launch tools' descriptions say: launch with
`repo` and `branch`, not with a worktree you made. A stage after this one (10, C4) makes the room refuse an agent launch
whose `cwd` is outside `card_root` unless it is `scratch: true` with a reason, so agents stop making worktrees of their
own. Launches by the human are never refused for this.

## 4. Temp files stay in the home

For a card with a home, the room sets in the runner's environment, through `childEnv`'s atrium map, so they win over
the harness and the machine:

- `TMP`, `TEMP` and `TMPDIR`: `<home>/tmp`
- `ATRIUM_HOME`: the home, for scripts and the brief

Every process the runner starts inherits them: shells, test runs, `go build` caches that honour `TMPDIR`, node. The
card's own shell terminal (`shell.go`, `shellEnv`) gets the same. Under `pty_host` the room passes the environment in
the spawn request as it does now, so nothing changes in the pty host.

What this does not catch, and the design says so rather than pretending: a tool that writes to a fixed path (`C:\temp`,
`D:\tmp`) or to its own cache under the home folder (Go's module cache, npm's cache, Claude Code's own `~/.claude`).
Those are shared caches, not litter, and stay where they are. The worker brief tells the worker to write nothing outside
its home and to use `$ATRIUM_HOME/tmp` for scratch files. The size walk (7) shows the human whether it worked.

## 5. Reclaim: when a home goes

A home is removed whole, by its ledger row. That is the change from today's "never `RemoveAll`": the row proves atrium
made the folder, so everything in it is the card's, and "cleanup only what atrium created" allows it.

**When.**

| Event | Home |
| --- | --- |
| `atrium_cull` by the launcher, card done (today's reclaim) | removed, after the checks below |
| item-task capture acknowledged by the hub | removed (item-tasks design 4) |
| throwaway runner exits, not promoted | removed |
| throwaway promoted | kept: the ledger row goes `kept` with "promoted by <who>". No move any more |
| card not done: ended, dead, blocked, waiting, never reported | kept, for the human (H4) |
| card clint started | kept. Only the human removes it, from the board |
| card tagged to stay open, or an investigation | kept |
| the sweep ARCHIVES the card (dead after a minute) | untouched. Archive is a board change, not a disk one |
| the sweep PRUNES the card (`prune_after`) | untouched. The row stays and the home shows as orphaned (6) |

Archive and prune never remove a home, because neither is a statement that the work is safe. Only a launcher's
acceptance, a captured task or a throwaway's own contract is.

**The checks before removal, for kind `worktree`:**

1. The runner has exited, waiting up to 60 seconds, so nothing is still writing and Windows will let the folder go.
2. The worktree is clean. Untracked files that are not ignored count as dirty.
3. The branch's tip is on the hub, read back from the hub's mirror, or its patches are on `claude/main` by patch id
   (the rule `b-landed-workers-never-reclaimed` sets out).

A check that fails keeps the home: state `kept`, the reason ("work/ has 2 uncommitted files", "claude/fix-x is 3
commits ahead of the hub"), the card keeps `atrium:keep-open`, and the launcher gets one notice. Nothing is deleted.

**The removal**, the same walk the item-tasks design defines, now in one place (`internal/daemon/cardhome.go`):

- The path comes from the ledger row, never from a request.
- It must resolve inside `card_root` through `internal/safepath`, must not be `card_root` itself, and must not be a
  link.
- For `worktree`: `git worktree remove --force` in the clone (the checks above already passed), then `git worktree
  prune`, then the local branch deleted when its tip is on the hub.
- Then the walk removes what remains. A link or junction inside is removed as a link and never followed.
- A removal that fails partway is `kept` with the error, and the next sweep tries again.
- Done: state `removed`, `removed_at` set.

REPORT.md and BRIEF.md go with the home. What lasts is what the worker committed and pushed, its `atrium_done` line and
recap, and its report. The worker brief says so in one line: "what you want kept goes in a commit or in your report".

**The sweep gains one step**, on its existing timer: rows `live` whose folder is gone go `removed`, rows `kept` with a
failed removal are retried, and throwaway rows whose card's runner has exited are removed (a backstop for a room that
restarted between exit and cleanup).

## 6. Existing cards: left where they are

Nothing moves. A live runner's working directory cannot be moved on Windows, a worktree moved by hand loses its admin
link, and the old folders were made by launchers, so the reclaim rule forbids removing them whole.

- Cards launched before this keep their `cwd`, their reclaim and their litter.
- The board's **Disk** view (7) lists them separately, as "outside the card root", by the `worktree` path on each card
  row, with sizes, read only. That is the list the human cleans by hand, once.
- New launches get homes. Within a few days of the change the old list is only cards somebody chose to keep.

A ledger row whose card was pruned is an **orphaned home**. It shows in the Disk view with a remove button, because
nothing else will remove it.

## 7. Size, and what the board shows

**The walk.** A room goroutine walks each `live` and `kept` home on a ten-minute tick, one home at a time, links never
followed, at most two seconds of walking per tick, and resumes where it stopped on the next. It writes `bytes`, `files`
and `measured_at` on the ledger row. A home is walked again soon after its card goes done, since that is when the
number matters. A walk error leaves the last number with its age. Free space on the root's drive is read every tick.

The size is an observation of the disk, not of a runner, so it may be stored. The board shows its age when it is over
an hour old.

**The board.** Only what the human acts on.

- **The card** (terminals tab and card detail): the home's path, with a copy button and an open-folder button where the
  board can, and its size. A card with no home shows its `cwd` as now.
- **The room** (room header, and the hub's room list): `card_root`, the total of every home, the drive's free space.
  The total turns orange (`--wait`) when free space is under 10 percent or under 20 GB, because that is when the human
  has something to do.
- **The Disk view**, one panel in the room's settings, not a new tab: the homes sorted by size, each with its card, its
  state and its reason. A button removes a `kept` home or an orphaned one, after a confirm that names the path and the
  size. It runs the same removal as 5, checks included, and the human may override a failed check from here and nowhere
  else. Below them, "outside the card root" (6), read only.

The hub's room list carries each room's total and free space in the room stats the link already sends.

## 8. Rules that hold

- A home is only ever removed by its ledger row. A folder under `card_root` with no row is never removed, and the Disk
  view lists it as "not atrium's".
- Never a card clint started, never a card whose work is not done, never the main checkout, never a clone.
- A source, a hook, an archive or a prune never removes a home.
- `card_root` never on a drive root, the home folder, the state directory or under `git.scm_root`.
- Every path through `internal/safepath`. Links removed, never followed.

## 9. Item tasks fold in

The item-tasks design's `task_root` IS `card_root`, and its `task_dir` table IS `card_home` with kind `task`. A task
folder is a home: `<card_root>/<backlog id>-<task id>-a<attempt>-<id6>`, with `work/` as the worktree or the scratch
folder, `tmp/` and BRIEF.md at the top, and capture before removal exactly as that design says. Item-tasks T1 builds on
this design's stage C1 instead of making its own table and setting. Neither is built, so nothing migrates.

## 10. Build stages

1. **C1, the home.** `card_root` and its refusals, `card_home` and its migration, the launch fields `repo`, `branch`,
   `base`, `home`, homes for `worktree` and `scratch`, BRIEF.md at the home's top, `TMP`, `TEMP`, `TMPDIR` and
   `ATRIUM_HOME`. Reclaim on cull with the checks and the removal walk. Throwaways become homes.
2. **C2, the board.** The path and size on the card, the room total and free space, the Disk view with remove, the
   new-card form's "new home". Board links and board scratch folders become homes. The size walk.
3. **C3, atrium's own litter.** says and handoffs into `.atrium/` for cards with homes.
4. **C4, the rule.** The worker brief and launch tool descriptions say `repo` and `branch`. The room refuses an agent
   launch with a `cwd` outside `card_root` unless `scratch: true`. Behind room setting `card_root_required`, default
   off for one release, then on.

## 11. Tests

With temp directories, no live hub, the link harness where a hub is needed:

- **Root:** each refused value (drive root, home, an ancestor of home, the state dir, under `git.scm_root`). Windows
  with nothing set refuses a home launch with the sentence, and a `cwd` launch still works.
- **Name:** slug rules, the cut at 40, a title of only symbols falls back to the wire name, two cards with one title
  get two folders, an existing folder is refused.
- **Launch:** `repo` makes a worktree in `<home>/work` on the branch from `base`. `repo` as `host/owner/repo` with no
  clone clones from the hub first. `cwd` and `repo` together are refused. `cwd` alone behaves exactly as today.
- **Env:** the runner and its shell see `TMP`, `TEMP`, `TMPDIR` and `ATRIUM_HOME` under the home, over a harness value.
- **Reclaim:** cull on a done card with a clean, pushed worktree removes the home, the worktree record in the clone
  and the local branch. Dirty, unpushed, or a runner still alive past 60 seconds: kept with the reason, one notice.
  A card clint started, a card not done, a keep-open card: kept. Archive and prune: home untouched, and a pruned
  card's home shows orphaned.
- **Removal:** a junction or symlink inside pointing at another folder: the link goes, the target is untouched. A
  ledger path outside `card_root`, or `card_root` itself: refused. A folder with no row is never removed. A removal
  that fails partway is `kept` and retried by the sweep.
- **Throwaway:** removed on exit. Promoted: kept, not moved. A room restart between exit and removal: the sweep
  removes it.
- **Size:** the walk stops at its budget and resumes. Links are not followed. The board shows the age past an hour.
- **Board** (headless, mocked): the card shows path and size, the room total turns orange only under the free-space
  line, the Disk view's remove posts the row id and never a path.
