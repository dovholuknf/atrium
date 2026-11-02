## Test plan

## @LETTER@. A pasted repo link asks for the worktree name

### @LETTER@1. Enter shows the name step

1. Open the switcher and paste `https://github.com/openziti-test-kitchen/zssh`.
2. Press Enter on the "new worktree" row.

**Expected:** nothing starts. The field reads "worktree name", `claude/` then `work-<yymmdd>-<hhmm>` selected, and
the branch, off and folder lines below follow what you type. The footer reads enter start, shift enter edit first,
esc back.

### @LETTER@2. Enter starts on the typed name

1. Type `fix/thing` and press Enter.

**Expected:** a card comes up on branch `claude/fix/thing`.

### @LETTER@3. Shift-Enter and Esc

1. Paste the link again, press Enter, type `edited`, press shift-Enter.
2. Paste it again, press Enter, then Esc.

**Expected:** shift-Enter opens "start new work" with the repo and `claude/edited` filled in. Esc goes back to the
list with the link still in the field and the switcher open.
