# r-new-sec-auth-guard: the published board's login can be switched off under a live share, and has no limit

Status: held (pause, 2026-10-01). @runtime. Medium-High. Source: docs/backlog/review/review-new-security-audit-kimi.md (the Kimi audit, checked by @review on 2026-10-01).

## Why

1. **Login off, share still public.** `requireLoginForPublic` runs only when a public share starts
   (internal/daemon/overlay_api.go:209,313). `SaveAuth` accepts `enabled:false` with no check for a running share
   (internal/daemon/auth.go:295-311), and `authGuard` reads the setting per request, so the board is then open
   (internal/daemon/auth_flow.go:222). `PUT /v1/auth` is reachable by anyone signed in to that published board,
   since the room API has no `LocalOperator` gate. The hub's control routes have one (internal/link/proxy.go:1359).
2. **No limit, and scrypt before anything.** No rate limit or lockout in `authGuard` (auth_flow.go:251-267), and
   every request with a Basic header runs scrypt N=2^15, r=8 (about 32 MiB) (auth.go:187,200). Parallel
   unauthenticated requests are a memory denial of service on a public share, and guesses run unthrottled.
3. **Sessions are not revoked.** The cookie is a stateless HMAC (auth.go:352-391), and the guard never re-checks the
   subject against the allowlist. A password change or an allowlist removal leaves cookies valid for 12h. Logout is
   a GET, and with Basic the browser signs straight back in (auth_flow.go:252-256,295-300).

## Wanted

- `SaveAuth` refuses to disable or weaken the login while a public share runs, or stops the share first and says
  so. `PUT /v1/auth` is loopback-operator only.
- A per-source failure limit with a delay, and a bound on concurrent scrypt checks.
- The session carries a generation that a password or allowlist change bumps, and the guard re-checks the subject.
- Tests for each.
