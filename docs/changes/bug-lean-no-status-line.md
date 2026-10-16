## Test plan

## @LETTER@. A lean card draws the status line

### @LETTER@1. Quoted program path

1. On a machine whose `settings.json` `statusLine` command opens with a quoted program path that has no space, launch a
   lean card.
2. Read the card's launch args: the `--settings` `statusLine` command starts with the program unquoted.

**Expected:** the status line draws under the prompt with its `ctx` figure.

### @LETTER@2. Path with a space

1. Use a `statusLine` command whose program path holds a space, quoted.

**Expected:** the quotes stay and the status line draws.
