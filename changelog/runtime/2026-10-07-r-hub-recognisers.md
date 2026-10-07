The hub owns the recogniser table, so a pull request pasted on the hub's pulls tab is recognised whichever room it is
placed on (u-pulls-no-recogniser-on-hub). Before, rows lived per room and were loaded by hand, only claude-sg4 had any,
and the hub placed each paste on the least busy room, which answered "no recogniser matches this". The hub now seeds
its table once from `scripts/recognisers/*.json`, built into the binary, and keeps edits and deletes. A room with a
hub matches against the hub's rows over its link (`/_forge/recognisers`), and falls back to its own table only when
the hub cannot be asked. The board lists, saves and removes the hub's rows in every view. `POST /v1/recognise` with
no room is placed on the least busy room and answers which in `X-Atrium-Placed-Room`. Design:
`docs/rnd/card-lifecycle-design.md` section 2, phase 1.

Test plan:
- On the hub board (:7778), with no room picked, paste `https://github.com/openziti/ziti-console/pull/967/changes`
  in the pulls tab. A row appears on a room that had no recogniser rows of its own, and its review starts.
- Settings, recognisers: the list shows the seeded rows. Edit one, restart the hub, and the edit is kept. Delete one,
  restart, and it stays deleted.
- A pattern that does not compile is refused with a 400 and the regular expression's own message.
- Stop the hub and paste a link on a room's own board: the room's own table answers, and its log says so.
