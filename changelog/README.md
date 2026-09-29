# Changelog entries

Each change writes its own file, `changelog/<dept>/<yyyy-mm-dd>-<item>.md`. `dept` is one of ui, terminal, runtime,
fabric, review, rnd or merge. Two branches never write the same file, so entries never conflict.

A file holds 1 to 5 lines: what changed for a user, and the item id. The root `CHANGELOG.md` is frozen and is not
edited, except by the merger.
