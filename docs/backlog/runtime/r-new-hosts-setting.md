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
- Wildcards: `*.shares.zrok.io`. Landed in c1427c21 (e25a2c2f).
- From the review of e25a2c2f (`r-new-review-e25a2c2f.md`): the wildcard is safe only for a domain whose DNS records
  one operator alone sets. Say so in the code comment, on the gear row and in the refusal. Name dynamic DNS
  (`*.duckdns.org`) as the case where it reopens rebinding on every listener. `*.co.uk` passes the bare-label guard
  today. An entry that is ignored (`*.com`, `*.`) logs once, and the gear row shows it as ignored.
- When a request is refused for its Host, the refusal page offers the fix: "add <that name> in the gear", with a
  link, instead of naming an environment variable.
- An overlay atrium drives itself (`docs/overlays.md`) already adds its own share name. Say so on the row.
