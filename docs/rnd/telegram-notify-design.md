# A Telegram bot for notifications

Written by @rnd on 2026-10-01, from `docs/backlog/rnd/rnd-new-telegram-notify.md`. Design only. Nothing here is
built, and the recommended shape needs no atrium code at all.

## The verdict

- **Outbound: fine, with limits.** It is not scary. It works today through the notify command
  (`internal/link/notify.go`), with a script clint writes, and atrium changes nothing. Telegram reads four short
  fields in plaintext. That is the whole cost.
- **The token: no line crossed,** as long as the token lives in clint's script and never in atrium. The notify
  command was built for exactly this.
- **Two-way: no.** Answering a question or approving a permission from a Telegram chat is remote control of agents
  through a third party, with a Telegram account as the only identity. That is the horrifying part, and atrium
  will not build it.
- **Does it beat the alternatives: no.** Web Push (`docs/rnd/web-push-design.md`) is encrypted end to end and needs
  no account. ntfy does the same job as a Telegram bot with no account and can be self-hosted. Telegram wins only if
  clint already lives in Telegram and wants the alerts next to his other chats.

## Outbound, and what leaves the machine

The notify command gets four fields and no more: the card's name, the reason (permission, question, input,
finished), the card id (`room~id`) and the room. They arrive in the environment (`ATRIUM_NOTIFY_NAME`, `_REASON`,
`_CARD`, `_ROOM`) and as one JSON line on stdin, never in argv. That rule is in `notify.go` and is not new here.

- **What Telegram sees:** those four fields, as plaintext, kept on Telegram's servers for as long as the chat
  exists. Bot chats are never end to end encrypted. Telegram also sees when alerts fire and how many.
- **What it never sees:** the command a permission wants to run, the question's text, reply text, a recap, code, a
  diff, a finding, a path on the machine, or anything from a ticket. The sink is never handed them, so a script
  cannot leak them by mistake.
- **The one real leak is the card name.** A model or an operator writes it, so it can carry a repo name, a customer
  name or a ticket title. If that matters, the script sends a fixed line ("atrium: a card wants you") plus the
  reason, and the tap opens the board, which is where the name is read.
- **Noise is already handled upstream.** The trigger sends only changes, one per card, and stays quiet while a
  desktop board tab is visible. A Telegram script adds no rules of its own. If clint wants "finished" quieter, the
  script sends it with `disable_notification`.

## The token

CLAUDE.md's rule: atrium may hold the NAME of a command that has a credential, and never somebody else's credential.

- **Recommended: the token lives in the script.** clint's script reads it from his own file or keychain and calls
  `https://api.telegram.org/bot<token>/sendMessage`. Atrium stores the argv of the script and nothing else. This
  is the shape `notify.go` documents.
- **The token must never be in the command's argv.** The easy setup is a one-line `curl` with `bot<token>` in the
  URL as the command. That argv is stored as `notify.command` in `hub_setting`, which is atrium's store, and
  `GET /_hub/notify` serves it back, over the overlay too. A token in the argv is the rejected option below by
  another route. The command names a script, and the script reads the token.
- **The script must never print the URL or the token.** The hub keeps the first stderr line as `last_error`,
  persisted and served by `GET /_hub/notify`, and `POST /_hub/notify/test` returns the command's output. Whatever the
  script prints is something atrium holds.
- **Rejected: the token in atrium's store or settings.** That is atrium holding a credential, and a leaked atrium
  database would then let anybody post as the bot and read its updates.
- **Rejected: a Telegram sink inside atrium,** even one that reads the token from the keychain. It is new code and a
  new dependency on one vendor's API, for something a script already does.

What a leaked token costs: whoever holds it can post as the bot and read any messages sent to it. It cannot read
clint's other chats and cannot reach atrium. Outbound only, that is a nuisance, not a breach. With two-way, it would
be a breach, which is one more reason for the next section.

## Two-way, and why not

A reply in Telegram that answers a card means:

- **The identity is a Telegram account.** Whoever holds clint's Telegram session (a stolen phone, a SIM swap, a
  linked desktop he forgot) can approve a tool call on his machine. The overlays give the phone board its own
  identity (zrok's login, an OpenZiti identity). Telegram would be a second door that skips it.
- **An approval needs what must never leave the machine.** To approve a permission safely you have to see the
  command, and the command must not go to Telegram in plaintext. So a Telegram approval is either blind or a leak.
  Web Push reached the same answer for lock-screen buttons, and the phone board is where the answer is given.
- **It is a control plane atrium did not have.** The script would poll Telegram and post into the loopback API.
  Loopback with no login is safe because only this machine can reach it. A poller that turns chat text into API
  calls ends that.
- **It adds nothing the phone board lacks.** `/m` over the share already answers questions and permissions, with
  the command in view.
- **A reply that is only a message is no safer.** A reply that does not approve anything still steers an agent that
  holds tools, so it reaches the same machine through the same Telegram account.

clint could still write such a poller against the loopback API, and atrium cannot stop him. Atrium will not ship
one, document one as supported, or add an endpoint to make one easier.

## Against the alternatives

| | encrypted from the machine to the phone | account needed | atrium code | what the service reads |
| --- | --- | --- | --- | --- |
| Web Push | yes | none | one hub worker and one board worker (designed) | timing only |
| ntfy, self-hosted | over TLS to his own server | none | none, a script | nothing outside his server |
| ntfy.sh | TLS only | none | none, a script | the four fields |
| Telegram bot | TLS only | Telegram | none, a script | the four fields, kept |
| Signal via signal-cli | yes | a second Signal number | none, a script | timing only, but a heavy daemon |
| Pushover | TLS only | paid app | none, a script | the four fields |

The order stays as the Web Push design set it: ntfy through the notify command first, Web Push if clint keeps it
on. Telegram is an even swap for ntfy.sh, with a chat history that ntfy does not keep, and with an account.

## What it would cost

- **The recommended shape:** about 15 minutes of clint's time. A 10-line script holding the token, then
  `PUT /_hub/notify` with the script's argv and `enabled: true` from loopback. No atrium change and no review.
- **A sketch of that script,** for whoever writes it. It reads the token from a file only clint can read. By
  default it sends the fixed line and the reason, with the card's `/m` link on the share. The card name goes in
  only when `ATRIUM_TELEGRAM_NAMES` is set. A failure prints a fixed line, never the URL:

  ```powershell
  $in    = [Console]::In.ReadLine() | ConvertFrom-Json
  $token = (Get-Content "$HOME/.config/atrium-telegram/token" -Raw).Trim()
  $chat  = (Get-Content "$HOME/.config/atrium-telegram/chat" -Raw).Trim()
  $who   = if ($env:ATRIUM_TELEGRAM_NAMES) { $in.name } else { 'atrium: a card' }
  $link  = 'https://<share>/m/#term=' + [uri]::EscapeDataString($in.card)
  try {
      Invoke-RestMethod "https://api.telegram.org/bot$token/sendMessage" -Method Post -ErrorAction Stop `
          -Body @{ chat_id = $chat; text = "$who`: $($in.reason)`n$link"; disable_web_page_preview = 'true' } |
          Out-Null
  } catch { [Console]::Error.WriteLine('telegram send failed'); exit 1 }
  ```

  The stdin fields `name`, `reason`, `card` and `room` are the ones `notify.go` sends, and `/m/#term=<escaped id>`
  is the board's card link. `POST /_hub/notify/test` runs it once.
- **Two-way:** not offered at any cost.
