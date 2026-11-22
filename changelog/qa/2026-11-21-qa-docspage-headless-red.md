# docsPage and boardDocs: the hub documents never showed

#419 made the board answer a hub-only `/_hub/*` read with a local 404 until `/_hub/rooms` says the server is a hub. The
headless sections that turn on `mock.boardDocs` never set `mock.hubMode`, so the mock's `/_hub/rooms` stayed a 404 and
`mDocs` never became available. Each of the five sections now sets `mock.hubMode` for its run and restores it.
`docsPage` is also on the `HEADLESS_ONLY` map, where it was missing.
