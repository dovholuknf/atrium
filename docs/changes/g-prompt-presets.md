## Test plan

## @LETTER@. A named prompt opens a launch

### @LETTER@1. From the launch dialog

1. Open the new agent dialog on a room with at least one enabled named prompt.
2. Pick one from the list above the first instruction, then launch.

**Expected:** the box fills with that prompt's text, and the new session opens on it.

### @LETTER@2. From the command line

1. Run `atrium launch --item <id> --prompt @run-tests`.
2. Run it again with `--prompt @nope`.

**Expected:** the first session opens on the run-tests prompt. The second is refused and lists the named prompts.
