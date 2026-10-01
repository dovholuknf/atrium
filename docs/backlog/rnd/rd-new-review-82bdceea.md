# Review: Telegram notify design 82bdceea (@rnd)

docs/rnd/telegram-notify-design.md and docs/backlog/rnd/rnd-new-telegram-notify.md, one commit on claude/rnd. Doc
only. Asked: the credential reasoning against internal/link/notify.go and CLAUDE.md, and the rejection of two-way.

## What holds

Every claim about the notify command matches the code:

- The four fields `name`, `reason`, `card` and `room`, given as `ATRIUM_NOTIFY_*` and as one JSON line on stdin, never
  in argv (notify.go:42-47, 398-412). The reasons are `permission`, `question`, `input` and `finished`.
- `PUT /_hub/notify {enabled, command}` and `POST /_hub/notify/test` are the routes, and both are loopback only
  (notifyapi.go:63-69). That backs "15 minutes from loopback".
- Only changes are sent, and they are held back while a desktop tab is visible (`presence`, `Suppressed`).
- The credential rule is quoted correctly. notify.go:34 says the same thing in its own words.

The rejection of two-way is right, and the reasons are the right ones. The identity would be a Telegram session,
which skips the overlay's own identity. A safe approval needs the command, and the command must not leave the machine,
so the approval is either blind or a leak. A poller that turns chat text into loopback API calls ends the reason
loopback with no login is safe. I would add one line: a Telegram reply that is only a MESSAGE to a card, and not an
approval, is no safer. A message reaches an agent that holds tools (permission chain step 2 puts it ahead of every
rule), so a stolen Telegram session could steer an agent without approving anything.

## Findings

### MEDIUM (doc): say that the token must not be in the argv, and why

The doc says the token lives in the script. It does not say what happens when it does not, and the easiest Telegram
setup is a one-line `curl https://api.telegram.org/bot<token>/sendMessage ...` set as the command. That argv is:

- stored in atrium's database as `notify.command` (hub_setting), which is the "token in atrium's store" the doc rejects,
- and served by `GET /_hub/notify` (`Status()` copies `n.command`, notify.go:762), which is NOT loopback only. The
  phone reads it over the overlay (notifyapi.go:31-32).

So a token in the argv is readable by anybody who reaches the board. Add to "The token": the argv is stored and served
to every board, the phone's included, so the token goes in a file the script reads, never in an argument. That is the
same reason notify.go keeps the card name out of argv, turned around.

### LOW (doc): what the script prints is kept and served too

A failed run's first stderr line becomes `last_error`, is persisted, and is in `GET /_hub/notify`. The test route
also returns up to 16 KiB of stdout and 8 KiB of stderr. A script that prints its request URL (`curl -v`, a
`Write-Host $uri` while debugging) puts the token there, since Telegram's token is in the URL path. Say: the script
must never print the URL or the token, and should print its own short error instead of the HTTP library's.

### NITS

- The sketch builds `https://<share>/m/$($in.card)`. The board's form for a card known by id is `/m/#term=<id>`
  (index.html:131, m/js/card.js:871), so use `"/m/#term=" + [uri]::EscapeDataString($in.card)` and drop "swap in
  whichever form".
- The doc names the card name as the one real leak, and then the sketch sends it. Make the sketch's default the fixed
  line plus the reason, and show the name as the opt-in.

## Verdict

CHANGES, small: fold the medium and the low into "The token" (the nits too, if convenient). No re-read is needed for
those words: send me the SHA and I check only the diff. Nothing is built, so there is no deploy verdict.

Quality: after the Sonnet switch, careful and correct against the code. The miss is the second-order one again: the
doc rules out the token in the store, without seeing that the obvious setup puts it in the store by the argv, and on
an open route as well.

## Fold: 324db7f8 (2026-10-01)

Diff 82bdceea..324db7f8, the design doc only. Everything is folded: the token-never-in-argv bullet (hub_setting, open
`GET /_hub/notify`), the print-nothing bullet (`last_error`, the test output), and the message-only line under
two-way. The sketch now defaults to a fixed line and makes the name opt-in through `ATRIUM_TELEGRAM_NAMES`. It links
`/m/#term=` with the escaped id and prints only `telegram send failed` on a failed send. Its `-ErrorAction Stop`
makes the catch see the HTTP error, so nothing from the library reaches stderr. A failed token-file read prints the
file's path, never the token.

OK. Design accepted. Nothing to build in atrium, and so nothing to deploy.
