# r-new-move-card-between-rooms. Move a card to another room, as a command and an MCP tool

Status: not started. Owned by @runtime. Filed by the orchestrator 2026-09-30, after clint asked for his five
win32crypto cards to move from claude-sg4 to sg4-control and said "this might be a good mcp tool?".

## What it took by hand

`D:\tmp\move-cards\move.sh <id>` does it for rooms on the same machine:

1. Read the card on the source room (`resume_id`, `worktree`, `runner`, title, `why`, tags, pin, overrides, theme,
   sound, icon, `peer_typing`, alias).
2. `POST /v1/tasks/{id}/exit` on the source and wait for the status to leave `running`/`needs-input`.
3. `POST /v1/launch` on the destination with `resume`, `cwd`, `title`, `why`, `tags`.
4. `PATCH /v1/tasks/{new}` with pinned, overrides, theme, sound, icon, peer_typing, alias.

It worked for all five cards. What it could not carry: `pin_order` (not patchable), the card's event history and
recap (they stay on the old card, which is left `done`), and any link between the old card and the new one.

## Wanted

- `atrium_move` (MCP, both the stdio control tool and the hub tool) and `atrium move <card> <room>`. Same machine
  first. A room on another machine needs the conversation file on that machine, which is out of scope until
  someone asks.
- The old card records where the work went, and the new one records where it came from, so history stays
  connected.
- Carry `pin_order` and the pin group.
- Refuse a card that is mid-turn unless forced. A move is an exit, and an exit mid-task leaves mid-task.

## Open for the design

- Whether the hub orchestrates the move (it sees both rooms) or the destination room pulls it.
- Whether the old card is archived or left `done`.
