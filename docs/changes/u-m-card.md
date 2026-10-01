## Test plan

## @LETTER@. The /m card view: working line, own messages, recap sheet

Screenshots: `docs/backlog/ui/img/u-m-card/` (before-mid-turn-390, working-thinking-390, own-messages-390, recap-sheet-390).

### @LETTER@1. The working line
Open a running card on /m at 390 and 412 wide.
**Expected:** a large teal line with a turning spinner and `thinking` sits just above the composer, and nothing about it is in the status chip row. A tool event shows `running <tool>`. A waiting or idle event removes it. After the stream drops and reopens it follows the next event.

### @LETTER@2. Your own messages
Type in the /m box and send.
**Expected:** your text appears at the end of the thread on the right, marked `you`, in time order with the replies. Close the page and open it again: it is still there. The room returns only the session's replies, so these are kept on the device per card.

### @LETTER@3. The recap sheet
Open a card that has a recap.
**Expected:** a `Recap` control with its time is at the top. Tapping it opens a sheet with `from HH:MM`. The close button, the backdrop and Escape close it. A recap older than the card's last turn shows dimmed with `before the last turn`.
