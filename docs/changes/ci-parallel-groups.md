Docs: none, CI only

## Test plan

## @LETTER@. CI runs its groups in parallel

### @LETTER@1. The groups cover every step once

1. Run `bash scripts/ci.sh --list-groups` and `bash scripts/ci.sh --list`.

**Expected:** every step except `machine` and `toolchain` is in exactly one group, and `clean-tree` is in all three.

### @LETTER@2. One group runs alone

1. Run `bash scripts/ci.sh --group go-checks`.

**Expected:** only `machine`, `toolchain`, `gofmt`, `go-vet`, `go-race`, `govulncheck` and `clean-tree` run, and the
summary lists those.

### @LETTER@3. The workflow

1. Push to a branch and open the run.

**Expected:** nine jobs, named `check (<os>, <group>)`, start together. The run ends when the slowest ends.
