# rd-new-reply-suggestions: the quick-reply buttons say what this question's answers are

Status: queued for @rnd design (clint, 2026-09-30 evening), @ui builds after, @runtime if the parse lives in the room.

## Why

On `/m` and in the growler, a waiting card offers fixed buttons: yes, go ahead, no, stop. clint: "is there any way to
embed hints to the mobile for the yes, go ahead, no, stop buttons so the llm, or atrium, could inject better words?
maybe we need some simple decision engine?" The question on screen was "Should I build the three worktrees to catch
compile errors next? Or do you want to review the diffs first?", and neither "yes" nor "no" answers that.

## The idea, two layers

1. **The agent says its options.** `atrium ask` and the `{choices}...{/choices}` block exist. Teach every runner
   (the agent instructions atrium injects) to end a question with its real options, short enough for a button.
   Add `atrium ask --choice "<words>"` (repeatable) if `ask` has no way to carry them today. The buttons show those.
2. **A small parser when the agent gave none.** Read the last assistant message's closing question: a numbered or
   bulleted list after it becomes one button per item, "A or B?" becomes A and B, a plain yes/no question keeps
   the fixed four. Rules, no model. A model is a later option and needs a reason.

## For the design to settle

- Where the parse runs: the room (one answer for every board and the phone) or the board.
- Button text limits on a 390 px phone, and what a long option turns into (the first clause, full text on press).
- What a press sends: the option's words, or the number, and whether the fixed four stay beside them.
- How it meets the growler reply work (`claude/u-growl-reply`, `{choices}` as buttons) so there is one renderer.
