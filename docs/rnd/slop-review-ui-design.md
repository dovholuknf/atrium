# Atrium review: slop review on the board

Spike, 2026-10-21. Status: designed, not built.

clint, 2026-10-21 12:02: his `/slop-review` skill as a board UI in atrium, "probably similar to how the chat view
works, maybe".

Mock: `docs/rnd/slop-review-ui/mock.png`, from `mock.html` beside it.

## Verdict

**Host it in the card, beside chat and terminal, as a third view called review. The chat view is the right place and
the wrong shape.**

- **The right place.** The review is a conversation with one session about one worktree, and the human's answers are
  messages to that session. Chat already sits on the card, already sends to it, and already knows when its turn ends.
- **The wrong shape.** Chat is a scrolling thread of bubbles. The skill's own rules exist to fight that shape. One
  finding per message (R5, R11, R37). A 35-line screen (R38). The question pinned next to the prompt because the
  description scrolls away (R18). Footer last (R19, R20, R39). Full diff with red and green (R18, R29). In a thread,
  finding 7 pushes finding 6 up and out of sight, a fenced `diff` block is drawn as plain code (`m/js/md.js` has no
  diff colouring), and "yes" in the composer is free text the session has to interpret.
- **The skill already asked for this.** Its own "What tooling would need", item 5, is a review pane: one finding at a
  time, sized to the diff, red and green, the decision prompt beside it, code references that open at the line.
- **Built into atrium, as a tool.** Not a skill, and `/slop-review` stays as it is. The model makes the judgment
  calls and atrium measures, draws and applies, so the model never prints a diff (see "The review lives in atrium").

## What the skill does, and what the human does at each step

| Phase | The session does | The human does |
| --- | --- | --- |
| 1. Base | Finds the merge-base with the base branch and diffs against it. | Nothing, or names another base. |
| 2. Find | Walks the whole diff and tags each candidate with a category (SCOPE, DEAD, DRY, CHURN, HYGIENE, TEST SCOPE, COMMENT, SIMPLIFY). | Nothing. |
| 3. Triage | Applies the no-brainers and lists them as one batch. Orders the rest: code before comments, a helper before its callers. | Glances at the batch and vetoes any. |
| 4. Present | Shows ONE change site: the full diff, one or two lines of trade-off, then the two-line footer (`CHANGE <CAT>` and `SUMMARY`, or `SUMMARY <CAT>` and `QUESTION`). Pages a large one with "Next?". Explains the mechanism first for a large or unfamiliar change. Then stops. | Reads one finding. |
| 5. Decide | Applies on yes, to the last thing shown only. Iterates when the yes came with a criticism. Records withdrawn findings with the reason. | Answers yes, no, "why?", "next", a conditional yes, or a sentence. |
| 6. Verify | Builds through the repo's script and runs the full suite and matrix. | Reads the result. |
| 7. Hand back | Never commits. Drafts commit lines and maintainer replies. | Commits and pushes. |

The optional lead and worker split (R40) has a worker draft each finding into `build.claude/slop/queue/NN-<slug>/`
and apply it on "apply NN". In atrium the room holds that queue instead, through `atrium_review`.

## What the human sees and clicks

One view, `review`, beside `chat` and `terminal` on the desktop chat tab's header (the mock's header). It appears on a
card only while the room holds a review for that card (below), so no card grows a dead tab.

- **The left rail** lists every finding by category chip and a short title, in the session's order, with its state:
  applied, kept, withdrawn, vetoed, waiting, queued. Two groups: "Applied for veto" (phase 3) and "One at a time"
  (phase 4). Clicking a finished one shows it read-only. Only the waiting one can be answered.
- **The site line** is the file and line, then the kind (code, comment-only, test) and "net new" or "existing vs
  base" (R12, R22), then the page ("page 2 of 3").
- **The diff** fills the middle: the full unified diff, every line, red and green, scrolling inside its own pane. The
  footer never scrolls away, which removes the reason for R38's 35-line limit. A second view against the base, when
  the session gives one (R35), is a toggle above the diff and not a second block.
- **The note**, one or two lines (R30), sits under the diff.
- **The footer** is the session's own two lines, verbatim, monospace, pinned at the bottom.
- **The buttons** under it: Yes (apply), No, Why?, and Next on a page that is not the last. A text box beside them
  takes anything else ("seems fine but scan first", "fix the comment then apply"). Keys: y, n, ?, and enter in the
  box.
- **The batch screen** (phase 3) is the same rail with every auto-applied item ticked and one "Keep these" button.
  Unticking one and pressing it sends the veto.
- **Code references** (`library/connect.c:982`) open atrium's file viewer at that line in one click (R1). In PR review
  mode they open the forge's diff line instead (R2).

Every click becomes one message to the session, typed the way a human would answer: `07 yes`, `07 no`, `07 why?`,
`07 next`, `batch veto 02`, or `07 <text>`. The finding's number travels with the answer, so "yes" can never approve
the wrong thing (R27), and the transcript in chat reads the same as if the human had typed it.


## The review lives in atrium, as tools, not as a skill

clint, 2026-10-21 14:53 and 14:54: it has to be in atrium, `/slop-review` is left alone, and "it might even just be
a tool to use, not even a skill, to avoid token burn". So atrium carries its own review: a launch `kind` that sets
up the card, and an `atrium_review` tool that does every mechanical step.

### Where the tokens go in `/slop-review` today

- **The rules.** About 15 KB of SKILL.md, read once per session. Most of it is presentation: one site per message,
  35 lines a screen, page with "Next?", no `...` elisions, footer last, footer shape. The board enforces all of that
  by how it draws, so none of it needs to be in the model's prompt.
- **The diff, twice.** The model prints the full diff into its reply for the human, then spends an Edit call with the
  same text to apply it. A large finding is paged across several replies.
- **Measuring.** The merge-base, the file list, and the line count against the base (R10, R14) are shell calls and
  their output, every time.
- **Re-showing.** After a "yes" the model shows the diff again as done (R34).

### What moves into atrium

`atrium_review` is one control tool with a few actions, served to a card tagged `atrium:review` only:

| Action | The model sends | Atrium does |
| --- | --- | --- |
| `start {base}` | the base branch | finds the merge-base, lists the changed files with their line counts, records the review on the card, and answers with that list |
| `finding {cat, kind, site, title, edits, note, footer}` | the category, the site, the edits as `{path, old, new}` (the same shape as an Edit), one or two lines of note, and the two footer lines | applies the edits to a scratch copy, computes the full diff with `git diff --no-index`, the diff against the base, and the line count against the base, stores the finding, and shows it on the board. Answers with the finding's number and the measured count, nothing else |
| `batch {items}` | the no-brainers, each with its edits | applies them, stores them as applied for veto, shows the batch screen |
| `wait` | nothing | blocks until the human answers the waiting finding on the board, then returns the answer: `yes`, `no`, `why`, `next`, a veto list, or the human's text |
| `apply {id}` | the finding's number | applies the stored edits to the worktree and marks the finding applied. The model writes no Edit |
| `revise {id, edits, note, footer}` | a new version after a criticized yes (R31) | replaces the stored finding and shows it again |
| `close {id, state, reason}` | withdrawn, kept or vetoed, and why (S2) | marks it |

So the model's reply to the human is at most the two footer lines. The diff is computed by atrium and drawn by the
board, never printed by the model. An approved change is applied by atrium from the edits it already holds. Line
counts are measured, so R10 and R14 hold by construction.

`wait` returns the answer as the tool's result, so a click does not need a say, and the card is never prompted by
typing into its terminal. A say typed into the terminal (`07 why?` from chat) still works and is read the same way, so
the review also runs without the board, just more expensively.

### The launch

- **`kind: review`** on `atrium_launch`, beside `kind: interview` (`internal/link/interview.go`). Atrium writes its
  review rules at the top of BRIEF.md and tags the card `atrium:review`. The rules are a Go constant in the binary.
  They ship and version with the tool and the board that draw their output, and no room needs a skill installed.
- **The rules shrink to judgment.** The categories and how to find them (phase 2), triage (phase 3), the decision
  rules (phase 5), and "use `atrium_review` for every finding". Everything in phase 4 about how a message looks goes,
  because the board draws it. Expect about a third of SKILL.md.
- **A "Review" button** on a card whose directory is a git working tree on a branch launches a review card on that
  directory, in the same room. `atrium launch --kind review --base main` does the same from a shell.
- **A review card blocked in `wait` is not nudged**, as an interview stopped on a question is not.
- `/slop-review` keeps working as it does, untouched.

## What runs where

- **The runner** is the review card: a claude runner in the worktree under review. It makes the judgment calls.
- **The room** holds the review: the findings, their edits and their state, in a table keyed by card, alongside the
  card's other state. It computes diffs and counts, applies edits inside the card's directory only (through
  `internal/safepath`), and serves `GET /v1/tasks/{id}/review` to the board and `POST /v1/tasks/{id}/review/answer`
  for a click.
- **The hub** proxies both like every room route, and serves `atrium_review` from the control MCP like every other
  control tool, forwarding it to the card's room. No review logic on the hub.

## What atrium already has to reuse

| Need | Already there |
| --- | --- |
| A launch kind with rules in the binary | `kind: interview` (`internal/link/interview.go`) |
| A view on the card beside chat and terminal | the desktop chat tab and its header links (`docs/changes/chat-tab.md`) |
| A tool that blocks until the human answers | the permission gate's wait in `onPermRequest`, the closest model |
| Reading and writing safely inside a card's directory | `internal/safepath` |
| A diff renderer with hunks | the changes sheet (`m/js/changes.js`) and `diffHTML` in `js/stack.js` |
| Markdown for the note | `m/js/md.js` |
| Telling the board something changed | the room's event stream, which chat already follows |

Items and reports do not fit. A finding lives for minutes inside one session, and an item is durable cross-room work.

## What is new

1. `kind: review` and its rules constant.
2. `atrium_review` and the room's review table, diff and apply.
3. The two room routes, and the hub's proxy of them.
4. The review view: rail, diff pane with red and green and the base toggle, pinned footer, buttons and keys.
5. Opening a file at a line in one click (R1). The board has no `file:line` link today, and chat needs it too.
6. PR review mode (R2), opening the forge's diff line. Last, and only once the hub's PR view can take a line.

## Stages

1. `kind: review`, the rules, and `atrium_review` with `start`, `finding`, `batch`, `apply` and `close`. The review
   view read-only. The human answers in chat, and the model reads the say. Already cheaper: no printed diffs, no Edit
   calls, measured counts.
2. `wait`, the answer route, the buttons, keys and text box, and the batch screen.
3. `revise`, paging a large finding, and the base-diff toggle.
4. `file:line` opens the viewer at the line, in chat and in review.
5. PR review mode.

Stage 1 is where clint can judge the shape on a real run, and where the token saving can be measured against a
`/slop-review` run on the same branch.

## Not in scope

- Atrium making the judgment calls. The model finds, ranks and words the findings. Atrium measures, draws, applies
  and carries answers.
- A phone review view. The phone's chat stays as it is, and the diff pane needs a desktop width.
