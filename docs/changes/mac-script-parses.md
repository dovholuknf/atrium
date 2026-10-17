## Change note

`TestBoardScriptParses` spent its time starting node: one process per script in `index.html` and one for the joined
file, about 26 starts. Process start dominates parsing, and macOS CI ran it over the 13s ceiling.

The test now runs a single `node -e` that reads every script, parses each with `vm.Script` (which compiles without
running, like `--check`) and then parses the concatenation. A failure still names the file, or `JOINED` for the
concatenation, with the line from the parser. The test takes about 0.07s, so the work is minimal and the
`internal/api.TestBoardScriptParses` line in `scripts/test-time-allow.txt` is deleted rather than raised.
