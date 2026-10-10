## Test plan

## @LETTER@. Secret names with Windows suffixes are refused

### @LETTER@1. Trailing dot, space and stream

1. In a card's worktree make `key.pem` and `.env`.
2. Ask `atrium_publish` for `key.pem.`, `key.pem ` (trailing space), `key.pem::$DATA` and `.env:alt`.

**Expected:** each is refused with `secret-file-name`.

### @LETTER@2. Near misses still publish

1. Publish `notes.md.` and `notes.md:alt` from a card holding `notes.md`.

**Expected:** both are accepted.
