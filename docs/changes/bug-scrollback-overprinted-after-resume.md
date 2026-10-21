Docs: none, a fix that makes the saved scrollback match what the docs already say

## Test plan

## @LETTER@. Scrollback after a restart is not overprinted

### @LETTER@1. Resize, restart, reload the history

1. In a claude terminal, resize the pane narrower, then let the session draw a few hooks or says.
2. Restart the room and open the terminal in a narrower pane than it was saved at.
3. Use "load all in here" on the older output notice and scroll up.

**Expected:** no row has characters of two lines on it, no line is left behind by a repaint, and the text is whole.

### @LETTER@2. What a Restart types

1. Press Restart on a claude card and watch the old terminal.

**Expected:** two Ctrl-D presses and nothing else, "Resume this session with: claude --resume" and the restart marker
follow, and the new session resumes the same conversation. `/clear` appears only after a new-context cycle or when typed.
