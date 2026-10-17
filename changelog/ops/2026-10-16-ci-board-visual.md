# CI: the board's pixel and timing units run in their own group

The board job split in two. `board` runs every unit except those on `scripts/board-visual-units.txt`. The new
`board-visual` group runs the listed units (layout, contrast and timer-bound ones) and its job has
`continue-on-error`, so a runner's fonts or load cannot redden a push. They still run and still report.

The group also leaves `board-visual-env.txt` (Chromium, fonts it really used, fc-match) and the failed shards' own output
in the run's artifacts, so the units that fail only on hosted runners can be read against the machine. `CR_CONTRAST=1`
adds the colour and background to each contrast failure. A name on the list that the suite lacks fails `ci.sh`.
