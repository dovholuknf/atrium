# r-new-telegram-notify-build. Build Telegram notify through the notify command

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @runtime.

## What is missing

A build item. The research (404f6def) and the design (5abf6282) are done for `rnd-new-telegram-notify` and no queue
holds the build.

## Why it is needed

A card that needs input while clint is away from the board reaches him on a channel he reads.

## Depends on it

Nothing else. It needs the f-017 notify sink, which is built.

## Done looks like

- Outbound notify through a user-configured command, so atrium holds the command's name and never the bot token.
- Events, batching and quiet hours follow the design and reuse the board's notification rules.
- Two-way replies only as far as the design's verdict allows.
- Tests with a fake command. Atrium never sends code, diffs or secrets, as the design lists.
