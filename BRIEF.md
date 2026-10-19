BUG: opening a pull request by link took 32s and the board showed nothing. A probe found why. You fix all four
parts below in this worktree (branch claude/bug-pr-link-open-slow-no-feedback, based on hub claude/orch 182c7dd04).
Follow AGENTS.md (comment rules, no em-dashes, `make build` to build.claude/).

What the probe found:
- The PR-head fetch took 22.8s. Git finished the transfer in ~6s, then the process stayed alive ~17s more, most
  likely `git gc --auto` running on a clone with 24k loose objects (gc can never shrink them, so it runs every fetch).
  `fetchPRHead` waits with `cmd.CombinedOutput()` at internal/api/prworktree.go:356.
- The Pull requests popover paste (internal/api/web/js/pulls.js `pullsPaste` ~line 483, post at ~492) calls
  `/v1/open` with `{url, why}` and NO `progress` id. It draws no card, no spinner, and the popover stays open until the
  call returns. The board-paste path (quickpaste.js:179 `quickPasteGo`, fixtures.js ~1858-1870) does it right: it
  calls `linkOpeningBegin` (js/link-opening.js:20), sends `progress: id`, and closes its dialog first.
- On the card path a slow step shows a static line ("fetching the head of pull request 972…") with no elapsed time.

The fix, all four:
1. In `fetchArgs` (internal/api/prworktree.go ~333 and the hub branch ~335) add `-c gc.auto=0 -c maintenance.auto=false`
   to the git args, so a PR-head fetch never collects garbage in the operator's clone. Add a Go test that pins the
   args contain both.
2. Set `cmd.WaitDelay = 5 * time.Second` before `CombinedOutput()` at prworktree.go ~356, so a child holding the pipe
   cannot hold the open.
3. pulls.js `pullsPaste`: close the popover BEFORE the call (move the `pullsPopToggle(false)` up), make an id with the
   same helper quickpaste uses, `linkOpeningBegin(id, url)`, send `progress: id` in the body, `linkOpeningEnd(id)` on
   success, `linkOpeningFail(id, msg)` on error instead of only `pullsSay`. Reuse the existing link-opening functions,
   do not write new ones.
4. link-opening.js: record when the current step began (in `linkOpeningStep`) and show elapsed seconds after the step
   text once it passes 3s ("fetching the head of pull request 972… 14s"), repainted by a 1s interval that runs only
   while an opening is in flight and stops when none are.

Tests: headless board units for both (the popover paste draws a card and sends a progress id, the step line shows
elapsed seconds). See the `pulls` unit and the link-opening tests for the pattern; mock `/v1/open` to answer late.
Save before and after PNGs of (a) the popover paste in flight and (b) a card on a slow step under
docs/backlog/ui/bug-pr-link-open-slow-no-feedback/ and commit them.

Run `bash scripts/test-touched.sh --base hub/claude/orch` and get it green. A failure that also fails on hub/claude/orch
with your change stashed is pre-existing: name it in your done, do not fix it.

Write `docs/changes/bug-pr-link-open-slow-no-feedback.md` (format in docs/changes/README.md, with a test plan section)
and `changelog/ui/2026-10-19-bug-pr-link-open-slow-no-feedback.md`. Commit with a one-line subject, no body, no trailer.
Push: `git push hub HEAD:claude/bug-pr-link-open-slow-no-feedback`.

End with atrium_done: your SHA, the test-touched result, and the PNG paths. atrium_blocked if anything stops you.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
