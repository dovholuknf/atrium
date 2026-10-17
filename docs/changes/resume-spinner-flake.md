Docs: none, a fix with no new verb, flag, endpoint, setting or tab. `docs/troubleshooting-tests.md` is new and is
about the method, not the product.

## Test plan

## @LETTER@. A resume keeps its pane and its line until the first output

### @LETTER@1. Resume right after a reload

1. Open the board, go to the Terminals tab, and within half a second of the page loading resume a card that has a
   conversation but no runner (the stack row's menu, "resume the last conversation").
2. Watch the pane while the runner starts.

**Expected:** the pane stays up and says "opening the conversation" until the runner's first output, then the line
and the card's chip go. The pane is never torn down and re-opened, and the row never loses its chip.

### @LETTER@2. The remembered session comes back on its own

1. Attach a terminal, reload the board, and leave it alone.

**Expected:** the pane says "waiting for ... to come back" and then attaches, exactly as before. The restore still
clears its own line.

### @LETTER@3. The headless unit

1. Run the unit twenty times:

```powershell
$env:HEADLESS_UNITS='resumeSpinner'
for ($i=1; $i -le 20; $i++) { node scripts/test-board-headless.js | Out-Null; if ($LASTEXITCODE) { "FAIL" } }
```

**Expected:** no FAIL, with or without other load on the machine.
