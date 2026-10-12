## Test plan

## @LETTER@. The hub file drop

### @LETTER@1. A file crosses from one room to another

1. On a hub with two rooms, ask a card on the first room to `atrium_put` a small file from its worktree.
2. On a card of the second room, run `atrium_files` and then `atrium_get` with the name it shows.

**Expected:** the file lands in the second card's `.atrium/incoming` folder with the same bytes. Putting it again with
the same name makes a new version and the older one is not listed.

### @LETTER@2. A room's data is collected without ssh

1. Run `atrium_export` with `what` usage and one room that is offline named in `rooms`.

**Expected:** the online rooms each drop a file into the hub. The offline room is named in `skipped` and the call does
not fail.

### @LETTER@3. The Files tab

1. Open the board served by the hub and click the `files` tab. On the phone open `/m/` and tap `files`.
2. Upload a file with a note. Click its name. Delete it, first choosing `keep it`, then `delete` and `yes`.

**Expected:** the row shows name, size, who sent it, when and how long is left, and a file with under a day left is
marked. The name downloads the file as an attachment and never previews it. `keep it` deletes nothing. The usage bar
moves after the upload and the delete. On a room's own board, with no hub, the tab and the button are not there.

### @LETTER@4. Caps and expiry

1. As the operator, `PUT /_hub/files/settings` with a small `file` cap, then upload a file over it.
2. Leave a file past its lifetime.

**Expected:** the upload is refused with 413 and the settings change is audited. Past its lifetime the file answers
404 or 410 and the hourly sweep removes its bytes.
