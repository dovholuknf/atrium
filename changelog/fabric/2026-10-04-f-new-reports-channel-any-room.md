The hub now holds the backlog and the director reports, so a director on any room reads and writes the same rows
(migration 0011_backlog_reports). `atrium_backlog` lists, gets, files and changes the status of items, and
`atrium_reports` adds, lists and marks reports read. Both are full class tools that go through the hub's
`/_hub/backlog` and `/_hub/reports` routes. Reads are open to the board and writes come only from the machine the hub
runs on, as the deps routes do. An item id is the one the filer gives and is never reused, and a report is
append-only. The interim rule for files is in `docs/rnd/reports-channel-design.md`. Item f-new-reports-channel-any-room.
