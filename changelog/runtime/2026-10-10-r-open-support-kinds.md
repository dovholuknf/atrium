A Zendesk or Discourse link opened with `atrium open` now always makes a card titled `<kind>-<N>`, on a `<kind>-<N>`
branch, tagged `<kind>,support,<kind>-<N>`. A recogniser row whose fetch filled those fields with ticket text no longer
reaches the card, the branch or the worktree path. Item r-open-support-kinds, design section 11.
