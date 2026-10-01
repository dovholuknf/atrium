# u-new-sec-dialog-html: `askUser` renders its body as HTML, and server text with an agent's card title reaches it

Status: held (pause, 2026-10-01). @ui. High. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

internal/api/web/js/browser-dialogs.js:209 sets `#ask-body` with `innerHTML = opts.body`. `tellUser` and
`confirmUser` pass the body straight through. About 125 callers exist, and many pass `e.message`, which `api()`
builds from the server's error text. Proven path: card-menu.js:331,336 show `ResumeBusy.Error()`
(internal/daemon/launch.go:503-511). That embeds the holder card's title, which an agent sets through an MCP launch
(internal/link/control_mcp.go:1296) or an intake source copies from an external ticket
(internal/store/intake.go:133,277). Second path: themes.js:974,980 show `SaveTermTheme`'s error, which quotes the
imported value with `%q` (internal/store/termtheme.go:228), and `%q` leaves `<>` alone. Also check runners.js:923
(`res.error`) and providers.js:138 (`d.error`). Script on the board can approve permissions.

## Wanted

- `askUser` treats `body` as text. An explicit `html` option for the few callers that build markup from escaped
  parts.
- A headless case: a card titled `<img src=x onerror=...>` holding a conversation, a second resume, and the dialog
  shows the text and runs nothing.
