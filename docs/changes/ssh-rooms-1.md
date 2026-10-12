## Test plan

## @LETTER@. ssh targets are listed and edited

### @LETTER@1. Add, list and remove

1. Run `atrium ssh add wsl 127.0.0.1:22`.
2. Run `atrium ssh add win11 10.0.0.9:22 --from '*' --user clint`.
3. Run `atrium ssh ls`.
4. Run `atrium ssh rm wsl`, then `atrium ssh ls`.

**Expected:** step 1 prints `wsl is offered to no room yet. add --from <room> or --from '*'`. The list shows both
targets, `wsl` with `(nobody)`. After the remove only `win11` is left. `ssh_targets.json` sits beside `room.json`.

### @LETTER@2. A bad name or address is refused

1. Run `atrium ssh add WSL 127.0.0.1:22`, then `atrium ssh add wsl host:0`.

**Expected:** each fails with a sentence naming the field, and the file is unchanged.
