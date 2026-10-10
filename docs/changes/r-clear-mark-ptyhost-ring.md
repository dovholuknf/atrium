## Test plan

## @LETTER@. The clear mark survives a room restart with the pty host on

### @LETTER@1. Divider after a restart

1. Turn `pty_host` on and start a session card. Let it print a few lines.
2. Run `/clear` in the session so the "context cleared" divider appears, and let it print a few more lines.
3. Restart the room (not the pty host) so it re-adopts the terminal.

**Expected:** the divider is still drawn between the two groups of lines, and the page before it is still in
scrollback. It is not at the end of the output.
