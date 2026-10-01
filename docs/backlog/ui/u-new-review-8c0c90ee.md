# Review of b8c4e552 + 8c0c90ee (@ui u-m-switcher: the /m card switcher)

Reviewed by @review, 2026-10-01, from `git diff e94f997b 8c0c90ee -- internal/api/web`, read only. Hub side. Board and
headless checks are @ui's, and I ran none.

## What holds

- **Everything drawn is escaped.** Chip labels, names, statuses and `data-id` all go through `U.esc`. The search is a
  substring match over names and statuses, on the client.
- **The filters.** "running", "needs you" (any reason other than ready, from `mHome.reasons`, which is exported),
  "ready", "done" (done, dead or shelved) and "all". "All" excludes the others, and an empty choice falls back to the
  default (running and needs you). The choice is kept per device.
- **The overlay.** A solid surface, a 1 px edge and a shadow, over a dimmed backdrop that closes it on a tap, with
  `overscroll-behavior: contain` so scrolling the list does not scroll the thread. Text follows `--m-fs`.

## Findings

### Nit

1. A stored choice that holds only unknown keys (for example a chip renamed later) loads as an empty set, not as the
   default. Every row then fails `pickMatches` and the list says "no cards here". Apply the empty-falls-back rule
   after filtering, too.

Quality: after the Sonnet switch. Small and careful. No drop seen.

HUB DEPLOY OK b8c4e552~1..8c0c90ee

## Re-read of 00bc2e83 (@ui, nit 1)

`git diff 8c0c90ee 00bc2e83 -- internal/api/web`, read only. `pickSet` filters unknown keys first, then falls back to
running and needs you when none are left. Closed. No findings.

HUB DEPLOY OK 00bc2e83
