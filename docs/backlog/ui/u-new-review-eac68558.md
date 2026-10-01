# Review of eac68558 (@ui u-m-hidden: why a card is missing from /m)

Reviewed by @review, 2026-09-30, from `git diff 98e0cdbc eac68558`, read only. Hub side. Board and headless checks
are @ui's, and I ran none.

## What holds

- **The room chip.** It shows only while `atrium.room` scopes the page. The name goes in through `textContent`.
  "all rooms" calls `mNet.clearRoom`, which removes the key, empties the cards, marks the lists not loaded, and
  reconnects. `connect` closes the old stream first. Cards and permissions are fetched again unscoped.
- **"N hidden by filters".** The count is the listed rows minus the ones the filters keep, and the line's text is
  built from that number alone. Tapping it shows the hidden rows until any filter changes (`setOpt` resets
  `revealed`). The "no sessions" state now shows only when there are no cards at all, not when filters hid every
  one.
- **`hideSubs` keeps a running card that has an alias.** That narrows the filter and takes nothing away. Done and
  dead cards are still left to `hideDone`.
- The mock now scopes `/v1/tasks` by `X-Atrium-Room`, so the test sees the bug the real hub showed.

## Findings

### Nit

1. A `/v1/tasks` request for the old room that is still in flight when "all rooms" is tapped can arrive after the
   unscoped one and leave a scoped list in place until the next refresh. A sequence number on the tasks fetch would
   drop the late answer.

HUB DEPLOY OK eac68558
