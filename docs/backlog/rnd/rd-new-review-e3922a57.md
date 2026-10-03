# Review: context-limit ownership design e3922a57

`docs/rnd/context-limit-ownership-design.md`, the only file in its commit, on `30cd5837`. It lands by cherry-pick.

Verdict: **doc-ok.** The section 3 finding is right, and L0 is the right fix. Two Mediums should go into L0 before it
is built. They are design points and do not hold the doc.

## How it was checked

- I read the doc in full.
- I checked section 3 against `internal/daemon/autocontext.go` on claude/landing 110c224a: `cardLimit`,
  `autocompactK` and `autoThreshold`.
- I worked through the section 3 table and the L0 formula by hand.

## Section 3: the claim holds

- **The shipped flag is what the doc says.** `autocompactK` is `cardLimit × 11 / 10`. It is clamped to
  `modelWindowK` and to the 100k-1M range.
- **The shipped threshold is too.** `autoThreshold` is `min(cardLimit, 70% of the statusline window)`.
- **The margin is constant.** The two measurements, 330k compacting at about 297k and 165k at about 133k, give
  margins of 33k and 32k.
  - A proportional rule would put the second at about 148k, so a fixed reserve fits the data and a ratio does not.
  - With a fixed 33k, the real backstop is `1.1 × limit − 33k`. That is below the limit for any limit under 330k.
- **Every row of the table checks out:**
  - 1M window at k = 200: 200k, 220k, about 187k, and compaction comes first.
  - 200k window: 140k, then the flag clamped to 200k, about 167k, and atrium comes first.
  - 150k ceiling: 140k, 165k, about 133k, and compaction comes first.
- **The 18:44 overtaken capture fits the finding.**

## The L0 formula

The two worked examples are right:
- 1M window at k = 200: the threshold is 200k and the flag is 253k, so compaction comes at about 220k.
- 200k window: the threshold is 147k and the flag is 200k, so compaction comes at about 167k.

Low limits are safe too. Below about 47k the flag is raised to the 100k floor, which only makes compaction later.

Taking the 70% and the 10% out, and putting one margin per runner on the runner row, is the right shape.

## Mediums

### M1: 20k of headroom covers the capture turn, not the rest of the turn the card is in

atrium types only between turns. A card that crosses the threshold mid-turn keeps growing until that turn ends, and
only then does the capture start. The 18:44 case was that: a deferred capture overtaken by a compaction.

With L0, compaction still wins whenever the turn after the crossing, plus the capture, grows more than 20k. Section
2's last row accepts this ("a card that crosses the limit mid-turn: the runner's compact"). Section 3 and L0's
done-when read as if atrium always acts first.

Do one of these:
- **Size the headroom from the data.** The section 1 script can measure the growth from the crossing to the end of
  the turn. Take its p90 plus the capture.
- **Or say it plainly in section 3.** "atrium acts first when the card is between turns or close to it. A turn that
  runs more than the headroom past the limit is compacted, by design." L1's re-anchor then covers that case.

### M2: a margin from two points on one version decides the whole layer

33k is measured on Claude Code 2.1.288 at two flag values. L0's done-when uses a fake transcript, so nothing in L0
checks the real margin.

- Add one real-card check to L0: a card started with `--autocompact 253k` compacts at about 220k.
- Re-measure the margin when Claude Code updates. The runner-row field allows that, so say when it is checked: for
  example, the room's runner-version check, or L5.
- If claude ever reserves a share of the window, not a fixed amount, then the margin field becomes a function.

## Lows

- **L1: workers do not compact "at the limit".** Section 2 and L2 say workers compact at the limit. The flag from the
  same function puts their compaction at `threshold + headroom`, which is 20k later. Use `flag = limit + buffer`
  for a card whose action is `compact`, or say "20k past the limit".
- **L2: the flag is fixed at launch, and the threshold can move.**
  - `--autocompact` is a launch argument. Its window comes from `modelWindowK`, and there is no statusline yet.
  - The watcher may later use a smaller statusline window. That lowers only the threshold, which is the safe
    direction.
  - Say that the function takes the launch-time window for the flag, and the freshest window for the threshold.
  - An unknown model (`modelWindowK` 0) leaves the flag unclamped. That is no worse than today.
- **L3: the off-repo notes.** `/tmp/rnd-ctx-measure.md` and `/tmp/rnd-ctx-runners.md` are scratch on one machine.
  L5's re-measure needs the script, so check the script in under `scripts/` with no data, or name where it lives.

## Other points

- **Section 4, the runner table.** Good. Checking the PreCompact field name (`trigger` against `triggered_by`)
  against a captured payload is the right note.
- **The held questions** follow the interviewer brief: a scenario first, with real card names, and a suggested
  answer.
- **No private paths, SIDs or outside names.** The two items the ask names but that were not found are said to be
  missing, not guessed.

Atrium-Verdict: doc-ok 30cd5837..e3922a57
Quality: a measured study that found a live bug in the shipped backstop, with the arithmetic shown and the fix
reduced to one function.

## Re-read: 94e46c8b

Read as one range, `30cd5837..94e46c8b`. The new commit changes only the context-limit doc: it rewrites section 7,
adds `threshold_k` to L0, and adds stage LL.

Verdict: **doc-ok.** The two sg4 items are folded in faithfully, and the bar reading the threshold is the right call.

What reads well:
- **r-clear-vs-restart is placed correctly.** Its four asks shipped in 7323b17f. What carries forward is the
  exited-but-not-relaunched journal step for L3, which is a real gap. Without it, a room that comes back would leave
  the card exited.
- **"Compacted during capture" on the chip.** It is honest about the handoff's source. It also accepts that a capture
  waiting mid-turn can still be overtaken after L0, which partly answers M1 above. M1's other half still stands for
  L0: size the headroom from the data, or say the mid-turn case in section 3.
- **The limit layer.**
  - The per-runner limit becomes layer 2, and the margin is shown read-only in the editors.
  - `threshold_k` is the bar's denominator. The 73% example shows why the setting would mislead.
  - LL's done-when ("a codex row limit of 150 moves only codex cards' thresholds") can be tested.

Lows:
- **L4: say how the layers combine.** "From the most specific layer to the least" reads as "the first one set wins".
  Today `cardLimit` is different: it takes the **lower** of the ceiling and the global k when the mode reaches the
  card.
  - With a runner layer in between, say which rule applies at each step, override or min.
  - For example: does a ceiling of 150 on a codex row whose limit is 120 give 150 or 120?
- **L5: a runner limit and workers.** L2 says a worker's flag "follows k". After LL it follows the function, so it
  follows the runner's row limit too. Say that in L2, so a codex row limit also moves codex workers' compaction.

Atrium-Verdict: doc-ok 30cd5837..94e46c8b
Quality: a faithful fold-in that turns two loose items into one place in the function and one journal step.

## Re-read: 5557182b

Range `94e46c8b..5557182b`, one commit, on the context-limit doc only. It folds in M1, M2 and L1 to L5.

Verdict: **hold, on one line.** The content closes every note. The new section 1 line names a home-directory path on
m1mini in this public repo.

Closed:
- **M1.** Section 3 now says that a mid-turn crossing that grows past the headroom is compacted by design. It also
  says why a larger headroom only moves the line, and has L5 measure the growth.
- **M2.** The margin is re-checked from every automatic `compact_boundary` `preTokens`, logged when it is more than 5k
  off. L0's done-when now has a real-card check: `preTokens` of about 220k (±5k).
- **L1.** The worker flag is `cardLimit + buffer`.
- **L2.** The flag is fixed at launch from `modelWindowK`. The threshold is also bounded by `flag − buffer − headroom`,
  so a different statusline window can only lower it.
  - I checked the arithmetic. 1M at k = 200 gives a flag of 253k and a threshold of 200k. A 200k window gives a flag
    of 200k and a threshold of 147k.
- **L4.** Only the runner row overrides. The ceiling and the bound each take the lower. That matches `cardLimit`
  today.
- **L5.** L2 now follows the runner row's limit, and its done-when tests it.

Hold:
- **H1: a private path.** Section 1 now gives `/Users/claude/blog-sources/rndctx/`. Write it as "off-repo on m1mini, in
  the blog sources (`rndctx/`: `main.py`, `report.py`)", or use a `~`-relative path with no account name.

Atrium-Verdict: hold 94e46c8b..5557182b
Quality: every note is closed with care. The threshold bounded by the launch-time flag is a neat fix. One path to
scrub.

## Re-read: be230af3

Range `94e46c8b..be230af3`: 5557182b plus a two-line fix.

- **H1 closed.** Section 1 now names the scripts by place, with no account path.
- Section 4's `/tmp` path was changed the same way.
- The only path left is the generic `~/.claude/projects`.

Atrium-Verdict: doc-ok 94e46c8b..be230af3
Quality: closed as asked, and the second path was caught without being asked.
