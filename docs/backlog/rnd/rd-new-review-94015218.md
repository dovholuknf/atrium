# Review: blog expedition 94015218 (docs/blog/, 49 files)

@review on m1mini read this design and docs commit, which stands alone on claude/rnd and adds only docs/blog/. It
lands by cherry-pick. The commit is unsigned, like every m1mini commit.

## (1) The public repo: HOLD on M1

- **Quotes and names: clean.**
  - Grepped every file for clint with said, says, wrote or asked, and for curly or straight quotes. Every mention
    is a paraphrase, and the "far more than the few files" line in inventory 428 paraphrases the in-repo
    decisions-log.
  - No outside person is named. The OpenWiki stub says "a colleague", and the inventory's Slack mention is
    OpenWiki's OAuth tunnel, not a message.
  - No private paths.
  - The factory status, evaluations and log are cited as off-repo, paraphrase only.
- **M1: working-notes-in-a-public-repo.md signposts what is still public.**
  - Beat 2, "Out of the tree, still in history", tells a reader the leaked files are still in the public history.
    They are: e77b447d and 8b9e6761 (HANDOFF.md) and b3631dac (PLAN.md).
  - Those files carry private Windows paths, scratchpad paths with session ids, room ssh URLs and card ids.
  - A post built on this beat invites a reader to go and dig them out. Whether to rewrite history, or to leave it
    and say nothing, is clint's call. Until he makes it:
    - drop beat 2, or reword it to "removed from the tree";
    - add a writer note: "do not name the commits or say what the files held";
    - put the question in INDEX's held questions.
  - **Its source is wrong.** docs/backlog/release/76.md is the worktree helper that links every CLAUDE.md, not the
    working-notes guard. Cite the gitignore and merge-check change instead.

## (2) Security lane: OK

- No audit findings anywhere.
- a-grade-of-d-plus grades the model only and says so ("the audit's findings are not the story").
- inventory 395 names the follow-up items with "details deliberately omitted".
- **The Enter-key post can stay.** It's fixed (704b2e9a, 2026-09-16), it's in CHANGELOG, and it's told as an
  input-automation lesson. The stub names no unfixed relative.

## (3) Spot check of the claims: OK

- **Shas:** checked 704b2e9a (the dialog guard), 367a38da (personas reverted on 09-23), f00f44c2 (f-002 superseded),
  db2cf2c2 (history, 09-03), d32a34fd, 1460c81a and 1c30e5f7 (hub forge rev 1), and 018c141e (rev 2). All match.
- **Figures:**
  - $1,202.50 over 09-29 00:00 to 09-30 17:15, which is 41.25 h (factory-shape.md:46).
  - 2.3 times the throughput at about the same cost per commit (factory-shape.md:98).
  - $0.78 for @review and $0.07 for @ui, about 11 times (operator-focus.md:43).
- **Not checkable from the repo,** because they come from off-repo sources:
  - "thirty-one gaps" (the OpenWiki stub's sources don't hold that count);
  - "02:40", "about thirty items, ten thousand lines" and "more than forty open questions".

  That's fine for stubs. Mark each "figure from the off-repo evaluation", so the writer checks it against the
  source before drafting.

## (4) Held sources: OK

- factory-refactor.md is marked "(held on claude/rnd)" at every citation, five in all.
- pr-review-story isn't cited.
- The off-repo notes file behind factory-shape's figures isn't cited by the blog directly.

Closed: none (first read) / Open: M1, plus the source marks in (3)

Verdict: HOLD 94015218, on the one stub and its citation. The re-read is that stub plus INDEX's held questions.

Quality: a wide, well-sourced plan. The series grouping and the five starters are good choices, and the public-repo
care is visible in every stub.
