# r-new-hosts-setting: the names the hub answers, in the gear

Status: parked (clint, 2026-09-30). @runtime the setting, @ui the gear row. Review by @review: it is the DNS
rebinding guard from `docs/rnd/security-design.md` stage 0.

## Why

Sharing the board over zrok at `atrium.shares.zrok.io` failed with "this listener does not answer to that name.
$ATRIUM_HOSTS adds one". The fix was an environment variable for the claude user and a hub restart, by hand, from
the orchestrator. A random-name share (`hdzujxlq0dan.shares.zrok.io`) failed the same way. clint: "this should be
a settings cog thing too".

## Wanted

- A hub setting, `extra_hosts`, read by `internal/edge` as well as `$ATRIUM_HOSTS` (the variable stays, for a
  headless install). Changing it takes effect without a restart: `Named` reads a snapshot the setting updates.
- A gear row: the names, one per line, with a note on what a wrong entry costs (a name here is a name a rebinding
  page could use if it controlled that DNS).
- Wildcards: `*.shares.zrok.io`. Built and tested on `claude/edge-wildcard-hosts` (e25a2c2f), not landed.
- When a request is refused for its Host, the refusal page offers the fix: "add <that name> in the gear", with a
  link, instead of naming an environment variable.
- An overlay atrium drives itself (`docs/overlays.md`) already adds its own share name. Say so on the row.
