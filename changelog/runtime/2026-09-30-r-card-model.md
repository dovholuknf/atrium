- **A live card's model switches with one call.** `POST /v1/tasks/{id}/model` takes `sonnet`, `opus`, `haiku`, `fable` or
  a `claude-...` id, types `/model <id>` into the card's terminal through the same gate an immediate say uses, records
  the model on the card so a resume keeps it, and writes a timeline entry. A closed gate waits and the answer says so.
  The orchestrator gets it as `atrium_model`. Room side, needs a room deploy. (r-card-model)
- **A switch survives the room restart.** Every start path (reopen, park wake, one-runner restart, unshelve, fixture, board relaunch) launches with `--model <task.Model>`, and a test pins the room-restart resume. A `/model <alias>` (sonnet, opus, haiku or fable) typed by hand is recorded on the card too, and a typed full id is not, since a mistyped one would fail the next resume, so the resume no longer puts it back. (r-card-model)
