# r-new-sec-auth-guard

Nothing in the three points was already met on bcdbb267. I checked each in the code first.

## Point 1: login off, share still public
- `SaveAuth` (internal/daemon/auth.go) refuses a config that is disabled or not `ready()` while a public zrok share
  runs (`publicShareRunning` in overlay_api.go). It refuses and names the fix (stop the share first) rather than
  stopping the share. A change to another working login is allowed.
- `PUT /v1/auth` (internal/api/auth.go) is `edge.LocalOperator` only, 403 with `edge.ProxyNote`. `GET /v1/auth` is
  unchanged. It never returns secrets but it does return the allowlist, which @ui may want to look at.

## Point 2: no limit, scrypt before anything
- New internal/daemon/auth_limit.go. Per source: 5 free failures, then a wait of 2s doubling to 15 min, answered 429
  with Retry-After BEFORE any scrypt. Success clears it. The table is bounded at 10000 sources.
- At most 2 scrypt checks at once. A third is answered 503 with Retry-After rather than queued.
- Source is RemoteAddr, or for a loopback peer (a share's proxy) the LAST X-Forwarded-For entry. Not verified
  against a live zrok frontend, so this assumes it appends the client address as httputil does.

## Point 3: sessions not revoked
- Cookie body is now expiry, generation, subject, email. A stored generation (setting `auth_session_gen`, so no schema
  migration) is bumped by `SaveAuth` when the password, name, basic flag, provider, client, secret or allowlist
  changes, or the login is turned off. The guard re-checks the generation and the subject (basic user, or allowlist
  match on subject or email) on every request.
- Old-format cookies fail to parse, so everyone signs in once after this lands.
- `/auth/logout` is POST only (GET answers 405). With a password login it leaves a cookie that makes the first
  request carrying the same cached Authorization header get a fresh 401 challenge, so the browser does not sign
  straight back in.
- Board: nothing links to `/auth/logout` today (grepped), so no board change. If @ui adds a sign-out it must POST.

## Tests
internal/daemon/auth_guard_test.go and internal/api/auth_test.go cover each point. Existing auth tests were updated
for the new session type. In internal/daemon only the known failures remain (TestGlobalAutoSurvivesAReopen,
TestAnOlderClaudeIsStartedWithoutTheFlag...). internal/api and internal/edge pass.

## Left
- A caller reaching the guard through a non-loopback proxy is keyed by RemoteAddr.
- The limiter is in memory and resets on restart.
