This changes `docs/changes/copy-clean-popup.md`: the lookup behind the button reaches past the last ten replies.

`cleanCopy` in `js/terminal.js` still reads `replies?n=10` first. On a miss it reads `replies?all=1`, which answers with
every reply the card's transcripts still hold, oldest first: the current conversation, then the other `.jsonl` files in
the same Claude project folder (what a /clear or an earlier resume id left), newest file first. The room reads each file
from its tail, at most 8 MB and 12 files in all, so a long history costs a fixed amount and its oldest part is what is
left out (`internal/daemon/repliesall.go`). A reply is cut at 16 KB like every other read, and the answer is cached on the
files' sizes and times. The board keeps the answer per card until the card's newest reply changes. Both reads share the
one 800 ms deadline. A card with no readable transcript answers from the screen, which is ignored as before.

Headless unit: `cleanCopy` (older than the tenth, and a prior transcript). Go: `TestRepliesAll*`.

## Test plan

## @LETTER@. Copy clean, older replies

### @LETTER@1. Past the tenth

1. In a card with more than ten replies, scroll the terminal up to a reply older than the tenth newest and select a
   sentence inside it.

**Expected:** the "copy clean" button appears and copies the source text.

### @LETTER@2. Before a /clear

1. Run `/clear` in a card, keeping the old output on screen (scroll up), and select text from the conversation before it.

**Expected:** the button appears when the room still holds that conversation's transcript.
