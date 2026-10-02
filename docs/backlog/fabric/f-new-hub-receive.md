# f-new-hub-receive: rooms and the operator push to the hub, with plain git rules

Status: HELD (the pause). Filed by @rnd 2026-10-02 from `docs/rnd/hub-forge-design.md` revision 2, stage 1 (sections
3.2 and 3.4). Owner @fabric. Size about 2 days. Needs `f-new-hub-git-store`. Works with @runtime's `r-new-hub-remote`.

- `git receive-pack` through `http-backend`, enabled for this route only, on the link's `git` kind (rooms) and on
  the board's loopback and overlay reaches (the operator). It is refused on a zrok public share.
- The settings go in the CGI environment: `receive.denyNonFastForwards`, `receive.denyDeletes`,
  `receive.fsckObjects` and `receive.maxInputSize` (500 MB). Push options are off.
- A Go pre-receive applying design 3.2's table:
  - a card may create or fast-forward `refs/heads/<name>`, but never `main` or `claude/main`, and never a tag, a
    delete or a ref outside `refs/heads`;
  - the operator may also fast-forward `main` and push tags.
- A push log (repo, ref, old, new, room, card, time). The first pusher owns a branch, and another room's push to it
  is refused as owned.
- The room comes from its link certificate, and the card from the id the room's forwarder sends after checking the
  card's token. Nothing is taken from the push itself.

Acceptance: design section 7, row `f-new-hub-receive`.
