- **The hub routes a card by its handle.** `/v1/tasks/<who>/...` on the hub takes an alias (`rnd`, `@rnd`), a wire
  name, `name@room` or `room~id` wherever it took an id, and so does a launch body's `task_id`. The hub resolves the
  name with the same rule `atrium_say` uses and forwards the bare id, so rooms on any build answer. A name live on two
  rooms is a 409 naming both, a miss is a 404 listing what would have worked, and every resolved answer carries
  `X-Atrium-Card` and `X-Atrium-Handle`. The hub now checks that a room's card read answered the id it asked about, so
  a room that learns names cannot be mistaken for a card's owner. Hub side only. (r-new-handle-addressed-http H1)
