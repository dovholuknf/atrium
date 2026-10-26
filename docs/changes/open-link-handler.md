Docs: none yet, the install steps are in this note

`atrium open-handler install` registers an `atrium-open:` link under the current Windows user's HKCU, and `uninstall`
removes it. In a browser loaded from loopback, for a card whose room is on that machine, open externally asks the room
for a link (`files/launch-link`) and the browser hands it to the handler, which opens the file with that user's default
app. The link carries no path, only a token the room issued for one launchable file, good once and for a minute. The
handler redeems it (`files/launch-link/redeem`), the room judges the file again, and the handler refuses an extension
`files/launch` would. Anywhere else the board still posts to `files/launch`. A link that moves no focus in 3s toasts the install command.

## Test plan

## @LETTER@. Open externally by link

### @LETTER@1. Install and open

1. As the desktop user, run `atrium open-handler install`. In the board's browser console run
   `localStorage.atriumOpenLink = "1"`.
2. Open the board from 127.0.0.1, click a `.png` link in a terminal, choose open externally.

**Expected:** the browser asks to open atrium the first time, then the picture opens in that user's default app.

### @LETTER@2. A bare link does nothing

1. Browse to `atrium-open://open?addr=127.0.0.1%3A7778&id=x&t=guess`.

**Expected:** nothing opens. `open-handler.log` under the user's cache directory says the link is used up or expired.

