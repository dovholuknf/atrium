# The forge: a provider that knows its forge, so PRs come from providers (rnd-new-scm-forge)

Status: design by @rnd, 2026-10-04, ordered by clint ("keep going with scm, move it along"), revised the same day
after the item files landed on claude/main (ebf16c00). Design only, nothing under `internal/` or `cmd/` changes.

**clint answered the open questions on 2026-10-04, and several answers overrule the body below.** Read "Answers"
at the end first. Where a section disagrees with it, the answer wins.

Read for this design:
- `docs/backlog/rnd/rnd-new-scm-forge.md`, the seven questions. Section numbers 1 to 7 below are its questions.
- the six waiting items: `r-new-forge-interface`, `r-new-pr-lifecycle`, `r-new-scm-recognisers-salvage`,
  `f-new-pr-room-and-dedupe`, `f-new-gh-login-requirement`, `u-new-review-tab-read-back-comments`.
- `docs/runtime/providers-design.md` (built), `docs/runtime/scm-design.md` (built), `docs/runtime/intake-design.md`
  (built), `docs/rnd/pulls-view-design.md`, `docs/rnd/pulls-api.md`, `docs/rnd/pr-ci-state-design.md`,
  `docs/rnd/hub-forge-design.md`, `docs/fabric/room-requirements-design.md`, `atrium.requirements.yaml`.
- `internal/daemon/prrunner.go`, `internal/daemon/preflight.go`, `internal/api/providerapi.go`.

## 0. The answer, in one paragraph

A **forge** is a small Go interface in the room daemon. An implementation holds nothing. It builds an argv for a named
CLI (`gh` first, then `bb` and `glab`), runs it through the bounded runner the PR runner already has, and parses what
the CLI prints. A provider gets a `forge` field (`github`, `bitbucket`, `gitlab`, `none`), and a PR's host picks the
forge through the provider with that host, falling back to a built-in `github.com` default for a repo with no
provider. Every forge call runs **in the room**, because the CLI, its login and the checkout are per machine. The hub
never calls a forge and holds no credential. The hub's one new job is deciding **which room runs a PR** and keeping
**one row per PR across rooms** (section 6). The pulls view gets a PR list per provider, filled by a source row that
posts to `POST /v1/prs`, so there is no second intake. A PR can be checked out at the provider's
`<worktree_root>/<org>/<repo>/<branch>`, so gwt stops being the only way in (section 5). Room requirements learn to
check a forge login and its scopes (section 2).

Stage 1 is GitHub through `gh`: the interface, the per-provider PR list, and the login check. It is useful alone.

## 1. Forge on a provider, or a separate row

**Options.**
- A. A `forge` field on the provider, beside `kind: git`.
- B. A separate `forge` row a provider points at, holding kind, command and host.
- C. Key the forge by host alone and ignore providers.

**Recommend A, with a built-in fallback for a host no provider names.** `providers-design.md` says the type is a field
and not an assumption, `kind` carries no `CHECK` so a new value is one line in a Go list, and `host` already exists on
the provider "as the join to a recogniser". `forge` follows the same rule: a column with no `CHECK`, validated in
`store` (`ProviderForges`), empty meaning "infer from `host`" (`github.com` is `github`, `bitbucket.org` is
`bitbucket`, anything else is `none` until set). A GitHub Enterprise host is a provider with `host` and
`forge: github`. The command is the forge kind's fixed default (`gh`, `bb`, `glab`), and a provider may override its
NAME in `forge_cmd` for a wrapper on a machine. It never holds a token.

B fails because there is exactly one forge per host and a row per provider would repeat it, and a second table needs a
migration for no new fact. C fails because the runner reviews repos nobody cloned: the PR runner fetches a blobless
shallow copy into its own `src/` and needs no provider.

**How the runner picks one.** `r-new-forge-interface` says "the runner picks the forge from the provider of the
checkout". Precisely: the PR row has `host`, `org`, `repo`. Look up an enabled provider with that `host`. Use its
`forge`. With none, use the built-in default for the host. With none of those, the row fails `no_forge` saying which
host and how to set a provider. The checkout is only needed for section 5.

## 2. The credential rule, and what each command must answer

**The rule.** Atrium holds the NAME of a command (`gh`, `bb`) and a host, never a token. No `GH_TOKEN`, header or key
in a row, a setting, an export, a log or `atrium.requirements.yaml`. Anything that stores a credential is out. No
code path runs `gh auth token`, `gh auth status --show-token` or the equivalent.

**What each forge's command must answer.**

| question | `gh` | `bb` | `glab` |
| --- | --- | --- | --- |
| installed | `gh --version` | `bb --version` | `glab --version` |
| logged in to host H, as whom | `gh auth status --hostname H` | the CLI's status verb | `glab auth status --hostname H` |
| scopes held | the `Token scopes:` line of `gh auth status` | not applicable | the same line |
| read a PR, its diff, a list | section 3 | verbs checked in stage 4 | later |

`bb` and `glab` verbs are named when their stage is built, against the installed version. They are not guessed here.

**Three states, never a crash.** Every forge error carries a code, so the board says what to fix:
- `no_cli`: the command is not on the room's PATH. "gh is not installed on sg3."
- `not_signed_in`: the command ran and says there is no login for H. "gh is not logged in on sg3 for github.com."
- `missing_scopes`: logged in, short of a scope the stage needs. "gh on sg3 lacks read:org."

Everything else is `other` with the first line of the CLI's stderr, as `runBounded` already returns.

**Scopes.** Stage 1 needs `repo` (read a private repo's PRs) and `read:org` (the review-requested search in an org).
These two are what to verify against a real `gh auth status` at build, and the requirement file below is where they are
named, so changing them is one line. A fine-grained or app token prints no `Token scopes:` line. Then the answer is
`scopes: unknown`, which is a warning on the room row and not a failure, because a run that reads fine must not be
refused on a line the CLI does not print.

**The requirement, and the preflight.** `f-new-gh-login-requirement` and r-018:

- `atrium.requirements.yaml` gains a `forges:` block, a map from host to what the room needs:

  ```yaml
  forges:
    github.com: { cli: gh, scopes: [repo, read:org] }
  ```

  `cli` is a name from the same fixed table the preflight uses, and `scopes` is a list of strings. Nothing else is
  accepted, so the file cannot carry a secret and a lint of it can say so.
- `POST /v1/preflight` (`internal/daemon/preflight.go`) gains a `forge_auth` list beside `runner_auth`, `tools` and
  `env_present`. Its entries are `host` names, validated as hostnames. A fixed table maps the CLI key to its argv, as
  `preflightRunners` maps `claude` to `auth status`, so the request names keys and never commands. The answer item
  gains `scopes` and a `code` from the list above. Output is bounded while read, as every preflight command is.
- The room's page shows one line per required forge, and the PR source and the PR runner check the same answer before
  they start: "gh is not logged in on sg3" appears before a run, not halfway through one. The check is made at daemon
  start, when the pulls view opens and when a provider with a forge is saved. Never on a timer.
- The fix is `human`: `ssh -t <room> gh auth login`. The requirement report says so, as it does for `runner_auth`.

## 3. What the forge answers

The interface, as the runner and the sources see it. Every method takes a context and a PR reference
`{Host, Org, Repo, Number}` (or a repo reference for the lists), runs through the bounded runner (timeout, read cap,
`GIT_TERMINAL_PROMPT=0`) and returns Go values in atrium's shape, not the CLI's.

| method | returns | `gh` spelling | needed first by |
| --- | --- | --- | --- |
| `View(ref)` | title, author, head sha and ref, base ref, cross-repo flag, requested reviewers, files, state, draft | `gh pr view N --repo R --json title,author,headRefOid,headRefName,isCrossRepository,baseRefName,reviewRequests,files,state,isDraft` | the runner |
| `Diff(ref)` | the unified diff, capped | `gh pr diff N --repo R` | the runner |
| `Head(ref)` | the head sha only | `gh pr view N --repo R --json headRefOid` | the head check |
| `FetchSpec(ref)` | git remote URL and the ref holding the head | `https://host/org/repo.git`, `pull/N/head` | the runner |
| `PRURL(ref)` | the PR's web URL | `https://host/org/repo/pull/N` | the runner, the view |
| `List(repo)` | open PRs, newest first, capped | `gh pr list --repo R --json number,title,author,url,headRefOid,updatedAt` | the PR list |
| `ReviewRequested(host)` | PRs requesting review from the signed-in user | `gh search prs --review-requested=@me --state open --json ...` | the P3 source door |
| `ForBranch(repo, branch)` | the open PR for a branch, or none, or an error if two | `gh pr list --head B --repo R --json number,url --limit 2` | the card's PR chip |
| `Auth(host)` | signed in, login, scopes, and the code of section 2 | `gh auth status --hostname H`, `gh api user --jq .login` | preflight, the review-requested test |

**Which first.** The runner needs `View`, `Diff`, `FetchSpec` and `PRURL`: they are today's calls. The pulls P3 source
door needs `ReviewRequested`, `Auth` and `Head`. The list per provider needs `List`. `ForBranch` has no consumer until
the card's PR chip (`pr-ci-state-design.md`), so it is in the interface from the start, to avoid reshaping it later,
and implemented in stage 1a because it is one command, but nothing calls it before stage 5.

**Rules.**
- **Read only.** No method writes to a forge.
- **Capabilities.** A forge says which methods it has. Where `bb` lacks a diff, the runner computes
  `git diff base...head` in the `src/` it already fetches. A missing method is a named fallback and never a nil.
- **The shape out is atrium's.** The runner reads `Head`, never `headRefOid`, so a second forge can fill the same
  struct from different JSON.
- **The canonical key.** `Key(ref)` is `host/org/repo#N` with host and org lower-cased, so a paste of `.../pull/5/files`
  and a source's `.../pull/5` and a GHE URL are one key. Sections 4 and 6 key on it.

## 4. Intake: reuse, do not invent

There is no second intake. `POST /v1/intake` and sources serve the inbox. `POST /v1/prs` serves reviews, and
`pulls-view-design.md` section 6 already says every door ends there.

**The PR list per provider** is door 3 of that section, "a source that lists PRs", with the forge doing the listing.

**Options.**
- A. The board calls `List` on page load and shows it, storing nothing.
- B. A source row per provider calls `List` and `ReviewRequested` on a timer and posts to `POST /v1/prs`.
- C. Both, with A as a live browse.

**Recommend B for stage 1.** A cannot be deduped or filtered and costs a forge call per page load. A provider with a
forge gets a button "list PRs", which creates a source row whose argv is the built-in `atrium forge list --host H`,
printing intake-shaped items. The source runner supplies the timer, the bounds and the three-failures switch-off.

- **Dedupe key.** `source` plus `external_id`, as intake already does. `source` is `forge:<host>` and `external_id`
  is the canonical key of section 3. At `POST /v1/prs` the existing rule applies, same PR and same head is one row.
- **Review on arrival** is only for a PR whose review is requested of the signed-in user, decided in
  `pulls-view-design.md` section 11. `Auth` supplies the login. Every other found PR waits on its `review` button.
- **The recogniser rows** that turn a pasted URL into these captures are `r-new-scm-recognisers-salvage`. A
  Bitbucket row there needs stage 4's `bb` forge to read anything, but resolving the URL to a card does not.
- **The view** groups rows by host with a count and one line of forge state per host: `github.com: 14 open, read 2m
  ago`, or `gh is not logged in on sg3`.

## 5. A worktree for a PR, so gwt is not the only way in

A provider already computes `<worktree_root>/<org>/<repo>/<branch>` (`worktreeDest` in `internal/api/providerapi.go`)
and makes it with `git worktree add` through `POST /v1/providers/{name}/worktrees`. A PR checkout is that, with the
branch and the fetch supplied by the forge.

**Options.**
- A. Always a detached worktree at the PR head.
- B. A branch named for the PR head ref, like gwt.
- C. The real head branch for a same-repo PR, and `pr-<N>` for a fork PR.

**Recommend C.** A same-repo PR's real branch is what gwt does and what an author iterating wants. A fork PR's head
ref is the fork's `main` or `fix`, which collides with the repo's own branches, so it gets `pr-<N>`. `View` returns
`headRefName` and `isCrossRepository`, so the choice is data. The `flattenBranch` rule already turns `feature/x` into
`feature-x`, so the directory is predictable, and a collision is already reported rather than overwritten.

**The verb.** `POST /v1/providers/{name}/worktrees` accepts `pr: N` beside `org` and `repo`, and drops `branch`. The
daemon then:
1. finds the provider for `host/org/repo` and its `forge`, and refuses with a sentence if worktrees are off, as now;
2. refuses, as now, when the repo is not a checkout here: "atrium does not clone". The row's `src/` fetch is
   unaffected, so a review still runs;
3. calls `View` for the head and `FetchSpec` for the ref, runs `git fetch origin <ref>` in the repo, then
   `git worktree add` at the destination, creating the branch at the fetched head;
4. is idempotent, as the verb is: a worktree already on that branch is the answer, with `existed: true`. A branch that
   exists at a different head than the PR's is reported, never moved.

Nothing here pushes, and it never touches a credential: the fetch uses whatever the repo's own `origin` already has.

**The way in.** Three doors, one function: a "check out" button on a PR row in the pulls view, the recogniser's
`prepare` for a PR URL (`scm-design.md`: `prepare` runs when `cwd` does not exist, and the provider join that doc calls
"not built" is this verb), and `atrium open <pr url>`. The launch dialog's directory then points at the path the verb
returns. A card started there is an ordinary card in a worktree. `gwt pr` keeps working and the next discovery adopts
its worktrees as it does today.

## 6. Hub or room: where the forge call runs, and which room runs a PR

**Where the call runs: the room.** Providers, checkouts, the CLI and its login are per machine. The room has `gh`, the
reviews root and the checkout. The hub proxies `/v1/prs` to a room as it proxies `/v1/tasks`, and it never calls a
forge and holds no credential.

**Which room runs a PR** (`f-new-pr-room-and-dedupe`: "room" there is an atrium room, a machine). Providers, checkouts
and logins differ by room, so a PR needs a rule.

The hub asks, at claim time, each online room one question, `can_run {key}`, and the room answers from what it
already knows with no new standing index on the hub:
`{login: ok | not_signed_in | no_cli | missing_scopes, checkout: present | absent}`.

**The rule, in order.**
1. Candidates are online rooms whose login is `ok` for the PR's host. A room that cannot read the forge cannot run
   the review, whatever it has on disk.
2. Prefer a candidate with the checkout `present`. That is clint's rule: the room with the repo's checkout and a
   working login. A review needs only the login, because the runner fetches its own `src/`, so a room with a login and
   no checkout is a candidate, ranked after one with both. It is the room that can run the PR but cannot make section
   5's worktree.
3. **The tiebreak, stated.** Among equals: the room that already holds a row for the key (sticky), then the room the
   request came from, then the room with the fewest runs in flight, then the lowest room name. Each step is
   deterministic, so two hubs asked the same question give the same answer.
4. No candidate: the PR is `unplaced` on the hub with the reason per room ("sg3: gh not logged in, m1mini: gh not
   installed"). It is not queued on a room that cannot read it.

**One row per PR across rooms.** The hub keeps a small claim table, `pr_claim(key, room, row_id, source, claimed_at)`,
keyed on the canonical key. It is an index, not a copy: findings, diffs and the run folder stay on the room that holds
the row, and the room stays the source of truth.

**Options.**
- A. A room asks the hub before it creates a row.
- B. Rooms create freely and the hub folds duplicates when it reads.
- C. The hub alone creates rows and hands them to a room.

**Recommend A, with B as the safety net.** C would move the doors (sources, paste, gwt) off the rooms they run on.
B alone pays for two runs before it notices the second.

The flow:
1. A source on room R finds PR K, or a paste reaches R. Before creating a row, R sends `claim {key, source}` to the hub.
2. The hub looks K up. A live claim names the owner. Otherwise it applies the rule above and records the owner.
3. If the owner is R, R creates the row and starts the run. If the owner is another room O, R creates **nothing** and
   the source logs "held by O". When the hub chose O and O never found the PR itself, the hub forwards the
   `POST /v1/prs` to O, over the proxy it already has.
4. **A source enabled on two rooms yields one row and one run.** Both rooms find K, the first claim wins by the rule,
   and the second is told the owner. The view shows the row with the room that holds it, as a `room` field on the row
   (`pulls-api.md` row) and a column in the pulls view.
5. **A hub that cannot be reached** does not stop a review clint pasted. R proceeds, records `claim: pending`, and
   claims when the link returns. If another room claimed first meanwhile, the hub names the owner and the later row is
   marked `folded into O` and aborted if it has not started. If both already ran, the hub does not delete either, it
   shows both with the fold named, because deleting a finished review is worse than showing two.
6. **The safety net.** The pulls view through the hub folds any two rows with one key and different rooms to the one the
   claim names, with the other labelled.
7. **An owner that goes away.** A claim whose room has been offline for ten minutes, with the row not yet `running` or
   `ready`, is released, and the next claim places it again. A `ready` row stays with its room, since the findings
   are on that room's disk, and it is reachable when the room returns.
8. **Room handoff.** When a PR card moves, `moved_to` updates the claim's room, so the key keeps one owner.

**Tests** (`f-new-pr-room-and-dedupe` asks for two fake rooms): both fake rooms report K and one row results. A room
that cannot read the forge is never chosen. The room with the checkout beats the room without when both are logged in.
The tiebreak is stable across ten runs. An unreachable hub, a later claim and a fold, as in 5. A released claim places
again.

## 7. Staging, owners, acceptance, and what each unblocks

Each stage is useful alone. Acceptance is run in a throwaway room with its own `ATRIUM_LOCATION`, and stage 3 with two.

### Stage 1: GitHub through `gh`, the PR list per provider, and the login check

Three build items that can run side by side after 1a's interface is merged.

**1a. `r-new-forge-interface`. @runtime. About 2 days.**
- `internal/forge`: the interface of section 3, the `gh` implementation, error codes, the canonical key, the provider
  `forge` and `forge_cmd` columns (migration at the end of the slice) with the built-in `github.com` fallback.
- Move every "runtime, the real moves" site in section 8 onto it. The runner takes a `Forge`, and a fake forge in
  tests proves nothing in the runner still names `gh`.
- `atrium forge list --host H`, the argv a source row calls.
- **Acceptance:** `prrunner_test.go` passes with a fake forge. A review of PR 378 at `ad5ddf4` produces a `pr.json`,
  `pr.diff` and `bundle.md` identical to the run before the change. `grep -n '"gh"' internal/daemon/prrunner.go` finds
  nothing. With `gh` off the PATH the row fails `no_cli` with "gh is not installed". A non-`github.com` host is passed
  as `host/org/repo`, as now.
- **Unblocks:** itself, and `r-new-scm-recognisers-salvage` (where the host goes).

**1b. The PR list per provider. @ui, with @runtime for the source row. About 2 days.**
- The "list PRs" button on a provider with a forge, the source row of section 4, and the pulls view grouped by host
  with a count and the forge-state line.
- **Acceptance:** with a provider on `github.com` and `gh` signed in, the button fills the pulls view with the user's
  open PRs in that provider's repos within one source tick, under `github.com`. Pressing it twice makes no second row.
  A PR also pasted by URL is one row. Signed out, the group says "gh is not logged in" and makes no row. A found PR
  not requested of the user waits on `review` and starts nothing.
- **Unblocks:** the pulls P3 source door, the pulls E2E off sg4.

**1c. `f-new-gh-login-requirement`. @fabric, with a small @runtime part. About 1.5 days.**
- The `forges:` block of `atrium.requirements.yaml`, the `forge_auth` key in `POST /v1/preflight` (runtime), the hub's
  per-room report, and the room page line of section 2.
- **Acceptance:** a room with `gh` logged out reports "gh is not logged in on <room> for github.com" before any run,
  with the fix `ssh -t <room> gh auth login`. A room whose token lacks `read:org` reports `missing_scopes` naming it.
  A fine-grained token reports `scopes: unknown` as a warning. `gh auth token` is not run, which a test asserts by
  recording the argv. The requirements file rejects any key outside `cli` and `scopes`.
- **Unblocks:** itself, the 378 acceptance replay, and the login half of stage 3's `can_run`.

### Stage 2: a worktree for a PR. @runtime, with @ui for the button. About 1.5 days.

- Section 5: `pr: N` on the provider worktree verb, the recogniser `prepare` for a PR URL, the "check out" button.
- **Acceptance:** a same-repo PR gives `<worktree_root>/<org>/<repo>/<branch>` on the PR's real branch, a fork PR gives
  `.../pr-<N>`, and pressing it twice answers `existed: true`. A repo with no checkout refuses with the existing
  "atrium does not clone" sentence and the review still runs. A card started in the worktree is an ordinary card.
  `gwt` is not on the path in the test.
- **Unblocks:** the provider join `scm-design.md` calls not built, and paste-a-link-get-a-card
  (backlog-2026-09-14-003) through `r-new-scm-recognisers-salvage`.

### Stage 3: which room runs a PR, and one row across rooms. @fabric, with @runtime for `can_run` and `claim`. About 3 days.

- Section 6: `can_run`, `pr_claim`, the placement rule and tiebreak, the claim call from the source and `POST /v1/prs`,
  the hub forward, the fold, release and handoff, and the `room` field and column.
- **Acceptance:** two fake rooms report the same PR and one row and one run result, shown with its room. The room with
  checkout and login wins over a room with login only, and a room with no login never wins. A source enabled on both
  rooms of a real pair, sg4 and sg3, makes one row. An unreachable hub, a later claim and a fold behave as in 6.5.
- **Unblocks:** `f-new-pr-room-and-dedupe`, the pulls E2E off sg4, room handoff for PR cards.

### Stage 4: Bitbucket through `bb`. @runtime. About 2 days.

- A `bb` implementation, the `git diff base...head` fallback where it has no diff, and a provider with
  `forge: bitbucket`.
- **Acceptance:** a Bitbucket PR URL makes a row and a full review with the runner unchanged from stage 1. The only
  code outside `internal/forge` that changes is recogniser rows and the preflight key table. If the runner needs an
  edit, the interface was wrong and stage 1 is reopened.
- **Unblocks:** `r-new-scm-recognisers-salvage` in full, and the pulls E2E on a non-GitHub repo.

### Stage 5: read-back, CI state and lifecycle. @runtime for the methods, @ui for the surfaces. About 3 days.

- `Comments(ref)` and `Checks(ref)` on the interface, the PR chip, the moved-head mark from `Head`, the review tab's
  read-back.
- **State arrival, for `r-new-pr-lifecycle`.** Options: a bounded poll, a webhook, or on demand. **Recommend a bounded
  poll plus on demand:** the owning room calls `Head` and `View` for each row that is not archived or terminal, every
  ten minutes and whenever the pulls view opens, stops for a merged or closed PR, and never polls a row nobody asked
  for. No webhook, because that needs a listener and a secret (`scm-design.md`). `intake-design.md`'s "never poll
  ticket state" is amended to say it applies to support tickets, where state is the customer's, and not to a PR a
  person asked atrium to review.
- **Acceptance:** a PR with three existing comments shows them beside the diff, read-only, and a posted finding is
  matched and marked done with a link (`u-new-review-tab-read-back-comments`). A new head on a reviewed PR offers a
  re-run and keeps the earlier findings marked against the old head. A merged PR leaves the default view and stays
  reachable by filter. Signed out, all of it degrades to nothing with the reason shown once.
- **Unblocks:** `u-new-review-tab-read-back-comments`, `r-new-pr-lifecycle`, and the card's PR chip.

### Stage 6: only on clint's ask

GitLab through `glab`. `PostPendingReview` (the P4 of the pulls view). A hub mirror as an alternative `FetchSpec`.

| item that waits | unblocked by |
| --- | --- |
| `r-new-forge-interface` | 1a is this item |
| `r-new-scm-recognisers-salvage` | 1a, then stage 2 and stage 4 for the whole |
| `f-new-gh-login-requirement` | 1c is this item |
| `f-new-pr-room-and-dedupe` | stage 3, using 1c's login answer |
| `r-new-pr-lifecycle` | stage 5 |
| `u-new-review-tab-read-back-comments` | stage 5 |

## 8. Every call site that moves

`r-new-forge-interface` moves these. Found by grepping `internal/` and `cmd/` for `gh pr`, `"gh"`, `pull/` and
`github.com`. Line numbers are at commit 6b407bbd.

**Runtime, the real moves**

| site | what it hardcodes | moves to |
| --- | --- | --- |
| `internal/daemon/prrunner.go:559` | `gh pr view N --repo R --json ...` in `fetch()` | `Forge.View` |
| `internal/daemon/prrunner.go:581` | `gh pr diff N --repo R` | `Forge.Diff` |
| `internal/daemon/prrunner.go:523` `repoArg` | `org/repo`, or `host/org/repo` off github.com | inside the `gh` implementation |
| `internal/daemon/prrunner.go:535` `prURL` | `https://host/org/repo/pull/N`, host defaults to `github.com` | `Forge.PRURL` |
| `internal/daemon/prrunner.go:655` to `660` `fetchSource` | remote `https://host/org/repo.git` and refspec `pull/N/head` | `Forge.FetchSpec` |
| `internal/daemon/prrunner.go:163` | the log label special-cases `c.Name == "gh"` | the forge supplies the label |
| `internal/daemon/prrunner.go:507` to `520` `ghPRView` | the `gh` JSON shape used as the runner's own type | the forge's returned struct |
| `internal/daemon/prrunner.go:567`, `:570` | error text "gh pr view ..." | the forge's error |
| `internal/daemon/preflight.go:57` | `"gh": {"gh","--version"}` in the tool table | kept, plus the `forge_auth` key table of section 2 |

**Designed but landing with P3, to be written against the interface and not against `gh`**

| site | what it hardcodes |
| --- | --- |
| `pulls-view-design.md` section 6 | `gh api user --jq .login` and `reviewRequests` for the review-requested test: `Auth` and `View` |
| `pulls-view-design.md` section 6 door 3 | `gh search prs --review-requested=@me` as a source row in `scripts/`: `ReviewRequested` |
| `pr-ci-state-design.md` sections 1 and 2 | `gh pr list --head B` and `gh pr view --json statusCheckRollup,...`: `ForBranch`, `Checks` |

**GitHub-shaped text that is not a call, and is the recogniser's concern (`r-new-scm-recognisers-salvage`)**

| site | note |
| --- | --- |
| `internal/store/recognisers.go:40` and `:78` | comments naming `/pull/5/files` and `gh issue view`, and the seeded rows |
| `internal/store/prs.go:503` and `:587` | `RunFolder` defaults the host to `github.com`, and a comment names `gh pr view` |
| `internal/prreview/render/types.go:40` | `PRURL` documented as `github.com/<org>/<repo>/pull/<n>`, and the golden files hold it |
| `internal/api/web/index.html:766` and `:2321` | the paste placeholder and the recogniser example pattern |
| `internal/api/web/js/terminal-links.js:429` to `438` | the PR link walk keys on `github.com` and `/pull/` |
| `internal/daemon/launch.go:83` | a comment about turning a PR URL into a worktree |

**Not moved, on purpose.** `internal/daemon/recognise.go` (`fetch`) and `internal/daemon/sources.go` run the
operator's own argv, which names `gh` in the operator's row. That is already a name the operator wrote. The forge does
not replace it. `internal/hubstore/docs_secrets.go:53` is a secret-scan rule for the string `gh`, unrelated.

## 9. How this relates to the hub-forge, and how they stay apart

`docs/rnd/hub-forge-design.md` makes the **hub** a git server for rooms: rooms push finished work to it, other rooms
fetch from it, and `atrium_git_url` says where. It answers "where do I get the work a swarm made". It has no PRs,
reviewers or CI.

The forge here reads what **GitHub** (or Bitbucket) says about a PR the world made. It has no git store and serves no
clones.

| | hub-forge | this forge |
| --- | --- | --- |
| lives on | the hub | the room, as a CLI call |
| holds | bare repos, push log | nothing |
| speaks | smart HTTP git | `gh`, `bb`, `glab` |
| credential | the machine's hub certificate | the CLI's own, never atrium's |
| ends at | a branch the orchestrator merges and pushes on | a PR row in the pulls view |

**Where they meet:** `FetchSpec`. When the hub holds a mirror of a repo, `atrium_git_url` could answer a nearer URL for
the same fetch. That is a stage 6 option and not a dependency. The forge never reads the hub's store and the hub never
calls a forge. The one thing the hub does for PRs is section 6's placement and claim table, which are about rooms and
not about git.

**The name.** Docs call the hub's feature "the hub as a forge" and this design says "forge". In code, this is
`internal/forge`, and the hub's is `internal/gitsync` and `internal/hubstore`. The hub's feature stays "the hub as a
forge" and never "forge" alone.

## 10. What is out

- **No credential in atrium.** A row holds a command name and a host. The requirement file holds a CLI key and scope
  names.
- **No HTTP client for any forge API.** If `gh` cannot do it, the answer is a different `gh` call or no feature.
- **No webhook and no listener.** A source on a timer, as `scm-design.md` rules.
- **No second intake.**
- **No write to a forge before P4.** The interface has no write method until P4 is ordered, and that is its own design
  because it changes "read only".
- **The hub does not call a forge, and a forge is not the hub.**
- **Atrium does not clone.** Section 5 makes a worktree of a checkout that exists.

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
