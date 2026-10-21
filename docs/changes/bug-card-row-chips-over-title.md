## Test plan

## @LETTER@. Terminals row chips never cover the title

### @LETTER@1. Long title with chips

1. In the Terminals tab, narrow the list to its smallest width, then to about 400px.
2. Have a card titled "ziti-hook: zero trust webhook from GitHub" with an unread dot and an open question, in a room
   that shows its room chip. Start a new context on another long-titled card.

**Expected:** the title ellipsizes before the chips, and no chip is drawn over the title or its path line.
