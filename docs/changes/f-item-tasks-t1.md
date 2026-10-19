Docs: none, the design is docs/runtime/item-tasks-design.md and the reference pages follow with stage T2

## Choices in stage T1

- The hub renders no brief. It sends the task and the room writes BRIEF.md, so the words cannot drift from the folder.
- Task files go to the hub drop as `<item>-<task>-<name>` and expire (the drop's longest TTL, 30 days), where the design
  said kept. One file per relay frame, and the relay request frame is now 8 MB each way, so an old hub refuses a bigger one.
- Learnings are `learning` facts on the item, capped at 50 entries or 20 KB. At the cap a `task-learning-capped` fact says
  so once per task, in place of an alert.
- Only the item's owner (its earliest assignment) and the board may add, hold, release, cancel or retry. Orchestrator and
  relay roles are not honoured, because nothing writes a card's role yet.
- `code` tasks and `after` are refused with a sentence. Tasks run one at a time per item, in the order added.
- A launch whose answer was lost, or that failed at the gateway, is `unconfirmed` and never sent again. A new item push for
  a card made after the task began resolves it. An old room (a bare 404) fails the task with "room too old".
- A capture that fails keeps the folder and the task is `capture-failed`. There is no retry or discard for it until T4, and
  `task_retry` says so.
- The room refuses a `task_root` that is a drive root, the home directory, an ancestor of it, or holds atrium's state.
  Without `task_root` the folders go under `$WORKTREE_ROOT/atrium-tasks`, and without either the task stays pending.
- A task worker's `done` moves its item as any item worker's does, but the owner gets no per-task notice. The owner hears
  only of a capture that failed, once.
- A worker that is blocked is an item blocked: the owner unblocks it as before. A worker that ends with no done fails the
  task and keeps the folder.
## Test plan

## @LETTER@. Tasks of an item (stage T1)

### @LETTER@1. Add a task and read its result

1. On an item with a card on a room, call `atrium_item task_add` with a title, a brief and `outputs: ["out.txt"]`.
2. Wait for the task to start. It shows `running` in `atrium_item task_list` and the item's Tasks list on the board.
3. Let the worker write `out.txt` and `RECAP.md` and end with `atrium_done` naming `RECAP.md`, with a `why`.

**Expected:** the task reads `captured` with its line, why and one file. The file is in the hub's drop as
`<item>-<task>-out.txt`. The task folder under `<task_root>/<item>/` is gone, and the room has one `removed` row for it.

### @LETTER@2. A capture that fails keeps the folder

1. Add a task with an output that is larger than 5 MB.
2. Let the worker end.

**Expected:** the task reads `capture-failed` with the reason, the folder is still there, and the owner gets one notice.

### @LETTER@3. A worker that ends with no done

1. Add a task, then kill its runner before it ends.

**Expected:** the task reads `failed`, the folder is kept, and `task_retry` starts attempt 2 and removes attempt 1's folder
after attempt 2 is captured.

### @LETTER@4. No place for folders

1. On a room with no `task_root` setting and no `WORKTREE_ROOT`, add a task.

**Expected:** the task stays pending with a note saying where to set `task_root`, and nothing is started.