The hub now keeps a desired `room.yaml` and the latest observed `room.lock` for each room. Set a spec with
`atrium rooms add <name> --spec <file>` or `atrium rooms spec set`, read it with `spec get` and `lock get`, and a room
fetches its own with `atrium room spec pull`. (f-room-spec-hub)
