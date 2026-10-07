# Review: hub as a forge, git-sync stage 4 (d32a34fd + e34e461a, m1mini, 2026-10-02): HOLD

`docs/fabric/hub-forge-design.md`, the only two commits touching it. Questions are held in the doc.

This is the right answer to "never a paste":
- every repo a card works in is mirrored automatically;
- rooms' branches are served under `rooms/<room>/`;
- smart HTTP on the board's own reaches, with no ssh and no accounts;
- one lookup, `atrium_git_url`;
- pass-through for freshness, with the hub copy as fallback;
- read-only first, push and landing later behind stage 3;
- the hub never moves a room's ref.

It holds on two ways the new namespaces can be confused or widened.

## Medium 1 (holds): the forge fetch can write into `refs/heads/rooms/*`

Section 2 fetches the forge with `+refs/heads/*:refs/heads/*` into the same mirror where rooms' branches are served,
by mapping, as `refs/heads/rooms/<room>/<branch>`. An outside repo's branch names belong to its authors. A branch
named `rooms/sg4/fix/x` on the forge lands at `refs/heads/rooms/sg4/fix/x` and collides with the mapped name of sg4's
real work. A reader fetching `rooms/sg4/fix/x` then gets the outside author's branch or sg4's, depending on which
wins, and a reviewer reviews the wrong code. Fix:
- store forge heads under their own namespace (`refs/forge/heads/*`) and map them to `refs/heads/*` at serve time,
  as the rooms are mapped; or exclude `refs/heads/rooms/*` from the forge refspec, beside the `claude/*` exclusion
  already there;
- refuse to serve any name that two sources claim, and say so in `atrium_git_url`;
- a test: a forge branch named `rooms/<room>/x` never shadows the room's.

## Medium 2 (holds): pass-through must serve exactly the collect's set

1a forwards the reader's upload-pack request to the source room's link-only git route. Say that the room's served set
governs: what collect may fetch (`claude/*` and each live card's branch), with everything else hidden on the room's
side (`uploadpack.hideRefs`, no tip or reachable sha wants). Otherwise a pass-through can ask a room for refs the
collect never takes: other branches in the clone, `refs/stash`, notes, a worktree's private branch. The forwarding is
a request the reader writes, so the room side has to bound it, not the hub. Also verify the pack before caching it
(index-pack and fsck), and rate-limit pass-through per reader, so one card cannot make a room serve continuously.
F2b's acceptance: a pass-through `want` for a branch the room does not serve is refused.

## Your checks

- **Section 2's reversal of `hideRefs=refs/rooms`, and section 5's argument: right, given Medium 1.** The rooms are
  all the operator's, and stage 1 called the hiding tidiness. The sg3 failure cannot recur:
  - sync is unchanged, with its exact refspec onto `claude/main` and `hub-main`;
  - `atrium_git_fetch` writes only `refs/remotes/hub/`;
  - F2's acceptance launches a worker after a `rooms/*` fetch and checks it starts at `claude/main`.

  Low L1: say whether a room already has a remote called `hub`. If it does, a `git fetch hub --prune` by anything
  using that remote's refspec would prune the tool's refs, or overwrite them. Use a dedicated remote name.
- **1a, live pass-through and the cache: right in shape.** See Medium 2.
- **Section 3, `/git/` on the board's loopback and overlay reaches, 404 on a zrok public share, the token via
  `http.extraHeader`, no ssh: right.**
  - On the loopback board, edge's CrossOriginProtection stops a web page from POSTing `git-upload-pack`, and its
    responses are unreadable cross-origin. A git client sends no Origin, so it passes.
  - The public-share 404 is tested by the listener that knows which share it is. Good.
  - L2: `extraHeader` puts the token in clint's `~/.gitconfig` in the clear, and git sends it to that URL prefix
    only. Keep `http.followRedirects` at its default (`initial`) in `atrium git setup`, and say so, so the header
    never follows a redirect to another host. A credential helper would be the tidier later option.
- **3.1, `atrium_git_url`, including dirty and stale: right.**
  - A miss is an answer, the reach-specific URL is right, and `stale` collects that one ref first.
  - L3: for a persona job, `dirty: true` should mean refused ("commit it first"), as review-on-atrium 7.7 says. Only
    a plain lookup returns it as a warning. Say which callers refuse.
- **F5, room-to-room change requests: right.**
  - The owner card merges in its own worktree, the hub never moves a room's ref, and `claude/main` waits for
    stage 3.
  - L4: the request's title and why come from the source room, so mark them as data in the owner's report. The
    merged commits reach `claude/main` only through review, whose verdict range will include them. Say that.

## Verdict

HOLD on Medium 1 (the forge fetch kept out of `refs/heads/rooms/*`, and no name served twice) and Medium 2
(pass-through bounded by the room's served set, the pack verified, a per-reader rate). Fold in L1 to L4. A re-read
covers sections 1a, 2, 3 and 3.1 and F1, F2 and F2b. doc-ok on OK.

Closed: none (first read)
Open: M1, M2, L1, L2, L3, L4

Quality: an ambitious design that still stays small at each stage, with stage 1's safety argument carried forward
explicitly. The two gaps are namespace questions, which is where a forge's security usually lives.

## Re-read at 1c965657 (2026-10-02): OK, doc-ok

One commit, only the design doc.
- **M1, closed.** Forge refs are stored under `refs/forge/heads` and `refs/forge/tags` and mapped only at serve time.
  A served name two sources claim is refused. A forge branch starting `rooms/`, `claude/main` or `pull/` is withheld
  and listed on the board. F1's acceptance covers a forge branch named `rooms/sg4/fix/x`.
- **M2, closed, and better than asked.** Pass-through is replaced by fetch-through. The hub never forwards a reader's
  wants. It does its own one-ref collect under the room's served set (`claude/*` and live card branches, no
  tip-sha or reachable-sha wants), runs `index-pack` and `fsck` (`transfer.fsckObjects` on every mirror fetch), and
  only then serves from the mirror. A reader gets 6 fetch-throughs a minute, with one per room at a time. F2b's
  acceptance refuses stash, notes, unserved branches and bare shas with nothing asked of the room, and a corrupt
  pack moves no ref.
- **L1 to L4, closed.** The remote is `atrium-hub`, setup sets `http.followRedirects=initial`, persona jobs refuse a
  dirty target while a plain lookup warns, and a PR's title and why are data in the owner's report.

Closed: M1, M2, L1, L2, L3, L4
Open: none

Verdict: OK, doc-ok d32a34fd^..1c965657. It lands alone by cherry-pick of d32a34fd, e34e461a and 1c965657.
