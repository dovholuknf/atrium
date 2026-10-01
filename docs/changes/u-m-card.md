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

### @LETTER@4. Stay at the newest message
Open a card with a long thread on /m, at 390 and 412.
**Expected:** it opens on the newest message. A new reply, your own message, closing the Recap sheet and the keyboard opening all land on the newest. Scroll up on purpose and it stops following and shows `Jump to latest`, which brings you back.

### @LETTER@5. Sending never blocks
Send on /m over a slow link.
**Expected:** the box clears at once and takes the next message while the first is in flight. The thread shows your message as `sending`, then `delivered` or `queued`. If the send fails the text goes back in the box ahead of anything typed since, and the row says `not sent`.

### @LETTER@6. Upload from the card view
Screenshots: `upload-row-390.jpg`, `upload-progress-390.jpg` in `docs/backlog/ui/img/u-m-card/`.
On /m open a card. Tap the paperclip, pick two files, paste an image, then type a line and send while they go up.
**Expected:** each chip shows `uploading N%`. The box frees at once, the thread shows your message as `sending`, and it leaves with the uploaded paths when the files finish. The camera button opens the phone camera.

### @LETTER@7. A failed upload or send keeps everything
Fail an upload, then fail a send.
**Expected:** a failed upload puts your text back and the note names the file and the reason. A failed send puts the text back and the chips come back with their paths, and a chip's X still removes only its own path.
