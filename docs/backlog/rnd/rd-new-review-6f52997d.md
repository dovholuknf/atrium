# Review: hub as a forge, revision 2 (6f52997d, m1mini, 2026-10-02): HOLD

The design rewritten from clint's interview, plus four stage-1 build items, one commit, five files. Questions are held
in the doc.

clint's answers make it simpler and are carried through consistently:
- no mirror and no copy;
- pass-through to the owning room, and offline is a 503;
- the hub owns `main` and takes pushes with plain git rules;
- cards push to `hub` only;
- clones in the scm folder with `origin` and `hub`.

It holds on three points in the push and identity path.

## Your questions

- **3.3, raw pass-through under the room's served set: yes, that answers my old M2.** The reader's wants go to the
  room's own upload-pack, with environment-only config, `getanyfile` off, v0 (the hub drops `Git-Protocol`), no tip
  or reachable sha wants, and only `claude/*` and live branches advertised. A want for a hidden ref is refused by the
  room's git. Nothing is cached, so the fsck-before-cache half falls away. Two notes for the build:
  - `uploadpack.hideRefs` is a deny list. Write it as "hide `refs/`, then un-hide `!refs/heads/claude/` and each live
    branch", built per request from the room's live set, and test that a branch outside it, `refs/stash` and notes
    are all refused.
  - Keep `uploadpack.allowFilter` and the shallow and deepen options at their defaults (off), so nothing widens what
    is served.
- **3.2, the pre-receive table, the push log and first-pusher ownership: right in shape**, with Medium 2 and two lows:
  - L1: say whether ownership is per room or per card. "owned by sg4's <card>" reads as per card. A moved card's
    successor (`moved_to`) should inherit it, and a culled card's branches need a way to be released, or they stay
    unpushable forever.
  - L2: `refs/heads/claude/*` other than `claude/main` is open to cards under "any other name". Fine, but say so.
- **3.4 and 5.1, identity is the link certificate plus the card's atrium token at a stable forwarder, with the token
  in env only: right, with Medium 1.** One more point: a card that runs outside code (a PR's tests) hands that code
  the token in its environment, so the code can push branches as the card. The pre-receive limits the damage: no
  `main`, no force, only the card's own branches. Say that cards working on outside code run with `git.push=none`,
  or with no push token.
- **5.3, the three walls: right**, with Medium 3.
- **The remote back to `hub`, with "never overwrite an existing `hub`": right**, and that rule now creates Medium 3.

## Medium 1 (holds): the token header must be scoped to the hub's URL

5.1 sets the token "through the card's own environment (`GIT_CONFIG_COUNT`, `http.extraHeader`)". An unscoped
`http.extraHeader` is sent to **every** HTTP remote. A `git fetch origin`, a clone of a dependency or a submodule fetch
would send the card's atrium token to github.com, or to any URL a repo points at. Use the URL-scoped key,
`http.http://127.0.0.1:<agent port>/git/.extraHeader`, so git sends it only to the forwarder. Test it with a fetch from
a second local HTTP server that records headers: no token arrives.

## Medium 2 (holds): ref names that differ only in case, on the hub's own disk

The hub's bare repos sit under the hub's atrium-dir, on sg4's NTFS, which is case-insensitive. With loose refs,
`refs/heads/Fix/x` and `refs/heads/fix/x` are one file. A push of one can overwrite the other, and first-pusher
ownership then names the wrong owner. Refuse in pre-receive any new ref that equals an existing one when case is
ignored ("a branch that differs only in case exists"), or run the store with `core.ignoreCase=false` and reftable.
Test it on the Windows hub.

## Medium 3 (holds): `git push hub` must mean atrium's hub

The hook allows exactly `git push hub <branch>`. But 5.2 leaves an operator's existing `hub` remote alone when it
points elsewhere. On such a clone, the hook would let a card push to the operator's other `hub`, perhaps a real
server. Either:
- the hook allows the push only when `remote.hub.url` (and `pushurl`) is atrium's stable forwarder URL, checked at
  push time; or
- atrium uses a name it controls (`atrium-hub`) on any clone whose `hub` is taken, and the hook allows that name
  there.

`atrium_git_push` checks the same thing.

## Smaller

- **The stage-1 items** match the doc: `f-new-hub-git-store`, `f-new-hub-receive`, `r-new-hub-remote` and
  `u-new-hub-repos-list`. Carry Mediums 1 to 3 into `f-new-hub-receive` and `r-new-hub-remote` as acceptance lines.
- **The push log** is the record a later change record and the "who pushed" display read. Keep it append-only.

## Verdict

HOLD on Medium 1 (URL-scoped extraHeader), Medium 2 (case-insensitive ref collisions on the Windows hub) and Medium 3
(`hub` must be atrium's forwarder). A re-read covers 3.2, 3.3, 5.1, 5.2, 5.3 and the four items. doc-ok on OK.

Closed: (revision 1's M2 and L1 hold in the new shape)
Open: M1, M2, M3, L1, L2

Quality: a clean rewrite that takes clint's answers at their word and keeps revision 1's safety ideas where they still
apply. The gaps are in how git config and filesystems behave, not in the design's logic.
