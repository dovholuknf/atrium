## Test plan

## @LETTER@. A lean card draws the status line

### @LETTER@1. Bash script status line on Windows

1. On a Windows machine whose `settings.json` `statusLine` command is a bash program and a script path, launch a lean
   card.
2. Read the card's launch args: the `--settings` `statusLine` command is `<bash> -c '<script>'`, the program unquoted
   when its path has no space.

**Expected:** the status line draws under the prompt with its `ctx` figure.

### @LETTER@2. Anything else is untouched

1. Use a `statusLine` command that is another program, or already has flags.

**Expected:** the launch args carry it exactly as written.
