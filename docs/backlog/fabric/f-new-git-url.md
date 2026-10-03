# f-new-git-url: a card asks the hub where to fetch code (hub forge stage 4, `atrium_git_url`)

Status: BUILT on claude/f-git-url (from claude/landing e9be3fc3), waiting for @review via @fabric. Not landed. Owner @fabric,
with @runtime for the card-brief line.
Design: `docs/rnd/hub-forge-design.md` section 4 and the stage 4 row of 7.

Done:
- `GET /_hub/git/url?repo=&branch=` on the hub: per branch, source `hub` (finished) or `room` (in progress, passed
  through, with `online`), both with shas and `ahead` when a branch is in both, `not found` with the closest repos and
  branches (bounded), `offline` for a room that is not attached, no `collected_at`;
- a room's branches come from the room's own served-set advertisement through the link git kind (so a branch the
  pass-through would refuse is never listed), bounded (5 s, 4 MiB, 500 branches) and cached 10 s;
- URLs use the host the caller reached the hub by;
- the control tool `atrium_git_url` (repo, branch, room) returning the same JSON and a `fetch it with:` line, a worker
  tool;
- who may ask is the pass-through's rule, now one function (`gitReach`): loopback, overlay and zrok private; zrok public
  is 404;
- tests for every case in the brief and 12 mutations, each red.

Left:
- The card-brief line is @runtime's (`internal/daemon` launch brief / CLAUDE-side text), not edited here: "To read code
  that is not in your cwd, call atrium_git_url, then fetch it from the URL it gives. Never ask for a paste."
- No CLI door: `atrium_git_sync` and the other git tools have none. Add one if a person at a terminal should ask.
- The URL host a card on ANOTHER room sees is the board address its control MCP asks by (loopback for the hub's own
  control MCP). Until @runtime's stable forwarder (stage 1) lands, a card on another room cannot always use it as is.
- A pass-through of a card on a room over the link is still stage 1's forwarder and answers 404 on the board.
- Matching a room's branches to a repository by FULL name for every card waits for stage 2's scm path (as in stage 3).
- The real run across sg3 and m1mini (`docs/changes/f-new-git-url.md`, step 1) is the orchestrator's.
