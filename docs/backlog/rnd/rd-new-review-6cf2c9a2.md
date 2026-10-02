# Review: factory refactor, the first factory log entry, and the OTel fold (m1mini, 2026-10-02)

Range claude/landing 6d5aec07..claude/rnd 6cf2c9a2:
- **02cc329e, the OTel fold: OK.** Both lows are folded in as written. It overrides `OTEL_*` by explicit options
  and never unsets, a permission ends at its decision or the next turn, and a subagent ends with its parent.
- **2793a0f2, factory log entry 2026-10-01-02: HOLD for clint**, see the High.
- **6cf2c9a2, factory refactor and four held items: HOLD**, for the same High and two corrections.

## High: the factory log publishes private words, in a public repository

`dovholuknf/atrium` is public (GitHub API today: `"visibility": "public"`). Once claude/main reaches GitHub, the
first log entry puts the following on the internet:
- clint's own words, verbatim, profanity included ("these fucking nags are getting old…");
- his purchases ("i have bought opencode go");
- a named third party's private Slack message about another project (geowa4, OpenWiki);
- the state of his credentials ("the PAT cannot fork, m1mini has no gh");
- an sg4 path (`D:\tmp\backlog-deps\BACKLOG-DEPS.md`).

The refactor's section 3 makes this daily and automatic. The digest writes "clint's feedback quoted from his says
and answers", together with every worker's `by_hand` lines, into `docs/rnd/factory-log/<date>.md`, which lands
through the landing op.

On your question 2: yes, `by_hand` stays off the OTel export. Section 3 says so, and the allowlist would drop an
unlisted field anyway. But a public git repository is a wider audience than an operator's collector. `by_hand` is
free text from workers, so it can carry paths, PR content, quoted messages and anything a prompt-injected worker
chooses. The rule that keeps it off OTel has to keep it out of a public repository too.

That is clint's decision, not mine, and it is not reversible once pushed. The options for him:
- **(a) The log lives off the public repo.** It goes in the hub's document store (`docs/rnd/hub-documents-design.md`),
  in a private repo such as dotagents, or in atrium's store once the backlog moves there. The refactor doc, which
  holds only counts and changes, can stay public.
- **(b) The log stays in the repo, redacted.** No verbatim quotes, no third-party names or messages, no credential
  states and no paths. The digest applies the same rule mechanically, the way the OTel allowlist does.
- **(c) Publish as is.** clint says so explicitly.

Default for clint: (a), and the refactor doc stays. I should have raised this on the OpenWiki spike, whose status
line quotes geowa4's messages. That spike is already landed on claude/main, so it goes into the same question for
clint.

## Corrections

- **The Kimi count, in the log and in refactor item 7.** "1 of 27 findings right at its stated severity" is not what
  the review found. It found **0 of 27 at their stated severity**: 6 wrong, 20 overstated, 1 confirmed at a lower
  severity (H15). 6 of the 27 point at a real defect (`docs/backlog/review/review-new-security-audit-kimi.md`,
  Counts). Item 7's "2 steps" estimate stands either way.
- **Item 2.** "`atrium_cull` already does the job safely. It lacks a caller": correct. `atrium_cull` exits, removes
  the worktree and deletes the branch, and refuses an unmerged branch or a dirty tree (daemon/cull.go:17-45). The
  log's fourth step, the card DELETE, is not part of it. Say so, or the count is 3 steps.
- **Item 5, your question 1.** The note is accurate: "the hub already collects every room's `claude/*`, but a room
  gets only `claude/main`". It claims nothing built that is not. Items 1, 4, 6 and 7 map to designs, not built code,
  and the doc says so. No overclaim found.

## clint's four questions

1. **Directors address clint directly: yes, with one carve-out.** A default that is an outward or destructive action
   (deploy, push or open a PR upstream, delete, spend, publish) is never answered by a one-tap "yes" from the phone.
   It shows the action in full and takes an explicit choice. The inbox makes "yes" cheap, and that is the point of
   it. It must not become the gate's auto-approve by other means.
2. **The landing op takes over landing: yes,** with a recorded break-glass path. A hand landing is allowed only when
   the op is down, and it is logged as a `byhand` entry, so it shows in the counts.
3. **The contribution gate before any outside-repo build: yes.**
4. **Build order: fine as ranked.** But item 1, the landing op, must carry the M3 conditions, and item 3, the inbox,
   the carve-out above, or the two highest-ranked items each open a hole.

## Verdict

- 02cc329e: **OK, doc-ok**, and landed alone. It is first in the range.
- 2793a0f2 and 6cf2c9a2: **HOLD** until clint picks (a), (b) or (c), with the Kimi count corrected and the
  cull-step note. If (a), the log entry moves out of the repo and the refactor doc's inputs point to its new home.
  A re-read covers the log's home and section 3's digest.

Quality: the refactor is the useful kind of doc. Each change is ranked by measured hand steps and maps to a design
that exists, and the self-feeding log is a good idea. The miss is where the log is published, which the OTel
review's own rule should have prompted.
