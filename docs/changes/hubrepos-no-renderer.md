Docs: none, a fix. Nothing a reader can name changed.

## Test plan

## @LETTER@. The repos tab keeps the console quiet

### @LETTER@1. Board events while the repos tab is open

1. Open the board with the browser console showing.
2. Open the repos tab.
3. Let a few board events arrive, such as a session starting or a tool call.

**Expected:** the console shows no "no renderer for the hubrepos view" error. The repos list still loads on open and
on its refresh button.
