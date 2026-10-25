Docs: docs/fabric/room-over-zrok.md says the hub starts without zrok and retries

## Test plan

## @LETTER@. The hub starts with the zrok controller unreachable

### @LETTER@1. Dead proxy

1. With a saved `zrok-share`, start `atrium run --transport zrok` with `HTTPS_PROXY` pointing at a dead port.

**Expected:** the hub and its board come up. The log says `not listening for rooms over zrok yet (attempt N)` on a
growing backoff, the board's listeners show zrok down with why, and `zrok-share` is unchanged. Clear the proxy and
restart: the same share is bound.
