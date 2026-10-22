## Test plan

## @LETTER@. The ubuntu CI image

### @LETTER@1. Build the image locally

1. Run `bash scripts/ci-image.sh` with docker available.

**Expected:** it builds `ghcr.io/dovholuknf/atrium-ci:1` and prints the Go and playwright versions it used.

### @LETTER@2. A board group skips the browser install

1. Run `bash scripts/ci.sh --group flaky-board` in a container from that image.

**Expected:** the log says `ok: playwright browser already installed` and the group passes.

### @LETTER@3. A mismatched browser still installs

1. Run the same group on a machine whose browser does not match the playwright in `node_modules`.

**Expected:** ci.sh runs `playwright install` as before.
