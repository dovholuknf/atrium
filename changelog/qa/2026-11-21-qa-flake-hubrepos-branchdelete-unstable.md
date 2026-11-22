# hubReposBranchDelete: "element is not stable"

The Branches view repainted `#hubrepos-list` with `innerHTML` on every poll, request load and refresh, even when the
markup was identical. A repaint landing while Playwright checks a delete button for stability replaces the button, so
the click retries until its 5s timeout. `hubReposPaint` now returns early when the markup is unchanged.
