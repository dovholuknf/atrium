- **The sharded board suite runs again.** `scripts/test-board-sharded.js` had refused to start because `mockVars()` lacked
  `cardColorsMock`, `blockerEsc`, `idleParkNow` and `idleParkWrites`, and eleven sections had no unit. All are added.
  A listing that finds a missing mock variable now fails too, and says to add it to `mockVars()` and `mockSet()`.
  The three sections that need a capture or an output directory (repliesShots, switchBackCost, keepMemory) pass at
  once when it is not given. (headless-sharded)
