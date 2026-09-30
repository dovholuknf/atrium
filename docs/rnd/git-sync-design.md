# f-019: git sync over the hub and rooms (design)

Status: design, for review. Owned by @rnd (design) and @fabric (the hub side). Item: `docs/backlog/fabric/f-019.md`.
Direction settled by decision 19 in `docs/decisions.md`. First live proof: sg3. Second: m1mini.

## 1. The problem, in one paragraph

Code reaches a room today by a person or by an agent running `scripts/room-git.ps1` over ssh. The agent tool hook
refuses every git command that reaches a remote, which is right, so the job lands on clint. sg3 and m1mini both sit on
a stale `hub-main`, m1mini's clone has no `claude/main`, and clint will not run `push-base` by hand. Decision 19
says the hub owns the integration branches and atrium moves code between rooms. This is how.

**What the hook actually stops, found 2026-09-30.** @runtime ran `room-git.ps1 push-base sg3` from its own card and
it worked (sg3's clone is at 4815d47). The rule lives in dotfiles' `claude/hooks/pre-tool-use-hook.ps1`, rule 1, and
it matches the COMMAND TEXT: `\bgit\s+(?:-\S+\s+)*(push|pull|fetch)\b`. `pwsh -File scripts/room-git.ps1 push-base
sg3` has no such text, so it passes, and the script runs `git push` itself. So the refusal is a policy held by
matching text, and any script that runs git gets round it. That is what decision 19 meant by "would only have got
round the same rule". This design does not depend on the hook being a wall. It removes the reason to go round it,
and after stage 1 a director that runs `room-git.ps1` is doing by hand what atrium does on its own. Whether the hook
should also refuse `room-git.ps1` by name, once stage 1 is live, is for clint and @dotfiles (question 4).

## 2. The one rule: nobody receives a push

Every transfer is a FETCH by the side that wants the objects, into a namespace that side owns.

- A room fetches `claude/main` from the hub into its own clone.
- The hub fetches a room's `claude/*` branches into `refs/rooms/<room>/claude/*` in its own repository.

So no `git receive-pack` runs anywhere, no ref on either side can be moved by what the other side sends, and the
question "may this room update that branch" never comes up. `receive.denyCurrentBranch=updateInstead` and the dirty
work tree refusals that came with it (f-005, sg3's deleted files) stop mattering, because nothing pushes into a
checked-out branch.

Both sides serve `git-upload-pack` only. Upload-pack is read-only by construction.

## 3. What is built when

| Stage | What | Replaces |
| --- | --- | --- |
| 1 | The hub's bare repository per repo, mirrored from the hub machine's checkout. A room SYNCS `claude/main` from it. The hub COLLECTS a room's `claude/*` branches and delivers them into the checkout as `refs/remotes/<room>/claude/*`. | `room-git.ps1 init`, `push-base`, `fetch`, `init -Check` |
| 2 | A worktree atrium makes for each card it launches on a room, off `claude/main`, on `claude/<id>`. | `room-git.ps1 worktree` |
| 3 | The merge queue in the hub's store. The bare repository becomes the truth and the checkout is fed from it. | the hand merge by @merge |

**Stage 1 is what gets built now**, and it is what brings sg3 and then m1mini onto current `claude/main`. Stages 2
and 3 are designed here far enough that stage 1 does not paint them into a corner, and are built after sg3 is
proven. Stage 3 changes how @merge works and needs clint's word.

## 4. The hub side

### 4.1 Where the repositories live

`<hub atrium-dir>/git/<host>/<owner>/<repo>.git`, bare. `<host>/<owner>/<repo>` is the repo's NAME everywhere in this
design, read from the checkout's `origin` URL the same way `room-git.ps1` does (`github/dovholuknf/atrium`).

The hub learns its repos from a hub setting, `git_repos`, a list of `{name, checkout, branch}`:

```json
[{"name": "github/dovholuknf/atrium", "checkout": "D:/git/github/dovholuknf/atrium", "branch": "claude/main"}]
```

`checkout` is the hub machine's own clone, the one @merge writes. It is a path on the hub's machine, set by the
operator on the hub (the CLI or the board's hub settings), never by a room.

### 4.2 Why a bare repository and not the checkout itself

Serving the checkout directly would be one moving part fewer. It is not done, for two reasons.

- **Isolation by construction.** The checkout holds every branch anybody ever made here. Hiding refs is a promise the
  server makes about wants (section 4.4), and a repository that simply does not HAVE the other objects needs no
  promise. The bare repository only ever receives `claude/main` and the rooms' `claude/*`, so a room reading it can
  read nothing else.
- **Stage 3 needs it anyway.** Decision 19 puts the truth in a hub repository. Building stage 1 on the checkout would
  mean moving every room's remote at stage 3.

### 4.3 Keeping it current (stage 1)

- **Mirror in.** Every 30 seconds, and at once when asked, the hub reads `branch` in `checkout` (`git rev-parse`) and,
  when it moved, runs `git -C <bare> fetch --no-tags <checkout> +refs/heads/<branch>:refs/heads/<branch>`. A local
  fetch, run by the hub process, not by an agent.
- **Tell the rooms.** When the mirrored ref moved, the hub asks every attached room that has the repo to sync
  (section 6). A room that is not attached syncs when it next attaches.
- **Deliver out.** After a collect (section 7), the hub runs
  `git -C <checkout> fetch --no-tags --prune <bare> +refs/rooms/<room>/claude/*:refs/remotes/<room>/claude/*`.
  That is exactly where `room-git.ps1 fetch` put them, so @merge's way of working does not change in stage 1.

The hub never writes a branch under `refs/heads` in the checkout. It writes `refs/remotes/<room>/*` only.

### 4.4 Serving it

`git http-backend` behind `net/http/cgi`, with:

- `GIT_PROJECT_ROOT` the hub's `git` directory and `GIT_HTTP_EXPORT_ALL=1`
- `http.receivepack=false` and `http.uploadarch=false`, so only upload-pack answers
- `uploadpack.hideRefs=refs/rooms`, so one room does not read another's branches. They are all the operator's rooms,
  so this is tidiness, not a wall
- the request's `Git-Protocol` header dropped, so the server speaks protocol v0. In v0 upload-pack refuses a want
  that is not an advertised tip unless `allowTipSHA1InWant` or `allowReachableSHA1InWant` is set, and neither is.
  Protocol v2's rules for wants are looser and are not relied on. A test fetches a hidden sha by id and must fail.
- the repo name from the URL checked against `git_repos` before the CGI runs. A name not on the list is 404, and a
  path with `..` or a drive letter never reaches git.

`git http-backend` ships with every git the hub could run (Git for Windows has it in `libexec/git-core`). Smart HTTP
rather than a bundle per transfer, because a bundle has to be told what the other side has already, and that
negotiation is exactly what upload-pack does. A bundle is kept for backup (section 10).

## 5. The transport

Git speaks HTTP to a URL. Neither side has a URL for the other, only the link. So each side opens a LOOPBACK
FORWARDER for the length of one git command:

```
room                                             hub
git fetch http://127.0.0.1:P/T/<name>.git
  -> forwarder on 127.0.0.1:P  --(link conn, kind "git")-->  http-backend on the bare repo

hub                                              room
git fetch http://127.0.0.1:Q/T/<name>.git
  -> forwarder on 127.0.0.1:Q  --(link data conn, the pool)-->  room handler /v1/git/<name>.git
```

- **The forwarder** is an `httputil.ReverseProxy` on an ephemeral `127.0.0.1` listener. `T` is a random token for
  that one command. A request without it is 404 and never reaches the link. The listener closes when the git command
  exits. So another process on the same machine has a few seconds and needs a 128-bit guess.
- **Room to hub: a new connection kind, `git`.** Dialled by the room the way `upgrade`, `announce` and `relay` are,
  with the same hello and the control connection's `Session`. After the welcome the connection is plain HTTP/1.1 and
  the hub serves the handler of section 4.4 on it. `hearHello`'s list of kinds and its refusal sentence gain `git`
  together, with a test. An older hub refuses with the old sentence, which lists the kinds and does not contain
  `git`, and the room turns that into "the hub predates git sync", the way `Room.Relay` already reads a refusal.
- **Hub to room: the data pool.** `Hub.Dial(room)` is already `http.Transport.DialContext` shaped, and the room's
  handler is already on the other end. Nothing new on the wire.
- **Leaves dial out still holds.** Both paths ride connections the room dialled.

The git command on each side runs with `GIT_TERMINAL_PROMPT=0`, `-c credential.helper=` and `-c http.proxy=`, so no
prompt, no helper and no proxy variable can interfere. Every command is bounded (ten minutes) and killed at the bound.
A clone of atrium from scratch is tens of megabytes, which is minutes on a bad overlay and seconds on the LAN.

## 6. The room side: sync

### 6.1 Where a room keeps a repo

`<git_root>/<name>`, where `git_root` is a room setting defaulting to `~/git`. So atrium lands at
`~/git/github/dovholuknf/atrium`, which is where `room-git.ps1 init` already put it on sg3 and m1mini. Their existing
clones and worktrees keep working.

### 6.2 What a sync does

`POST /v1/git/sync {"name": "...", "init": false}` on the room, sent by the hub over the link. The room:

1. **Checks git.** The git on the ROOM's PATH (room-env.ps1 is loaded at room start). On Windows a Cygwin or MSYS git
   is refused with the same sentence `room-git.ps1 -Check` prints, because it writes `/cygdrive` gitfiles (f-005).
2. **Finds the clone.** Missing and `init` false answers `absent`. Missing and `init` true runs `git init` there.
3. **Fetches.** Through the forwarder:
   `git fetch --no-tags <url> +refs/heads/claude/main:refs/remotes/hub/claude/main`.
4. **Moves the local branches**, each with its own refusal rather than a force:
   - `refs/heads/claude/main`, which m1mini's clone is missing today. Created or moved with `git branch -f`, which git
     itself refuses when a worktree has it checked out. That refusal is reported, not worked round.
   - `refs/heads/hub-main`, kept for the worktrees and briefs that name it. When the clone's own HEAD is `hub-main`,
     `git reset --keep`, which refuses when the work tree has changes it would lose, like `updateInstead` did.
     Otherwise `git branch -f`.
   Both are FORCE moves in the sense that they need not fast-forward, because `claude/main` is re-signed from time to
   time and a re-sign rewrites every sha.
5. **Answers** `{"state": "ok|absent|behind|failed", "sha": "...", "detail": "..."}`. Exactly one of:
   - `ok`: fetched, and both branches are at the hub's sha
   - `absent`: no clone and `init` was false. Nothing was run
   - `behind`: the fetch worked and git refused a move (a worktree holds `claude/main`, or `hub-main` is checked out
     with changes). `sha` is what was fetched
   - `failed`: nothing was fetched. Git missing or the wrong git, the hub refused the `git` kind, or the fetch failed
   `detail` is git's own words, first line.

The last answer is also kept in memory on the room and served at `GET /v1/git/status`, which is what `room-git.ps1
init -Check` answered by ssh.

### 6.3 When a sync happens

- When the hub's mirror moves (section 4.3).
- When a room attaches, for every repo the hub has, with `init` false. A hub restart is a reattach, so it converges.
- When asked: `atrium rooms git sync <room> [<name>] [--init]` on the hub, `POST /_hub/git/sync`, and the control
  MCP tool `atrium_git_sync(room, name, init)` for @merge and the directors. `init` only ever comes from a person or a
  director asking, never from an attach.

## 7. The room side: serving its branches, and the hub collecting them

The room serves `/v1/git/<name>.git/` on its handler, upload-pack only, protocol v0, with
`uploadpack.hideRefs=refs` then `uploadpack.hideRefs=!refs/heads/claude/` then `uploadpack.hideRefs=refs/heads/claude/main`.
So it offers `claude/*` except `claude/main`, which came from the hub in the first place.

That endpoint sits where `/v1/files` sits: reachable through the hub's board and on the room's own loopback. It offers
less than `/v1/files` already does, since a card's directory is readable there, `.git` included.

The hub collects with
`git -C <bare> fetch --no-tags --prune <url> +refs/heads/claude/*:refs/rooms/<room>/claude/*`, then delivers
(section 4.3). A collect happens:

- when asked: `atrium rooms git collect <room>`, `POST /_hub/git/collect`, `atrium_git_collect(room)`
- when a room attaches, after its syncs
- every five minutes per attached room. A fetch with nothing new is one round trip

A branch deleted on the room is pruned from `refs/rooms/<room>` and from `refs/remotes/<room>` in the checkout, since
a culled worker's branch is gone on purpose. A branch that only ever existed on a room and was pruned before anyone
merged it is lost here but is still in the room's reflog, which is where it would be today.

## 8. What decision 17 asks of this

- **Restart freely.** Stage 1 keeps no state outside git refs and the `git_repos` setting. A ref update is atomic, so
  a hub killed mid-fetch leaves the old ref or the new one. A sync the hub was driving dies with it, and the room's
  fetch fails and is retried at the reattach.
- **No mutation queue to lose.** Stage 1 has no queue. Stage 3's queue is rows in the hub store (section 11).
- **Converges on reconnect.** Every attach syncs and collects.
- **The hub down costs nobody a session.** A room without its hub keeps every clone, worktree and running card. It
  just does not get a new `claude/main` until the hub is back.

## 9. Compatibility and the rollout

- An old room answers 404 on `/v1/git/sync`. The hub records "the room's build predates git sync" on the room's row
  and does not ask again until it reattaches. An old hub refuses the `git` kind, and a new room says so once.
- **sg3 first.** It needs a room build with stage 1 on it and one restart, which is clint's word through
  @orchestrator. Its clone already exists (`init` false), and is at 4815d47 by @runtime's hand push, so the proof is
  the NEXT `claude/main` reaching it with nobody pushing. After the restart: the attach syncs, `GET /v1/git/status`
  says ok, `claude/main` in the clone equals the hub's, and a collect brings its branches here.
- **m1mini second.** Old build ac9d85d. The same, plus `claude/main` is created in its clone on the first sync.
- Every step, what broke and what fixed it goes in f-005's log, under a new heading for f-019.

## 10. Backup

In stage 1 the bare repository holds nothing that is not somewhere else: `claude/main` is in the checkout and on
GitHub, and `refs/rooms/*` is on the rooms and delivered into the checkout. So it needs no backup, and losing it is a
re-mirror. Stage 3 changes that: once the bare repository is the truth, the hub's snapshot runs
`git bundle create <snapshot>/<name>.bundle --all` beside `VACUUM INTO`, and the snapshot count covers both.

## 11. Stages 2 and 3, far enough to not block them

**Stage 2, a worktree per card.** `atrium_launch` gains `worktree: true` (with `room`). The room makes
`<clone>-worktrees/<id>` on `claude/<id>` off `claude/main` after a sync, and launches the card there. The card
carries the worktree path, and Cull removes it (Cull already removes a merged worker's worktree and branch). This
replaces `room-git.ps1 worktree` and the hand step before every remote launch.

**Stage 3, the merge queue.** Rows in the hub store: `{repo, room, branch, sha, state, checks, requested_by}`. The hub
lands in order: fetch, check the branch is based on the current integration tip, run the repo's checks in a scratch
worktree of the bare repository, merge, update the ref. The checks are the repo's own (`merge-check.ps1` for atrium),
which is why stage 3 changes @merge's job and is clint's call. Then `checkout` is fed from the bare repository instead
of the other way round, and `mirror in` becomes `feed out`.

## 12. What happens to `scripts/room-git.ps1`

| Verb | After stage 1 |
| --- | --- |
| `init` | `atrium rooms git sync <room> --init` |
| `init -Check` | `GET /v1/git/status` on the room, and room-check.ps1 calls that |
| `push-base` | gone. The hub does it when `claude/main` moves |
| `fetch` | `atrium rooms git collect <room>`, and automatic |
| `worktree` | stays until stage 2 |
| `remove` | stays. Removing a clone is rare and destructive and stays a human's ssh step |

The script stays in the repo until stage 2 lands, with its header pointing here.

## 13. The agent-facing surface

An agent commits on `claude/<id>` and finishes. That is all. Directors and @merge get two MCP tools,
`atrium_git_sync` and `atrium_git_collect`, which ask the hub to do what it would do on its own timers anyway. No
tool takes a path, a url or a refspec, so no agent can point atrium's git at anything but the configured repos.

## 14. Tests

- The hub serves upload-pack only: a push to the served url fails.
- A hidden ref's sha fetched by id fails, over v0, with the client asking for v2.
- A repo name not in `git_repos`, and names with `..`, `%2e%2e`, a drive letter or a backslash, are 404.
- The forwarder refuses a request without its token and closes when the command exits.
- Sync end to end against a real git in temp dirs: `absent`, `init`, ok, a re-signed (non fast-forward) `claude/main`,
  `behind` with `claude/main` checked out in a worktree, `behind` with a dirty `hub-main` checkout, `failed` against
  an old hub's refusal.
- Collect: a new branch arrives, a deleted one is pruned, `claude/main` on the room is never collected.
- A hub killed mid-fetch leaves every ref at its old or its new sha.
- An old room's 404 and an old hub's refusal are each said once.

## 15. Open questions

1. `hub-main`: keep it indefinitely, or drop it once briefs and worktrees name `claude/main`? This design keeps it.
2. Should a room's `git_root` be settable from the hub, or only on the room? This design says only on the room.
3. The five-minute collect: frequent enough for @merge, or should a card's `done` report trigger a collect at once?
   The report reaches the hub already, so it is cheap to add.
4. Once stage 1 is live on a room, should the dotfiles hook refuse `room-git.ps1 init|push-base|fetch` by name, so the
   bypass found in section 1 closes? That is @dotfiles' file and clint's rule.
