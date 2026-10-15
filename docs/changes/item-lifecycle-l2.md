## Test plan

Needs a room restart (the room now pushes the step and answers `sendback`) and a hub restart. Launch the designer with
`atrium launch --item <id> --step designed ...`, which tags the card `atrium:step:designed`. The designer ends with
`atrium_done` whose note is the commit sha of the design doc. Use a test hub and rooms, never the live ones.

## @LETTER@. The design approval gate

### @LETTER@1. A designer finishing makes approved wait on you

1. Launch a designer for an item with `--step designed`, and have it commit a markdown design doc and finish with that sha.
2. GET /_hub/work/<id> on the hub (the board buttons come with L7).

**Expected:** `GET /_hub/work/<id>` shows `designed`
done and `approved` active with party `human`.

### @LETTER@2. Approve pins the commit

1. `POST /_hub/work/<id>` with `{"do":"approve","sha":"<design sha>"}` from the hub machine.

**Expected:** `approved` is done, and a fact exists at the design sha. A stale sha through the API answers 409.

### @LETTER@3. A later commit to the doc shows stale

1. Commit a change to the design doc on the hub's mirror.
2. `GET /_hub/work/<id>` again.

**Expected:** `approved` shows `stale` and its reason says the design changed since it was approved.

### @LETTER@4. Send back types the note to the designer

1. POST `{"do":"send_back"}` with no note, then with `"note":"cover the halt"`.

**Expected:** the empty note is refused. The note is typed into the designer's terminal as from atrium, `designed` is
active again, and a designer that had exited gets the note queued.

### @LETTER@5. Build directly skips both steps

1. File an item with no design in play and POST `{"do":"build_directly"}` to `/_hub/work/<id>`.

**Expected:** `designed` and `approved` are skipped. The same call on an item with a design answers 409.

Known limits: the step rides the first push of a card only, and the finished note is the sha.
