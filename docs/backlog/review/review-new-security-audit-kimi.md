# Review: the Kimi security audit of 2026-10-01

Audit: `docs/security/SECURITY-AUDIT-2026-10-01.md`, written by an opencode session (Kimi k2.7-code via OpenCode Go).
It is untracked on sg4, and its full text came to @review by atrium say. It claims 10 Critical, 17 High, 18 Medium
and 8 Low. Checked against claude/main 6bce194f on m1mini. A pause exception, asked for by clint.

How it was checked: five read-only verifiers, one per surface (network, data at rest, frontend, auth and config,
dependencies), then my own reading of every finding that holds up or is new. No live listener was touched. Tools run:
`govulncheck` v1.8.0 (database 2026-10-01) on a `git archive` copy, `npm audit` on a copy of `website/`, the phone
markdown renderer in node against XSS payloads, and `ls` of the live state directories. Nothing here is a fix.

## Verdicts, every Critical and High

Severity "right" is under atrium's threat model: one operator, a loopback board by default, and publishing over
zrok or ziti behind a login. An attacker who can read the operator's files already has the operator's account.

| # | Claim | Verdict | Right severity | Why |
| --- | --- | --- | --- | --- |
| C1 | Binds agent and board to every interface by default | **Overstated, but real** | High | `atrium daemon` defaults to `:7777` and `:7778` (cli/cli.go:169-170), and `net.Listen` takes them as given (daemon/daemon.go:1090,1106). The current entry points do not: `atrium run` and the room use 127.0.0.1 (cli/atrium_defaults.go:20-23). The audit missed the live path: `restart_atrium` respawns `atrium daemon --db` with no addresses (cli/control.go:348), so a restart brings the daemon back wide. With a wildcard bind, `edge.For` accepts the machine's own IPs as Host (edge/edge.go:180-193), so a LAN client reaches the board API with no login. |
| C2 | Agent API has no auth beyond browser checks | Overstated | Medium | True, and by design: edge stops browsers only (edge.go:9-12). On loopback that leaves local processes, which is the 2026-09-30 audit's H2 and H3, still open. Only C1's wide bind makes it a network issue. |
| C3 | SQLite not encrypted at rest | Overstated | Low | True, and normal for the tool class. The real issue next to it was missed: the room's DB is not owner-only (new finding N3). |
| C4 | zrok share password stored plaintext | Overstated | Low | It has to be replayed to zrok on every share (cli/atrium_share.go:137). It is in hub.db, in a 0700 directory (hubstore/store.go:125), and the board only ever sees `share_pass_set` (link/fanout.go:827). |
| C5 | No CSP on board or phone page | Overstated | Low | The headers are indeed absent (api/web/web.go:146), but missing CSP is defence in depth. The board's inline `onclick=` handlers would force `'unsafe-inline'` anyway. HSTS does not apply to loopback http. |
| C6 | Grouping `new Function` from localStorage | **Wrong** | Info | Only same-origin script can write `atrium.grouping` (board.js:1052-1055). The server refuses a grouping expression (api/settings.go:355-389), and workspace files are served as attachment with nosniff (api/files.go:319-326). Writing the key already needs XSS. The code documents this boundary (board.js:1078-1106). |
| C7 | `askUser` puts its body in `innerHTML` | **Overstated, but real** | High | browser-dialogs.js:209 does write `innerHTML`. The audit gave no path; one exists: card-menu.js:331,336 passes `e.message`, the server's `ResumeBusy.Error()` (daemon/launch.go:503-511). That embeds the holder card's title, which an agent sets through MCP launch (link/control_mcp.go:1296) or an intake source takes from an external ticket (store/intake.go:133,277). Second path: themes.js:974,980 show `SaveTermTheme`'s `%q` error (store/termtheme.go:228), and `%q` leaves `<>` alone, so an imported theme value runs. It needs an operator click, so High, not Critical. |
| C8 | Attach skips origin check for link traffic | **Wrong** | Info | `ViaLink` reads a context value that only `MarkLink` sets (edge.go:294-304). That wraps only the room's handler for connections the room dialled to the hub over mTLS (roomrun.go:396), and no header sets it. The hub checks origin at its own edge before forwarding (cli/atrium_run.go:451). What remains is "the hub is trusted", the 2026-09-30 audit's M4. |
| C9 | OIDC secret and cookie key in `hub_setting` | Overstated, location wrong | Low (Medium with N3) | They are in the room's `setting` table in atrium.db, not hub.db (daemon/auth.go:303,316-324). Both are never exported (daemon/export.go:178-185), and the API returns only `has_client_secret` (daemon/daemon.go:442-456). The real exposure is N3: that file is world-readable on disk, and the cookie key mints sessions. |
| C10 | `launch_env` stored plaintext | Overstated | Low | True. Never returned by the API: `LaunchEnv` is `json:"-"`, only the key names go out (store/store.go:199-200). |
| H1 | CA key unencrypted PEM | Overstated | Info | 0600 in a 0700 directory (link/certs.go:76,560-573), ssh-style. |
| H2 | Ziti identity unencrypted, JWT left on disk | **Wrong** on the JWT | Info | The JWT is removed after enrolment (`defer os.Remove`, daemon/overlay_setup.go:292-297), and the SDK path never writes it (282-287). The identity is 0600 in a 0700 directory, standard ziti. |
| H3 | Backups plaintext | Overstated | Low | The backup directory is 0700 (hubstore/backup.go:80) and docs copies are 0600 (hubstore/docs.go:1113). |
| H4 | No rate limiting on login or API | Overstated, but real | Medium | No limiter or lockout in `authGuard` (daemon/auth_flow.go:251-267). The audit missed the worse half: every request with a Basic header runs scrypt N=2^15 (about 32 MiB) before anything else, so unauthenticated parallel requests can exhaust memory (daemon/auth.go:187,200). |
| H5 | No CSRF protection | **Wrong** | Info | Every browser-facing listener is wrapped in `http.CrossOriginProtection` (edge.go:251-258), published ones included (daemon/overlay_native.go:268,311,323, cli/atrium_run.go:451,504,768). That was the 2026-09-30 audit's C1, fixed. |
| H6 | OIDC has no nonce | Overstated | Info | Code flow with S256 PKCE and single-use 5-minute state. The token comes straight from the token endpoint, and the signature is checked with iss, aud and exp required (daemon/auth_flow.go:136-182,385-392,469-478). The nonce adds nothing here. |
| H7 | Basic auth in clear over HTTP | **Wrong** | Info | No such path. Public zrok terminates TLS at its frontend, private zrok is the guest's own localhost, ziti is encrypted end to end, and loopback has no login at all. |
| H8 | Shutdown trusts loopback, a tunnel bypasses it | Overstated | Low | Documented in the code (edge/local.go:20-23). Making a tunnel needs a local account, which can already POST. The worst case is a denial of service. |
| H9 | Settings take arbitrary editor, terminal and shell commands | Overstated | Info | True (api/settings.go:479-500). But the route is POST, not PUT (api/api.go:389), and a board user can `POST /v1/launch` anything anyway. No privilege boundary is crossed. Guests are refused (daemon/overlay_guest.go:745). |
| H10 | Config import installs commands | Overstated | Info | Same as H9. Cross-origin is gated, and auth config is never exported. |
| H11 | Guest share gives full terminal control | Overstated | Medium | Deliberate and documented, with no read-only mode by design (daemon/overlay_guest.go:66-75). Public shares use a 60-bit random reserved name (313-340). Guests reach only the runner's terminal (684-745). It rises with `global_auto` on. |
| H12 | Sources and harnesses are stored command execution | Overstated | Info | By design. The cross-site write that made it dangerous is closed by edge. |
| H13 | Reachable vulnerable Go deps | Overstated, and missed the real one | Medium | Versions and lines are right. Reachability is mostly wrong: atrium imports `x/crypto/scrypt` only (no ssh), `x/net/publicsuffix` only (no html), and neither the x/text nor the otel baggage advisory is called. The go-jose JWE panic (GO-2026-4945) is the one module hit: govulncheck reports it called through the ziti/zrok SDKs, a possible panic. **Missed: go.mod pins Go 1.26.2 (go.mod:3), CI builds with it, and govulncheck reports 11 called stdlib vulns** (new finding N1). |
| H14 | Website `serialize-javascript` RCE | Overstated | Low | The real version is 6.0.2, and the advisory range is <=7.0.4. It is a build-time webpack plugin for the static docs site, with the repo's own files as input, and is not shipped in atrium. |
| H15 | Release binaries unsigned | Confirmed | Medium | True, and already documented: cut-release.sh:496 "SIGNING. Nothing is signed." Signing is optional and switched on by environment variable (package-windows.ps1, package-macos.sh). It needs a certificate from a person. No new item. |
| H16 | Phone markdown `innerHTML` from model output | **Wrong** | Info | md.js escapes first and adds markup after. Tested in node with img onerror, `javascript:` in mixed case, `data:`, quote breakouts, entities, nested links, fence attributes and NUL placeholder forging: all came out inert. `safeURL` allows http(s) only. One cosmetic bug: `[a](http://x/`code`)` leaves NULs in the href. |
| H17 | No framing protection | Overstated, but real | Medium | No X-Frame-Options or frame-ancestors anywhere (the only CSP is on icons, icon.go:173). A framed loopback board's fetches are same-origin and pass CrossOriginProtection, and it has one-click state changes (approve all, settings.js:334; permission approvals). zrok share frontends can be framed from the internet. |

### Counts

- **Critical (10):** 2 wrong (C6, C8). 8 overstated, two of them real defects (C1 at High, C7 at High). None
  confirmed at Critical.
- **High (17):** 4 wrong (H2, H5, H7, H16). 12 overstated, three of them real defects (H4 and H17 at Medium, H13
  at Medium through the go-jose panic, with its stdlib miss filed as N1). 1 confirmed but already documented (H15,
  Medium).
- **Overall, 27 Critical and High:** 6 wrong (22%), 20 overstated (74%), 1 confirmed (H15, known), 0 confirmed at
  the severity given. 6 point at a real defect (C1, C7, H4, H13, H15, H17), and H15 was already documented, so 5
  are worth filing. None of the 27 holds its stated severity.
- From its Medium list, M12 (`esc()` misses `'`) is real and worse than stated: Medium, filed with C7. M3 (ziti with
  no `ATRIUM_HOSTS`) is a known open low from the 2026-09-30 audit.

## What it missed (found while checking)

- **N1. High-ish. The Go toolchain.** go.mod:3 `go 1.26.2`, built by CI through `go-version-file`. govulncheck reports
  11 called stdlib vulns, fixed in 1.26.3 to 1.26.6: crypto/tls (GO-2026-6090, 5856), net/http (6089, 4918, 5026),
  httputil ReverseProxy (4976, reached at link/proxy.go:695), encoding/asn1, encoding/xml, net/url, crypto/x509,
  net/textproto and net. The audit says it "queried OSV for Go module vulnerabilities" and never looked at the
  standard library.
- **N2. High. A restart widens the bind.** This is C1's actual trigger: `restart_atrium`, then `atrium daemon --db`
  (control.go:348), on `:7777` and `:7778` with no board login.
- **N3. Medium (multi-user Linux), Low (macOS).** The room's state is not owner-only. daemon/daemon.go:319 creates
  `~/.atrium` 0755, and atrium.db, -wal and -shm come out 0644. Confirmed on m1mini: `drwxr-xr-x ~/.atrium` and
  `-rw-r--r-- atrium.db`. That file holds the cookie HMAC key (a forged session on the published board), the OIDC
  client secret and launch_env. The hub got 0700 for exactly this (hubstore/store.go:120-127). Same pattern, Low:
  the cold event sink (store/filesink.go:69,175), `ATRIUM_TAP_DIR` (daemon/supervisor.go:2067-2070), and the PR
  runner's run.log and review.json (daemon/prrunner.go:439,461,474).
- **N4. Medium-High. Turning the login off leaves a running public share open.** `requireLoginForPublic` runs only
  when a share starts (daemon/overlay_api.go:209,313). `SaveAuth` accepts `enabled:false` with no check for a running
  share (daemon/auth.go:295-311), and `authGuard` then passes everything (daemon/auth_flow.go:222). It is reachable
  by anyone signed in to that board, since the room's published API has no `LocalOperator` gate on `PUT /v1/auth`,
  unlike the hub's control routes (link/proxy.go:1359).
- **N5. Medium. Agent to operator XSS through attribute JS strings.** About 40 templates build
  `onclick="fn('${esc(x)}')"`. HTML entities are decoded before the JS runs, so neither `esc` nor
  `.replace(/'/g,"&#39;")` protects them. Real case: settings-spine.js:1020, `addTag('…')` from every card's tags.
  An agent sets tags through MCP (link/control_mcp.go:1303), and `NormalizeTags` only lowercases and strips commas
  (store/tasks.go:1467). The tag `x');alert(1);//` runs on a click. The same pattern carries room names
  (rooms.js:218,224,300), ziti service names (expose2.js:436, expose3.js:336, overlays.js:1140), alias.js:24 and
  fixtures.js:34-42,406. Script in the board can approve permissions, so a prompt-injected agent escalates.
- **N6. Low-Medium.** Sessions cannot be revoked. The cookie is a stateless HMAC (daemon/auth.go:352-391), and the
  guard never re-checks the subject against the allowlist. A password change or an allowlist removal leaves old
  cookies valid for up to 12h. Low: logout is a GET, and with Basic the browser signs straight back in
  (daemon/auth_flow.go:252-256,295-300).
- Lead, not verified: runners.js:210,249 put `r.board`, a room's advertised URL, in an href with only `esc()`. If a
  room can send `javascript:`, that is clickable. Folded into the @ui XSS item to check.

## The audit itself, and the model

**Grade: D+.** It is useful as a checklist of places to look. It is not usable as a severity ranking or as a list of
findings to act on.

- **Calibration is the failure.** 0 of 27 Critical and High hold at the severity given. Its rule is effectively
  "plaintext on disk = Critical" and "missing header = Critical", with no threat model. It never asks who can reach
  a listener or who already owns the files. Seven of its ten Criticals are at-rest or header findings that are
  Low or Info for this tool class.
- **False positives: 6 of 27 wrong outright (22%).** The worst two contradict the code's central security design:
  H5 says there is no CSRF protection, and every listener is wrapped in `http.CrossOriginProtection`. C8 says attach
  is open to link spoofing, and the flag is an unforgeable context value. It read `InsecureSkipVerify` and stopped.
  H2 claims a leftover JWT that is deleted three lines later. H16 claims a markdown bypass that it never tried and
  that does not exist. None of them was tested, despite the stated methodology.
- **What it got right.** Line citations are mostly accurate: C9's table is wrong, and H9 says PUT where the route is
  POST. Dependency versions and fixed-in versions are real, not invented. It found a real dangerous sink in C7
  without finding the path, and a real config gap in C1 without finding the trigger. The "positive controls" list is
  accurate.
- **What it missed matters more than what it found.** It missed the 11 reachable stdlib vulns, despite claiming an
  OSV query. It missed the restart that widens the bind, the world-readable room database holding the session key,
  login-off leaving a public share open, and the agent-to-operator attribute XSS. Every one of those needs a trace
  across two files: a caller to a sink, a setting to a guard, a spawn to its flags. It did not make those traces.
- **Compared with this team's 2026-09-30 audit**, which proved CSRF and CSWSH with live probes and shipped them as
  fixes: this one did no dynamic work, and reports the fixed classes as still open.
- **For clint's first look at Kimi:** fast and broad, with real surface coverage and few invented facts. But its
  severities are noise, and about one finding in five is wrong. Every finding needs a verifier before anyone acts on
  it. As an unsupervised security reviewer it would have sent the team after encrypting SQLite while the stdlib,
  the bind regression and the XSS stayed open. As a first-pass lead generator under a verifying reviewer, it earns
  its cost.

## Items filed, all held (pause)

| Item | Director | Severity | Covers |
| --- | --- | --- | --- |
| `docs/backlog/runtime/r-new-sec-daemon-wide-bind.md` | @runtime | High | C1 and N2 |
| `docs/backlog/runtime/r-new-sec-go-toolchain.md` | @runtime | High | N1 and the go-jose part of H13 |
| `docs/backlog/runtime/r-new-sec-auth-guard.md` | @runtime | Medium-High | N4, H4 (with the scrypt DoS), N6 |
| `docs/backlog/runtime/r-new-sec-room-state-modes.md` | @runtime | Medium | N3 |
| `docs/backlog/ui/u-new-sec-dialog-html.md` | @ui | High | C7 |
| `docs/backlog/ui/u-new-sec-attr-js-strings.md` | @ui | Medium | N5 and M12, plus the `r.board` lead |
| `docs/backlog/ui/u-new-sec-framing.md` | @ui | Medium | H17, and C5 as its follow-on |

Quality: after the Sonnet switch, not applicable. This reviews another model's audit, not a director's work.
