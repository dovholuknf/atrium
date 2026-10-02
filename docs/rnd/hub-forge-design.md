# The hub as a forge: clone any repo any card works in, from any room or any of clint's machines

> "any room pull from any room, and any room can inform any room of where their repo is, to fork and
> mediate/pr/push amongst the swarm" (clint, 2026-10-02)

So it is **peer to peer with the hub as the go-between and the index**, not only a central mirror:
- a room announces its repos and branches;
- any room fetches from any other through the hub;
- a change moves between rooms as a fork, a pull request (atrium's own, between rooms, carrying the change record
  and its verdicts) and a push.

Status: design by @rnd, 2026-10-02. Nothing built. This is git-sync stage 4, written as its own doc because it
changes what the hub is. Asked by clint through the orchestrator: the hub should act like GitHub would, so he can
clone a URL like `git@sg4.atrium:claude/ziti-tunnel-sdk-c.git` and have it work.

Read for this design, 2026-10-02:
- `docs/rnd/git-sync-design.md` (stage 1 built: bare mirrors of the repos in `git_repos`, rooms fetching
  `claude/main` over smart HTTP on the link's `git` connection kind, upload-pack only; the hub collecting rooms'
  `claude/*` into `refs/rooms/<room>/`; stage 3, the merge queue, waiting on decision 46);
- `internal/gitsync/` and `internal/link/git_hub.go`;
- `docs/rnd/security-design.md` section 1 and the reach table (loopback, an OpenZiti intercept, a zrok share);
- `docs/rnd/overlay-room-identity.md`;
- `docs/rnd/review-on-atrium-design.md` section 7.7.

## 0. The answer

- **The hub is the index and the go-between (section 1a).** Rooms announce what they hold, and a fetch of another
  room's branch passes live through the hub to that room. The hub's own copy is the cache and the fallback when that
  room is offline. A change moves between rooms as an atrium pull request with its change record attached.

- **Every repo any card works in is mirrored on the hub, with no hand-kept list.**
  - A room reports each card's repo (its checkout's `origin`) and branch.
  - The hub makes a bare mirror the first time it sees a repo.
  - The hub collects every room's `claude/*` branches, and the branch each live card's worktree is on, whatever its
    name.
  - It also fetches the repo's own branches and tags from its forge.
- **Any room and any of clint's machines clone by a plain URL**, `<hub>/git/<host>/<owner>/<repo>.git`, read-only.
  - **Smart HTTP is enough. No ssh server.** The hub serves the URL on the paths that already reach the board:
    loopback, the link for rooms, the OpenZiti intercept, and a zrok private share. Never on a public share.
  - One line of git config makes clint's own spelling work:
    `git config --global url."http://atrium.ziti/git/github/".insteadOf "git@sg4.atrium:"`.
  - An ssh server would bring keys, and so the account layer this design does not invent. It is a later option
    (question 2).
- **The refs look like GitHub's, plus the work in progress:**
  - `refs/heads/*` and `refs/tags/*` as the forge has them;
  - `refs/heads/claude/main` from the hub;
  - every room's branches under `refs/heads/rooms/<room>/<branch>`.

  So `git clone` gives clint what GitHub shows, and `git fetch origin rooms/sg4/claude/fabric` gives any reader a
  branch that exists only in a sg4 worktree.
- **An agent never guesses or asks where code lives.** `atrium_git_url` (control MCP, and
  `GET /v1/git/where` on the API) takes a repo, a branch, a card or a worktree path. It answers:
  - the hub URL to fetch from, on the caller's own reach;
  - the ref to fetch;
  - the sha;
  - which room the code came from;
  - when that room's copy was last collected.

  Every agent calls it first (section 3.1). Launching a reviewer where the code is was only a stopgap.
- **This answers the reviewer case today.** A reviewer on m1mini reads a branch that lives only in a sg4 worktree
  with one fetch from the hub, after sg4's next collect. There is no paste and no room-to-room route.
- **Read-only first. Push comes later** (stage F4):
  - a room pushes its own branches into its own namespace, instead of the hub collecting them;
  - a fork is a new name sharing objects;
  - landing is the hub-side merge of git-sync stage 3, so decision 46.

  F1 to F3 need no decision 46, because the hub holds copies, not the truth.
- **No accounts and no new auth.** Who can clone is who can reach the board: loopback, the link (the room's
  hub-signed certificate), and the overlay's own policy. If the board's operator token is turned on (security
  design stage 2), git sends the same token through `http.extraHeader`, set once by `atrium git setup`.

## 1. What is there today

- **Mirrors:** only the repos in the hub's `git_repos` setting, each with one integration branch (`claude/main` or
  `main`). Today that is atrium.
- **Rooms get one branch.** They fetch only the integration branch, mapped to `claude/main` and `hub-main`. Section
  4.1 there refuses anything else, after sg3's `hub-main` was set by hand to a `claude/ui` commit and workers started
  on unmerged work.
- **Collect:** rooms serve `claude/*` (not `claude/main`) on a link-only route. The hub fetches them into
  `refs/rooms/<room>/claude/*` and hides that namespace from every reader (`uploadpack.hideRefs=refs/rooms`).
- **Not served:**
  - other repos, such as the openziti checkouts under `D:/worktrees/...` on sg4;
  - branches not named `claude/*`;
  - any reader other than a room on the link;
  - clint's own machines. The bare repos are "served only on the `git` connection kind, never on the board listener"
    (git-sync 4.4).

## 1a. Peer to peer, with the hub as the go-between and the index

**Announce.** A room's report of section 2 is its announcement. It covers each repo it holds, each branch a card
works on, and each branch's sha. The hub keeps them as the **index**: which rooms hold which repo, and which branch
at which sha. Any room can read the index, through `atrium_git_url` (3.1), or as a list with
`atrium_git_where {repo}`. That is "any room can inform any room where their repo is", with no room needing another's
address.

**Pull from any room, live.** A fetch of `rooms/sg4/fix/x` from m1mini reaches the hub. Then:
1. **Pass-through when sg4 is attached.** The hub forwards the upload-pack request to sg4's link-only git route
   (git-sync section 7), over the data connections sg4 dialled. It streams the answer back and keeps a copy in its
   mirror on the way. So m1mini reads sg4's branch as sg4 has it now, not as of the last collect. Leaves still only
   dial out, and the hub still moves no ref on either room.
2. **The hub's copy when sg4 is offline.** The answer comes from the mirror, and `atrium_git_url` says `from: hub
   copy, collected <time>`.
3. **Neither.** The answer is `sg4 is offline and the hub has no copy of fix/x`. There is no paste.

The per-command token and loopback forwarder of git-sync section 5 stay as they are, on both legs.

**Fork.** A room starts its own branch from another room's: `atrium git fork rooms/sg4/fix/x claude/fix-x` fetches
through the hub and makes a local branch with `rooms/sg4/fix/x` as its upstream. The index records the fork, so the
change record of each links to the other. There is no copy on a forge and no account.

**Pull requests between rooms: atrium's own.** A change request is a hub row:
- the source, `{room, repo, branch, sha}`;
- the target, either a room's branch (`{room: m1mini, branch: claude/fabric}`) or the integration branch
  `claude/main`;
- the change record's id (`docs/rnd/change-record-design.md`): tested, reviewed, pushed and open, with its
  artifacts;
- the `Atrium-Verdict` coverage of the commits it carries;
- a title and a one-line why.

`atrium pr open <source> <target>`, or `atrium_pr_open`, files it, and the hub tells the target's owner card (the
card whose worktree is on that branch) once, as a report. The owner can:
- read the change record;
- fetch the source through the hub (pull);
- ask a standing reviewer for a job (`atrium:qa-reviewer`, review-on-atrium section 7);
- **merge it itself.** A room's branch is that room's own, so its owner card merges in its own worktree and
  `atrium_record`s the result;
- or **close it with a reason.**

A request whose target is `claude/main` is a landing. It goes to git-sync stage 3's merge queue and its checks, so
decision 46.

**Push.** A room's push into its own namespace on the hub is stage F4 (section 4). It does the same job as
pass-through for a room that is about to go offline: its branch is on the hub before it leaves.

**What the hub mediates, and what it never does.**
- It mediates:
  - the index;
  - every transfer, since no room dials another;
  - the change requests;
  - and, from F4, pushes into a room's own namespace.
- It never:
  - moves a ref on a room;
  - merges into a room's branch;
  - lands without stage 3.

A room's branches are changed only by that room's own cards.

## 2. Mirroring every repo, automatically

**What a room reports.** On each attach, and when a card's `repo`, `branch` or worktree changes, the room sends the
hub `{name, origin, branch, card}` per live card. `name` is `<host>/<owner>/<repo>` read from `origin` the way
git-sync 4.1 reads it. A worktree with no `origin`, or one whose `origin` is a local path, is reported with
`origin: ""` and is mirrored from the room only.

**What the hub keeps.** `<hub atrium-dir>/git/<host>/<owner>/<repo>.git`, bare, one per name, made on first report.
The `git_repos` list stays only for repos whose integration branch the hub serves to `hub-main` (atrium). Every other
repo is "seen".

**Three fetches fill it. All three are fetches, so git-sync's one rule, that nobody receives a push, still holds:**
1. **From the forge,** every 10 minutes and when a card reports a new repo:
   `fetch --prune --no-tags <origin> +refs/heads/*:refs/heads/* +refs/tags/*:refs/tags/*`.
   - Public repos need no login.
   - A private repo uses the hub's own forge login, if the operator gave it one.
   - Without one, the forge fetch is skipped, and the mirror has only what rooms give it. The board says so.
   - The forge's `claude/*` is never fetched over the hub's, so `claude/main` stays the hub's own.
2. **From each room,** at the collect of git-sync section 7, widened. The room serves, and the hub fetches into
   `refs/rooms/<room>/`:
   - every `claude/*` branch;
   - the branch each live card's worktree is on, whatever its name (`fix/xyz` in an openziti worktree);
   - nothing else.
3. **Outside PR heads, on request:** `refs/pull/<n>/head` into `refs/pull/<n>/head`, when a PR run or a persona job
   names one. It is pruned when the PR closes.

**Serving the rooms' branches.** The hub keeps `refs/rooms/<room>/<branch>` as the store, and advertises each as
`refs/heads/rooms/<room>/<branch>` through a namespace mapping at serve time. It does not copy them. So two rooms with
a `claude/fabric` each are two names, `rooms/sg4/claude/fabric` and `rooms/m1mini/claude/fabric`, and never one name
that hides the other. The hideRefs line of git-sync 4.4 changes from "hide `refs/rooms`" to "serve it under
`rooms/`". This is the one stage 1 rule this design reverses, and section 5 says why it stays safe.

**Untrusted content.** An outside repo's mirror is bytes in a bare repo:
- nothing is checked out on the hub;
- no hook in the fetched repo runs (a fetch runs none, and a bare mirror has no work tree);
- upload-pack serves objects, never a build.

A reader that checks it out does so under its own rules: section 3 step 3 of review-on-atrium, for a reviewer.

**Size and pruning.** A repo no card has reported for 30 days is dropped from the hub (question 3). Its rooms' clones
are untouched. Mirrors are capped per repo (default 5 GB) and in total (default 50 GB). A repo over its cap is not
fetched further, and the board says so. LFS objects are not mirrored.

## 3. Serving it

**One handler, three reaches.** The same `git http-backend` setup as git-sync 4.4:
- environment-only config;
- `http.receivepack=false`, `http.uploadarch=false` and `http.getanyfile=false`;
- protocol v0, with no tip-sha wants.

The repo name from the URL is checked against the hub's mirrors before the CGI runs. It is mounted on three paths:

| reach | URL | who |
| --- | --- | --- |
| the link, `git` connection kind | `http://127.0.0.1:<P>/<T>/<name>.git` through the room's forwarder (git-sync 5) | any room. The room's certificate is the identity |
| the hub's board listener, loopback | `http://127.0.0.1:<board port>/git/<name>.git` | anyone on the hub machine, as for the board |
| the board's overlay reach | `http://atrium.ziti/git/<name>.git`, or the zrok private share's URL | clint's machines, by the overlay's own policy |

**Never on a zrok public share.** The public share is how the phone gets HTTPS for Web Push, and anyone with its URL
reaches it. `/git/` on a public share answers 404, checked by the listener that knows which share it is, with a test.

**An agent reads through a tool, not raw git.** `atrium_git_fetch {repo, ref}` fetches one ref from the hub into the
card's clone, under `refs/remotes/hub/<ref>`. It never writes `refs/heads`, `hub-main` or `claude/main`. A card's
own git commands to a remote are still refused by the dotfiles hook, and this tool is the sanctioned way. It is the
same pattern as `atrium_git_sync`.

### 3.1 The lookup: `atrium_git_url`

The one way an agent, a persona job or a script finds code. Input, any one of:
- `{repo, branch}`;
- `{card}`, a handle or id on any room;
- `{path}`, a worktree path on any room (`D:/worktrees/github/openziti/ziti-tunnel-sdk-c/pr-1441`).

The hub resolves it from the rooms' reports (section 2) and answers:

```json
{"repo": "github/openziti/ziti-tunnel-sdk-c",
 "url": "http://127.0.0.1:<P>/<T>/github/openziti/ziti-tunnel-sdk-c.git",
 "ref": "refs/heads/rooms/sg4/fix/x", "sha": "3f2a91c...", "room": "sg4",
 "collected_at": "2026-10-02T21:04:11Z", "dirty": false}
```

- **`url` is for the caller's own reach.** A card gets the link forwarder's URL, which `atrium_git_fetch` uses. The
  CLI on clint's machine gets the overlay URL.
- **`collected_at` and `dirty` say how current it is.** `dirty: true` means the room reported uncommitted changes in
  that worktree, which are not in the hub's copy. `stale: true` means the room's branch moved after the last
  collect. Then the call collects that one ref first, by asking the room, and answers with the new sha.
- **A miss is an answer, not a question.** An unknown path or branch answers `not found`, with the closest repos and
  branches the hub knows. An agent reports that miss. It does not ask a person to paste code.
- **Every brief says so.** Every card brief, persona job brief and PR run brief carries one line: "to read code that
  is not in your cwd, call `atrium_git_url`, then `atrium_git_fetch`. Never ask for a paste."
- **Stopgap until F2.** Until F2 is built, `atrium_git_url` still answers the room and the path, with no URL, and
  review-on-atrium 7.2 runs the reviewer on that room. That is the interim workaround, and it ends with F2.

`atrium git url <repo|path>` is the same lookup in the CLI.

**What clint types, once per machine:**

```
atrium git setup            # prints and applies, after a yes, the two lines below
git config --global url."http://atrium.ziti/git/github/".insteadOf "git@sg4.atrium:"
git config --global http."http://atrium.ziti/git/".extraHeader "Authorization: Bearer <token>"   # only if the board's token is on
```

Then `git clone git@sg4.atrium:openziti/ziti-tunnel-sdk-c.git` works. `atrium git url <repo>` prints a repo's URL on
each reach.

**The board** gains a "repos" list: each mirrored repo, its forge, the rooms that report it, and its branches by
room, with the last commit of each. Clicking one copies its clone URL, from the same lookup. It is a list, not a code browser.

## 4. Push, later (F4), and forks

- **A room pushes its own branches** to `refs/rooms/<its room>/*` on the hub, instead of the hub collecting them. The
  hub runs `receive-pack` on the link only, with one check before any ref moves: the pushing room's certificate
  name must equal `<room>` in every updated ref. A room can never move another room's refs, the forge's refs or
  `claude/main`. Collect stays as the fallback for rooms that do not push.
- **Clint's machines push** only when he asks for it (question 4), into `refs/rooms/<machine>/*`, with the same
  rule. The machine's name comes from its overlay identity or a joined room certificate, never from what it says
  about itself (overlay-room-identity).
- **A fork is a new name.** `atrium git fork <repo> <new-name>` makes `<hub>/git/<new-name>.git` with the original's
  objects shared through `objects/info/alternates`, and no forge behind it.
- **Landing** (`claude/main` moved by the hub after checks) is git-sync stage 3, the merge queue. It is not
  designed again here, and it needs decision 46.

## 5. What stays safe from stage 1

- **The sg3 failure cannot come back.** A room's `sync` still maps only the integration branch onto `claude/main`
  and `hub-main`, by the same code and refspec as stage 1. The new branches reach a room only through
  `atrium_git_fetch`, into `refs/remotes/hub/`, which no launch, worktree or sync ever bases work on. A test
  launches a worker after a `rooms/sg3/claude/ui` fetch and checks that its worktree starts at `claude/main`.
- **One room reading another's branches** was hidden in stage 1 "as tidiness, not a wall" (git-sync 4.4). They are
  all the operator's rooms, so serving them under `rooms/` is the point, not a leak.
- **Private repos** are served only on the three reaches above, which are the board's own, and never on a public
  share. A private repo is readable by exactly whoever can already read the board.
- **Uncommitted work is never served.** Only refs are. A reviewer asking about a dirty worktree gets "commit it
  first", as review-on-atrium 7.7 says.

## 6. Stages

All held by the pause. Owner @fabric unless named.

| stage | what | size | acceptance |
| --- | --- | --- | --- |
| F1 | Automatic mirrors: rooms report each card's repo and branch, the hub makes bare mirrors, the forge fetch (public, or with the hub's login), and the widened collect (all `claude/*` plus each live card's branch) | 3 days | a card started on sg4 in an openziti/ziti-tunnel-sdk-c worktree on branch `fix/x` makes a hub mirror of that repo within one report. After the next collect it holds `refs/rooms/sg4/fix/x` and the forge's `main`. A repo with a local-path origin is mirrored from the room only |
| F2 | Serving to rooms: the `rooms/` namespace mapping, `refs/pull/<n>/head` on request, `atrium_git_url` (3.1) and `atrium_git_fetch` into `refs/remotes/hub/`, and the line in every brief | 2 days | a reviewer card on m1mini fetches `rooms/sg4/fix/x` and reads a file from it, with no paste and no room-to-room route. A worker launched after that fetch starts at `claude/main`. A fetch of a hidden sha by id fails. `atrium_git_url {path: "D:/worktrees/.../pr-1441"}` from m1mini answers sg4, the ref and the sha. After a new commit on sg4, it answers the new sha, having collected that ref. An unknown path answers `not found` with the nearest matches |
| F2b | Pass-through: a fetch of `rooms/<room>/*` is forwarded live to that room when it is attached and cached in the mirror, with the hub's copy when it is not. Also `atrium_git_where` and `atrium git fork` | 1.5 days | m1mini fetches a commit made on sg4 a minute ago, before any collect, and the hub's mirror then holds it. With sg4 detached, the same fetch answers from the hub's copy and says when it was collected |
| F3 | Serving to clint's machines: `/git/` on the board's loopback and overlay reaches, 404 on a public share, `atrium git setup` and `atrium git url`, and the board's repos list | @fabric, @ui, 2 days | from clint's laptop on the ziti overlay, `git clone git@sg4.atrium:openziti/ziti-tunnel-sdk-c.git` (after setup) clones with the forge's branches and every `rooms/*` branch. The same URL through the zrok public share answers 404 |
| F5 | Pull requests between rooms: the change-request row, `atrium pr open`, the one report to the target's owner, the change record and verdict coverage attached, close with a reason, and `claude/main` targets handed to stage 3 | @fabric, @ui (a list on the board), 2 days | a request from `rooms/sg4/fix/x` to m1mini's `claude/fabric` reaches m1mini's fabric card once, with the four lines of the change record. The card fetches, merges in its own worktree and records it. A request to `claude/main` is refused until stage 3 exists |
| F4 | Push into a room's own namespace, forks as names, then git-sync stage 3 for landing | 2 days, plus stage 3 | a room's push to its own `refs/rooms/<room>/claude/x` lands. A push naming another room, a forge ref or `claude/main` is refused before any ref moves |

Review-on-atrium 7.7's default (a) is F1 and F2. Every persona job resolves its code with `atrium_git_url`.

## 7. Questions for clint, held until he asks

1. **Mirror everything automatically.** Every repo any card works in gets a copy on the hub, including outside
   projects, with their own branches and every machine's work in progress. **Suggested: yes.**
2. **The address.** Your `git@sg4.atrium:...` spelling works through a one-line git setting that turns it into the
   web address the board already uses, with no ssh server. Add a real ssh server later only if a tool you use needs
   it? **Suggested: yes, web address now, ssh only if needed.**
3. **Cleaning up.** Drop a repo's hub copy after 30 days with no card working in it, with 5 GB per repo and 50 GB in
   all? **Suggested: yes.**
4. **Pushing from your own machines.** Later, once rooms push to the hub, may your laptop push its branches to the
   hub too, under its own name? **Suggested: yes, in the push stage, not before.**
5. **Pull requests between machines.** One machine can ask another to take a change, with its test and review record
   attached, and the receiving machine's director decides. Changes to the main branch still wait for the hub's
   merge queue, which you have not approved yet. **Suggested: yes.**
