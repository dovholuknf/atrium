## Test plan

## @LETTER@. Open externally a path outside the card

### @LETTER@1. Off by default

1. In a card's terminal, print an absolute path to a pdf outside every atrium folder and every `open_paths` folder.
2. Hover it, then click it.

**Expected:** the tip says "outside this card, the board cannot open it" and the click opens nothing.

### @LETTER@2. Switched on at the room's machine

1. On the room's machine, open settings and tick "open externally, anywhere on this machine".
2. Click the same path.

**Expected:** a menu offers "open externally, on <room>", and choosing it opens the pdf in the room machine's default
app. The board shows no part of the file.

### @LETTER@3. Never a program

1. With the setting on, print an absolute path to an `.exe` or `.ps1` outside the card and click it.
2. Ask `POST /v1/tasks/<id>/files/launch` with that path.

**Expected:** the click offers nothing, and the request is refused with "atrium never opens a .exe file with the
default app, whatever the settings say".

### @LETTER@4. Not from a share

1. Over a published board, tick the setting.

**Expected:** the save is refused: it is switched from the machine the room runs on.
