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
| `rnd/` | @rnd | none yet |

Old numeric items (`1` to `96`, and `12a`) keep their numbers and sit in the folder of the area that owns the code
each is about. They came from `docs/backlog-2.md`, which is now a one-line pointer here.

## Adding an item

1. Take the next free number for your prefix: `ls docs/backlog/ui/u-*.md`.
2. Write `docs/backlog/<dept>/<id>.md`. The first line is `# <id>. <title> (<kind>)`, where kind is bug, feature,
   design, chore or housekeeping. The first paragraph starts `Status:`, which says where it stands and who owns it.
   The body is whatever the item needs.
3. Change `Status:` as the item moves. Nothing else has to be kept in step, because there is no shared table.

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
