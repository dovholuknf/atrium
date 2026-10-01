# rnd-new-telegram-notify: notifications through a Telegram bot, and how scary that is

Filed by the orchestrator, 2026-10-01, from clint: "telegram bot for notifications. is that scary? fucking
horrifying? what?"

## The question

Would a Telegram bot that tells clint what the board would alert him about (a card needs input, a permission is
waiting, a worker reported, a deploy finished) be a reasonable add, and where does it cross a line?

## What the design must weigh

1. **Outbound only.** Atrium posts a short message to a chat. What leaves the machine: card titles, questions,
   finding text, paths. Bot chats are not end-to-end encrypted, so Telegram's servers see all of it. What a
   message may carry and what it must never carry (code, diffs, secrets, customer data from a ticket).
2. **The credential.** A bot token is a credential atrium would hold. CLAUDE.md's line: atrium may hold the NAME of
   a command that has a credential, never the credential itself. Options: a user-configured command (a script that
   sends, holding its own token), the token in the OS keychain, or the overlay pattern.
3. **Two-way.** Replying from Telegram to answer a question or approve a permission is remote control of agents
   through a third party, authenticated only by a Telegram account. That is the scary part. Compare with what the
   overlays (zrok, OpenZiti) already give: the board on the phone, with its own identity. Does Telegram add anything
   the phone board does not?
4. **Noise.** Which events, batching, quiet hours, one message per card not per event. Reuse the toast log and the
   notification rules the board already has rather than a second set.
5. **Alternatives.** ntfy (self-hostable), Pushover, Signal via signal-cli, the phone board's own web push.

## Output

A short design: a verdict on each of outbound and two-way (fine / fine with limits / no), the recommended shape,
and what it would cost to build. Design only.
