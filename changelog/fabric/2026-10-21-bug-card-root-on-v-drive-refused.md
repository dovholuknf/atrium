A card root on a drive the room's account cannot list, such as a per-logon `V:` mapping, no longer refuses every repo
launch as "not inside the card root". The containment check now treats an access denied on an ancestor the way it
treats a missing one: it resolves the nearest ancestor it can and re-joins the rest. A path outside the root is still
refused. bug-card-root-on-v-drive-refused. Room restart.
