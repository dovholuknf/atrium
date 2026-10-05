# Reply suggestions (rd-new-reply-suggestions)

Status: designed by @rnd 2026-09-30. RS1 and RS4 (@ui) are built on `claude/u-new-reply-suggestions-build`: `js/replies.js`
is the one renderer for both growlers and `/m` compose, and `replies.of` reads `replies` and `fixed` off the card view,
tested against mocked cards. RS2 (`atrium ask --choice`) and RS3 (`closingOptions`, the `options` column, and
`replies` and `fixed` on the card view and the notify payload) are @runtime's and not built. Until RS3 lands a card
carries neither field, so only an ask's own `{choices}` block draws buttons. For R5 the room must send `fixed: false`
and an empty `replies`, or the box is not focused. Backlog item `docs/backlog/rnd/rd-new-reply-suggestions.md`.

## The answer

A card's quick replies come from the first of these that has something:

1. **What the agent offered.** A `{choices}...{/choices}` block in its ask, which already exists and which the
   growler already turns into buttons. `atrium ask --choice "<words>"` (repeatable) writes that block for it, and the
   help text of `atrium ask` and `atrium_report` teaches it. One convention, no new field.
2. **What a rule read off the turn's closing question.** Parsed in the Stop hook, in the runner's own process, next
   to the Open Questions parse that already runs there (`internal/cli/turnquestions.go`). The hook sends the question
   and its options, never the message, which keeps atrium's rule that it does not record what a session said.
3. **The fixed four**, `yes`, `go ahead`, `no`, `stop`, for a yes or no question and for a card that said nothing.

The parse runs in the hook, not the board, so every board and the phone get one answer. It is rules only, no model.
@ui builds one module, `internal/api/web/js/replies.js`, that the board's growler, `/m`'s growler and `/m`'s compose
chips all call. That replaces the two copies of the `{choices}` split that exist today.

## What is there today

- `/m` compose (`m/js/compose.js`) shows `QUICK = ["yes", "go ahead", "no", "stop"]` on any card that is asking.
- The growler, board (`js/growl.js` `growlSplitChoices`) and phone (`m/js/growl.js` `splitChoices`), turns a
  `{choices}` block in the body into buttons, only when the reason is `blocked` or `question`. Two copies of one
  function, landed with `claude/u-growl-reply` (8b316172).
- The Stop hook (`atrium turn --event end`) reads the turn's last message from the Stop payload or the transcript
  tail, keeps only the numbered items of its last `Open Questions:` block, and posts them. The room keeps them on
  the card's seen row (`internal/store/seen.go`). A card with open questions notifies as `question`, with the
  questions as its text (`internal/link/notify.go`).
- A turn that ends on a prose question ("Should I build the three worktrees next? Or do you want to review the diffs
  first?") has no Open Questions block. The hook sends nothing, the card notifies as `input` with no text, and the
  buttons are the fixed four. That is the case clint hit.
- The Stop hook is optional and not in "install all" (`internal/claudeconf/hooks.go`). Where it is not installed,
  layer 2 does nothing and layers 1 and 3 still work.

## 1. The agent says its options

- `atrium ask "<question>" --choice "build the worktrees" --choice "review the diffs first"` appends
  `{choices}\nbuild the worktrees\nreview the diffs first\n{/choices}` to the ask text before posting it. It changes
  only the CLI. The block is the documented convention (`CLAUDE.md`, "Agent-facing formatting affordances").
- `atrium ask --help` and the `ask` field of `atrium_report` get one sentence each: "End with your real options,
  as `--choice` or a `{choices}` block, one per line, a few words each. They become the buttons on the phone."
- Every place that draws ask text calls `replies.split` (section 4) so the markers never show. Today the growler
  strips them and the card row does not. @ui checks every renderer of `ask`.

## 2. The rule-based parse, in the Stop hook

One function beside `openQuestions`, `closingOptions(text) (question string, options []string)`, with table tests.
Nothing runs when layer 1 already has choices, but the hook cannot know that, so the room applies the precedence
(section 3).

**Which question.** The single item of the last Open Questions block when it has exactly one. Otherwise the last
paragraph of the message, when it contains a `?`. A message whose last paragraph asks nothing has no closing
question. More than one open question is left to the Open Questions list as it is today, with no options, because
one button cannot answer two questions.

**Rules, in order. The first that matches wins.**

| rule | shape | options |
| --- | --- | --- |
| R1 list | a question followed by 2 to 6 lines that are numbered, lettered or bulleted (`1.` `1)` `a)` `A.` `-` `*`) | one per line, marker removed |
| R2 split question | two questions in a row, the second starting with `Or` ("Should I X? Or do you want Y?") | X and Y, lead-ins removed |
| R3 inline or | one question with `or` between clauses, or `A, B, or C` | each clause, lead-ins removed |
| R4 yes or no | starts with `should`, `shall`, `can`, `could`, `do`, `does`, `did`, `is`, `are`, `will`, `would`, `want`, `ok`, `okay`, no `or` | none, the fixed four |
| R5 open | `which`, `what`, `how`, `where`, `when`, `who`, `why` | none, and no fixed four either |

**Lead-ins removed** from a clause, case-insensitive: `should I`, `shall I`, `do you want me to`, `do you want to`,
`would you like me to`, `would you like to`, `want me to`, `can I`, `or`, `or should I`, and a trailing `?`. Then the
first letter is lowered. So clint's example gives `build the three worktrees to catch compile errors next` and
`review the diffs first`.

**Refused splits.** `or not`, `or so`, `or two`, `or later`, `or else`, `either way` never make an option. `Should I
proceed or not?` is R4. A clause over 12 words is not an option: that `or` is inside a sentence, not between
answers. When any clause is refused, the whole question falls through to the next rule.

**Bounds.** At most 6 options, each at most 200 bytes, the question at most 500 bytes (`turnQuestionLen`), the same
bounds the room applies again on the way in.

**What leaves the process.** The question and its options, and only when the hook found a closing question. This is
text the operator is meant to answer, which is the line `seen-design.md` already draws for Open Questions.

## 3. The room

- The Stop post gains `closing_question` and `options`. The seen row gains one column, `options` (TEXT, JSON list),
  in a migration added at the END of the slice.
- A closing question with no Open Questions block is stored as a one-item question list. So the card notifies as
  `question` with the question as its text, where today it notifies as `input` with none. This is the change clint
  will see most: the growler and the phone show what was asked.
- Precedence is applied when the card view is built, never stored: the ask's `{choices}` if any, else the stored
  `options`, else nothing. The card view and the notify payload carry one field, `replies: [...]`, and a flag
  `fixed: true` when the board should show the fixed four. Renderers never choose.
- Answering clears both, the same way an answer clears the questions today.

## 4. One renderer (@ui)

`internal/api/web/js/replies.js`, loaded by the board's `index.html` and by `m/index.html` as `/js/replies.js`.
Plain script, no build step, like the rest of the board.

- `replies.split(text)` returns `{text, choices}`. It replaces `growlSplitChoices` and `/m`'s `splitChoices`.
- `replies.of(card)` returns the button list: `card.replies` when present, then `stop`, or the fixed four when
  `card.fixed`, or nothing.
- `replies.buttons(list, {narrow})` returns one element: a wrapping row of buttons with class `rq-btn` inside
  `rq-row`. Each surface styles those classes and puts the row where it wants it. The growler on both surfaces and
  the `/m` compose chips use it, so a choice looks and behaves the same everywhere.

**Labels.** On a phone (`narrow`), a label is at most 28 characters: the first clause (cut at `,`, `:`, ` - `, ` so
`, ` to `, ` because `), then cut at a word boundary with a trailing `...` if still too long. On the board, 60. The full
option is the button's `title`, and a long press on the phone shows it above the row.

**What a press sends.** The option's full words, never a number. A number means something only against the list
the agent wrote, and the agent may have compacted since. The words stand on their own.

**The fixed four beside real options.** No. `yes` and `no` next to "build the worktrees" and "review the diffs
first" answer nothing. `stop` stays, always last, because stopping is an answer to every question.

**R5, an open question.** No buttons. The reply box is focused instead, because the only answer is words.

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| RS1 | `replies.js`, used by both growlers and `/m` compose, reading `{choices}` and the fixed four only | @ui | small | headless: a `{choices}` ask draws its buttons and `stop` on the board growler, the phone growler and the phone compose, with identical labels. A 70 character option is cut to 28 on a 390 px viewport with its full text in `title`. A press posts the full words. No `{choices}` markers are visible anywhere the ask is drawn |
| RS2 | `atrium ask --choice`, and the help sentences | @runtime | small | `--choice a --choice b` posts an ask ending in a two-line `{choices}` block. Zero `--choice` changes nothing |
| RS3 | `closingOptions` in the hook, `options` column, `replies` and `fixed` on the card view and notify | @runtime | medium | table tests for R1 to R5, including clint's example, `Should I proceed or not?` (R4), `either way works, which do you prefer?` (R5), and a 15 word clause refused. Room: a closing question with no block notifies as `question` with its text. An ask's `{choices}` beats stored options |
| RS4 | `replies.of` reads `replies` and `fixed` | @ui | small | headless: mocked cards for each case: options plus `stop`, the fixed four, and R5 with no buttons and the box focused |

RS1 and RS2 need nothing else and fix the explicit case at once. RS3 and RS4 add the parsed case. RS1 should land
before any more growler reply work, so the growler stops carrying its own copy.

## Questions for later

None for clint. Decided here: parse in the hook, words not numbers, `stop` always kept, the other three dropped
beside real options, no buttons for an open question, and no model.
