# Review of u-deploy-ready addbde02 (@ui, the board half of deploy ready)

Range `c5be337f..addbde02`, one commit, from m1mini/claude/u-deploy-ready. Files: `js/deployready.js` (new),
`css/deployready.css` (new), `index.html` (pill, dialog, two includes), `js/settings-spine.js` (load on stream open,
reload on the `deploy-ready` event), the headless section, and the changelog line.

Read against `internal/link/deployready.go` and `internal/deployready/deployready.go` on claude/main.

## Asked: text only

Every server string is set with `textContent` through `drEl`: the subject, `why`, `detail`, each note, `error`,
`line`, the branch, the deploy's `error`. The pill uses `textContent` and `setAttribute`. The one HTML sink is the
confirm body (`askUser` sets `ask-body` with `innerHTML`), and the whole body, `line` included, goes through `esc()`.
The confirm title is `textContent`. Nothing reaches HTML unescaped.

## Asked: no deploy on a stale tip

The tip posted is captured from the report BEFORE the confirm, so a `deploy-ready` event replacing `drReport` while
the confirm is open cannot change what is posted. The post carries the full tip. The hub re-reads with `fresh=true`,
refuses `!Ready` and `rep.Tip != tip` with 409, and checks `running` twice around the spawn. The board's greying is
advisory only, and the hub decides. A stale tip cannot deploy.

## Also checked

- The view shape matches: `deployReadyView` embeds `Report` (top-level `state`, `ready`, `tip`, `blocking`,
  `notes`, `line`) with `deploy` beside it. A 409 carries `report` or `deploy`, and both are merged. 202 carries
  `deploy`.
- The hub broadcasts `deploy-ready` when the report's signature changes, when a deploy starts and when it ends. The
  board reads it again on each, and on a stream reopen. It never polls.
- Guests never fetch. A non-hub answers 404 and the pill stays hidden.
- `scripts/check-board.sh` passes at addbde02 (Playwright is not installed here, so the headless sections are @ui's).

## Findings

1. **Low.** A failed GET (`!r.ok`) hides the pill and nulls `drReport`, but it does not repaint or close an open
   dialog. That dialog keeps the last report and an enabled Deploy button. A click is still safe (the hub
   rechecks, and `deployNow` returns on `!drReport`), but the dialog shows a state the hub no longer reports. Close the
   dialog or paint "no report" on that path.
2. **Nit.** `pill.dataset.tip = drLine(v)` stores the line, not the tip. Drop it, or set `v.tip`.
3. **Nit.** In `settings-spine.js` the new listener sits between the "THE HUB IS ABOUT TO RESTART" comment and the
   `hub-restart` listener it describes. Move it above that comment.

Quality: after the Sonnet switch, no drop. Every server string is text, and the stale-tip refusal was left to the hub
rather than duplicated.

**HUB DEPLOY OK and ROOM DEPLOY OK c5be337f..addbde02.**

## Re-read of 20c8e7c8 (u-deploy-ready-low, off b1f88b4c)

- Low 1 closed. A thrown fetch, a non-404 failure and an unreadable body all go to `drFailed`, which nulls
  `drReport`, says "no report" on the pill, and repaints an open dialog. With no report the dialog draws
  "no report: <why>" and a Deploy button that is disabled and has no click handler. `deployNow` still returns on a null
  report. A 404 still hides the pill, so a daemon that is not a hub is unchanged. The `why` text is the hub's `error`
  field, set through `textContent` and the tooltip's `textContent`.
- Nit 2 was mine and wrong: `data-tip` is the board's tooltip (`js/tooltips.js`), not a git tip. @ui kept it and
  added a comment saying so, which is correct.
- Nit 3 closed. The hub-restart comment sits above its own listener again.
- `scripts/check-board.sh` passes at 20c8e7c8.

No new findings. Quality: after the Sonnet switch, no drop.

**HUB DEPLOY OK and ROOM DEPLOY OK c5be337f..20c8e7c8.**
