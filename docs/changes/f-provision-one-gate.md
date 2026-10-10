## Test plan

## @LETTER@. A room has one permission gate

### @LETTER@1. Both gates become one

1. In a scratch folder make a `.claude` with a settings.json whose PreToolUse holds both `atrium-perm-hook.ps1` and
   `atrium hook --event permission`, and a `hooks/atrium-perm-hook.ps1` file.
2. Run `pwsh scripts/room-gate.ps1 x -Local <that folder>`. Never point it at the real `~/.claude`.

**Expected:** a `legacy warn two permission gates` line, then `legacy done`. settings.json holds one gate, a
`settings.json.bak-<yyyymmddHHmmss>` sits beside it, and the script file is gone. A second run changes nothing.
`pwsh scripts/test-room-gate.ps1` passes.

### @LETTER@2. The board names it

1. Attach a room whose settings.json holds both gates.

**Expected:** its setup issue reads "two permission gates: atrium-perm-hook.ps1 and atrium hook", not
"atrium claude hooks: 1 missing".
