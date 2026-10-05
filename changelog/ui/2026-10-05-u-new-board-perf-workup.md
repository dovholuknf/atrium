# The board's performance is now measured, with a rerunnable table

`scripts/perf-board.js` starts a throwaway board with 120 cards and measures load, idle CPU and GPU, a burst of card
events, attaching a terminal and memory over time, and prints a table that two runs can be diffed with. The findings
are in `docs/ui/board-perf-findings.md`. The board itself is unchanged: the biggest remaining idle cost is the waiting
dots that pulse forever, and the findings propose stopping them after a few pulses for clint to decide.
