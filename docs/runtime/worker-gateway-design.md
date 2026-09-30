# Worker gateway (r-035)

## For clint

In the order you act:

1. Copy the mcp-gateway config to a worker config that holds only the mercurius backend.
2. Start a second gateway from that config on `127.0.0.1:8089`, from a folder only you can write.
3. Add a `mercurius-worker` entry pointing at `:8089` to the runner's mcp.json.
4. Set `lean_worker_gateway` to `mercurius-worker` (settings pane, or `POST /v1/settings`).

Until step 4 nothing changes. With it empty atrium behaves exactly as before.

## What it is

Lean workers reach mercurius through the same gateway a director uses, so a worker can call every backend behind it.
This gives workers a narrower gateway and leaves directors and review managers on the wide one.

**With `lean_worker_gateway` set, the key `mercurius` means the NARROW server for a default lean launch and the WIDE
one when a launch names `mercurius` explicitly.** The card's details say which one it got: `mercurius: mercurius-worker`
or `mercurius: mercurius (wide)`.

## The setting

- `lean_worker_gateway`, empty by default. It holds a server NAME from the runner's mcp.json, never a URL, so no header
  or token lands in atrium's table. The settings API refuses anything that looks like a URL.
- An unknown name is refused at launch with the list of names mcp.json has. It is checked at launch because that is
  where the config is read.
- It is not exported (export.go, `neverExported`). It names a machine's own config.
- No migration. Empty is today's behaviour, byte for byte.

## How the launch changes

- Default lean launch: `leanServers` writes the `lean_worker_gateway` entry under the key `mercurius`. Tool names
  (`mcp__mercurius__...`) and gate rules do not change.
- A launch naming `mercurius` in its lean `mcp` field, or a card carrying the tag `atrium:mcp:mercurius`, gets the real
  `mercurius` entry, the WIDE server. That is the override for directors and review managers. It survives a reopen or a
  restart through the tag, the way every `atrium:mcp:` name does.
- With the setting empty a `mercurius` named in `mcp` is dropped as a default server, as it always was.
- A restart re-reads the card's tags and the setting, so a card keeps narrow or wide across a restart. Changing the
  setting later moves narrow cards to the new server on their next restart, and a running card keeps what it started
  with until then.

## Notes

- **One rule set.** A standing rule naming `mcp__mercurius__*` applies to both servers. That is fine because the narrow
  set is a subset of the wide one, so nobody writes a second rule set.
- **Workers keep all six mercurius tools.** They are the review loop. Where a round's output goes is mercurius's own
  config, not the worker's choice of tool.
- **Card details.** `GET /v1/tasks` and `/v1/tasks/{id}` carry a `mercurius` field when the setting is set and the card
  is lean, and omit it otherwise. The board is @ui's to draw.
- **For mcp-gateway's maintainer.** The caller check on `:8088` is mcp-gateway's to add, not atrium's. Atrium only picks
  which server a worker is pointed at.
