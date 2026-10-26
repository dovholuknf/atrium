## Test plan

## @LETTER@. A group matches several tags

### @LETTER@1. Add aliases

1. Group by custom groups and add a group `prs`.
2. Right click the `prs` heading, pick "also matches tags…" and type `atrium:pr, pull-request`.
3. Tag one card `atrium:pr` and another `pull-request`.

**Expected:** both cards are drawn under `prs`, not under untagged.

### @LETTER@2. Rename and remove

1. Rename `prs` to `reviews`.

**Expected:** the two cards move to `reviews` with it. Removing `reviews` from the view returns them to untagged.
