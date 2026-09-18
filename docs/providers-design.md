# Providers: where a repository lives on this machine

A provider is the operator saying "under `D:/git/github`, directories are `org/repo`". Atrium reads that
instead of walking the disk guessing at a convention.

Built 2026-09-16 from `docs/backlog/backlog-2026-09-13-001.md` and its plan.

------------

## The word, first, because three things wanted it

`docs/scm-design.md` already owns `scm` for two unrelated features, both built:

- **Inbound:** the `recogniser` table. A URL becomes a filled-in launch dialog.
- **Outbound:** atrium's own configuration as files a repository can hold, with a per-field rule for what may
  leave this machine.

This is a third thing, so it is called **providers** and nothing here is called `scm`. Three features under one
noun is how a package grows a file nobody can describe.

**The relationship to the recognisers is a join, not an overlap.** A recogniser captures `host`, `org` and
`repo` out of a URL and then has to template an absolute path by hand, which is exactly the field
`docs/scm-design.md` calls `local` and forbids from leaving the machine. A provider knows how to turn those
three into a path. That join is not built. It is named here so the two panes sitting next to each other on the
runners page makes sense, and so the `host` field on a provider has a reason to exist.

**What a provider deliberately does not do:** no URL parsing, no cloning, no credential, no forge API call, no
writing back to anything. It answers one question, "where does org/repo live", out of what somebody typed.

------------

## The data model, and the one decision everything follows from

Two tables, `provider` and `provider_repo`, in migration `0053_provider`.

### The name is the key

The operator types `github` and everything refers to it by that string, so the name is the primary key rather
than a ULID with a name column beside it. Same rule `docs/scm-design.md` states for identity across machines:
by name, not by id.

The cost, stated plainly: renaming is a delete plus an add. The dialog disables the name field once a provider
exists, with the reason on the field, because anything holding the old string would be silently orphaned by a
rename and a cascade is a separate question.

`name_key`, `org_key` and `repo_key` are lowercase COLUMNS rather than `COLLATE NOCASE`. The schema is written
to stay Postgres portable and `NOCASE` is SQLite's own spelling. They are also what stops `Dovholuknf/Atrium`
and `dovholuknf/atrium` becoming two rows for one directory on a Windows filesystem.

`kind` carries no `CHECK`. Adding a second provider type would otherwise be a table-rebuild migration, since
SQLite cannot alter a constraint in place, and the requirement is explicit that the type is a field and not an
assumption. Validation lives in `store.ProviderKinds`, where a new value is one line.

There is no `path` column and no `org` table. A path is `root/org/repo`, computed by `store.ProviderPath`, and
orgs are `SELECT DISTINCT`. An org is a directory that happens to hold repositories, and a row for one would
mean deciding what an empty org folder is.

### The row is durable and the presence is derived

This is the answer to "stored or computed", and it is the sentence the rest of the design hangs from.

The backlog states it as either/or and names the cost of each side: storing means a repository deleted from
disk lingers, deriving means the org and repo somebody typed are not authoritative. **Neither cost has to be
paid, because they attach to different fields.**

- **The row survives**, including for a repository nobody has cloned yet. Typing an org and a repo is how one is
  added, and a derived-only list could not hold one that is not on disk, which is the case where somebody most
  wants the record.
- **`Path` and `Present` are worked out on every read and never written.** So a repository deleted from disk
  does not linger as a lie. It lingers as a row that says "not on disk".

The case this protects is a root on a drive that is not plugged in. If presence were stored, one discovery run
against the dead drive would mark everything absent and a tidy-up would delete it. If rows were derived, the
list would be empty and every typed entry gone. As written, the list is every repository ever adopted, drawn
dim, and plugging the drive back in restores the picture with no action.

The cost, said plainly: removal has to be deliberate, so there is a `forget` button. Discovery never removes a
row.

------------

## Discovery

On save, and on the `look again` button. **Not on a timer, and not at daemon start.**

A timer would prevent one failure, a repository cloned in a terminal not appearing until somebody presses a
button. It would cause a worse one: a background walk of up to 4000 directories, possibly across a network
share, on a repeating tick. Atrium already has exactly one mechanism for commands on a timer, `sources`, with a
whole design document about how one must never halt anything and must switch itself off after three failures. A
second timer with different rules is a second mechanism answering the same question.

### Bounds, applied while reading

- 4000 directories visited, then stop and report `truncated`
- 400 repositories adopted per provider by default, `max_repos` per provider, hard ceiling 2000
- **depth fixed at 2, and no longer a setting.** The mechanism this replaces needed a depth knob because it was
  inferring a layout. A provider declares one, so the depth is a consequence of `root/org/repo`
- a 30 second deadline over the whole walk, which the old scan did not have. A dead share answers `ReadDir`
  slowly rather than never

Everything is bounded WHILE reading rather than after, which is the one thing the old scan got right.

### What it does with each kind of directory

| what it finds | what happens |
| --- | --- |
| `root/org/repo` holding a `.git` | adopted |
| `root/repo` holding a `.git`, no org above it | adopted with an empty org, shown as `(no org)/repo` |
| a directory that is not a checkout | **counted and reported**, not adopted, not an error |
| a checkout inside a checkout | never reached. The walk does not descend into a checkout |
| a bare repository | not a checkout, so counted as one of the ignored. A card cannot start in one |
| a root that is itself a checkout | refused, pointing at the parent. A root is a container and a checkout is a leaf |
| two paths resolving to one directory | one row. Deduped on the RESOLVED path, so a junction cannot adopt twice |
| a match for an `exclude` glob | skipped and counted separately |
| a row already hidden | left hidden and counted |

Counting the skipped ones rather than dropping them is the difference between a list you trust and an afternoon
spent working out why one repository is missing.

`isCheckout` counts both kinds of `.git`: a directory is an ordinary checkout and a FILE is a worktree or a
submodule, and both are directories a card can start in.

------------

## The refusal

> Worktree support cannot be turned off unless the worktree directory is fully empty.

`worktreeRootBlockers` in `internal/api/providers.go`, called from three places: the save on a toggle-off
transition, the delete, and `POST /v1/providers/{name}/check-worktrees`, which asks without saving.

**The check runs before any write, and a body carrying a blocked toggle is refused whole.** This is the posture
the settings boundary already takes: applying half a request and reporting a failure is the worst of both,
because the caller reads an error and the machine has changed anyway. A request that turns the toggle off and
also renames the root leaves the root alone.

### What "fully empty" means

`os.ReadDir`, one level, and:

- **any entry at all blocks**
- **three names are ignored and nothing else:** `.DS_Store`, `Thumbs.db`, `desktop.ini`. These are written by
  an operating system without being asked, so treating them as evidence would mean a directory that can never
  be emptied by the person looking at it. A NAMED LIST rather than a dotfile rule, because a `.git` under the
  worktree root is a real thing and must block
- **a directory that is not there holds nothing**
- **unreadable blocks and says so.** Treating it as empty fails open on exactly the case where the directories
  are most likely to be there and least likely to be noticed

**Directory entries rather than `git worktree list`, and that is the load-bearing choice.** Asking git would
look more precise and would be weaker: git can only see worktrees whose repository is still present and still
registers them. A worktree whose repository was deleted is invisible to git and is precisely the case that must
not be silently abandoned. Counting entries is stronger and cheaper.

The cost: a stray `notes.txt` in the worktree root blocks the toggle. That is why the refusal names every entry
rather than saying "not empty", bounded at 20 plus a count.

**The checkbox is not disabled preemptively.** Greying it out would need the board to walk the worktree root
every time the dialog opens, and a greyed control with no explanation is the refusal with the explanation
deleted. So the box unticks freely, the save is refused, the board re-ticks it and renders the blockers under
the field.

Deleting a provider runs the same check for the same reason.

------------

## Making a worktree

`git worktree add`, as an argv, with no shell. The old `worktree_command` does not come back under a new name.

`docs/scm-design.md` argued that making a worktree is somebody else's job and atrium has no business owning a
checkout layout. That had force when the layout was a convention atrium could not see. This changes the
premise: the operator DECLARES the layout in a form field, so atrium is executing one it was told rather than
guessing at one.

Once the layout is declared, shelling out buys nothing and costs four things, every one of which was a real
defect in what was removed:

- **the double start.** The default template `gwt new {branch} -y` also launches a session, so make followed by
  start started two. `git worktree add` makes a directory and does nothing else.
- **the shell.** A template had to be quoted correctly under three grammars, which is the entire reason a
  branch name had to pass a regular expression first. As argv, a path with a space or an apostrophe is a
  string.
- **the read-back.** Because a command's output is for a person, the old code had to ask git where the worktree
  landed. Atrium chose the path, so it already knows.
- **`{repo}` needed quoting** in a custom template.

What is lost: `gwt new` also fetches, writes a session ledger entry and seeds a prompt. Somebody who wants those
keeps using it in a terminal, and because the worktree lands under the declared root the next look adopts it.

The layout is `<worktree_root>/<org>/<repo>/<branch>`, which is what is already on this machine. A branch name
containing a slash is FLATTENED to a dash. Nesting would round-trip more cleanly and git would not mind, but it
leaves an empty parent behind when a worktree is removed, and an empty directory in the worktree root blocks the
toggle-off, which would make the feature's own refusal fire on debris the feature created. The cost is a
collision between `a/b` and `a-b`, which is reported rather than silently overwritten.

A branch that does not exist yet is created with `-b` off the repository's own `HEAD`, and the response says
`created_branch` so the board can say which of the two happened. **A default branch per provider is
deliberately not a setting:** the branch is a fact about a repository, and two repositories under one root
routinely disagree about `main` versus `master`. A provider-level default would be right most of the time,
which is the worst kind of wrong.

------------

## Settings the requirement did not name

Each prevents a specific failure.

- **`enabled`, per provider.** Without it the only way to stop a provider describing an unplugged drive is to
  delete it, throwing away every hidden decision and every typed row.
- **`host`.** The join to a recogniser, and the only way to get from `org/repo` back to a URL. Empty means the
  provider is local only.
- **`exclude`, newline-separated globs.** Stops a `.trash/` or a 300-repository mirror burning the cap.
  Composes with `hidden` rather than duplicating it: exclude is "never adopt these", hidden is "I adopted this
  and I do not want it".
- **`max_repos`.** Per provider, because the failure is per root.
- **`hidden`, per row.** The durable no. Without it a dismissal lasts until the next look.

### Answered without a new setting

**A provider root that does not exist yet saves, and atrium does not create it.** Saving follows the precedent
already written for `browse_roots`: a root that is not there is a list somebody is preparing, and a drive can be
unplugged. Not creating it is the other half: `os.MkdirAll` on a typo turns a mistake into a directory, and
discovery then adopts an empty tree and reports success, which looks exactly like a correct provider with no
repositories in it.

The exception is the WORKTREE root, where parent directories are created at worktree-creation time. That is a
directory atrium is about to put something into, which is a different act from one it was merely told to look
at.

------------

## What this did not touch

**No card was migrated and none is rewritten.** A card's `worktree` column stays the exact string it holds.
Rewriting it because a provider now describes that directory would be atrium overwriting what a human typed, to
cards with live processes attached. A card outlives the process it describes, and it certainly outlives a
configuration row.

**The two old settings are not deleted from any database.** `worktree_command` and `project_scan_depth` stay in
the `setting` table on every machine that has one. A migration running `DELETE` would destroy a value a human
typed, nothing reads the two rows, and somebody rolling back to an earlier binary after a bad release finds
their worktree command intact. They stop being EXPORTED from this release, which is correct, because nothing on
the other end would read them either.

------------

## Where it lives

| | |
| --- | --- |
| `internal/store/providers.go` | the two tables, and what is true of a row on its own |
| `internal/store/schema.go` | migration `0053_provider`, at the end of the slice |
| `internal/api/providers.go` | discovery, presence, the refusal, and asking git about worktrees |
| `internal/api/providerapi.go` | the routes, and `git worktree add` |
| `internal/api/web/js/providers.js` | the pane, the dialog, and the repository picker |
| `docs/test-plan-z-providers.md` | the human walkthrough |
| `scripts/walkthrough/providers.spec.js` | the same walkthrough, recorded at human speed |
| `scripts/walkthrough/playwright.config.js` | and the config it needs, which is not optional |

The recording runs against a LIVE daemon and a live board:

```
npm i -D @playwright/test
npx playwright install chromium
npx playwright test --config scripts/walkthrough/playwright.config.js --headed
```

It builds and removes a throwaway tree under `D:/zztest`, so it never touches a real repository. The config is
required: without it Playwright answers "No tests found" even when handed the spec by name, because a
positional argument is a regular expression matched against paths and the default test directory is not that
one. It also raises the per-test timeout, which a deliberately slow recording walks straight through.
