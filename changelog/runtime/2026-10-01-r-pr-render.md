- **Step 8 of the pulls-view recipe exists as a pure package.** `internal/prreview/render` turns the merge step's
  finding JSON, `pr.diff` and the run folder into `findings/NN-<sev>-<file>-L<line>.txt` and `walk.txt` (every line
  `open`). The order is disputes left for clint, then severity band, then `rank`, ties in `pr.diff` file order and
  line. `Suggested fix:` is written only for `proven: code`. The 5.6 checks (rules 5, 8 and 35, 26, 34, 36, 40, 43)
  come back as `Checks`, `Render` returns `Resend` requests for the merge fork once, and a second failure either acts
  (34, 36, 40, 43) or fails the run at `merge` naming the finding (5, 8, 26). No store, no network, no model, and
  nothing calls it yet: the runner is another worker's.
