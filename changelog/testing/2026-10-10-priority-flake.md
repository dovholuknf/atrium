Fixed a flaky TestRunnerPriorityNormalLeavesBothAlone. It ran in parallel with a raised spawn and counted that spawn's
console host as its own, so it read above normal. It now runs serially, so no other spawn overlaps it.
