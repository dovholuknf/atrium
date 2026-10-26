Docs: none, the reference page lists atrium_backlog without its actions, and the tool and its --help name both

## Test plan

## @LETTER@. atrium_backlog edits an item's title or body

### @LETTER@1. Edit the body

1. From a card, file an item with `atrium_backlog file {dept: fabric, title: "t1", body: "b1"}`.
2. Call `atrium_backlog edit {id, body: "b2"}`, then `atrium_backlog get {id}`.

**Expected:** the body is `b2` and the title is still `t1`.

### @LETTER@2. Edit the title

1. Call `atrium_backlog edit {id, title: "t2"}`, then `get`.

**Expected:** the title is `t2`, the body is still `b2`, and the board's backlog shows `t2`.

### @LETTER@3. Refused with nothing to change

1. Call `atrium_backlog edit {id}` with no title and no body.

**Expected:** it is refused with "edit needs id and a title or a body".

### @LETTER@4. Append a paragraph

1. Call `atrium_backlog append {id, text: "one more"}`, then `get`.

**Expected:** the body is `b2`, a blank line, then `<your card> <today>: one more`, and changed_by is your card.

### @LETTER@5. The CLI

1. Run `atrium backlog edit <id> --title t3`, then `atrium backlog append <id> "from the shell"`.
2. Run `atrium backlog show <id>`.

**Expected:** the title is `t3`, the body ends with the appended paragraph, and a `changed ... by ...` line shows
the last change.
