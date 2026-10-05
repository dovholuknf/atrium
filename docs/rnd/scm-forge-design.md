# The forge: a provider that knows its forge, so a pasted PR is read through the CLI (rnd-new-scm-forge)

Status: design by @rnd, 2026-10-04, ordered by clint ("keep going with scm, move it along"). clint's answers of the same
day are recorded at the end and this body states the design they decided. Part of it is built on claude/main, and
"Built" says which.

Read for this design:
- `docs/backlog/rnd/rnd-new-scm-forge.md`, the seven questions. The open questions near the end are its questions.
- the six items: `r-new-forge-interface`, `r-new-pr-lifecycle`, `r-new-scm-recognisers-salvage`,
  `f-new-pr-room-and-dedupe`, `f-new-gh-login-requirement`, `u-new-review-tab-read-back-comments`.
- `docs/runtime/providers-design.md`, `docs/runtime/scm-design.md`, `docs/runtime/intake-design.md`,
  `docs/rnd/pulls-view-design.md`, `docs/rnd/pulls-api.md`, `docs/rnd/pr-ci-state-design.md`,
  `docs/rnd/hub-forge-design.md`, `docs/fabric/room-requirements-design.md`, `atrium.requirements.yaml`.

## 0. The design in one paragraph

A **forge** is a small Go interface. An implementation holds nothing. It builds an argv for a named CLI (`gh` for
GitHub, `bb` for Bitbucket, `glab` for GitLab), runs it through a bounded runner, and parses what the CLI prints. **The
PR's host picks the forge.** `github.com` uses `gh` and `bitbucket.org` uses `bb`, with no entry needed. Any other host
needs an entry on the hub that names its forge. **Every forge call runs on the hub** (clint, 2026-10-04: "Rooms should
exclusively use the hub. Only the hub integrates with bitbucket/github"). A room attached to a hub never runs `gh` or
`bb` and never fetches from a forge host. It asks the hub over its link, and the hub runs the CLI under its own login
and fetches the PR's head into its store. A room with no link is its own hub and runs the forge itself.

**Atrium never goes looking for PRs.** It does not list them, poll for them or take a webhook. A PR reaches the board
when the operator puts it there, as a pasted URL through a recogniser or through gwt. Once there, the PR is placed on
the **least busy room**, kept as **one row per PR across rooms**, and read through the hub's forge. A room with no
checkout of the repo gets the code from the hub's store, and the PR's head from the same store. A PR can be checked out
as a worktree at the provider's `<worktree_root>/<org>/<repo>/<branch>`. A forge is used only when an action needs it,
and when its access is missing the hub raises an alert saying what is needed and where to fix it.

## 1. Which forge: the PR's host

The PR row has `host`, `org` and `repo`. The forge is chosen from the host.

1. An enabled provider with that `host` decides. Its `forge` field is one of `github`, `bitbucket`, `gitlab` or `none`.
   Empty means infer from the host.
2. With no such provider, the built-in table decides: `github.com` is `github`, `bitbucket.org` is `bitbucket`.
3. Any other host is `none`. The row fails `no_forge`, saying which host and how to add a provider row that names its
   forge.

A GitHub Enterprise host is a provider with `host` and `forge: github`. The command is the forge kind's fixed default
(`gh`, `bb`, `glab`), and a provider may override its NAME in `forge_cmd` for a wrapper on a machine. It never holds a
token. The `forge` column has no `CHECK`, so a new kind is one line in a Go list (`ProviderForges`), and it follows the
rule `providers-design.md` set for `kind` and `host`.

A repo with no provider is still worked. The PR runner fetches a blobless shallow copy into its own `src/`, so a
review needs no provider and no checkout.

## 2. The credential rule, and access on ask

**The rule.** Atrium holds the NAME of a command (`gh`, `bb`) and a host, never a token. No `GH_TOKEN`, header or key
in a row, a setting, an export, a log or `atrium.requirements.yaml`. No code path runs `gh auth token`,
`gh auth status --show-token` or the equivalent.

**All three forges are on ask.** Nothing is checked or built ahead of a need, and nothing runs on a timer. Access is
checked in two situations only:
- an action needs the forge, such as reading a PR or making a PR worktree, and the CLI says it is missing or not logged
  in. The code that hit it raises the alert.
- a preflight or the settings view's "check now" names a forge and asks the CLI's own status command once.

**One alert for every shortfall.** A CLI that is not installed, not logged in to the host, or short of a scope the
requirement names is the same thing: an alert is raised, a message says what is needed on which room, and the
configuration to fix it is offered. There is no separate scope policy. A login that prints no `Token scopes:` line, as
a fine-grained token does not, has nothing that can be said to be missing, so it is not an alert.

| question | `gh` | `bb` | `glab` |
| --- | --- | --- | --- |
| logged in to host H | `gh auth status --hostname H` | `bb auth status` | `glab auth status --hostname H` |
| scopes held | the `Token scopes:` line | not applicable | the same line |

**The logins are the hub's.** The `gh` and `bb` logins live on the hub alone. The hub holds its forge per host in one
setting, `forge.entries`, a list of `{host, forge, cmd}` read and written at `GET` and `PUT /_hub/forge`. The command
name comes from that list, never from a request body. A room holds no forge setting. Migration 0083 deleted the room's
old `forge.<tool>.host` and `forge.<tool>.cmd` rows and the board's room settings no longer show a forge block.

**The alert is the hub's.** When a room's question finds the hub's CLI missing or logged out, or the forge refuses the
hub a credential while fetching a PR's head, the hub raises one growler, `forge|<tool>@<host>|<ts>`, hung on the room
that asked. Its sentence names the hub and the login command to run there. It is raised once while open, and the next
forge answer that works ends it. `POST /_hub/forge/check` asks the hub's CLIs for their status. It answers a state per
CLI and ends an open alert on `ok`. It raises nothing on a failure, since no room is waiting on it.

**The requirement.** `atrium.requirements.yaml` has a `forges:` block keyed by the CLI, with the host it must be logged
in to and the scopes it must hold:

```yaml
forges:
  gh: { host: github.com, scopes: [repo, read:org] }
```

The key is one of `gh`, `bb` or `glab`, the host must be a host name and each scope a plain scope string. Nothing else
is accepted, so the file cannot carry a secret and a lint of it can say so. The block names the hub's login and is
checked on the hub with `POST /_hub/forge/check`, which takes the same `{tool, host, scopes}` list. `POST /v1/preflight`
on a room with a hub runs no CLI and answers that the logins are the hub's. On a room with no hub it still asks the
room's own CLI, by its default name against its default host.

## 3. What the forge answers

The interface as the PR runner sees it. Every method takes a context and a PR reference `{Host, Org, Repo, Number}`,
runs through the bounded runner (timeout, read cap, `GIT_TERMINAL_PROMPT=0`) and returns Go values in atrium's shape,
not the CLI's.

| method | returns | `gh` spelling |
| --- | --- | --- |
| `View(ref)` | title, author, head sha and ref, base ref, from-fork flag, files | `gh pr view N --repo R --json ...` |
| `Diff(ref)` | the unified diff, capped | `gh pr diff N --repo R` |
| `Head(ref)` | the head sha only | `gh pr view N --repo R --json headRefOid` |
| `FetchSpec(ref)` | git remote URL and the ref holding the head | `https://host/org/repo.git`, `pull/N/head` |
| `PRURL(ref)` | the PR's web URL | `https://host/org/repo/pull/N` |

**Rules.**
- **Read only.** No method writes to a forge, and none lists PRs or searches for them. A list and a review-requested
  search are out with the PR list (section 4).
- **The shape out is atrium's.** The runner reads `Head`, never `headRefOid`, so a second forge fills the same struct
  from different JSON.
- **Capabilities.** Where `bb` has no diff, the runner computes `git diff base...head` in the `src/` it already
  fetches. A missing method is a named fallback and never a nil.
- **The canonical key.** The key of a PR is `host/org/repo#N` with host and org lower-cased, so a paste of
  `.../pull/5/files` and `.../pull/5` and a GHE URL are one key. Placement and the claim table key on it.
- **Errors carry a code.** `no_forge` for a host with no forge, an access error for a CLI that is missing or logged
  out, which raises the alert of section 2, and `other` with the first line of the CLI's stderr for the rest.

Methods that no action needs yet, such as comments, checks and a branch's open PR, are added when something asks for
them and are not designed here.

## 4. How a PR reaches the board

**The operator puts it there.** A PR arrives as a pasted URL, which a recogniser resolves to a card and a row, or
through gwt. Nothing arrives on its own.

**Out, on purpose.**
- No PR list per provider and no source row that lists PRs.
- No polling of GitHub or Bitbucket, and no webhook from them, since a webhook needs reach the hub does not always
  have. Timers stay only for atrium's own hygiene.
- No review on arrival. It needs something to arrive on its own, and nothing does. A pasted PR waits on its `review`
  button like any other.
- No review-requested search and so no need to know who the signed-in user is.

`POST /v1/prs` is the one door for a row, and every paste ends there. The existing dedupe rule applies, the same PR at
the same head is one row.

**Recognisers.** The rows in `scripts/recognisers/` turn a pasted GitHub or Bitbucket URL into the captures the row
needs (`host`, `org`, `repo`, `num`). Resolving a Bitbucket URL to a card needs nothing from the `bb` forge, and reading
the PR does.

## 5. A worktree for a PR, so gwt is not the only way in

A provider computes `<worktree_root>/<org>/<repo>/<branch>` (`worktreeDest`) and makes it with `git worktree add`. A PR
checkout is that, with the branch and the fetch supplied by the forge.

**The branch.** A same-repo PR is checked out on its real head branch, which is what gwt does and what an author
iterating wants. A fork PR gets `pr-<N>`, since a fork's head ref is its own `main` or `fix`, which collides with the
repo's branches. `View` returns the head ref and the from-fork flag, so the choice is data. The `flattenBranch` rule
turns `feature/x` into `feature-x`, so the directory is predictable.

**The verb.** `POST /v1/providers/{name}/worktrees` accepts `number` beside `org` and `repo` for a PR. The daemon:
1. finds the provider and refuses with a sentence if worktrees are off.
2. asks the hub's forge for the PR, so a missing or logged-out login on the hub answers its own sentence first.
3. when the repo is not a checkout on this room, gets the code through the scm clone path. That path asks the hub to
   hold the repository, clones the hub's copy, and keeps a guarded `origin` set to the forge's https URL. A private
   repo the hub cannot read fails with a sentence that names the hub's login.
4. fetches the head the hub fetched into its store, from `refs/atrium/pr/<N>` there into `refs/atrium/pr/<N>` here,
   then runs `git worktree add` at the destination, creating the branch at the fetched head.
5. is idempotent. A worktree already on that branch is the answer, with `existed: true`. A branch that exists
   locally is checked out as it is, so a local commit is never overwritten, and a destination that exists and is not
   that worktree is a conflict that is reported and never overwritten.

Nothing here pushes, and the room never touches a forge credential. A room with no hub fetches the head from the
forge itself, with its CLI's credential helper.

**The way in.** A "check out" action on a PR, the recogniser's `prepare` for a PR URL, and `atrium open <pr url>`, all
one function. The launch dialog's directory then points at the path the verb returns. A card started there is an
ordinary card in a worktree, and `gwt pr` keeps working, since the next discovery adopts its worktrees.

## 6. Which room runs a PR, and one row across rooms

**Where the call runs: the hub runs the forge, the room runs the review.** The reviews root and the run folder are per
room. The hub proxies `/v1/prs` to a room as it proxies `/v1/tasks`, and the room asks the hub for the PR's metadata,
diff and head. The CLI and its login are the hub's alone.

**Which room: the least busy.** The hub picks the online room running the fewest sessions. A session is a card that is
running or waiting on an answer or a permission. Ties are broken by idle CPU. Whether a room has a checkout does not
rank it, because a room without one gets the code from the hub's clone (section 5). The count is each room's own
`/v1/tasks`, asked when a claim is placed, so the hub keeps no standing index. A room marked for deletion starts no
new work, and a room that could not be asked is passed over unless nothing else answered.

**One row per PR across rooms.** The hub keeps a small claim table, `pr_claim`, keyed on the canonical key with the
room that owns it. It is an index, not a copy. Findings, diffs and the run folder stay on the room that holds the row.

The flow:
1. A paste reaches a room, or reaches the hub with no room named, in which case the hub places it on the least busy
   room first. Before creating a row the room sends the hub a claim for the key.
2. The hub looks the key up. A live claim names the owner. Otherwise it picks the least busy room and records it.
3. If the owner is the asker, the asker creates the row and starts the run. If the owner is another room, the asker
   creates nothing, and the hub forwards the paste to the owner when the paste came with the claim. A forward the
   owner does not take gives the key to the asker, who is online.
4. **A hub that cannot be reached** does not stop a review the operator pasted. The room proceeds, records
   `claim: pending`, and reconciles when it attaches again, with no timer. A row whose key another room claimed first is
   marked `folded into <room>`, aborted if it has not started, and left to finish and labelled if it has, since
   deleting a finished review is worse than showing two.
5. **The safety net.** The pulls view through the hub folds any two rows with one key on different rooms into the one
   the claim names, and lists the others on it as folded.
6. **A PR on an offline room is never re-placed automatically.** The claim stays and the PR waits for that room or for
   the operator to move it by hand. After the room has been offline for two minutes the board raises a warning alert
   saying the PR is on that room and that it is offline, and ends the alert when the room returns or the claim moves.
7. **Room handoff.** When the operator moves a PR card, the claim's room is updated so the key keeps one owner.
8. **A refused placement lets go.** When the room a worktree was placed on answers 4xx or 5xx, such as a clone or a
   head fetch the hub could not do, the claim that placement made is released, so a retry places the key again.

## 7. Staging

Each stage is useful alone. Acceptance is run in a throwaway room with its own `ATRIUM_LOCATION`, and stage 3 with two.

### Stage 1: the forge interface, `gh`, and forge access. @runtime and @fabric.
- `internal/forge`: the interface of section 3, the `gh` implementation, error codes, the provider `forge` and
  `forge_cmd` columns, and the host-picks-forge rule. The PR runner takes a `Forge`.
- Forge access of section 2: the room's settings, the check, the alert, the `forges:` requirement and its preflight.
- **Acceptance:** `prrunner_test.go` passes with a fake forge. A review of PR 378 at `ad5ddf4` produces a `pr.json`,
  `pr.diff` and `bundle.md` identical to the run before the change. With `gh` off the PATH the row fails with the
  not-installed sentence and the alert is raised. `gh auth token` is never run, which a test asserts by recording the
  argv. A host with no provider and no built-in forge fails `no_forge`.

### Stage 2: a worktree for a PR. @runtime.
- Section 5.
- **Acceptance:** a same-repo PR gives `<worktree_root>/<org>/<repo>/<branch>` on the PR's real branch, a fork PR gives
  `.../pr-<N>`, and pressing it twice answers `existed: true`. A repo with no checkout is cloned through the scm path.
  `gwt` is not on the path in the test.

### Stage 3: which room runs a PR, and one row across rooms. @fabric.
- Section 6.
- **Acceptance:** two fake rooms report the same PR and one row and one run result. The least busy room wins, and the
  tie-break is stable across ten runs. An unreachable hub, a later claim and a fold behave as in 6.4. A claim whose
  room is offline stays and raises the warning.

### Stage 4: Bitbucket through `bb`. On ask.
- A `bb` implementation, the `git diff base...head` fallback where it has no diff, and `bitbucket.org` read with no
  provider.
- **Acceptance:** a Bitbucket PR URL makes a row and a full review with the runner unchanged from stage 1. The only code
  outside `internal/forge` that changes is recogniser rows and the access check. If the runner needs an edit, the
  interface was wrong and stage 1 is reopened.

### Stage 5: pasting a PR gets a card. @runtime.
- The glue that takes a pasted PR URL through a recogniser to a card and a row, using sections 4 and 5.
- **Acceptance:** pasting a GitHub PR URL gives a card in the PR's worktree, and pasting the same PR twice gives one
  row.

### Not built, and only on clint's ask
GitLab through `glab`. Comments, checks and PR state read through the forge, for the review tab and the card's PR chip.
Posting a review to a forge, which is its own design because it changes "read only". A hub mirror as an alternative
`FetchSpec`.

## 8. Every call site that moves

`r-new-forge-interface` moved the PR runner's `gh` sites onto the forge (`View`, `Diff`, `PRURL`, `FetchSpec`, the
error text and the log label). Sites that remain are GitHub-shaped text and not calls, and they are the recogniser's
concern (`r-new-scm-recognisers-salvage`):

| site | note |
| --- | --- |
| `internal/store/recognisers.go` | comments naming `/pull/5/files` and `gh issue view`, and the seeded rows |
| `internal/store/prs.go` | `RunFolder` defaults the host to `github.com`, and a comment names `gh pr view` |
| `internal/prreview/render/types.go` | `PRURL` documented as a github.com URL, and the golden files hold it |
| `internal/api/web/index.html` | the paste placeholder and the recogniser example pattern |
| `internal/api/web/js/terminal-links.js` | the PR link walk keys on `github.com` and `/pull/` |

**Moved to the hub.** `internal/daemon/recognise.go` has a built-in fetch, `forge` with the argument `pr` or `issue`,
which reads through the hub's forge and gives facts under gh's names (`title`, `headRefName`, `baseRefName` for a PR,
`title` and `body` for an issue). On a room with a hub a row whose fetch names `gh`, `bb` or `glab` is refused with a
sentence saying to use `forge`. The rows in `scripts/recognisers/` use it. `internal/daemon/sources.go` still runs the
operator's own argv. `internal/hubstore/docs_secrets.go` has a secret-scan rule for the string `gh`, unrelated.

## 9. How this relates to the hub as a forge

`docs/rnd/hub-forge-design.md` makes the **hub** a git server for rooms: rooms push finished work to it, other rooms
fetch from it, and `atrium_git_url` says where. It answers "where do I get the work a swarm made", and it is also where
a room without a checkout gets the code of a PR (section 5). It has no PRs, reviewers or CI.

The forge here reads what **GitHub** (or Bitbucket) says about a PR the world made. It runs on the hub, and it fills
the hub's store with what a room needs.

| | hub as a forge | this forge |
| --- | --- | --- |
| lives on | the hub | the hub, as a CLI call, or a room with no hub |
| holds | bare repos, push log | nothing of its own. PR heads go in the hub's store |
| speaks | smart HTTP git | `gh`, `bb`, `glab` |
| credential | the machine's hub certificate | the hub's CLI login, never atrium's |
| ends at | a branch the orchestrator merges and pushes on | a PR row in the pulls view |

**Where they meet:** the code of a PR. A room asks the hub (`POST /_forge/pr` on the link's git kind with `fetch`).
The hub holds the repository in its store, cloning it from the forge when it does not, and fetches the PR's head into
`refs/atrium/pr/<N>` there, over https with the forge CLI's own credential helper for that one command. When the
store's `main` is still empty, as for a private repository the seed could not read, it fetches the base branch into
`main` in the same command. The room then reads the head from the store through a read-only loopback to its link, like
any other ref. The hub only ever fetches from a forge and never pushes to one.

**The name.** Docs call the hub's feature "the hub as a forge" and this design says "forge". In code, this is
`internal/forge`, and the hub's is `internal/gitsync` and `internal/hubstore`. The hub's forge route is
`internal/link/forgeroute.go`. The hub's feature stays "the hub as a forge" and never "forge" alone.

## 10. What is out

- **No credential in atrium.** A row holds a command name and a host. The requirement file holds a CLI key, a host and
  scope names.
- **No HTTP client for any forge API.** If the CLI cannot do it, the answer is a different CLI call or no feature.
- **No PR list, no source row, no polling, no webhook, no listener.** A PR reaches the board when the operator puts it
  there.
- **No review on arrival.**
- **No second intake.** `POST /v1/prs` is the one door for a row.
- **No write to a forge.** The interface has no write method until posting a review is ordered, and that is its own
  design.
- **A room with a hub does not call a forge.** It does not run `gh`, `bb` or `glab`, as a forge call or as a
  recogniser's fetch, and it does not fetch from a forge host. A room whose hub is down fails with a sentence and never
  falls back to a forge of its own.
- **The hub never pushes to a forge.** It views, diffs and fetches.
- **No automatic re-placement.** A PR on an offline room waits and warns.

## Built

On claude/main as of d35d9c1e, plus `r-hub-forge` (2026-10-04), read from the code.

- **`internal/forge`.** The `Forge` interface with `View`, `Diff`, `Head`, `FetchSpec` and `PRURL`, the `PR`, `Ref`
  and `FetchSpec` types, and `gh` and `bb` implementations behind a bounded `Runner` (`forge.Exec` on the hub). `Pick`
  and `For` choose the forge from the host with entries first and the built-in table (`github.com`, `bitbucket.org`)
  second, and a host with neither fails `NoForgeError` (`no_forge`). `AccessError` carries a not-installed or
  not-logged-in sentence. `IssueReader` reads an issue (`gh issue view`, `bb api .../issues/N`), as its own interface
  so a fake that reads only PRs is still a `Forge`. `Remote` is the Forge of a room with a hub: every call is a
  request to the hub, `FetchSpec` names a ref in the hub's store (`FetchSpec.Hub`), and a refusal is a `HubError`
  with the hub's sentence.
- **The hub's forge route, `internal/link/forgeroute.go`.** `POST /_forge/pr`, `/_forge/issue` and `/_forge/repo` on
  the link's git kind, with the asking room set from the hello. The forge is picked per host from the hub setting
  `forge.entries`. A PR asked with `fetch` is held in the store and its head fetched into `refs/atrium/pr/<N>`
  (`Store.Hold` and `Store.FetchPR` in `internal/gitsync/storepr.go`). An `AccessError`, or a head fetch the forge
  refused a credential for (`FetchError.Auth`), raises the growler of section 2, and a later success ends it.
  `GET` and `PUT /_hub/forge` hold the entries, and `POST /_hub/forge/check` checks the hub's logins.
- **The room's side.** `Room.Forge` asks the hub, and `d.SetHubForge` gives a room started with a link the `Remote`.
  The PR runner (`internal/daemon/prrunner.go`) and the PR worktree verb (`internal/api/prworktree.go`) use it, and
  read the head and a clone from the hub's store through `gitsync.HubLoopback`, a loopback that serves the fetch of
  one repository of the store and nothing else. A whole fetch, since the store serves no shallow or filtered one. The
  PR worktree verb finds its provider by name, else by the body's host. A room with no link builds its forge from its
  providers' `forge` and `forge_cmd` fields as before.
- **`internal/api/prworktree.go`.** The PR form of the provider worktree verb, taking `org`, `repo` and `number`. It
  asks the forge for the head ref and fork flag, uses the real branch for a same-repo PR and `pr-<N>` for a fork,
  clones through the scm clone path when the room has no checkout, fetches the head into `refs/atrium/pr/<N>`, makes
  the worktree, and answers `existed: true` on a repeat.
- **`internal/daemon/forgeaccess.go`.** On a room with no hub only: the status check per CLI from the CLI's own status
  command under its default name, the scopes read from the `Token scopes:` line, the one alert for not installed,
  logged out or missing scope, `RaiseForgeAccess` for the code that hits missing access, and the open alerts in the
  room settings (`forge_access`). The room's `forge` settings key, `POST /v1/forge/check` and the board's "forge
  logins" block are gone (migration 0083).
- **`internal/requirements`.** The `forges:` block, keyed by `gh`, `bb` or `glab`, with a `host` and `scopes`, and
  validated so nothing else is accepted. It is the hub's login. `POST /v1/preflight` on a room with a hub runs no CLI
  and says the logins are the hub's.
- **PR placement and the fold.** `placePRRoom` in `internal/link/prclaim.go` picks the online room running the
  fewest sessions, breaking a tie by the lower room name. The hub's claim table and the claim call, the forward of a
  paste to the owner, the move of a claim, the offline-room warning alert, the room's `claim: pending` and its
  reconcile (`ReconcilePRClaims` in `internal/api/prclaim.go`), and the fold of rows with one key in the pulls view
  (`foldPRRows` in `internal/link/pulls.go`). A placement the room refuses releases the claim it made
  (`releaseOnRefusal` in `internal/link/prworktreeroute.go`, `ReleasePRClaim` in `internal/hubstore/prclaim.go`).
- **Recognisers.** The built-in `forge` fetch of section 8, and the refusal of a `gh`, `bb` or `glab` fetch on a room
  with a hub.
- **`scripts/recognisers/`.** Seed rows for GitHub (pull request, issue, branch, repository), Bitbucket (pull request,
  issue, branch) and support tickets (Zendesk, Discourse), with a `load.ps1` that loads them. The PR and issue rows
  fetch through `forge`.

**Not built, and in progress by other workers.**
- The idle-CPU tie-break of section 6. Today a tie goes to the lower room name, since a room reports no CPU figure.
- The paste-to-card glue, stage 5.

**Not built, on ask.** GitLab, comments, checks and PR state read through the forge, and posting a review.

## Open questions for clint

1. **Forge on the provider, with a built-in `github.com` fallback for a repo with no provider.** Section 1. The
   alternative is to require a provider for every reviewed repo, which is simpler and stops the pulls view reviewing
   a repo you never cloned.
2. **A fork PR checks out as `pr-<N>`, a same-repo PR on its real branch.** Section 5. The alternative is `pr-<N>` for
   all, which is uniform and loses the real branch name that gwt gives.
3. **A room is chosen on login first, then checkout.** Section 6 lets a room with a login and no checkout run a review,
   since the runner needs no checkout, and ranks it below one with both. Say so if you want checkout required.
4. **An offline room's unstarted PR is re-placed after ten minutes.** Fine, or longer?
5. **Is the PR list a source row per provider made by a button?** Section 4. The alternative is one global "my PRs"
   source, which has less to set up and no per-provider count.
6. **Bitbucket in stage 4, GitLab only on ask.** Is that the order?
7. **The scopes `repo` and `read:org`** are my reading of what stage 1 needs, to be checked against a real
   `gh auth status` at build. A fine-grained token is a warning, not a failure. Is that the policy you want?
8. **Review on arrival when requested of you** (decided in the pulls design) is kept. Confirm it holds across
   providers, since `bb` may not say "requested of me" at all.


## Answers (clint, 2026-10-04)

The questions above are kept as asked. These are the answers, and they overrule the body where the two disagree.

1. **The PR's host picks the forge.** A repo with no provider is still worked: `github.com` uses `gh`,
   `bitbucket.org` uses `bb`. Any other host needs a provider row naming its forge. This is section 1 as written.
   The question's "github.com fallback" wording was wrong, not the design.
2. **Yes.** A same-repo PR checks out on its real branch, a fork PR on `pr-<N>`.
3. **A PR goes to the least busy room.** Fewest running sessions, ties broken by idle CPU. Whether the room has a
   checkout does not rank it. A room without one gets the code **from the hub**, which clones and fetches. This was
   already decided on 2026-10-02 in `docs/rnd/hub-forge-answers.md` and this design missed it: section 9's "the hub
   never serves PR code" and section 10's "atrium does not clone" are both overruled. A private repo the hub cannot
   read fails with a sentence telling the operator to clone it.
4. **Never re-placed automatically.** A PR on an offline room waits for that room or for the operator, and the board
   raises a warning alert saying so.
5. **No PR list and no source row.** Atrium never polls GitHub or Bitbucket and never takes a webhook from them,
   since a webhook needs reach the hub does not always have. A PR reaches the board when the operator puts it there:
   a pasted URL through a recogniser, or gwt. Section 4's source row and the stage 1 PR list are dropped. Timers stay
   only for atrium's own hygiene.
6. **All three forges are on ask.** Nothing is built ahead of a need. When an action needs `gh`, `bb` or `glab` and
   the access is missing or not logged in, atrium raises an alert, says what is needed in a message, and offers the
   configuration to fix it.
7. **Answered by 6.** Missing access or a missing scope is the same alert, message and configuration. No separate
   scope policy.
8. **Dropped.** Review on arrival needs something to arrive on its own, and nothing does.
9. **(later the same day) Rooms use only the hub.** "Rooms should exclusively use the hub. Only the hub integrates with
   bitbucket/github. Right now it only pulls and has no push privs anyway." Issue data comes through the hub too, and
   the `gh` and `bb` logins live on the hub alone. Sections 0, 2, 5, 6, 9 and 10 state this.
