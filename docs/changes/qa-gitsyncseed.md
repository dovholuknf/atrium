## Test plan

## @LETTER@. A failed forge refresh is audited

### @LETTER@1. The forge cannot be reached

1. With a store repository seeded from its forge, move the forge's main by one commit and make the forge unreachable.
2. Take main through the hub's store route.
3. Make the forge reachable again and take main once more, after the route's freshness window.

**Expected:** the first transfer succeeds with the older main, and the hub audit holds one `git-store-forge-refresh`
row that starts `failed:` and names no path under the hub. The second serves the forge's new main, and the audit holds a
`git-store-forge-refresh` row `recovered`.
