Docs: none, design note only until accepted

# A pasted repository link starts a new worktree

Design note. Nothing is built yet.

## What happens today

- Ctrl-K or "paste a link" puts a link in the switcher. `quickPasteRow` (js/quickpaste.js) asks `/v1/recognise` and
  labels the row from `pastedOpenKind` (js/fixtures.js), a copy of `classify` in internal/api/openkinds.go. The kinds
  are pr, issue, branch and support. A repo URL has a host, org and repo but no number and no branch, so the kind is
  "" and the row reads "open link".
- Enter runs `quickPasteGo`. It calls `linkOpeningBegin` (js/link-opening.js), which draws a card at the top of
  `running` from the keypress. `linkOpeningStep` names the step, first "recognising the link", then the room's own
  steps arrive as `open-progress` events keyed by the progress id sent with `/v1/open` (internal/api/openprogress.go,
  `opSteps`). `launchNow` makes the call. A failure stays on the card with edit, again and dismiss. Shift-Enter fills
  the launch dialog in and leaves it open.
- "Start new work" in the launch dialog (js/launchrepo.js) already covers a repo the hub lacks. `launchRepoTyped`
  reads host/org/repo, `cloneFromHub` asks `/_hub/git/init` so the hub clones from the forge, then the room clones from
  the hub. `newWorkGo` posts `/v1/new` (internal/api/newwork.go: fetch origin, `branchWorktreeFrom`, setup hook, card).

## What the repo case reuses

- Board: a new kind `repo` in `pastedOpenKind`, true when host, org and repo are named, the number is zero, the branch
  is empty and the row is not a pull request. `QUICKPASTE_KINDS.repo` is "new worktree" and the label is org/repo.
  Trailing slash and `.git` are tolerated where the repo name is read, and a `/tree/...` or `/issues` path is NOT a
  repo link (the existing rows win by rank). I need to confirm the recogniser seed that labels it "a repository"
  captures host, org and repo. If it does not, I fix the seed, not the board.
- Enter: the same `quickPasteGo` path, with the card from the keypress. The repo kind does not go through `/v1/open`
  (which refuses it as `not_openable`) but through a small `repoNewWorktree` in the board that does what `newWorkGo`
  does without the dialog: `/v1/new` with the repo slug and a generated name.
- Shift-Enter: opens the launch dialog, then opens "start new work" with the repo selected and the name box focused.
  Branch name, room and prompt are all editable there. No new dialog.
- Fixtures: js/fixtures.js already branches on `pastedOpenKind`, so a link pasted on the board gets the repo path with
  no extra code beyond the kind. The phone board (m/) has no paste bar and no `/v1/open` call (checked: no
  quickPaste, pastedOpenKind or /v1/open in m/), so it is out of scope.

## Steps on the card

`/v1/new` has no progress today. I add an optional `progress` id to it and have it report through the same `opSteps`
the open verb uses: "fetching the hub" (hub clone from forge, only when the hub lacks it), "cloning onto this room",
"fetching origin", "making the worktree", "starting the card". No new event type.

## Where the repo is, and which room

1. Room has a checkout: fetch, worktree, card. Fast.
2. Room lacks it, the hub has it: `/v1/new` goes through `repoCheckout`, which calls `SCMClone` and clones from the
   hub's copy. This is how a PR on an unknown repo already works.
3. Hub lacks it: the board first calls `/_hub/git/init {url}` (the call `cloneFromHub` uses), then `/v1/new`. A forge
   the hub cannot reach, or a private repo with no credentials, fails on the card with the hub's sentence and
   `edit` opens start new work.
4. The room is the hub (single room, or no hub): there is no `/_hub/git/init`, and `SCMClone` clones from the forge.
   The board checks `hubIsHub` (as `loadLaunchRepos` does) to decide whether to ask the hub first.

Room choice is `linkRoomFor(got)`, the same as a PR: the last room used, else the hub decides. Shift-Enter shows the
room box.

## Default branch

Not looked up by a forge call. `newWorkBase` forks from `origin/<default>` after the fetch, via the clone's
`refs/remotes/origin/HEAD`, else `hub/HEAD`, else HEAD (`defaultBase`). A fresh clone sets origin/HEAD. A clone made
from the hub sets `hub/HEAD`. That is what `gwt new` does too.

## Branch name (decided by rnd)

`claude/work-<yymmdd>-<hhmm>`, for example `claude/work-261016-1432`. The `claude/` prefix is required because
`agentCheckoutRefusal` refuses launches outside `claude/*`. `/v1/new` places the folder as for any `claude/*` name.
The card's title is `org/repo`. Pasting twice in a minute reuses the worktree and its live card.

## Trust: one keypress does not start a repo nobody here has seen

`launchLocked` trusts the worktree, so its `.claude/settings.json` hooks and MCP servers would run. So:

- Cases 1 and 2 (the hub or the room already holds the repo): Enter starts at once.
- Cases 3 and 4 (the hub must fetch it from a forge, or the room clones straight from one): the fetch and clone steps
  run on the card, then the card STOPS at a confirm step, "new repository from <host>: its project settings and hooks
  will run. start / cancel". No worktree is made past the clone and no runner is started until start. Cancel drops the
  card. The board holds the step, then calls `/v1/new` once the person confirms.

## Progress

`/v1/new` takes an optional `progress` id and reports through `opSteps`. The hub fetch is a board call to
`/_hub/git/init`, so the board names that step on the card itself under the same id ("the hub is fetching
host/org/repo") and the room's steps follow under it.

## Scope and out of scope

- Not the dialog's "start new work" rewrite, not a new endpoint, no migration, no setting.
- No forge API call for the default branch.
- The phone board is untouched.

## Test plan

## @LETTER@. A pasted repository link starts a new worktree

### @LETTER@1. The switcher row

1. Press Ctrl-K and paste `https://github.com/openziti-test-kitchen/ziti-tv/`.

**Expected:** the top row has the chip "new worktree" and the label `openziti-test-kitchen/ziti-tv`. The same with no
trailing slash and with `.git`. A `/pull/5` link still says PR.

### @LETTER@2. Enter starts at once

1. Press Enter on that row.

**Expected:** a card is on the board from the keypress naming the step (fetching, cloning, worktree). It ends as a
running card in a new worktree off the default branch on a new `claude/work-` branch. Pasting it again attaches the same card.

### @LETTER@3. A repo the hub lacks

1. Paste a repo no hub has cloned, then Enter.

**Expected:** the card says the hub is fetching it, then cloning, then the worktree. A repo that cannot be fetched
fails on the card with the reason and `edit` opens start new work with the repo filled in.

### @LETTER@3b. Confirm before an unseen repo runs

1. Paste a repo the hub lacks and press Enter.

**Expected:** the card names the hub fetch, then stops at "new repository from <host>: its project settings and hooks
will run. start / cancel". No runner exists. Cancel removes the card and starts nothing. Start finishes the worktree.

### @LETTER@4. Shift-Enter

1. Press Shift-Enter on the row.

**Expected:** the launch dialog opens with start new work showing that repo and the name box focused. Nothing starts
until the operator does.

### @LETTER@5. A link pasted on the board

1. Paste the link with no dialog open.

**Expected:** the same as Enter.
