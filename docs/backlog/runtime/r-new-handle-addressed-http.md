# r-new-handle-addressed-http. HTTP takes the @ handle, and the hub routes it to the right room

Status: not started. Owned by @runtime. Design through @rnd. Filed by the orchestrator 2026-09-30 on clint's word:
"we also need a way for curl's and whatnot to reach intended agents. right now it's using a uuid but using the @
handle would be better and atrium hub should dispatch accordingly to the proper agent".

## Today

Scripts and curl reach a card only by its id: `POST /v1/tasks/<uuid>/exit` on the room's own port. The MCP tools
already take `alias`, `handle`, `name@room` and `room~id` (`atrium_say`, `atrium_task`, `atrium_exit`), and the
hub already routes those across rooms. The HTTP surface does not. Measured 2026-09-30 on build cc4ee8f3:

- hub `GET /v1/tasks/rnd@claude-sg4` answers 404.
- room `GET /v1/tasks/rnd` answers **500**. An unknown id is not a server fault. That is a bug on its own, fix it
  first (404, and the body names the handles that would have worked, as `atrium_say` does).

## Wanted

- Every route that takes `{id}` on the hub also takes `alias`, `@alias`, `handle`, `alias@room`, `handle@room` and
  `room~id`, and the hub resolves it with the same resolver the MCP tools use, then forwards to the owning room.
  One resolver, not a second copy.
- A bare alias on the hub resolves when exactly one live card holds it across all rooms. Two holders answer 409
  with both, so a script never reaches the wrong agent.
- The room's own port takes a bare alias or handle for its own cards, so a script on the room machine needs no hub.
- A small CLI on top so nobody hand-writes curl: `atrium task <handle>`, `atrium exit <handle>`, and what the
  scripts in D:\tmp do by hand (`new-context`, launch onto a card).

## Open for the design

- Which routes are in stage 1. Suggested: task read, exit, new-context, say, patch.
- Whether a handle in a URL needs escaping rules (`@` and `~` are legal in a path segment, but document it).
