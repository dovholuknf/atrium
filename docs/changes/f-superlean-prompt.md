Docs: none, a fix that makes a superlean launch do what docs/runtime/superlean-design.md already says

## Test plan

## @LETTER@. A superlean worker gets its prompt

### @LETTER@1. Superlean with no model

1. From a session, call `atrium_launch` with `superlean: true`, a `prompt`, and no `model` or `effort`, onto this room
   and onto another room with `room`.
2. Open the card's terminal.

**Expected:** the worker starts working on the prompt, the card's context climbs above 0, and the command line in the
card details shows `--tools=Read,Edit,Write,Grep,Glob,Bash,ToolSearch` as one argument.

### @LETTER@2. A prompt an option would take is refused

1. Launch claude with a prompt and `args: ["--add-dir", "../shared"]`.

**Expected:** the launch fails with "the opening prompt would never reach" and names `--add-dir`, and no card starts.
The same launch with `args: ["--add-dir=../shared"]` starts and the worker gets its prompt.
