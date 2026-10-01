## Test plan

## @LETTER@. Fixes from the live phone test

### @LETTER@1. Hide subagents keeps the directors

On /m open the all list and tap "hide subagents". Directors tagged only `atrium:context-ceiling`, any named card that is
not finished, and the orchestrator stay. Unnamed helpers go.

### @LETTER@2. The sound hint never blocks a tap

On a fresh page the "tap to enable sound" pill sits at the bottom edge and passes every tap through. Open /m/docs and tap
the middle of the title filter: it takes focus.

### @LETTER@3. The card top bar fits at every pinch size

Open a card on /m and pinch to the largest size. The bar stays one line with "all cards", "cards", "changes" and "open
terminal" all inside the screen. The buttons stop growing at 15px.

### @LETTER@4. The published documents line shows

Have a card publish a document and open it on /m. The line "published 1 document" shows and opens the list for that card.

### @LETTER@5. Filter pills have a 40px hit area

On the board at phone size a tap 6px above or below a filter pill still hits it, and the pill looks the same.
Run `HEADLESS_ONLY=liveHome node scripts/test-board-headless.js`.
