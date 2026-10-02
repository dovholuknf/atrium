# Review: f-launch-routing 64a7e436..0ce86ade (m1mini, 2026-10-02): HOLD

Two commits on claude/f-launch-routing: 5c01b221 adds the room route `GET /v1/launch/cwd`, and 0ce86ade routes an
unscoped launch at the hub. Asked by clint through the orchestrator. Unsigned.

## What holds (the five asks)

1. **It cannot misroute or swallow a launch that works today.** `placeLaunch` acts only when `roomFor` names no
   room and two or more rooms are attached. With two or more rooms and no room named, the old code always answered
   `needsARoom`'s 409 (fanout.go:1054), so every request it touches was already refused. A launch named by header,
   tag or card, one with no cwd, and the single-room case pass through untouched (tested). The body is read within
   `launchBodyLimit` (1 MiB, cardroute.go:41), put back, and `ContentLength` is set again. An over-limit body is
   restored whole through a MultiReader, so the room refuses it as before. A match goes on as a clone with the room
   header, so the deletion gate, the caps and the dial see a named launch.
2. **The host test.** Both sides read `os.Hostname` on the same machine, which is the only case that matters (the
   filter keeps the rooms on the hub's own machine). So the spelling, the case and any domain suffix are identical.
   A Windows `os.Hostname` is the DNS host name of that one machine for both, and `equalFold` covers case anyway. A
   room with an empty `Host` is skipped. If nothing matches, every room is asked. One corner: a remote machine with
   the same hostname is a candidate, and the directory check still decides.
3. **The oracle.** The route answers exists and is-a-directory for one absolute path, a boolean, with no listing.
   The worker's argument holds: `POST /v1/launch` already answers "is not a directory" on the same listener, to
   anyone who can launch. A cross-origin page cannot read the GET (there are no CORS headers, and edge's host check
   stops rebinding). A guest share does not reach it (allowlist). It adds no reach.
4. **Rollout.** A room without the route answers 404, and that counts as "did not answer". So a fleet where some
   rooms are older behaves worse than today. See the High.
5. **Tests.** The 13 hub cases (`launchroute_test.go`) and the 4 room cases pass, and `go vet` is clean on link and
   api. They cover one match, none, two (listing only the matches, then the pick honoured), silence, named launches
   untouched, no cwd, other writes, a remote caller, the host filter fallback, and case.

## Medium (holds): "none matched" turns a working picker into a dead end in two cases

- **When a room did not answer.** That means a room on an older build, a slow one, or one restarting. Today the
  launch gets the 409 picker, and picking that room works. With this change it gets a 422 with no `rooms`, so gwt
  shows no picker and falls back to a plain tab, even though the directory is on the silent room. During a rollout
  that is every launch whose directory lives on a not-yet-updated room.
- **When the cwd is not absolute.** `.`, `~/x` and `src` are refused as "not a directory" by every probe
  (launchcwd.go). The caller then gets a 422, where today it gets a picker that works when the room resolves the
  path the same way. Check what gwt sends. If it is always absolute this is moot, but the route must not turn a
  client quirk into a hard failure.

Fix: when nothing matched and any candidate was quiet, or the cwd is not absolute, answer the old `needsARoom` 409
with every room, exactly today's behaviour. Keep the 422 for "every room answered, and none has it". Test both.

## Medium, hardening: no network paths in the probe

`os.Stat` on a UNC path (`\\host\share\x`, or `//host/share`) on a Windows room makes it open an SMB connection to
that host, and Windows authenticates with the user's NTLM credentials. The probe fans the caller's cwd out to every
candidate, and a non-local caller makes that every room. An existing launch stats only on its one room, so this
widens it. Refuse a path starting with `\\` or `//` in `launchCwd` as not a directory, and in `placeLaunch` fall
back to the 409 as above. Test it.

## Low

- A retiring room (`startsNothing`) is still a candidate. A launch routed to it is then refused as retiring, where
  the other match would have worked. Leave retiring rooms out of `launchCandidates`.

Verdict: HOLD 64a7e436..0ce86ade on the fallback (Medium 1), with the UNC refusal (Medium 2). A re-read starts at
64a7e436, hub-ok and room-ok. Rollout needs the room deploy and the hub deploy, and with the fallback the order
stops mattering.

Quality: careful and well tested. The scope is tight (it touches only what was refused anyway) and the body handling
is right. The miss is that "nobody answered" and "nobody has it" are different answers, and only the second earns
the end of the picker.
