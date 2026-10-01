## Test plan

## @LETTER@. The /m file viewer

### @LETTER@1. A file opens in the page

1. On `/m`, open a card whose message names a file of the card (a path, or a markdown link such as `[plan](notes/plan.md)`).
2. Tap it. A sheet opens over the thread with the file: markdown rendered, text and code in monospace, json pretty printed,
   an image inline. Nothing downloads.
3. Press Back. The sheet closes and the thread is where it was, at the same scroll.
4. Pinch inside the viewer. The text size changes with the thread's.

### @LETTER@2. Limits and refusals

1. A text file over 1 MB shows its first megabyte and "showing the first 1 MB of N MB" with "download the rest".
2. A type it cannot show (a zip, or a `.log` that is binary) asks "Can't preview X.zip (1.2 MB). Download?" and downloads only on Download.
3. A path outside the card's folder says "not in this card's folder". A missing file says "no such file".
4. Markdown naming another file of the card opens it in turn, and Back steps back through them.
