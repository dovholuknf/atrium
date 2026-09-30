# The backlog, one file per item

Every item is its own file, `docs/backlog/<dept>/<id>.md`, so two branches never write the same file and a new item
never conflicts with another. Each director's folder is that director's backlog.

| Folder | Owner | Id prefix |
|---|---|---|
| `ui/` | @ui | `u-` |
| `terminal/` | @terminal | `t-` |
| `runtime/` | @runtime | `r-` |
| `fabric/` | @fabric | `f-` |
| `release/` | @merge | `m-` |
| `review/` | @review | none yet |
| `rnd/` | @rnd | `rd-` |

Old numeric items (`1` to `96`, and `12a`) keep their numbers and sit in the folder of the area that owns the code
each is about. They came from `docs/backlog-2.md`, which is now a one-line pointer here.

## Adding an item

1. **Do not pick a number.** Only @merge mints ids, when the item lands on `claude/main`, the same way test-plan
   letters are handed out at the fold. Directors on different machines work from different bases, and two of them
   picking "the next free number" is how cr48 got an id that two items claimed.
2. Name the new item by a slug: `<prefix>new-<slug>`, for example `u-new-composer-paste`. The slug is lowercase
   letters, digits and hyphens, and it is unique enough that nobody else would pick it.
3. Write `docs/backlog/<dept>/<prefix>new-<slug>.md`. The first line is `# <prefix>new-<slug>. <title> (<kind>)`,
   where kind is bug, feature, design, chore or housekeeping. The first paragraph starts `Status:`, which says
   where it stands and who owns it. The body is whatever the item needs.
4. Until the item lands, everything calls it by its slug: the worker's name, title and alias (`u-new-composer-paste:
   <what>`), its branch and worktree, and its `docs/changes/` and `changelog/` files.
5. When it lands, @merge gives it the next free number from `claude/main`, renames the file, rewrites the slug in
   that branch's files, and tells the director the real id. From then on it is referred to only by the real id.
6. Change `Status:` as the item moves. Nothing else has to be kept in step, because there is no shared table.

A branch that adds a `docs/backlog/` file under an id already on `claude/main` is refused at merge.

An item that moves to another area moves folders with `git mv`. Its id does not change.

## Reading the whole backlog

```
pwsh scripts/backlog-index.ps1              # every item: id, folder, title, status
pwsh scripts/backlog-index.ps1 -Dept ui     # one folder
pwsh scripts/backlog-index.ps1 -Open        # leave out the ones whose status says DONE
```

The index is printed, never written to a file, because a written index is a shared file again.

`HISTORY.md` holds the group introductions `backlog-2.md` had between its items. `docs/backlog.md` is a different,
older list owned by another session, and is not part of this.
