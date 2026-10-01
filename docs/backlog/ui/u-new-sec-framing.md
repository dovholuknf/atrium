# u-new-sec-framing: the board can be framed, so a page can trick the operator into one-click approvals

Status: held (pause, 2026-10-01). @ui. Medium. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

No response sets X-Frame-Options or a CSP frame-ancestors (internal/api/web/web.go:146 sets only Cache-Control).
The only CSP is on icons (internal/api/web/icon.go:173). A page can iframe the loopback board. Its fetches are
same-origin, so they pass CrossOriginProtection, and the board has one-click state changes: approve all
(settings.js:334), permission approvals, stop and delete. Shared zrok frontends can be framed from the internet.

## Wanted

- `X-Frame-Options: DENY` and `Content-Security-Policy: frame-ancestors 'none'` on every board, phone and API
  response, on loopback and published listeners alike.
- Later, as its own step: a real CSP once the inline handlers from u-new-sec-attr-js-strings are gone (the audit's
  C5).
