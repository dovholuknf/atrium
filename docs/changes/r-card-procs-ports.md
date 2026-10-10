## Test plan

## @LETTER@. A card owns the process the room starts for it

### @LETTER@1. Start and see it in the inventory

1. From a card call `atrium_proc_start {argv: ["ping", "-n", "600", "127.0.0.1"], name: "ctl"}` (`sleep 600` on Linux).
2. Open the card's close preview.

**Expected:** the call answers a pid and a log named `<card short id>-ctl.log`. The preview lists a `proc` row that is
"stopped, with every process it started".

### @LETTER@2. Close stops it

1. Close the card.

**Expected:** the process is gone and the proc row is freed.

### @LETTER@3. Refusals

1. Call `atrium_proc_start` with a `cwd` outside the card's worktree, then with a program that does not exist.

**Expected:** both are refused and nothing is added to the inventory.
