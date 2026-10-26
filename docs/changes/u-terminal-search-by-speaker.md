## Test plan

## @LETTER@. Terminal find filters by speaker

### @LETTER@1. My text only

1. In a Claude Code card, send a prompt long enough to wrap that contains a word the agent also writes back.
2. Press ctrl-shift-f, type the word, and click the toggle after `.*` until it reads `me`.

**Expected:** the count covers only the hits inside your tinted prompt rows, the wrapped row included.

### @LETTER@2. Agent text and all

1. Click the toggle again so it reads `agent`, then again so it reads `all`.

**Expected:** `agent` finds only the hits outside your prompts, and `all` finds every hit as before.
