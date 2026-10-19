Docs: docs/runtime/card-home-design.md sections 3 and 10.4

## Test plan

## @LETTER@. Agents launch into the card root

### @LETTER@1. The setting

1. In room settings under Disk, tick "refuse an agent launch outside the card root".
2. Have an agent call `atrium_launch` with a `cwd` outside the card root, then again with `scratch: true`, then with
   `repo` and `branch`.

**Expected:** the first is refused with a sentence naming `repo`, `branch` and `scratch`. The other two start.

### @LETTER@2. Never the human

1. With the setting on, start a card from the board's new-card form with a directory outside the card root.

**Expected:** it starts. With the setting off nothing is refused.
