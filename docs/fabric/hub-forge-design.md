# The hub as a forge: rooms push finished work to the hub, and the hub routes everything else to the room that has it

Status: built. The hub holds `main` and rooms push finished work to it. Stage 4 is `atrium_git_url`, stage 5 is change
requests between rooms.

> "any room pull from any room, and any room can inform any room of where their repo is, to fork and
> mediate/pr/push amongst the swarm" (clint, 2026-10-02)

Origin: design by @rnd, 2026-10-02, **revision 2 after clint's interview** (his answers are on sg4, in the
appendix below, and the orchestrator relayed them in full). This is git-sync stage 4. Revision 1
(d32a34fd to 1c965657, @review doc-ok 8eaa86b1) mirrored every repo on the hub automatically and kept a fallback
copy. clint answered no to both, so this revision replaces it. Stage 1 is cut into build items in section 7.

Read for this design, 2026-10-02:
- `docs/fabric/git-sync-design.md` (stage 1 built: bare repos for the repos in `git_repos`, rooms fetching
  `claude/main` over smart HTTP on the link's `git` connection kind, upload-pack only, and the hub collecting rooms'
  `claude/*`);
- `internal/gitsync/`, `internal/link/git_hub.go`;
- `docs/rnd/security-design.md` (the reach table, the agent listener, card tokens in stage 2c);
- `docs/rnd/overlay-room-identity.md`;
- `docs/rnd/change-record-design.md` and `docs/rnd/review-on-atrium-design.md` section 7.7.

## 0. The answer, from clint's answers

- **The hub is a router for work in progress, and a store only for finished work.**
  - **In progress stays on the room that made it.** "Pull sg4's `fix/x`" is a fetch from the hub at sg4's name. The
    hub sees it is for sg4 and passes it through to sg4, live (Q1).
  - **No copy is kept.** If sg4 is off, asleep or disconnected, the fetch fails and says so (Q2).
- **The hub holds its own `main`, and finished means pushed to the hub** (Q1 clarification, Q3).
  - A feature that is only on a room is not finished.
  - Rooms push their branches to the hub when the feature is done. The orchestrator or clint pulls from there, merges,
    re-signs and pushes on to the real forge.
  - **Push is stage 1**, not "later".
- **Plain git rules** (Q6):
  - branches under their normal names, with no per-room namespace;
  - no force push;
  - a non-fast-forward push is refused;
  - two rooms pushing the same name: the first wins, and the second is refused, as on any git server.
- **Cards push to the `hub` remote only, never to `origin`** (Q7, Q8).
  - Pushing is a setting, and the hub is the one allowed target.
  - The dotfiles hook keeps refusing every other remote git command.
  - Atrium adds two more walls behind the hook, because a hook that matches command text is not a wall (git-sync
    section 1).
- **Clones go in the operator's scm folder** (Q4, Q5). The operator picks the folder at install, `<scm>/<host>/<owner>/<repo>`
  (clint's is `D:/git`).
  - "Go work on `<url>`" makes atrium clone the repo there, with two remotes, `origin` (the forge) and `hub`.
  - A public repo just works.
  - A private repo atrium has no credential for fails with the message clint gave: "that repo doesn't exist, check
    it, and if it is private have the operator clone it. Atrium can't."
- **The URLs clint asked for**:
  - `git@hub.atrium:<owner>/<repo>.git` for the hub's own store;
  - `git@sg4.atrium:<owner>/<repo>.git` for sg4's work in progress.

  Each is a one-line `insteadOf` over smart HTTP on the board's reaches, with no ssh server.
- **Three defaults the interview did not ask about**, each held as a question in section 8:
  - how a room proves who it is on a push;
  - how the pass-through is authenticated;
  - the shape of the install settings.

## 1. What changes from revision 1, and what stays

| revision 1 | revision 2 |
| --- | --- |
| every repo mirrored on the hub, automatically | **no mirror.** The hub stores only `main` and branches rooms pushed |
| forge branches fetched into `refs/forge/` | **only the branch a card asks for** (3.5, changed 2026-10-07). A branch neither the store nor a room has is fetched from the forge into `refs/forge/<branch>` when a card asks for it, and refreshed on each ask. Nothing is mirrored. A new hub repo's `main` is still seeded once (3.1) |
| fetch-through into a hub copy, with the copy as fallback | **pass-through to the room, no copy.** Offline fails |
| rooms' branches served under `rooms/<room>/` | **normal names.** In-progress branches are read at the room's own name (`sg4.atrium`), and pushed branches at the hub's |
| push in a later stage | **push is stage 1** |
| the agent's remote named `atrium-hub` | **`hub`**, clint's name. Atrium never overwrites an existing `hub` remote that points somewhere else (5.2) |
| `atrium_git_url` lookup | **kept** (section 4) |
| change requests between rooms | **kept**. "Pushed to the hub" is now what makes a change finished (section 6) |

Revision 1's @review findings still apply where their parts remain:
- M2's served set now governs the pass-through (3.3);
- L2's `followRedirects=initial`, L3's dirty rule and L4's PR text as data are all kept;
- M1 (forge refs shadowing rooms' work) is answered by the namespace: a forge branch lives only under `refs/forge/`,
  never `refs/heads/`, and a branch a room pushed under the same name always wins (3.5).

## 2. The URLs

| what | URL on the board's reaches | with clint's `insteadOf` |
| --- | --- | --- |
| the hub's store: `main` and pushed branches | `http://atrium.ziti/git/hub/<host>/<owner>/<repo>.git` | `git@hub.atrium:<owner>/<repo>.git` |
| a room's work in progress, passed through | `http://atrium.ziti/git/room/<room>/<host>/<owner>/<repo>.git` | `git@sg4.atrium:<owner>/<repo>.git` |

- `<host>` defaults to `github`, so `git@sg4.atrium:openziti/ziti-tunnel-sdk-c.git` means
  `github/openziti/ziti-tunnel-sdk-c`. A full `<host>/<owner>/<repo>` path is accepted too.
- `atrium git setup` writes one `insteadOf` line for the hub and one per room it knows, plus `followRedirects=initial`
  and, if the board's operator token is on, the `extraHeader`. It shows the lines and asks before applying them.
- The reaches are the board's own: loopback, the OpenZiti intercept and a zrok private share. On a zrok public
  share, `/git/` answers 404, with a test.
- A room reaches both on the link, through the stable forwarder of 5.1. It never uses the board listener.

## 3. The hub

### 3.1 Its store

- `<hub atrium-dir>/git/<host>/<owner>/<repo>.git`, bare, one per repo, under the hub's install setting
  `git.store`.
- **A repo is made in one of two ways:**
  - by the operator: `atrium hub git init <url>`;
  - or on a room's first push of a repo the hub does not have, when the push setting allows it (question 3);
  - or on a card's first ask for a branch of a repo the hub does not have, when the forge has that branch (3.5).
- **Its `main` is seeded once.** At creation, the hub fetches the forge's default branch into `main`, for a public
  repo. A private repo is created empty, and the operator pushes `main`. The seed is not repeated: after it, `main`
  moves only by an operator push (3.2). The only other fetches from a forge are a PR's head (`refs/atrium/pr/`) and
  the branches cards ask for (`refs/forge/`, 3.5). The second never writes `main` or anything under `refs/heads`.
- For atrium, the existing `git_repos` entry and its `claude/main` mirror stay as they are (git-sync stage 1).

### 3.2 Receiving a push

`git receive-pack`, served through the same `http-backend` setup as git-sync 4.4 with `http.receivepack=true` for
this route only. The settings are passed in the CGI environment, never written into the repo:
- `receive.denyNonFastForwards=true`;
- `receive.denyDeletes=true`;
- `receive.fsckObjects=true`;
- `receive.advertisePushOptions=false`.

Then **one hook the hub owns**, a `pre-receive` handler in Go run before any ref moves. A push is refused whole
when any updated ref breaks these rules:

| ref | a room's card may | the operator may |
| --- | --- | --- |
| `refs/heads/main`, `refs/heads/claude/main` | no | yes, fast-forward only |
| `refs/heads/<any other name>` | create, or fast-forward | create, or fast-forward |
| delete any ref | no | yes, by `atrium hub git delete`, not by a push |
| `refs/tags/*` | no | yes |
| anything outside `refs/heads` and `refs/tags` | no | no |

- **A branch's first pusher owns it, and the owner is a card.** The hub keeps a push log row per update: repo,
  ref, old and new sha, the room, the card, and the time. The card that first pushed a branch owns it.
  - A push to that branch from any other card is refused as `owned by sg4's <card>, fetch it and push under another
    name`. That includes another card on the same room.
  - **A moved card's successor inherits it.** Ownership follows the `moved_to` chain (room-handoff section 5), so
    the card's own work survives a move or a runner switch.
  - **A culled card releases it.** When its card is culled, or has been done for 7 days, the branch is released: the
    next card to push a fast-forward owns it. The operator can release a branch at any time with
    `atrium hub git release <repo> <branch>`.
  - A non-fast-forward is refused by git itself. Together these give Q6's plain-git rule plus who pushed what.
- **No two branches may differ only in case.** sg4's NTFS is case-insensitive, so loose refs `Fix/x` and `fix/x`
  would be one file, giving an overwrite and the wrong owner. The pre-receive refuses a new ref whose lowercased
  name matches an existing ref's. New hub repos are made with `--ref-format=reftable` where the hub's git has it
  (2.45 and later), which stores no ref as a file. The pre-receive rule holds either way.
- **Who the pusher is** comes from the reach, never from what the push says (3.4).
- **Size:** a push over 500 MB is refused (`receive.maxInputSize`).

### 3.3 Passing a fetch through to a room

A fetch at `/git/room/sg4/<repo>.git` is passed to sg4's link-only git route (git-sync section 7), over the data
connections sg4 dialled. The hub forwards the reader's request and streams the answer back, and stores nothing.

- **sg4's served set governs.** sg4's upload-pack runs with git-sync 4.4's settings:
  - environment-only config;
  - `http.getanyfile=false`, so no dumb-protocol file serving;
  - protocol v0, with the hub dropping the `Git-Protocol` header on the way;
  - no tip-sha or reachable-sha wants;
  - hideRefs set so that only `claude/*` and the branches of sg4's live cards are advertised.

  A want outside that set is refused by sg4's own git, so a reader cannot ask for stash, notes or any private branch
  through the hub. This was @review's M2 on revision 1 ("the room's served set must govern"), and it now holds by
  the room's own configuration. The fsck-before-cache half of M2 falls away, because nothing is cached.
- **The room serves normal names.** The served set is the room's branches as they are named there, so
  `git@sg4.atrium:...` shows `fix/x`, not `rooms/sg4/fix/x`.
- **Offline fails.** When sg4 is not attached, the answer is `503 sg4 is not connected`. git shows it, and
  `atrium_git_url` says the same before anyone tries.
- **Per reader:** 6 pass-throughs a minute, and at most 2 running at once per room.
- **For the build.**
  - The room writes hideRefs per request as `hide refs/`, then `!refs/heads/claude/`, then one `!refs/heads/<b>`
    for each live card's branch.
  - `uploadpack.allowFilter` is off. The hub refuses a request that carries `shallow`, `deepen` or `filter` lines
    before it is passed on.
  - Tests ask for `refs/stash`, a `refs/notes/*` ref and an unserved branch, and each must be refused.

### 3.4 Who is asking, and who is pushing (the defaults for the interview's open items)

| caller | reach | identity | may |
| --- | --- | --- | --- |
| a card on a room | the room's stable forwarder (5.1) to the link's `git` kind | the room from its hub-signed link certificate (direct), or as decision 18 settles it on an overlay. The card from its own atrium token, checked by the room's forwarder | fetch the hub's store and any room's pass-through. Push per 3.2's card column, when the room's push setting allows it |
| the operator on the hub machine | loopback | the board's own rule for loopback | everything in 3.2's operator column |
| the operator on another machine | the OpenZiti intercept or a zrok private share | the overlay's policy, plus the board's operator token through `extraHeader` when that token is on | the same as loopback |
| anyone on a zrok public share | refused | none | nothing |

**A card that runs outside code runs with `git.push=none` and no atrium token in its git environment.** That covers
the PR runner's `prove`, a PR's tests, and any card whose cwd is an outside target. Code it runs could otherwise read
the token from the environment and push as that card.

So no account and no new credential: rooms are their certificates, cards are their atrium tokens, and the operator
is whoever can reach the board as the operator.

### 3.5 A forge's branch, fetched when a card asks for it

Added 2026-10-07. A card was asked to work on `openziti/ziti-tunnel-sdk-c`'s `mfa-posture-tests`, a branch of a
public repo. The hub answered "not found" because the store held only `main`, and the card fell back to the GitHub API.
clint: "this is a fucking public repo. the hub can just pull it." So this section replaces "the hub never fetches a
forge's branches". It is still not a mirror: only a branch a card asked for is fetched.

- **When.** A lookup with a branch (`atrium_git_url`, `GET /_hub/git/url`) that neither the hub's `refs/heads` nor
  an attached room has. The hub asks the repo's forge (`https://<host>/<owner>/<repo>.git`, the URL the `main` seed
  reads) with one `ls-remote`, and fetches the branch if it is there. An ask with `room=` asks only that room and never
  the forge.
- **Where it lives.** `refs/forge/<branch>`, a namespace of its own. Never `refs/heads`, so `main` and a room's pushed
  work are never written over. A room may still push a branch of the same name, and the push takes `refs/heads/<branch>`
  as a create, under 3.2's rules.
- **How a card fetches it.** The store's fetch route advertises each `refs/forge/<b>` a second time as
  `refs/heads/<b>`, unless a pushed branch has that name (without regard to case). So `git fetch hub <branch>` just
  works, and `git fetch hub forge/<branch>` always names the forge's copy. git's own upload-pack serves the want,
  because the sha is the tip of `refs/forge/<b>`, which it advertised. No allow-sha setting changes.
- **The answer** is a `hub` source with `forge: true`, on the same store URL, and the note says both fetch commands.
  A room older than this reads it as a plain `hub` source, and its URL fetches the branch all the same.
- **Refreshed on each ask.** Every lookup fetches it again when the forge's tip moved. The fetch route also refreshes
  the forge branches the store holds before it advertises them, at most once every 10 seconds per repo and within
  20 seconds, so a colleague's new commits arrive. A forge that cannot be asked leaves the copy the store has. A
  branch the forge deleted is removed from `refs/forge/`. The forge may force-push its branch, and the copy follows,
  because `refs/forge/` is nobody's work but the forge's.
- **The fetch route cannot make the first fetch.** A protocol v0 fetch names no branch before the hub has advertised
  its refs, and v2 is not spoken (3.3). Fetching every forge branch there would be the mirror clint refused (Q1). So
  the first fetch of a branch is the lookup's, which every card brief already says to call.
- **A repo the hub does not hold yet** is made, as `atrium hub git init <url>` makes it with `main` seeded, when the
  ask names it fully (`<owner>/<repo>`, `<host>/<owner>/<repo>` or a URL) and the forge has that branch. A typo makes
  nothing. A `git_repos` mirror on the disk where it would go is the operator's, and a lookup never takes it.
- **Credentials.** None for a public repo. A private one uses the login the PR head fetch uses: the forge CLI's
  credential helper, for that one command only (`Hub.ForgeHelper`). When there is none, the answer is
  `no credential` with "the hub has no credential for <repo>". It is never `not found`. A forge that has the repo and
  not the branch answers `not found`, and says the forge has no such branch either.
- **Bounded.** One fetch per branch at a time: a second ask waits for the first and takes its answer. The seed's
  2-minute timeout applies to each fetch. The `ls-remote` runs with no lock held, and only a fetch that moves a ref
  takes the repo's lock, so a push waits only for a real fetch. At most 100 forge branches are held per repo. A forge
  branch that differs from a held one only in case is refused (NTFS).
- **Read-only.** Every transfer is a fetch from the forge with `transfer.fsckObjects`, and nothing is ever pushed to
  one.

## 4. The lookup: `atrium_git_url`

Revision 1's section 3.1, kept with three changes:
- The answer names one of two sources, `hub` (finished, pushed) or `room` (in progress, passed through), and
  `online` for a room.
- A branch that is both pushed and still on its room answers both, with the shas. `ahead` says whether the room has
  commits the hub does not.
- There is no `collected_at`, because there is no copy.

Every card brief keeps the line: "to read code that is not in your cwd, call `atrium_git_url`, then fetch it from the
URL it gives. Never ask for a paste." A miss answers `not found`, with the closest repos and branches, and an offline
room answers `offline`. A branch only the forge has is fetched into the hub and answered as a `hub` source with
`forge: true` (3.5), and a private repo the hub has no login for answers `no credential`.

## 5. A room

### 5.1 The stable `hub` remote

git needs one stable URL for a remote, and git-sync 5's forwarder lives for one command. So each room runs one
**stable forwarder on its agent listener**:
- `http://127.0.0.1:<agent port>/git/hub/<host>/<owner>/<repo>.git`;
- `http://127.0.0.1:<agent port>/git/room/<room>/<host>/<owner>/<repo>.git`.

Both are forwarded to the hub over the link's `git` kind. The forwarder:
- takes the card from its atrium token, the same check every agent-listener route makes (security design 2c);
- sends the card id with the request, so the hub's push log names the card;
- refuses a request with no card token.

A clone's `hub` remote is set to the first URL. The token reaches git through the card's own environment, set by
the room at launch, and is never written into `.git/config`.
- **The header is scoped to atrium's own URL.** The key is `http.http://127.0.0.1:<agent port>/git/.extraHeader`,
  never a bare `http.extraHeader`. A bare one goes to every http remote, so a fetch from `origin`, a dependency or a
  submodule would send the atrium token to github.com.
- **A test proves it.** A second, header-recording server is used as another remote, a fetch from it is made in a
  card, and it must receive no atrium header.

### 5.2 Clones in the scm folder

- **The scm folder** is a room setting, `git.scm_root`, set at install (`D:/git` on sg4, `~/git` on m1mini). The
  layout is `<scm_root>/<host>/<owner>/<repo>`. A card's worktree stays `<repo>-worktrees/<id>` beside it, as
  git-sync stage 2 has it.
- **"Go work on `<url>`"** is `atrium_git_clone {url}`. The room:
  1. resolves the path;
  2. if a clone is there, uses it;
  3. else runs `git clone <url> <path>` with `GIT_TERMINAL_PROMPT=0` and only the credential helper the operator
     configured for atrium;
  4. adds `hub`, with `git remote add hub <stable URL>`.

  It returns the path.
- **A clone that fails** (a private repo with no credential, or a wrong name) answers clint's sentence exactly: "that
  repo doesn't exist, check it, and if it is private have the operator clone it. Atrium can't."
- **An existing clone** the operator made gets `hub` added, after one yes on the board the first time for that clone.
  If it already has a `hub` remote pointing somewhere else, atrium leaves it alone and says so (@review's L1 on
  revision 1, kept). It adds atrium's remote under the name `atrium-hub` in that clone instead.
- **`origin` stays the forge** (Q5).

### 5.3 Cards push to `hub` only

Three walls, so no single one has to hold:
1. **The dotfiles hook** permits exactly `git push hub <branch>`, `git push -u hub <branch>` and `git fetch hub`,
   and the same with `atrium-hub`.
   - It does so only after it checks, in the command's cwd, that `git remote get-url --push <name>` starts with
     atrium's forwarder, `http://127.0.0.1:<agent port>/git/`. It checks the URL git will actually push to, after
     every `url.*.insteadOf` and `pushInsteadOf` rewrite, not the raw `remote.<name>.url` config. A global rewrite
     could otherwise send a push elsewhere while the config still matches. An operator's own `hub` pointing at
     another server is refused, so a card cannot push to the operator's other server.
   - It refuses any force flag (`-f`, `--force`, `--force-with-lease`), a `+` refspec, `--mirror`, `--all`, `--tags`,
     `--delete` or `:<ref>`, and any other remote.
   - Everything else stays refused as today.
   - `atrium_git_push` makes the same URL check.
2. **The room's forwarder** is the only place a card's push can go. `origin`'s URL is the forge, and a card has no
   forge credential unless the operator gave atrium one. On clones atrium makes, `remote.origin.pushurl` is set to
   `atrium-refused://origin-push`, so even a script that bypasses the hook cannot push to the forge. On an operator's
   existing clone, atrium adds that line only with the yes of 5.2.
3. **The hub's pre-receive** (3.2): no force, no fast-forward to `main`, no deletes, no tags.

The push permission is a room setting, `git.push`: `none`, or `hub`. Its default is `hub`, by clint's answer to Q7.
With `none`, the forwarder refuses receive-pack.

An MCP tool, `atrium_git_push {branch}`, does the same push for a card that would rather not shell out, and is
refused by the same rules.

## 6. Finished, and the change record

- **A change is finished when its branch is on the hub at the head the room has.** The change record's "Pushed" line
  (`docs/rnd/change-record-design.md`) reads the hub's push log, not a forge.
- **Change requests between rooms** (revision 1's F5) stay. A request names a source (a room's branch, or a pushed
  branch on the hub) and a target, with the change record attached. Its title and why reach the owner as data. A
  request whose target is `main` goes to the orchestrator or clint, who merges on the hub's side, re-signs and pushes
  on. The hub's merge queue (git-sync stage 3) automates that later, under decision 46.
- **Review-on-atrium 7.7** follows this design: a persona job reads in-progress code by pass-through from the room
  that has it, and fails with "the room is offline" otherwise. It never asks for a paste and never uses a copy.

## 7. Stages, and stage 1 cut into build items

All held by the pause until the orchestrator releases them. Owners from section 7's backlog files.

### Stage 1: the hub holds `main`, rooms push finished work, cards push to `hub` only

| item | owner | what | size | acceptance |
| --- | --- | --- | --- | --- |
| `f-new-hub-git-store` | @fabric | the hub's store (3.1): `git.store`, `atrium hub git init <url>`, the one seed of `main`, and creation on first push when allowed | 1 day | `atrium hub git init https://github.com/netfoundry/omnigent` makes the bare repo with `main` at the forge's default head. A second init is a no-op. A private URL with no credential makes an empty repo and says to push `main` |
| `f-new-hub-receive` | @fabric | receive-pack on the link and on the operator's reaches (3.2), the environment settings, the Go pre-receive with 3.2's table, the push log, per-card ownership with inheritance by `moved_to` and release on cull, case-only collisions refused, reftable where available, and the size cap | 2 days | a card's push of a new `fix/x` lands and is logged with room and card. A non-fast-forward push is refused. A push of `main` from a card is refused, and from the operator on loopback lands. Another card's push to `fix/x`, on any room, is refused as owned. The owner's successor after a move pushes to it, and after the owner is culled the next card's fast-forward takes it. A push of `Fix/x` while `fix/x` exists is refused on sg4. A delete, a tag or a `refs/notes` push from a card is refused, and nothing moves |
| `r-new-hub-remote` | @runtime | the room's stable forwarder on the agent listener (5.1) with card tokens, the card's git environment at launch with the header scoped to the forwarder's URL, `git.push` (5.3) and `none` for cards running outside code, `atrium_git_push` with the URL check, and `hub` (or `atrium-hub` where `hub` is taken) added to the clones the room already syncs | 1.5 days | in a card on m1mini, `git push hub fix/x` lands on the hub with that card in the push log. The same push from a process with no card token is refused by the forwarder. With `git.push: none`, it is refused. `git push origin fix/x` from a script on an atrium-made clone fails on the pushurl. A fetch in that card from a header-recording server receives no atrium header. A PR-test card has no token in its environment. On a clone whose own `hub` points elsewhere, `atrium_git_push` refuses and the remote added is `atrium-hub` |
| `dotfiles: hook allows git push hub` | clint or the orchestrator, in dotfiles | 5.3 wall 1, with the URL check | an hour | `git push hub fix/x` passes the hook when `hub` is atrium's forwarder, and is refused when `hub` points at another server. `git push -f hub fix/x`, `git push hub +fix/x`, `git push origin fix/x` and `git push hub :fix/x` are refused |
| `u-new-hub-repos-list` | @ui | the board's list of hub repos: `main`, the pushed branches with pusher, room and time, and each one's clone URL | 1 day | clint sees `fix/x` pushed by sg4's card, and copies its `git@hub.atrium:` URL |

Stage 1 needs none of the pass-through, the clone model or the lookup. It works for the repos the rooms already have.

### Later stages

| stage | what | owner | size |
| --- | --- | --- | --- |
| 2 | Clones in the scm folder (5.2): `git.scm_root`, `atrium_git_clone`, clint's failure sentence, `hub` on existing clones after a yes, and the origin pushurl guard on atrium-made clones | @runtime | 1.5 days |
| 3 | Pass-through to the owning room (3.3), under the room's served set, offline as 503, the per-reader rate, the `room/<room>` URLs and `atrium git setup` | @fabric, @runtime (the room's served set) | 2 days |
| 4 | `atrium_git_url` (section 4) and the brief line | @fabric, @runtime | 1 day |
| 5 | Change requests between rooms (section 6), and the change record's "Pushed" from the push log | @fabric, @ui | 2 days |

## 8. Questions for clint, held until he asks

From the interview's open items, each with the default this design takes:

1. **How a machine proves who it is when it pushes.** By the certificate the hub signed for it when it joined, and
   the card by its own atrium token, so the hub's log says which machine and which card pushed each branch.
   **Suggested: yes.**
2. **Who may read and push from outside the machines.** Your own machines reach it the way they reach the board, and
   never through the public share. Pushing to `main` is only for you, or for the orchestrator on the hub machine.
   **Suggested: yes.**
3. **The install settings.** At install, the hub asks for its store folder and whether a room's first push may
   create a new repo on the hub. Each room asks for its scm folder (yours: `D:/git`) and whether cards may push to
   the hub (yours: yes). **Suggested: yes, with "first push may create a repo" off until you turn it on.**
4. **A branch belongs to its first pusher.** If sg4 pushed `fix/x`, m1mini cannot push over it and must use another
   name, as on any git server. **Suggested: yes.**

## Appendix: Hub forge interview answers (clint)

Merged from `docs/rnd/hub-forge-answers.md` on 2026-10-07.

### Q1. Mirror every repo any card works in automatically?
Answer (2026-10-02): No, wasteful. Not auto, and not a copy made when someone asks either. Clint's model: m1mini is
told "pull sg4's fix/x" and git-fetches from the hub at sg4's name (e.g. sg4.atrium:...). The hub sees the request is
for sg4 and PROXIES it to sg4. The hub is a router, not a store.

Clarification (clint, same day): the hub holds its own `main`. It is where sg4 and m1mini PUSH when a feature is done,
so that the orchestrator or clint can pull, merge, re-sign and push from there. So the hub does store pushed work.
What it does not do is mirror work in progress: that stays on the room and is proxied live.

### Q2. Hub keeps a copy for when the room is offline?
Answer (2026-10-02): No. Room off, asleep or disconnected means the fetch fails ("you're fucked"). No fallback copy.

### Q3. Build order, push or proxy first?
Answer (2026-10-02): Push is core. Rule from clint: the hub is the only place a "finished" feature can be. If it is
only on a room, it is NOT finished. So push to the hub is first-class, not "later".

### Q4. Where does a pushed branch land on the hub (rooms/<room>/<branch>)?
Answer (2026-10-02): Not answered directly. Clint: this should be a configuration option, picked (forced) during hub
install. And the clone model, in his words:
- The operator sets an "scm folder" (his is d:\git). Layout is <scm>/<host>/<owner>/<repo>.
- "Go work on <url>": atrium tries to `git clone` it into the scm folder.
- Private repo and atrium has no credential: the clone FAILS, and the agent is told "that repo doesn't exist, check
  it, and if it is private have the operator clone it, atrium can't". If the operator gave atrium a real key, it works.
- Public repo (e.g. netfoundry/omnigent): the clone just works, into d:\git\github\netfoundry\omnigent.
- The clone has a remote named `hub` next to the usual one. Open: `origin` AND `hub`, or `hub` only. Clint can be
  convinced either way.

### Q5. origin as well as hub?
Answer (2026-10-02): Taken as `origin` AND `hub` (clint said "origin AND hub I suppose", and called a re-ask a repeat).

### Q6. Two rooms push the same branch name?
Answer (2026-10-02): Plain git rules, so B: no force push, a non-fast-forward push is refused. Branches live under
their normal names, not under per-room namespaces.

### Q7. What may a room push to the hub?
Answer (2026-10-02): Must match the operator's preferences. Clint normally allows no pushes at all (does not trust the
agent not to do dumb things), but would allow pushes to the hub repo only. Taken as: a push permission that is a
setting, with the hub as the one allowed target. (Confirming whether "never origin" is the rule.)

### Q8. Rule: cards may push to `hub`, never to `origin`?
Answer (2026-10-02): Yes ("a safe and smart design"). The dotfiles hook keeps refusing everything else.

Note for @rnd: this reverses the design's section 1a rule "the hub never forwards the reader's request to the room"
(it does a fetch-through into its own mirror instead). Whether a pass-through is acceptable given sg4's served-set
rules is still to be settled.
