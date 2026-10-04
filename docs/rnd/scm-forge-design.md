# The forge: one interface over GitHub, Bitbucket and the rest, and a PR list per provider (rnd-new-scm-forge)

Status: design by @rnd, 2026-10-04, ordered by clint ("keep going with scm, move it along"). Design only, nothing
under `internal/` or `cmd/` changes.

**Read this first.** The item file `docs/backlog/rnd/rnd-new-scm-forge.md` and the six waiting items are not in this
checkout. `QUEUE.md` says the item is held "on sg4". The brief says the item lists seven questions and does not
list them. So the seven below are the ones the brief's own reading list implies, and the last section asks clint to
correct any that differ. The six waiting items are described from their names and the brief, and section 11 says
which stage unblocks each. Where an item's real text disagrees with this, the item wins and the stage is cut again.

Read for this design:
- `docs/runtime/providers-design.md` (built): a provider is a directory layout on this machine, with a `host`.
- `docs/runtime/scm-design.md` (built): the recogniser table, `fetch`, `prepare`, and the outbound config files.
- `docs/runtime/intake-design.md` (built): `POST /v1/intake`, `source` plus `external_id` as the dedupe key, sources.
- `docs/rnd/pulls-view-design.md` and `docs/rnd/pulls-api.md`: P1 and P2 landed, P3 in review.
- `docs/rnd/pr-ci-state-design.md`: reads PR and CI state with `gh`.
- `docs/rnd/hub-forge-design.md` (commit 1460c81a and its revisions).
- `internal/daemon/prrunner.go`, the one runner that hardcodes `gh` today.

## 0. The answer, in one paragraph

A **forge** is a small Go interface in the room daemon with six read operations: list PRs, view a PR, read its diff,
name its URL, say where its head can be fetched, and say who the signed-in user is. A forge implementation does not speak HTTP and
holds nothing. It builds an argv for a named CLI (`gh` first, `bb` and `glab` later), runs it through the bounded
runner the PR runner already has, and parses what the CLI prints. The forge is chosen by the **host** of a PR URL, and
the host is already captured by the recogniser and already on the `pr_review` row, so the join to a provider is the
`host` field `providers-design.md` added for exactly this reason. The pulls view gets a **PR list per provider**, filled
by one source row per provider that calls the forge's list operation and posts what it finds to `POST /v1/prs`.
Nothing about this is the hub-forge. The hub-forge hosts git through the hub. The forge here talks to GitHub, and the
only place the two meet is a clone URL (section 6).

Stage 1 is GitHub through `gh` behind the interface, with the pulls view's per-provider list. It is useful alone and
changes no behaviour a user can see except the list.

## 1. What is a forge, and what is it not

A forge here is **the place where a PR lives and the command that reads it.** It is not a provider (a directory
layout), not a recogniser (a URL shape), not a source (a timer) and not the hub's git store. Each of those stays.
The forge is the thing those four call when they need a fact only the forge's CLI has.

| thing | question it answers | built |
| --- | --- | --- |
| recogniser | what is this URL | yes |
| provider | where does `org/repo` live on this disk | yes |
| source | what work should I look at on a timer | yes |
| **forge** | **what does the forge's CLI say about this PR, and how do I list them** | no |
| hub-forge | where do rooms push and pull finished work | stage 4 built |

**Options.**
- A. No new noun. Put a `forge` column on the recogniser and let its `fetch` argv do the work.
- B. A Go interface keyed by host, with one implementation per CLI. A recogniser still matches, the forge reads.
- C. A table of forge rows the operator edits, each row holding argv templates for every operation.

**Recommend B.** A fails because the PR runner needs five operations and a recogniser row has one `fetch`. C is the
pull of the whole design toward configuration: a row with eight argv templates and a JSON shape for each is a plugin
system, and the shape of `gh pr view --json` is not something an operator should retype. B keeps the argv in code
where it is tested, and keeps the one thing that varies per machine (the command name and its host) as data in
section 3.

## 2. Where it attaches

**Options.**
- A. To the provider. A provider row gains `forge` and the forge reads `host` from it.
- B. To the host string alone, with a small settings table `forge_host(host, kind, cmd)`.
- C. To the recogniser row that matched.

**Recommend B, joined to a provider when one exists.** The brief says the forge attaches to a provider. It can, but a
provider is a local directory, and the pulls view reviews PRs in repositories nobody has cloned (the runner fetches a
blobless shallow copy into its own `src/`). A forge that needed a provider could not read a PR in a repo with no
provider row. So the forge is keyed by host, and a provider with the same `host` shows it as a badge and offers it
for the PR list. The provider's `host` field is the join, as `providers-design.md` already says in "The word, first".

The settings row is `host`, `kind` (`github`, `bitbucket`, `gitlab`) and `cmd` (the NAME of the CLI, default `gh`).
`github.com` with `gh` is the default and needs no row. A GitHub Enterprise host is one row with `kind: github`. This
is the same shape `repoArg` already half-knows: it passes `host/org/repo` to `gh` for any host that is not
`github.com`.

**The credential rule, said once.** The row holds a command NAME and a host. It holds no token, no `GH_TOKEN`, no
header, and the importer of `scm-design.md` part two checks for that. Anything that stores a credential is out.

## 3. The contract

The interface, as the runner sees it. All methods take a context and a PR reference
`{Host, Org, Repo, Number}`, run through the bounded runner (timeout, read cap, `GIT_TERMINAL_PROMPT=0`), and
return Go values. Errors are the first line of the CLI's stderr, as `runBounded` does now.

| method | what it returns | `gh` spelling in stage 1 |
| --- | --- | --- |
| `View(ref)` | title, author login, head sha, base ref, requested reviewers, files, state, draft | `gh pr view N --repo R --json title,author,headRefOid,baseRefName,reviewRequests,files,state,isDraft` |
| `Diff(ref)` | the unified diff, capped | `gh pr diff N --repo R` |
| `List(host, query)` | PR refs with title and author, newest first, capped | `gh pr list --repo R --json number,title,author,url,headRefOid,updatedAt --limit N`, and `gh search prs ...` for review-requested |
| `PRURL(ref)` | the web URL of the PR | `https://host/org/repo/pull/N` |
| `FetchSpec(ref)` | the git remote URL and the ref that holds the head | `https://host/org/repo.git`, `pull/N/head` |
| `Whoami(host)` | the signed-in login, or "not signed in" | `gh api user --jq .login`, per host |

Rules of the contract:
- **Read only.** No method writes to the forge. Write-back is section 9 and is not stage 1.
- **A missing CLI or a signed-out CLI is a state, not a crash.** The error carries a code (`no_cli`, `not_signed_in`,
  `other`) so the pulls view says "run `gh auth login`" and `f-new-gh-login-requirement` has something to key on.
- **The shape out is atrium's, not the CLI's.** `View` returns a Go struct, so a `bb` implementation can fill it from
  different JSON. The runner never reads `headRefOid` again, it reads `Head`.
- **Capabilities.** A forge says which methods it implements. Bitbucket's `bb` may lack a diff, and the runner then
  gets the diff from `git diff base...head` in `src/`, which it already fetches. A missing capability is a named
  fallback and not a nil.

## 4. Which forges, in what order

**Options.** GitHub only. GitHub then Bitbucket. GitHub, GitLab and Bitbucket together.

**Recommend GitHub, then Bitbucket, and GitLab only on ask.** `scm-design.md` names `bb pr show {num}` and
`docs/rnd/bb.md` exists, so Bitbucket is the second forge clint expects. Building a second implementation behind a
one-implementation interface is the test that the interface is real, so stage 2 is Bitbucket and stage 1 does not
guess its shape. GitLab is a one-row addition after that and waits for a reason.

## 5. The PR list per provider

The pulls view today lists rows that something posted. Nothing finds PRs. The ask is a list per provider.

**Options.**
- A. The forge's `List` is called by the board on page load, and shown, never stored.
- B. A source row per provider calls `List` on a timer and posts to `POST /v1/prs`, so found PRs become rows.
- C. Both: B for rows, plus a lazy "browse" in the view that calls `List` live.

**Recommend B for stage 1.** There is no second intake: `pulls-view-design.md` section 6 says every door ends in
`POST /v1/prs`, and door 3 is already "a source that lists PRs". The list is that door with the forge doing the
listing. Concretely, one source row per provider, created from the provider's pane with a button "list PRs from this
provider". Its argv is a built-in subcommand, `atrium forge list --host H`, which calls `List` and prints the items in
the intake shape. The existing source runner supplies the timer, the bounds, and the three-failures switch-off.

**Dedupe is the intake key.** `source` plus `external_id`. For a PR the `external_id` is `host/org/repo#N`, and the
`source` is `forge:<host>`. The URL is not the key, for the reason `intake-design.md` already gives. A PR also found
by a second door (a paste, gwt) is the same PR at `POST /v1/prs`, where the existing rule applies: same PR, same head,
one row.

**The view.** The pulls view groups rows by provider host with a count per group and a one-line state for each forge:
`github.com: 14 open, last read 2m ago` or `github.com: gh is not signed in`. A queued PR has the `review` button the
design already gives it. **A found PR is never reviewed on arrival unless the review is requested of the signed-in
user**, which is decided in `pulls-view-design.md` section 11, and `Whoami` is what answers it.

Option A fails because a list that is not stored cannot be dedupe-checked or filtered, and it makes every page load
a call to the forge. C is a stage 3 nicety.

## 6. How this relates to the hub-forge, and how they stay apart

`docs/rnd/hub-forge-design.md` makes the **hub** a git server for rooms: rooms push finished work to it, other rooms
fetch from it, and `atrium_git_url` says where. It answers "where do I get the work a swarm made". It has no PRs,
no reviewers, no CI, and it is not a place a human reviews anything.

The forge here is the opposite. It reads what **GitHub** (or Bitbucket) says about a PR the world made. It has no
git store and serves no clones.

| | hub-forge | this forge |
| --- | --- | --- |
| lives on | the hub | the room, as a CLI call |
| holds | bare repos, push log | nothing |
| speaks | smart HTTP git | `gh`, `bb`, `glab` |
| credential | the machine's hub certificate | the CLI's own, never atrium's |
| ends at | a branch the orchestrator merges and pushes on | a PR row in the pulls view |

**Where they meet, and the only place:** `FetchSpec`. The PR runner clones from `FetchSpec`'s URL. Today that is
`https://github.com/org/repo.git`. When the hub holds a mirror of that repo, `atrium_git_url` could answer a nearer
URL for the same fetch. That is a stage 3 option and not a dependency. The forge never reads the hub's store and the
hub never calls a forge. If a later design wants "the hub opens a PR on GitHub for a finished branch", that is
section 9's write-back through `gh`, run by a card or the operator, and not the hub holding a credential.

**The name.** Docs call the hub's feature "the hub as a forge", and this design says "forge". To keep them apart in
code and docs: the code package is `internal/forge`, the hub's is `internal/gitsync` and `internal/hubstore`, and the
hub's feature stays "the hub as a forge" and never "forge" alone.

## 7. Every call site that moves

`r-new-forge-interface` moves these. Found by grepping `internal/` and `cmd/` for `gh pr`, `"gh"`, `pull/` and
`github.com`. Line numbers are at commit 6b407bbd.

**Runtime, the real moves**

| site | what it hardcodes | moves to |
| --- | --- | --- |
| `internal/daemon/prrunner.go:559` | `gh pr view N --repo R --json ...` in `fetch()` | `Forge.View` |
| `internal/daemon/prrunner.go:581` | `gh pr diff N --repo R` | `Forge.Diff` |
| `internal/daemon/prrunner.go:523` `repoArg` | `org/repo`, or `host/org/repo` off github.com | inside the `gh` implementation |
| `internal/daemon/prrunner.go:535` `prURL` | `https://host/org/repo/pull/N`, host defaults to `github.com` | `Forge.PRURL(ref)` |
| `internal/daemon/prrunner.go:655` to `660` `fetchSource` | remote `https://host/org/repo.git` and refspec `pull/N/head` | `Forge.FetchSpec` |
| `internal/daemon/prrunner.go:163` | the log label special-cases `c.Name == "gh"` | the forge supplies the label |
| `internal/daemon/prrunner.go:507` to `520` `ghPRView` | the `gh` JSON shape used as the runner's own type | the forge's returned struct |
| `internal/daemon/prrunner.go:567`, `:570` | error text "gh pr view ..." | the forge's error |
| `internal/daemon/preflight.go:57` | `"gh": {"gh","--version"}` | one check per configured forge `cmd` |

**Designed but landing with P3, to be written against the interface and not against `gh`**

| site | what it hardcodes |
| --- | --- |
| `pulls-view-design.md` section 6 | `gh api user --jq .login` for the review-requested test, and `reviewRequests` |
| `pulls-view-design.md` section 6 door 3 | `gh search prs --review-requested=@me` as a source row in `scripts/` |
| `pr-ci-state-design.md` section 1, 2 | `gh pr list --head B` and `gh pr view --json statusCheckRollup,...` |

**GitHub-shaped text that is not a call, and is the recogniser's concern (`r-new-scm-recognisers-salvage`)**

| site | note |
| --- | --- |
| `internal/store/recognisers.go:40` and `:78` | comments naming `/pull/5/files` and `gh issue view`, and the seeded rows |
| `internal/store/prs.go:503` | `RunFolder` defaults the host to `github.com`, and `:587` comments `gh pr view` |
| `internal/prreview/render/types.go:40` | `PRURL` documented as `github.com/<org>/<repo>/pull/<n>`, and the golden files under `testdata/golden/` hold it |
| `internal/api/web/index.html:766` and `:2321` | the paste placeholder and the recogniser example pattern |
| `internal/api/web/js/terminal-links.js:429` to `438` | the PR link walk keys on `github.com` and `/pull/` |
| `internal/daemon/launch.go:83` | a comment about turning a PR URL into a worktree |

**Not moved, on purpose.** `internal/daemon/recognise.go` (`fetch`) and `internal/daemon/sources.go` run the
operator's own argv, which names `gh` in the operator's row. That is already the right shape, a name the operator
wrote. The forge does not replace it. `internal/hubstore/docs_secrets.go:53` is a secret-scan rule for the string
`gh`, unrelated.

Bitbucket's `pull-requests/N` path and `bitbucket.org` are in `scm-design.md`'s table only. Nothing in code handles
them yet, so there is nothing to move, and the recogniser rows for them are stage 2.

## 8. PR lifecycle

`r-new-pr-lifecycle` presumably covers a PR's states and what atrium does as they change (open, draft, checks, review
requested, merged, closed). This design gives it the read side only.

- `View` returns `state`, `draft`, and `Head`. A head that moved since the row's `head` is the "moved head" mark
  `pulls-view-design.md` already plans for P3.
- CI state (`statusCheckRollup`) is a sixth method, `Checks(ref)`, added when `pr-ci-state-design.md` is built. It is
  left out of stage 1 because stage 1 has no consumer for it.
- **Polling, not webhooks.** The forge is read on a timer by the source row, and on an open of the PR. No listener,
  no secret, as `scm-design.md` rules.

## 9. Write-back, and read-back of comments

`u-new-review-tab-read-back-comments` wants the review tab to show comments already on the PR. That is a read:
`Comments(ref)`, `gh pr view --json comments,reviews` or `gh api repos/R/pulls/N/comments`. It is stage 3, a read-only
method, and fits the contract with no change to the credential rule.

**Posting** (a review, a comment, a status) is a different matter. `scm-design.md` says atrium does not write back
and `pulls-view-design.md` section 10 says nothing is posted before P4, and P4 is a pending review clint submits.
This design does not reopen that. The interface has no write method in stage 1 or 2. If P4 is ordered, it adds
`PostPendingReview(ref, body)` as one more method, run as `gh` with the operator's credential, and it is a separate
design because it changes the "reads only" claim.

## 10. A PR room, and the login requirement

**`f-new-pr-room-and-dedupe`** (a room for a PR, deduped across doors). The dedupe is already `source` plus
`external_id` for intake and "same PR same head" for `POST /v1/prs`. The forge adds one thing: a **canonical key**,
`Forge.Key(ref)` = `host/org/repo#N`, lower-cased on host and org, so a paste of `.../pull/5/files` and a source's
`.../pull/5` and a GHE URL all produce one key. That key is the `external_id`. A room for a PR is out of scope here
and keys on the same string.

**`f-new-gh-login-requirement`.** A room with no signed-in `gh` cannot fetch. The contract's error codes (`no_cli`,
`not_signed_in`) are what that item reports on the room row, one line per configured forge host. The check is
`Whoami(host)`, run at daemon start and when the pulls view opens, not on a timer. The message names the command to
run and holds no token.

## 11. Stages, owners, acceptance, and what each unblocks

Each stage is useful alone. All acceptance is run in a throwaway room with its own `ATRIUM_LOCATION`.

### Stage 1: GitHub through `gh` behind the interface, and the pulls view's per-provider list

Two build items, so two owners can work at once.

**1a. `r-new-forge-interface`. @runtime. About 2 days.**
- `internal/forge`: the interface of section 3, the `gh` implementation, the host settings row of section 2 (default
  `github.com` with `gh`, no row needed), the error codes, and the canonical key.
- Move every "runtime, the real moves" site in section 7 onto it. The runner takes a `Forge`, and a fake forge is
  how the runner's tests stop shelling out.
- `atrium forge list --host H` as the subcommand a source row calls.
- **Acceptance:** `prrunner_test.go` passes unchanged in behaviour with the `gh` calls replaced by a fake forge. A
  review of 378 at `ad5ddf4` produces a byte-identical `pr.json`, `pr.diff` and `bundle.md` to the run before the
  change. `grep -n '"gh"' internal/daemon/prrunner.go` finds nothing. With `gh` off the PATH the row fails `no_cli`
  with "gh is not installed", not a parse error. A host that is not `github.com` is passed as `host/org/repo`, as now.
- **Unblocks:** `r-new-forge-interface` itself, and `r-new-scm-recognisers-salvage` (it can add `bitbucket.org` rows
  knowing where the host goes).

**1b. The per-provider PR list. @ui, with @runtime for the source row. About 2 days, after 1a's `List`.**
- A "list PRs" button on a provider with a `host`, which creates the source row of section 5.
- The pulls view groups rows by host with the count and the one-line forge state.
- **Acceptance:** with a provider on `github.com` and `gh` signed in, pressing the button fills the pulls view with
  the user's open PRs in that provider's repositories within one source tick, grouped under `github.com`. Pressing it
  twice makes no second row. A PR also pasted by URL is one row. Signed out, the group says `gh is not signed in` and
  no row is made. A found PR that is not review-requested waits on its `review` button and starts nothing by itself.
- **Unblocks:** `f-new-pr-room-and-dedupe` (the canonical key and the dedupe it keys on),
  `f-new-gh-login-requirement` (the codes and the `Whoami` call).

### Stage 2: Bitbucket through `bb`

- **@runtime. About 2 days.** A `bb` implementation, the `bitbucket.org` recogniser rows from the salvage item, the
  `git diff base...head` fallback where `bb` has no diff, and a host row `bitbucket.org, kind: bitbucket, cmd: bb`.
- **Acceptance:** a Bitbucket PR URL pasted into the pulls view makes a row and a full review, with the forge chosen by
  host and the runner unchanged from stage 1. The only code outside `internal/forge` that changes is the recogniser
  rows. If the runner needs an edit, the interface was wrong and stage 1 is reopened.
- **Unblocks:** `r-new-scm-recognisers-salvage` in full, and the claim that the interface is real.

### Stage 3: read-back, CI state and lifecycle

- **@runtime for the methods, @ui for the surfaces. About 3 days.** `Comments(ref)` and `Checks(ref)`, the PR chip of
  `pr-ci-state-design.md`, the moved-head mark from `View.Head`, and the review tab showing existing comments.
- **Acceptance:** the review tab of a PR with three existing comments shows them beside the diff, read-only. A card
  whose branch has a PR shows the chip, and a failing check names itself. Both degrade to nothing when the CLI is signed
  out, with the reason shown once.
- **Unblocks:** `u-new-review-tab-read-back-comments`, and `r-new-pr-lifecycle`'s read side.

### Stage 4: only on clint's ask

GitLab through `glab`. `PostPendingReview` (the P4 of the pulls view). A hub mirror as an alternative `FetchSpec`.

| item that waits | unblocked by |
| --- | --- |
| `r-new-forge-interface` | stage 1a is this item |
| `r-new-scm-recognisers-salvage` | 1a (where the host goes), stage 2 (full) |
| `f-new-pr-room-and-dedupe` | 1b |
| `f-new-gh-login-requirement` | 1b |
| `r-new-pr-lifecycle` | stage 3 (read side), P4 for any write |
| `u-new-review-tab-read-back-comments` | stage 3 |

## 12. What is out

- **No credential in atrium.** No token, header, `GH_TOKEN` or key in a row, a setting, an export or a log. The row
  holds a command name and a host.
- **No HTTP client for any forge API.** If `gh` cannot do it, the answer is a different `gh` call or no feature.
- **No webhook and no listener.** A source on a timer, as `scm-design.md` rules.
- **No second intake.** `POST /v1/intake` for inbox items and `POST /v1/prs` for reviews, as now.
- **No write to a forge before P4.**
- **The hub is not a forge client and a forge is not the hub.** Section 6.
- **No repository model.** A provider says where a repo is. The forge says what the forge thinks of a PR.

## Open questions for clint

1. **Are these the seven questions?** I could not read `docs/backlog/rnd/rnd-new-scm-forge.md`, which is held on sg4.
   The seven answered here are: what a forge is (1), where it attaches (2), the contract (3), which forges (4), the
   PR list per provider (5), the relation to the hub-forge (6), and write-back and read-back (9), with the call
   sites in 7 and the stages in 11. If the item asks something else, say what and I will add a section.
2. **Attach to host, or require a provider?** Section 2 keys the forge by host and only decorates a provider that
   shares it, so a PR in a repo you never cloned can still be read. If you want the forge to need a provider row,
   stage 1b is simpler and the pulls view cannot list PRs for a repo with no provider.
3. **Is the PR list a source row per provider, created by a button?** Recommended in section 5. The alternative is one
   global "my PRs" source for all of `gh`, which is less to set up and has no per-provider count.
4. **Bitbucket second, GitLab on ask.** Is that the order you want?
5. **Does a found PR that is review-requested of you start a review by itself?** `pulls-view-design.md` section 11
   says yes. This design keeps that and only asks you to confirm it still holds across providers, since `bb` may not
   answer "requested of me" at all.
6. **The six waiting items.** I had only their names. If any of them already fixes a shape that section 3 contradicts,
   tell me which and I will cut again.
