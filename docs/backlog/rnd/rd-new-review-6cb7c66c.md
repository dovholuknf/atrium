# Review: operator focus design (6cb7c66c, m1mini, 2026-10-02): HOLD

`docs/rnd/operator-focus.md`, alone in its commit, for doc-ok. 76c4f663 (a pointer line in the held inbox file) is
out of scope and stays held.

**Nothing in it quotes clint.** A grep for his phrasing from the held log finds nothing, and the quoted strings are
UI labels, internal names ("landing op", which is a term and not his words) or @rnd's own example rewrite (3.3).
The numbers check out:
- @review: $41.39 over 61 turns is $0.68, and $30.27 over 39 message turns is $0.78.
- The causes sum to $166.69 of $167.12, which is rounding.
- 418 commits less 164 review commits is 254, so $167 / 254 is $0.66 per work commit.
- 92 says × 1,207 characters is 111k.

The method in 4.1 can be re-run, and the floor caveat (m1mini only) is stated.

## Lever 1, cycling @review after each verdict: fair, with one change

The measurement is right. This card runs at a very large context. It holds every patch pasted to it, and each
message turn re-reads all of it, so a per-message cost near $0.78 is what that context costs. Cycling is the right
lever, and it is mine to pull. Two corrections make the estimate honest:
- **A cycle is not free.** A fresh @review re-reads its handoff, QUEUE.md, the review file and the code under review,
  about 40 to 80k before the first useful turn. Per verdict that eats part of the saving on small reviews. **Cycle
  when idle, or after a batch of verdicts**, not after each one. Lever 2 (batched sends) makes the batch the natural
  unit.
- **What cycling loses is cross-review memory.** Things one review found and the next relied on:
  - the PowerShell quote class, found in one review and swept in the next;
  - the public-repo question;
  - "done is not session over";
  - which tests are known red on macOS.

  Keep them in a short standing file that every cycle reads, `docs/backlog/review/REVIEWER-NOTES.md`, alongside
  HANDOFF. I will start it.

So the lever reads: "@review cycles when idle or after a batch, with a standing notes file", still no build, and the
saving is somewhat under $25, not over it.

## Section 2.3, safe defaults: sound in principle, not enforceable as written (Medium)

The rules are right: internal work only, undoable, nothing published, deleted, deployed, spent or sent outside, and
nothing about people, other organisations, accounts or terms. Each default is information and never authorization,
backed by @review's check and the action's own gate. But "the hub refuses that mark unless all of these hold", and
L4's acceptance ("a `default_ok` row whose answer publishes is refused at filing"), cannot be met. The hub cannot read
a free-text answer and know that it publishes. Make it mechanical:
- the row carries an `effect` from a closed list (`design-choice`, `stage-order`, `setting-default`, `wording`);
- `default_ok` is allowed only with one of those;
- any other effect, or none, cannot default.

The asker can still pick the wrong effect, which is why @review's check and the gate stay. But the hub's refusal
then means something, and L4 has a testable rule.

## Section 2.2 and 2.5, answers and ideas typed into a card's input (Medium)

"Typing a number into any card's input does the same", and `idea:` "at the start of any card's input". A card's
input box is how clint talks to an agent. `3 files are wrong, fix them`, `12 more tests`, or a line starting with
`idea:` meant for the agent would be taken by the hub as an answer to #3 or #12, or as an idea row, and the agent
would never see it. Either:
- **(a)** the list's own box is the only place that parses, or
- **(b)** card input parses only an explicit form (`#12 y`, `/idea …`) that names an open row, shows what it
  understood, and asks before taking the text from the card.

Default: (a), with (b) later if clint wants it.

## Small

- `~/.atrium/records/` will hold quoted words and customer repo names. Make it 0700 and its files 0600, which
  `r-new-sec-room-state-modes` asks of the room directory too.
- 2.6's urgent limit is one per director a day. Say what happens to a second real blocker the same day: it waits for
  the batch, and its stage shows "waiting on #n" on the board.

## Verdict

HOLD on two design points: the typed `effect` for defaults (2.3, L4) and no parsing of card input by default (2.2,
2.5). Fold in lever 1's wording. The rest is ready. A re-read covers sections 2.2, 2.3, 2.5, 4.4 and L4, and then
doc-ok and a cherry-pick of the doc alone.

Quality: a strong, honest doc. It measures before it proposes, keeps clint's words out of a public repo while
still using what they say, and is plain about what L0 asks of each director, @review included.
