# Atrium Security Audit Report

**Repository:** `D:\git\github\dovholuknf\atrium`  
**Date:** 2026-10-01  
**Scope:** Full-stack security review of the Go backend, OpenZiti/mTLS overlay, zrok integration,
SQLite storage layer, web UI/frontend, build pipeline, and dependencies.  
**Methodology:** Static code review by focused security reviewers across six surfaces (API/auth,
network/overlay, secrets/storage, frontend, dependencies/CI). No dynamic exploitation was attempted.

---

## Executive Summary

Atrium is a Go backend that orchestrates cards, rooms, runners, and terminals. It can expose its
board over loopback, OpenZiti, or zrok shares, and it launches subprocesses on the operator's
machine. The code shows deliberate security thinking in several places: join secrets are hashed,
basic-auth passwords use scrypt, OIDC uses PKCE, path traversal is mitigated by `internal/safepath`,
and the board can be gated behind OIDC or basic auth when published.

The dominant risks are **network exposure by default** and **plaintext persistence of secrets and
keys**. On a default launch the daemon binds the agent API and the human board to every network
interface with no authentication beyond browser origin checks. At the same time, the SQLite
databases, backups, CA private key, ziti identity files, and several credentials stored inside the
database are all unencrypted on disk. Because Atrium is explicitly designed to be exposed over zrok
and to hold launch environment variables, an unencrypted local store becomes a remote attack surface.

This report is written as a living document. Add new findings, mark items fixed, and update severity
as the codebase evolves.

---

## Risk Summary

| Severity | Count | Top themes |
|----------|-------|------------|
| Critical | 10 | Default wide binding, unauthenticated agent API, unencrypted database, plaintext share password, missing CSP, `new Function`, `innerHTML` XSS, WebSocket origin skip |
| High | 17 | OIDC client secret / cookie key plaintext, launch env plaintext, CA / ziti key plaintext, unencrypted backups, no rate limiting, no CSRF, no OIDC nonce, Basic Auth over HTTP, settings as code execution, guest shares, vulnerable dependencies |
| Medium | 18 | Token logging, shutdown token in query string, legacy overlay default, scrypt cost, cookie `SameSite=Lax`, CSP/ framing gaps, `localStorage` trust, dependency update lag |
| Low | 8 | pprof default, test fixtures, PID disclosure, JWKS RSA-only, no key rotation, multipart temp files, vendor hash checks |

---

## Critical Findings

### C1. Daemon binds agent and board to all interfaces by default
- **Severity:** Critical
- **Files:** `internal/cli/cli.go:169-170`, `internal/daemon/daemon.go:1083-1110`
- **Description:** Default addresses are `:7777` (agent) and `:7778` (human/board). An empty host
  makes `net.Listen` bind every interface. The agent listener has no authentication; the human
  listener exposes the board and JSON API.
- **Proof:**
  ```go
  c.Flags().StringVar(&agentAddr, "addr", ":7777", "agent-facing listen address")
  c.Flags().StringVar(&humanAddr, "http", ":7778", "human-facing listen address")
  ```
- **Impact:** Any host that can reach the machine can register sessions, raise permissions, launch
  runners, kill runners, attach to terminals, and read or modify task state.
- **Remediation:** Change defaults to `127.0.0.1:7777` and `127.0.0.1:7778`. Require explicit
  `--addr 0.0.0.0:7777` to bind wide. When wide binding is used, add mandatory agent
  authentication (token, mTLS, or IP allowlist).

### C2. Agent API lacks authentication independent of browser origin checks
- **Severity:** Critical
- **Files:** `internal/daemon/daemon.go:1057-1084`, `internal/edge/edge.go:255-258`
- **Description:** The agent mux is wrapped only with `edge.For(...)`. That wrapper blocks
  cross-origin writes from browsers but does not authenticate callers. A non-browser client on the
  same network can POST to agent routes because it sends no `Sec-Fetch-Site`/`Origin` headers.
- **Impact:** Remote code execution and control of runners, permission gates, and terminal sessions.
- **Remediation:** Add a required agent API token (e.g., `X-Atrium-Agent-Token`) generated at daemon
  start and written to the location file with restricted permissions.

### C3. SQLite databases have no encryption at rest
- **Severity:** Critical
- **Files:** `internal/store/store.go:628-704`, `internal/hubstore/store.go:104-183`
- **Description:** Both `atrium.db` and `hub.db` are opened with `modernc.org/sqlite` using WAL,
  foreign keys, and normal synchronous pragmas. No `PRAGMA key`, SQLCipher, or encryption extension
  is used.
- **Impact:** Anyone with filesystem access, a backup, or an exposed share can read every card,
  event, permission, launch environment, setting, and credential.
- **Remediation:** Use an encrypted SQLite build (SQLCipher or SEE) and derive the key from a user
  passphrase or OS keyring. At minimum, document that full-disk encryption is mandatory.

### C4. Public zrok board-share password stored in plaintext
- **Severity:** Critical
- **Files:** `internal/hubstore/settings.go:107-188`, `internal/cli/atrium_share.go:125-140`
- **Description:** The username/password for a public zrok board share are stored in the hub SQLite
  settings table in the clear. The code acknowledges this is necessary because the password must be
  replayed to zrok on every share creation.
- **Impact:** A leaked hub database exposes the public board's login credential.
- **Remediation:** Encrypt `SettingSharePass` at rest using a key from the OS credential store, or
  re-prompt for the password on hub start instead of persisting it.

### C5. No Content Security Policy on the board or phone page
- **Severity:** Critical
- **Files:** `internal/api/web/web.go:146`, `internal/api/web/index.html`,
  `internal/api/web/m/index.html`
- **Description:** The static-file handler sets only `Cache-Control`. It does not set CSP,
  `X-Frame-Options`, `Referrer-Policy`, or HSTS. The board runs with the operator's full machine
  privileges; any script injection is effectively RCE against the operator's session.
- **Remediation:** Serve the board with a strict CSP:
  ```http
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline';
    img-src 'self' blob:; connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'self';
    form-action 'self';
  ```
  Move any inline boot script to a file or use a nonce.

### C6. Operator-supplied grouping expressions are compiled with `new Function`
- **Severity:** Critical
- **Files:** `internal/api/web/js/board.js:1110-1118`, `internal/api/web/js/cardrules.js:36,81`,
  `internal/api/web/js/overlays.js:1267-1271`, `internal/api/web/js/settings.js:97-98`
- **Description:** The board stores a custom JavaScript function body in `localStorage`
  (`atrium.grouping`) and compiles it with `new Function(...)`. The code's own comment admits this
  is safe only because it is local-only today.
- **Impact:** Any XSS, malicious browser extension, or `localStorage` sync poisoning can overwrite
  that key and execute arbitrary code every time the board paints.
- **Remediation:** Replace the custom expression with a restricted evaluator or a fixed menu of
  grouping rules. If free expressions must remain, do not persist them where XSS can edit them.

### C7. `askUser` dialog renders body via `innerHTML` with no central escaping
- **Severity:** Critical
- **Files:** `internal/api/web/js/browser-dialogs.js:209`, callers in `core.js:556`,
  `overlays.js:814`, `notify.js`
- **Description:** The shared dialog writes `opts.body` directly into `#ask-body` using
  `innerHTML`. Many callers pass server error messages or user-controlled strings without escaping.
- **Impact:** A server response or card title containing `<img src=x onerror=...>` executes in the
  board context.
- **Remediation:** Change `askUser` to treat `body` as text by default (`textContent`). Add a
  separate `html` option only for callers that intentionally render already-sanitized markup.

### C8. WebSocket attach endpoint skips origin validation for link traffic
- **Severity:** Critical
- **Files:** `internal/daemon/attach.go:278-285`
- **Description:** `websocket.Accept` is called with
  `InsecureSkipVerify: edge.ViaLink(r)`. The comment says this is safe because the hub's edge
  already checked the request.
- **Impact:** If the edge wrapper ever misidentifies a request, or if an attacker can reach the
  daemon directly through the same published listener, cross-origin WebSocket connections can
  attach to terminals.
- **Remediation:** Keep origin checks on for every path. Authenticate inside the WebSocket handshake
  with a signed token instead of disabling the browser's same-origin check.

### C9. OIDC client secret and session cookie signing key stored in the settings DB
- **Severity:** Critical
- **Files:** `internal/daemon/auth.go:62-69,229-237,294-328`, `internal/daemon/export.go:169-186`
- **Description:** `AuthConfig.ClientSecret` and a 32-byte HMAC cookie key are serialized and stored
  in the `hub_setting` table. The GET endpoint redacts them, but the database itself does not.
- **Impact:** Database compromise allows impersonation of the OIDC client and forgery of session
  cookies.
- **Remediation:** Store `client_secret` and the cookie key outside the database (OS credential
  store, DPAPI, Keychain, or a separate restricted key file).

### C10. Card launch environment variables stored plaintext in the room database
- **Severity:** Critical
- **Files:** `internal/store/tasks.go:1039-1077`, `internal/store/schema.go:1717`
- **Description:** When a card is launched with extra environment variables (`ANTHROPIC_API_KEY`,
  `GEMINI_API_KEY`, registry tokens, etc.), the entire `map[string]string` is JSON-marshalled into
  the `launch_env` column.
- **Impact:** Database theft exposes every API key, OAuth token, and secret passed to agents.
- **Remediation:** Encrypt `launch_env` values per-row with a key protected by the OS user context,
  or store only key names in the database and require re-entry of secret values on restart.

---

## High Findings

### H1. Certificate Authority private key stored unencrypted on disk
- **Severity:** High
- **Files:** `internal/link/certs.go:72-175`, `internal/link/certs.go:564-573`
- **Description:** The hub creates a self-signed CA (`ca.key`) and writes it to the key directory as
  unencrypted PEM with mode `0600`.
- **Impact:** Filesystem access allows an attacker to mint valid room certificates and impersonate
  any room to the hub.
- **Remediation:** Encrypt `ca.key` with a passphrase derived from the OS user context or prompt the
  operator once at first start.

### H2. OpenZiti identity files (private keys) stored unencrypted on disk
- **Severity:** High
- **Files:** `internal/daemon/overlay_setup.go:247-309`, `internal/daemon/overlay_ziti.go:23-84`
- **Description:** `EnrollZitiInto` writes enrollment JWTs to disk (`0o600`) and shells out to
  `ziti enroll`, producing an identity JSON file that is left on disk unencrypted.
- **Impact:** Filesystem access yields the ziti identity private key.
- **Remediation:** Use the ziti SDK's encrypted identity store where available, or encrypt the
  identity file with a key from the OS keychain. Delete the one-time JWT immediately after
  enrollment.

### H3. Backups are unencrypted copies of the unencrypted database and document blobs
- **Severity:** High
- **Files:** `internal/hubstore/backup.go:77-107`, `internal/hubstore/docs.go:1038-1103`
- **Description:** `Snapshot` uses `VACUUM INTO` to copy `hub.db` to a timestamped file. `CopyDocs`
  mirrors the blob directory. Both are plaintext.
- **Impact:** Backups placed on shared drives, cloud sync folders, or removable media expose the
  entire hub state.
- **Remediation:** Encrypt backups with a key derived from the operator's passphrase or OS keyring.

### H4. No rate limiting on login or any API surface
- **Severity:** High
- **Files:** `internal/daemon/auth_flow.go:136-172`, `internal/daemon/auth.go:193-227`, all
  `/v1/...` routes
- **Description:** `startLogin` caps in-flight states at 4096 but imposes no per-source rate limit.
  Every API endpoint lacks throttling.
- **Impact:** Brute force, reconnaissance, and resource exhaustion.
- **Remediation:** Add per-IP and per-account rate limiting to `/auth/*` and global request-rate
  limiting on the board. Log authentication failures with client address.

### H5. No CSRF protection on state-changing API calls
- **Severity:** High
- **Files:** `internal/api/api.go` (all `/v1/...` routes), `internal/daemon/auth.go:344-350`
- **Description:** The API accepts POST/PUT/DELETE/PATCH without CSRF tokens and without requiring
  custom headers such as `X-Requested-With`. The session cookie is `SameSite=Lax`.
- **Impact:** Same-site cross-origin or subdomain attackers can call state-changing routes.
- **Remediation:** Add synchronizer tokens for cookie-authenticated requests or require a custom
  request header.

### H6. OIDC login has no nonce and a global in-memory state cache
- **Severity:** High
- **Files:** `internal/daemon/auth_flow.go:136-172,306-335,451-488`
- **Description:** The OIDC flow uses PKCE and `state` but does not request or validate a `nonce`.
  The `pending` map is a process-global variable swept lazily.
- **Impact:** A replayed or cross-context ID token can be accepted.
- **Remediation:** Add a nonce to the authorization request and verify the `nonce` claim in the
  callback.

### H7. Basic auth password travels plaintext when board is served over HTTP
- **Severity:** High
- **Files:** `internal/daemon/auth_flow.go:251-267`
- **Description:** Basic Auth credentials are sent in every request. The daemon does not redirect to
  HTTPS or emit HSTS.
- **Impact:** Credential capture on a plain-HTTP published board.
- **Remediation:** Refuse Basic Auth over non-TLS transports or redirect to HTTPS when a TLS
  terminator is present.

### H8. Shutdown endpoint trusts loopback origin; bypassable via raw TCP tunnel
- **Severity:** High
- **Files:** `internal/daemon/shutdown.go:54-99`, `internal/edge/local.go:22-35`
- **Description:** Without `--shutdown-token`, `POST /v1/shutdown` is allowed if
  `edge.LocalOperator(r)` returns true. A raw TCP tunnel (`ssh -L`, `socat`) defeats this check.
- **Impact:** An attacker who can forward a port can shut down the daemon.
- **Remediation:** Require a shutdown token by default, or bind the tokenless path to a Unix socket
  / named pipe.

### H9. Settings endpoint allows configuring arbitrary command execution
- **Severity:** High
- **Files:** `internal/api/settings.go:479-500`, `internal/api/fileopen.go:98-103`,
  `internal/api/termopen.go`
- **Description:** `PUT /v1/settings` accepts `editor_command`, `terminal_command`, and
  `shell_command` with no validation beyond trimming whitespace. Any authenticated caller can set
  arbitrary executables.
- **Impact:** Stored remote code execution for any authenticated board user.
- **Remediation:** Require daemon restart before new commands take effect, or require out-of-band
  operator confirmation. Consider an allow-list of known-safe executables.

### H10. Import config can silently install command-execution primitives
- **Severity:** High
- **Files:** `internal/api/export.go`, `internal/daemon/import.go:121-140`
- **Description:** `POST /v1/config/import?apply=1` imports harnesses, sources, and settings. A
  malicious JSON file can set `cmd`/`args` for a source, harness `exe`, `editor_command`, etc.
- **Impact:** Stored RCE via authenticated import.
- **Remediation:** Sign exported configurations or require a separate admin approval step before
  applying imported settings/harnesses/sources.

### H11. Guest shares give full terminal control with no authentication
- **Severity:** High
- **Files:** `internal/daemon/overlay_guest.go:616-763`, `internal/daemon/overlay_guest.go:67-76`
- **Description:** A lent card share exposes the task and attach endpoints to anyone who knows the
  URL. The share URL is the sole credential and there is no read-only mode.
- **Impact:** Anyone with the link can type into the terminal.
- **Remediation:** Document the threat model clearly; consider optional short-lived tokens or PINs
  and audit guest keystrokes.

### H12. Sources and harnesses are stored arbitrary command execution
- **Severity:** High
- **Files:** `internal/daemon/sources.go:194-272`, `internal/daemon/launch.go:1161-1191`
- **Description:** Sources are run with `exec.CommandContext(ctx, s.Cmd, s.Args...)`. Harnesses
  build argv from stored templates. There is no sandbox or argument validation.
- **Impact:** A compromised board account or malicious import can run arbitrary commands as the
  daemon user.
- **Remediation:** Run sources/harnesses in a restricted subprocess context. Require a
  manifest/signature for imports.

### H13. Reachable vulnerable Go dependencies
- **Severity:** High
- **Files:** `go.mod:17-18,32,113`
- **Description:**
  - `golang.org/x/crypto v0.51.0` — SSH auth bypass, revoked-status failures, integer overflows
    (fixed in 0.52.0+).
  - `golang.org/x/net v0.53.0` — HTML-parser CPU-exhaustion DoS (fixed in 0.55.0+).
  - `github.com/go-jose/go-jose/v4 v4.1.3` — JWE decryption panic (fixed in v4.1.4+).
  - `golang.org/x/text v0.37.0` — infinite-loop DoS (fixed in 0.39.0+).
  - `go.opentelemetry.io/otel v1.38.0` — baggage amplification DoS (fixed in 1.41.0+).
- **Remediation:** Run `go get` for each to the latest patched version, `go mod tidy`, and run the
  full test suite.

### H14. Website npm dependency with RCE (`serialize-javascript`)
- **Severity:** High
- **Files:** `website/package.json`, `website/package-lock.json`
- **Description:** Docusaurus 3.10.2 transitively pulls `serialize-javascript <= 7.0.2`, which is
  vulnerable to RCE (GHSA-5c6j-r48x-rmvq, CVSS 8.1).
- **Remediation:** `cd website && npm audit fix` or upgrade Docusaurus. Run `npm audit` in CI.

### H15. Unsigned release binaries and installers
- **Severity:** High
- **Files:** `.github/workflows/release.yml`, `scripts/cut-release.sh`, `scripts/package-macos.sh`,
  `scripts/package-windows.ps1`, `scripts/package-linux.sh`
- **Description:** Windows `.msi`, macOS `.pkg`, Linux `.deb`/`.rpm`, and zipped binaries are not
  signed. SHA-256 checksums protect against accidental corruption only.
- **Impact:** An attacker who replaces an artifact or the scoop manifest can compromise
  installations.
- **Remediation:** Sign Windows binaries/MSI with `signtool`, sign macOS `.pkg` with a Developer ID
  Installer certificate, and publish SLSA provenance.

### H16. Mobile markdown renderer emits `innerHTML` from model output
- **Severity:** High
- **Files:** `internal/api/web/m/js/md.js:141-218`, `internal/api/web/m/js/viewer.js:216-219`,
  `internal/api/web/m/js/card.js:557`
- **Description:** The phone page renders agent/model replies as markdown and writes the result to
  `innerHTML`. The regex-based parser may have bypasses.
- **Impact:** XSS into the phone page, which has the same terminal/file access as the desktop board.
- **Remediation:** Use a hardened markdown library with sanitization (e.g., DOMPurify) or render to
  a sanitized DOM tree.

### H17. No framing protection on the board
- **Severity:** High
- **Files:** `internal/api/web/web.go`, `internal/api/web/index.html`
- **Description:** The board is not served with `X-Frame-Options` or CSP `frame-ancestors`.
- **Impact:** Clickjacking can trick the operator into approving permissions or typing into
  terminals.
- **Remediation:** Add `X-Frame-Options: DENY` and CSP `frame-ancestors 'none'`.

---

## Medium Findings

### M1. zrok private-share token logged in plain text
- **Severity:** Medium
- **Files:** `internal/daemon/overlay_guest.go:430`, `internal/daemon/overlay_native.go:190`
- **Description:** Guest-share creation logs the zrok share token. For a private share, the token is
  the credential.
- **Remediation:** Log only the mode and address; never log `shr.Token`.

### M2. Shutdown token accepted via URL query string
- **Severity:** Medium
- **Files:** `internal/daemon/shutdown.go:78-84`, `internal/cli/stop.go:66`
- **Description:** `handleShutdown` reads the token from `X-Atrium-Token` or the `token` query
  parameter. Query strings are commonly logged.
- **Remediation:** Remove the query-parameter fallback.

### M3. ziti board edge accepts any `Host` when `ATRIUM_HOSTS` is unset
- **Severity:** Medium
- **Files:** `internal/cli/atrium_run.go:764-770`, `internal/daemon/overlay_native.go:319-325`
- **Description:** When serving the board over a ziti service with no `ATRIUM_HOSTS`, the code falls
  back to `edge.Unnamed`, which checks origins but not the `Host` header. The code warns that this
  leaves DNS rebinding open.
- **Remediation:** Refuse to start the ziti board share unless an intercept address is configured.

### M4. Legacy certificate-less overlay path allowed by default
- **Severity:** Medium
- **Files:** `internal/link/hub.go:282-290,317-325`, `internal/hubstore/settings.go:35-67`,
  `internal/cli/atrium_run.go:251-262`
- **Description:** Rooms that joined before the proven mTLS overlay can still attach without a
  certificate. `LegacyRefused` defaults to false unless `overlay_legacy = refuse` is set.
- **Remediation:** Default new hubs to `refuse`; add a startup warning or require explicit opt-in.

### M5. Shared daemon location file is world-readable
- **Severity:** Medium
- **Files:** `internal/daemon/whereami.go:286`
- **Description:** The shared location file is written with `0o644`, disclosing the agent/board
  address and binary path to all local users.
- **Remediation:** Write the shared location file with `0o600`.

### M6. zrok account token persisted to disk in the zrok environment
- **Severity:** Medium
- **Files:** `internal/daemon/overlay_setup.go:117-182`, `internal/daemon/overlay_root.go:91-121`
- **Description:** `EnableZrok` stores the account token in the zrok environment file.
- **Remediation:** Ensure the environment directory is `0700` and warn operators not to sync it.
  Prefer short-lived tokens where supported.

### M7. Stale zrok private-share token left on disk if hub is killed
- **Severity:** Medium
- **Files:** `internal/cli/transport.go:174-191`
- **Description:** The hub writes the private share token to `zrok-share` in its key directory. The
  file is removed only during normal shutdown.
- **Remediation:** Clear the file at startup before creating a new share, or keep the token in
  memory only.

### M8. Basic-auth password hashing parameters are modest
- **Severity:** Medium
- **Files:** `internal/daemon/auth.go:193-227`, `internal/daemon/auth_basic_test.go`
- **Description:** `hashPassword` uses scrypt with `N=1<<15`, `r=8`, `p=1`.
- **Remediation:** Increase scrypt `N` to at least `1<<18` or migrate to `golang.org/x/crypto/argon2`.
  Upgrade existing hashes on next successful login.

### M9. Session cookie uses `SameSite=Lax`, not `Strict`
- **Severity:** Medium
- **Files:** `internal/daemon/auth.go:344-350`, `internal/daemon/auth_flow.go:298-299,436-438`
- **Description:** `Lax` still sends the cookie on top-level cross-site GET navigations.
- **Remediation:** Use `SameSite=Strict` for the session cookie.

### M10. Session cookie not invalidated server-side on logout
- **Severity:** Medium
- **Files:** `internal/daemon/auth_flow.go:295-303`
- **Description:** Logout sets an empty cookie but maintains no server-side revocation list. A
  stolen cookie remains valid until its `Expires` time (12 hours).
- **Remediation:** Maintain a short-lived revocation list or switch to opaque session tokens.

### M11. `localStorage` is trusted as a settings store
- **Severity:** Medium
- **Files:** `internal/api/web/js/board.js:1051-1062`,
  `internal/api/web/js/settings.js`, `internal/api/web/m/js/store.js`
- **Description:** UI settings, grouping expressions, toast log, and skin preferences live in
  `localStorage`. A malicious extension or XSS foothold can rewrite them.
- **Remediation:** Treat `localStorage` as untrusted input; validate shapes and sanitize strings.
  Do not store executable code.

### M12. `esc()` helper does not escape single quotes
- **Severity:** Medium
- **Files:** `internal/api/web/js/core.js:393-394`
- **Description:** The desktop board's `esc()` escapes `& < > "` but not `'`. A future
  single-quoted attribute could break out.
- **Remediation:** Add `'` → `&#39;` to the escape table.

### M13. Service worker message handler does not validate message origin
- **Severity:** Medium
- **Files:** `internal/api/web/sw.js:94-132`
- **Description:** The service worker listens for `message` events without inspecting
  `event.source` or `event.origin`.
- **Remediation:** Check `event.source` is a controlled client before showing notifications.

### M14. JSON responses do not HTML-escape stored user content
- **Severity:** Medium
- **Files:** `internal/api/api.go:705-711`
- **Description:** `writeJSON` uses `json.NewEncoder(w).Encode(v)` without
  `SetEscapeHTML(true)`.
- **Remediation:** Enable `SetEscapeHTML(true)` on the encoder as defense in depth.

### M15. WebSocket attach has no per-message authentication after handshake
- **Severity:** Medium
- **Files:** `internal/daemon/attach.go:165-676`
- **Description:** Once the WebSocket is accepted, all subsequent frames are trusted without
  re-authentication.
- **Remediation:** Periodically require a signed heartbeat frame or bind the WebSocket to a
  short-lived token.

### M16. Launch request accepts arbitrary `cwd`, `args`, `env`, `model`, `effort`
- **Severity:** Medium
- **Files:** `internal/daemon/launch.go`, `internal/api/api.go:579`
- **Description:** `POST /v1/launch` passes these fields to process spawning without strict
  allow-lists.
- **Remediation:** Validate `Env` keys, reject control characters and shell metacharacters.

### M17. Outdated OpenZiti SDK and transport libraries
- **Severity:** Medium
- **Files:** `go.mod:13-14,82`
- **Description:** `sdk-golang v1.2.8` and `transport/v2 v2.0.194` are behind current releases.
- **Remediation:** Upgrade to current stable releases after testing.

### M18. GitHub Actions use floating major-version tags
- **Severity:** Medium
- **Files:** `.github/workflows/ci.yml`, `.github/workflows/docs.yml`,
  `.github/workflows/release.yml`
- **Description:** Actions are pinned to `@v4` / `@v5` rather than commit SHAs.
- **Remediation:** Pin every third-party action to a verified commit SHA and let Dependabot bump
  them.

---

## Low Findings

### L1. `/debug/pprof` mounted on loopback human listener by default
- **Severity:** Low
- **Files:** `internal/daemon/pprof.go:38-58`
- **Description:** Profiling endpoints are enabled when the human listener is loopback.
- **Remediation:** Make pprof opt-in via a flag.

### L2. Test fixtures contain realistic-looking secret literals
- **Severity:** Low
- **Files:** `internal/daemon/shutdown_share_test.go:64`, `internal/link/docs_mcp_test.go:244-251`,
  `internal/store/launch_options_test.go:29`
- **Description:** Tests use strings like `"s3cret"`, JWTs, PEM private keys, AWS-like keys.
- **Remediation:** Replace with obviously fake placeholders or generate test secrets
  programmatically.

### L3. `/v1/preflight` exposes process PID and `StartedBy`
- **Severity:** Low
- **Files:** `internal/daemon/preflight.go:129-135`
- **Description:** Preflight returns `pid` and `started_by`.
- **Remediation:** Consider removing PID from the response.

### L4. OIDC/JWT verification supports only RSA signing keys
- **Severity:** Low
- **Files:** `internal/daemon/auth_jwks.go:41-113`
- **Description:** The JWKS parser accepts only `kty == "RSA"`.
- **Remediation:** Support ECDSA and explicitly reject `none`/`HS256`.

### L5. No automatic rotation for cookie signing key or CA
- **Severity:** Low
- **Files:** `internal/daemon/auth.go:314-328`, `internal/link/certs.go:72-114`
- **Description:** Both are generated once and kept indefinitely.
- **Remediation:** Provide documented rotation procedures.

### L6. `InsecureSkipVerify` used with custom verification
- **Severity:** Low
- **Files:** `internal/link/direct.go:257-258,366-367`,
  `internal/link/overlaytls.go:246`
- **Description:** TLS config disables default verification and supplies custom
  `VerifyPeerCertificate` callbacks.
- **Remediation:** Add extensive unit tests; consider moving to `VerifyConnection`.

### L7. Multipart upload size limits can be bypassed via temp files
- **Severity:** Low
- **Files:** `internal/api/files.go:166-241`
- **Description:** `ParseMultipartForm` writes non-memory parts to disk before the overall cap is
  applied.
- **Remediation:** Configure `ParseMultipartForm` memory limits and bound temp-directory usage.

### L8. Vendored xterm bundles lack CI integrity verification
- **Severity:** Low
- **Files:** `internal/api/web/vendor/VERSIONS.md`
- **Description:** Hashes are documented but CI never recomputes them.
- **Remediation:** Add a CI step that verifies each vendored file against `VERSIONS.md`.

---

## Positive Security Controls

The following controls are well-implemented and should be preserved:

- **Join secrets are SHA-256 hashed** and single-use (`internal/hubstore/secrets.go`).
- **Basic-auth passwords are salted and hashed with scrypt**, never returned by the API.
- **OIDC uses PKCE and short-lived `state`**; the PKCE verifier is kept in memory only.
- **Constant-time comparison** is used for tokens and passwords.
- **Config export scans for secret shapes** and refuses the export if found.
- **Document upload scans for secret-shaped content** (`internal/hubstore/docs_secrets.go`).
- **Directories created with `0700` and key files with `0600`**.
- **Direct mTLS is pinned:** the room ignores DNS names and verifies the hub certificate was issued
  by the pinned CA.
- **Overlay links add mTLS inside the overlay** (`internal/link/overlaytls.go`).
- **Path traversal is mitigated** via `internal/safepath`.
- **File downloads are forced as `application/octet-stream`** with `Content-Disposition: attachment`.
- **pprof is loopback-only** and shutdown is gated while a share is running.
- **No hardcoded production credentials** were found in the source.

---

## Prioritized Remediation Roadmap

### Immediate (this week)
1. Change daemon default bind addresses to `127.0.0.1`.
2. Add authentication to the agent listener when bound non-loopback.
3. Patch Go dependencies: `golang.org/x/crypto`, `golang.org/x/net`,
   `github.com/go-jose/go-jose/v4`, `golang.org/x/text`, `go.opentelemetry.io/otel`.
4. Run `npm audit fix` in `website/`.
5. Add a strict CSP plus `X-Frame-Options: DENY` to the board and phone pages.
6. Replace `new Function` grouping expressions with a safe evaluator.
7. Harden `askUser` to use `textContent` by default.

### Short term (next 2-4 weeks)
8. Encrypt the room and hub SQLite databases at rest.
9. Encrypt or eliminate plaintext storage of the public zrok board-share password.
10. Encrypt OIDC client secret, cookie signing key, and `launch_env` values before storage.
11. Encrypt the CA private key and ziti identity files, or store keys in the OS keychain.
12. Encrypt hub database backups and document blob backups.
13. Add rate limiting and failure logging to authentication and key API surfaces.
14. Add CSRF tokens or a required custom header for cookie-authenticated state-changing requests.
15. Add OIDC nonce validation.
16. Refuse Basic Auth over plain HTTP.
17. Require a shutdown token by default.
18. Restrict `editor_command`, `terminal_command`, `shell_command`, and harness/source imports to
    out-of-band approval or restart.
19. Sign and verify configuration exports.

### Medium term (next 1-3 months)
20. Sign release binaries and installers; publish SLSA provenance.
21. Upgrade OpenZiti SDK/transport/zrok dependencies to current stable releases.
22. Add WebSocket attach tokens and origin validation on every path.
23. Harden the mobile markdown renderer with DOMPurify or a sanitized DOM renderer.
24. Improve `localStorage` input validation and stop storing executable code.
25. Pin GitHub Actions to commit SHAs and add Dependabot.
26. Add `govulncheck ./...` and `npm audit` to CI.

### Ongoing
27. Re-run this audit after each major change or dependency update.
28. Add an integrity-check CI step for vendored xterm files.
29. Implement key/CA rotation procedures.

---

## Appendix A: Methodology

- Reviewed all Go source files under `internal/` and `cmd/` for security-critical paths.
- Reviewed frontend code under `internal/api/web/` and `website/`.
- Reviewed CI/build configuration under `.github/workflows/` and `scripts/`.
- Queried OSV for Go module vulnerabilities and ran `npm audit --json` for the docs site.
- Cross-referenced findings from five parallel specialist reviewers.

## Appendix B: Files Referenced Most Often

- `internal/daemon/daemon.go`
- `internal/daemon/auth.go`, `auth_flow.go`, `auth_jwks.go`
- `internal/daemon/shutdown.go`, `attach.go`, `overlay_guest.go`, `overlay_native.go`,
  `overlay_setup.go`
- `internal/link/certs.go`, `direct.go`, `overlaytls.go`, `hub.go`
- `internal/hubstore/settings.go`, `store.go`, `backup.go`, `secrets.go`
- `internal/store/store.go`, `tasks.go`, `schema.go`
- `internal/api/api.go`, `settings.go`, `export.go`, `files.go`, `browse.go`
- `internal/api/web/web.go`
- `internal/api/web/js/board.js`, `core.js`, `browser-dialogs.js`, `settings.js`, `overlays.js`
- `internal/api/web/m/js/card.js`, `md.js`, `viewer.js`
- `internal/edge/edge.go`, `local.go`
- `go.mod`, `website/package.json`

---

*Report written to `D:\git\github\dovholuknf\atrium\docs\security\SECURITY-AUDIT-2026-10-01.md`.*
*This is a living document. Update it as findings are confirmed, patched, or re-prioritized.*
