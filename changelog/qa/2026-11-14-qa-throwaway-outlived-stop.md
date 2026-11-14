# 2026-11-14 qa-throwaway-outlived-stop

TestAStoppedThrowawayLeavesNoDirectory polled only for the directory to vanish, then read the card once.
`discardThrowaway` removes the directory first and deletes the card after, so a slow Windows run could see the
directory gone while the card row was still there ("the card outlived the stop").

The test now waits for the directory and the card together. Product code is unchanged, its order is intended.
