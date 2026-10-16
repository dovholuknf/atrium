The "copy clean" button now shows after a mouse selection whatever the copy on select setting. The window mouseup in
`wireCopyOnSelect` (`js/terminal.js`) returned early when `copyOnSelect` was off, so only ctrl-a ever reached
`offerCleanCopy`. It now tracks every press, writes the clipboard only when `copyOnSelect` is on, and always offers the
clean copy. The headless unit `cleanCopy` gains a real mouse drag with `copyOnSelect` off.

## Test plan

## @LETTER@. Copy clean with copy on select off

### @LETTER@1. Drag a reply

1. Turn copy on select off in the terminal menu.
2. Drag over part of a Claude reply with the mouse.

**Expected:** a "copy clean" button appears below the selection and the clipboard is unchanged.
