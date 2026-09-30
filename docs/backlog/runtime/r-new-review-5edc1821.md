# r-new-review-5edc1821. Security stage 0 and 1: review

Status: open. Filed by @review 2026-09-30. Read-only review of 5edc1821 (merge of `claude/r-security-edge`), which
answers C1, C2, H1 and M1 of `docs/review/security-audit-2026-09-30.md`. Owned by @runtime.

## Verified live

At 16:2x on sg4, every audit probe that worked before is refused now, and a plain request still passes:

| Probe | Before | Now |
| --- | --- | --- |
| rebound `Host` on 7778, 7781, 7777 | 200 | 403 |
| cross-site `text/plain` dry-run import on 7778 and 7781 | 200 | 403 |
| cross-origin websocket handshake through 7778 | 101 | 403 |
| `curl` with no `Origin`, `GET /_hub/health` | 200 | 200 |

`internal/edge` is small and does the three things it says. `http.CrossOriginProtection` passes a request carrying
neither `Sec-Fetch-Site` nor `Origin`, so every hook, the CLI and the MCP client still pass, and
`internal/cli/hookrefused_test.go` pins that a refused hook stays silent. `MarkLink` marks the link by the handler a
request arrived on, never by a header, so a caller on the room's own port cannot claim it.

## 1. Medium. A listener that is not loopback gets no Host check, so DNS rebinding still works there

`For` (`internal/edge/edge.go:53`) picks `Shared` for any address that is not loopback, and `Shared`
(`edge.go:45`) has no `Host` check. A rebound page is same-origin: its `Origin` is `http://attacker.example:7778`
and so is its `Host`. `CrossOriginProtection` compares the two and passes, `upgradeCheck` compares the two and
passes, and the page reads, writes and attaches. That applies to:

- a room or hub started with `--http`, `--addr` or `--agent` on `0.0.0.0` or a LAN address. Not the case on sg4
  today, and it is what `For` exists for.
- every overlay listener, `overlay_native.go:266` and `:309`, and the hub's board share (`atrium_run.go:522`),
  when the machine opening the share runs a local access proxy that passes `Host` through, such as `zrok access
  private` on `127.0.0.1:9191`. The board login stops a rebound page where it is on, because its cookie belongs
  to the real host. A private hub share has no login.

Fix: `Shared` takes the names the listener is known by (the share's host, the bound address, and any configured
extras) and refuses any other `Host`. An empty list means loopback names only, which is safe as a default.

## 2. Low. Unrelated growler behaviour rode in on the security merge

`internal/link/growl.go` gained `growl.since` in this merge (the stat shows `growl.go` +47 and `growl_test.go` +48).
Reverting stage 1 would also revert the fix that stopped three-day-old questions growling. Keep security merges to
the security change, so a revert takes only that.

## 3. Low. Not yet tested: a terminal attach over a share

`upgradeCheck` requires the `Origin` host to equal `Host`. Through a zrok public frontend the browser's `Origin` is
the public name, and whether the `Host` that reaches the in-process listener is the same name depends on the
frontend. If it is not, the phone and every shared board lose terminal attach with a 403 and no other symptom. Add a
test-plan row: attach from a public share and a private access, both.

## 4. Info. GET is outside the cross-origin check, as designed

`CrossOriginProtection` does not look at safe methods. That is right as long as no GET changes anything. The GETs
checked here (`/v1/rooms/join`, the export, the browse) only read. Nothing pins that for GETs added later. A test that
walks the mux for GET handlers that write would.

## Tests

Run in `D:\worktrees\claude\atrium\review` at claude/main. `./internal/edge/`, `./internal/cli/` and
`./internal/hubstore/` pass. `./internal/link/` failed once, on `TestAPatientAskMadeWhilePausedWaitsForResume`
("after the resume the ask said busy", `restartgate_test.go:316`), with the four packages running together. Alone it
passes five of five. None of these merges touches the restart gate, and dbb77beb fixed three load flakes in the same file.
So it reads as one more flake of that kind, not a regression here.
