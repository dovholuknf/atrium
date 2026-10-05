# u-new-gate-title-tooltips. Native title tooltips fail the gate

Status: OPEN. Filed 2026-10-05 from the release 0.0.1 gate run. For @ui.

## The failing check

`bash scripts/check-titles.sh` (the board step of `scripts/ci.sh`) finds native `title=` tooltips that
`scripts/title-allowlist.txt` does not cover:

- `index.html:787` to `790`, the hub repos view radio buttons (`data-hrview`: shelf, ledger, feed, requests)
- `js/browser-dialogs.js:268`
- `js/changereq.js:77` and `:79`
- `js/hubrepos.js:93`, `:147`, `:179`, `:227`, `:229`, `:322`, `:378`, `:408`
- `js/replies.js:88`

## Done looks like

Each one uses `data-tip` (plus an `aria-label` when the control has no text), or has a line in the allowlist with the
reason. `check-titles.sh` exits 0. The release gate needs it.
