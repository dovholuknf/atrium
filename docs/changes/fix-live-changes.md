## Test plan

## @LETTER@. Phone file viewer and code review fixes

### @LETTER@1. An empty file says so

1. On /m, tap a link to a 0 byte text file in a reply.

**Expected:** the sheet says "empty file (0 bytes)" and shows no blank box.

### @LETTER@2. A reply that edited nothing has no chip

1. Have a card edit a file, then send it a message that needs no tools, such as a quote from the changes sheet.

**Expected:** the first reply has the "N files edited" chip and the reply to the message has none.

### @LETTER@3. The turn sheet says a missing shell change once

1. Open the chip on a reply whose turn ran a command.

**Expected:** one line about shell commands, not two.

### @LETTER@4. Small files keep a bar

1. Open the card's changes with one huge file among small ones.

**Expected:** every changed file has a visible bar and the +/- numbers are exact.

### @LETTER@5. The cut note reads once

1. Open changes with more than 400 files or a file over 256 KB.

**Expected:** "N more files not listed, 1 file shows counts only (reason)" with the right plural and the reason once.

### @LETTER@6. Tapping a quoted line again unquotes it

1. In a file's hunks tap a line, then tap it again, then a third time.

**Expected:** chip and mark appear, both go, both come back.
