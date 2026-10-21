## Test plan

## @LETTER@. Codex hook install reports its path

### @LETTER@1. Install for Codex

1. Run `atrium hook install --runner codex` with `CODEX_HOME` set to an empty folder.

**Expected:** the report path is the folder's `hooks.json` with forward slashes, nothing is missing, Stop stays
not wired, and the output mentions trust.
