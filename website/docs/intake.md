---
title: Intake and sources
description: Starting work from an issue or a ticket, and the inbox that holds work atrium did not start.
---

# Intake and sources

Atrium does not learn what an issue, a ticket or a CI failure is. Whatever already knows makes the worktree and
hands it over.

## One line at the end of a script

The cheapest version is one line at the end of a script you already have:

```powershell
atrium launch --cwd $wtPath --title "issue-$id" --tags "bug,issue-$id" `
  --source github --external $id --item-url "https://github.com/acme/api/issues/$id" `
  --prompt "read this issue, summarise it, and tell me where the fix belongs"
```

The card arrives supervised, gated, tagged, and carrying a link back to where the work came from.

## Sources

A source is a command atrium runs on a timer. Its output is a list of work items. Add one on the **rooms** tab.

Atrium holds an argv and an interval, never a credential. `gh` already has a token in the keyring it uses, and
atrium has the path to a script that calls `gh`. Names such as `github`, `zendesk` and `ci` are words on a badge.
Atrium never learns what they mean.

A source that fails is reported on its own row, and switched off after three consecutive failures with the reason
attached. An item that moves on rewrites its card while the card is still in the inbox.

## The inbox

Items land in the **inbox**, a column that appears only when it holds something. A card there has no runner
behind it. **start** opens the launch dialog with everything the source knew already filled in, and starts the
session on that same card, so the work keeps its link to where it came from.

:::tip Support queues are different
A support case names a customer, not a repository, and it carries somebody else's words. Put the identifier on the
card, not the subject line, and offer the work rather than preparing it.
:::
