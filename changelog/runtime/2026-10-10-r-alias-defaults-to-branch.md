A new card with no alias takes its git branch as its `@handle`: `claude/u-foo` gives `@u-foo`. A title prefix alias wins,
a typed alias is never overridden, a taken handle gets `-2`, `-3`, and `main`, `master`, no branch and `atrium` give
none. Applies on launch, join and adopt, and existing cards are not touched (r-alias-defaults-to-branch).
