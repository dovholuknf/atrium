CI's flaky suite is now three jobs per platform: flaky-go (no node), flaky-board-1 and flaky-board-2 (no Go), so the gate every other job waits on is shorter (flaky-split).
