# Review: N1 hint 44ae7853, and the claude/ui-director queue docs

## 44ae7853 (claude/u-switch-n1)

`df35bbbb..44ae7853`, one commit, changing the gear hint in `index.html` and the item file. It is the keep-alive N1
note: a kept terminal keeps its vote on the pty's size. It names the room frame "hidden: not watching, not voting" as
the real fix, owned by @runtime. Landed as a fast-forward.

Atrium-Verdict: doc-ok df35bbbb..44ae7853

## The queue (claude/landing..claude/ui-director)

All seven are docs under `docs/backlog/ui/`.

Landed by cherry-pick:
- `a1293c2b`, the burn-chart done line. The `D:/tmp/burn-chart` path was already in QUEUE.md, and it is a temp path
  with no user name.
- `f7164107`, the grouping spikes.
- `326384cf`, the bell repaint lows.
- `7ceccb6f`, the board performance workup.

Not landed, for @ui to fix:
- **`5710f2ec`, the terminal switch latency item.** Landing already has a newer copy of this file from
  claude/u-switch-latency, so this one is superseded: drop it.
  - That newer copy quotes clint verbatim on line 3. I scrubbed it on claude/review to a paraphrase ("asked whether
    the delay when changing terminals can be fixed").
  - The quote is still in landing's history, in `5865011f`. That is a quote, not a secret, so I am not asking for a
    rewrite.
- **`ef5145e6`, the usage chart item.**
  - It quotes clint verbatim ("I like the graph, but…").
  - It names a private sg4 path to a pasted screenshot under `.atrium/incoming/`.
  - Paraphrase the ask, and say "a screenshot on sg4" without the path.
- **`f14b20ee`, the ctx-bar land-the-plane item.** It names clint's dotfiles script by path and line numbers
  (`~/.claude/statusline-command.sh`, lines ~139-147). Say "the operator's status line script" instead. The status
  line text it quotes is program output, which is fine.

Atrium-Verdict: doc-ok 29151781..7ceccb6f
