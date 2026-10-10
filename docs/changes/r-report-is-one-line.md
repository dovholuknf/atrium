## Test plan

## @LETTER@. A report summary is one line

### @LETTER@1. A long summary is refused

1. As a launched worker, call atrium_report with a summary of two lines, or of more than 50 words.

**Expected:** the call is refused, saying to put the detail in REPORT.md on the branch and resend one line. The card
does not move.

### @LETTER@2. One line is taken

1. Call atrium_report with a one line summary, with or without a trailing newline.

**Expected:** the report is recorded and the launcher is told.
