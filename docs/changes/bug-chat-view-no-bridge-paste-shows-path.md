## Test plan

## @LETTER@. The Chat tab's selected row and pasted images

### @LETTER@1. The selected row is bridged

1. Open the Chat tab and click a card in the list.
2. Look at the gap between that row and the conversation pane.

**Expected:** the row is drawn as on the Terminals tab: square on the pane side, with a bridge across the gutter that
joins the pane's border. Switch to the Terminals tab and back, and the bridge is still there on both.

### @LETTER@2. A pasted image shows no path

1. In the Chat tab, paste an image into the message box.
2. Look at the box, then send.

**Expected:** a thumbnail chip with an X sits above the box, the box itself is empty, and send is enabled. The sent
message carries the uploaded file's `.atrium/incoming/...` path. Pressing the X before sending removes the chip and the
path is not sent.
