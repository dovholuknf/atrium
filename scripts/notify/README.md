# Notify command examples

Atrium sends a notification by running a command you wrote (`internal/link/notify.go`). It holds the command's name
and never a credential.

`telegram.ps1` sends a Telegram message. Put the bot token in `~/.config/atrium-telegram/token` and the chat id in
`~/.config/atrium-telegram/chat`, readable only by you, replace `<share>` with your board's address, then set the
command with `PUT /_hub/notify` from loopback and run `POST /_hub/notify/test` once.

- The command gets four fields and no more: the card's name, the reason, the card id and the room. They arrive in
  `ATRIUM_NOTIFY_NAME`, `_REASON`, `_CARD`, `_ROOM` and as one JSON line on stdin, never in argv. A test
  (`TestNotifyCommandNeverSeesWhatACardHolds`) proves a permission's command, a question, a recap and a diff never reach it.
- Never put the token in the command's argv. It is stored and served back by `GET /_hub/notify`.
- The script must not print the token or the URL. The hub keeps the first stderr line.
- Batching and quiet hours are the board's rules: only changes are sent, and nothing is sent while a desktop tab is
  visible.
- Two-way replies are not offered. See `docs/rnd/telegram-notify-design.md`.
