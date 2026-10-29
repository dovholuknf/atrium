# Changelog entries

Each change writes its own file, `changelog/<dept>/<yyyy-mm-dd>-<item>.md`. `dept` is one of ui, terminal, runtime,
fabric, review, rnd or merge. Two branches never write the same file, so entries never conflict.

A file holds 1 to 5 lines: what changed for a user, and the item id. A branch never edits the root `CHANGELOG.md`. A
weekly roll-up card owns it: it writes one dated section in broad buckets, `git rm`s the fragments it folded and
merges like any other branch.
