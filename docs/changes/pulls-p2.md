## Test plan

## @LETTER@. The pulls view

### @LETTER@1. Rows and states

Open the `pulls` tab. Rows are newest first. Each shows repo and number, title, head, author and a state in words: queued, fetching, reviewing with the step name and the cost so far, ready to walk, walking N of M, walked, failed with the step and why, aborted. A ready row also shows its finding counts and the second opinion.

### @LETTER@2. Live

Start a review from the paste box. The row moves through fetching and reviewing to ready without a reload, and the number on the `pulls` tab rises by one when it is ready or failed. Restart the daemon: the list is read again when the board reconnects.

### @LETTER@3. Buttons

`review` on a queued row, `abort` on a running one, `retry` on a failed or aborted one, `log` on a failed one. A refused press says why under the header. A pasted URL no recogniser knows shows the word the daemon gave, and a halted daemon shows the halted sentence.

### @LETTER@4. Walk

On a ready row press `walk`. The findings list with one line each. Press done, skip, defer or open on one and the row's count moves. `launch walker` starts the walker session and the button then reads `walker`.

### @LETTER@5. Alert

With the board behind another window, let a review finish. It rings and notifies like a question does, and a click lands on the `pulls` tab.
