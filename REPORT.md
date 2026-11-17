# group-by-fixes

Commit: see branch claude/group-by-fixes.

1. "by pile" removed: FD_GROUP_WORDS, the settings.js option, the cardrules GROUP_CHOICES entry, the board.js `window`
   grouper and the `defaultProjectOf` helper it left dead, and the comment. `groupingPrefs` maps a saved mode `window`
   to `project`.
2. Cause: the terminals tab nests by `terminalLabel` (solo.js), which ignored host and org and fell back to the last
   path segments, so `cards/<slug>/work` made a "cards" heading. Now the label uses host/org/repo when the card has
   them, and a card-home path with no repo has no project (uncategorized). The flat project rule in cardrules.js does
   the same.
3. Per card: csharp-ci and csharp-gaps group under github/openziti/ziti-sdk-csharp, mcp-gateway-transport under
   github/openziti/mcp-gateway-transport, interview-nfpreview under uncategorized if it has no origin.
   Caveat: atrium_peers does not expose host, org or repo, so these are assumed from the brief, not read off the live
   records.
4. test-touched passes. The headless units were skipped (playwright not installed), so the assertions I added in
   scripts/test-board-headless.js did not run.
5. Mock: http://127.0.0.1:7931/ is a static page with real board CSS, not the full board.
