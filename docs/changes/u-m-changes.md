## Test plan

## @LETTER@. Code review on /m

### @LETTER@1. The chip on a reply

1. Open a card whose reply edited files. The reply has a chip "N files edited". It carries no +/- numbers.
2. Tap it. A sheet opens over the thread with that turn's files, "N files edited +a -b", and one grey line "may miss
   changes made by commands (sed, generate, checkout)". Edits outside the card's folder are counted, never named.
3. Tap a file. Its hunks show, wrapped, with line numbers (new on + and context, old on -) and the changed words marked.
   A file edited in more than one turn says "all uncommitted edits to this file, not only this turn".
4. A binary file says "binary, not shown" with "open", which goes to the file viewer. A file over the diff bound says
   "+a -b, too large to show here" with "open the file".
5. "All files" returns to the list. Back from the list returns to the thread at its scroll.

### @LETTER@2. Quoting lines

1. Tap a line. A chip such as `card.js:68` appears in the composer. Tap it again, or its X, and it goes.
2. Tap several lines, type, send. One message arrives: for each line `review comment on <repo-relative path> line 68 (turn
   21:14, head 1a2b3c4):` and the diff line indented with its sign (a removed line is `line 55, removed`), then the text.

### @LETTER@3. The card's changes

1. Tap "changes" in the card's top bar. Uncommitted changes show, with a toggle for since base.
2. When the list was cut, a line says how many files are not listed.
3. While the agent runs the sheet says "the agent is still working", and it is read again when the turn ends.
4. A card whose folder is not a git worktree shows the server's sentence and no empty diff.
