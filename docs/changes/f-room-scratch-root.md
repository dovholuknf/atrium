## Test plan

## @LETTER@. A room names where cards put throwaway files

### @LETTER@1. Set the root

1. Open the gear and set "Scratch root" to an existing folder on this machine.
2. Try a relative path, then a folder that does not exist.

**Expected:** the folder saves. The relative path and the missing folder are refused with the reason.

### @LETTER@2. A card is told

1. Launch a card. In its terminal run `echo $env:ATRIUM_SCRATCH`.
2. Ask the agent where throwaway files go.

**Expected:** the variable names a folder under the root, it exists, and the agent names that folder.

### @LETTER@3. A joined card is told

1. In a session atrium did not start, run `atrium join`.

**Expected:** the output names a scratch folder under the root.

### @LETTER@4. The details panel

1. Write a file in the card's scratch folder. Open the card's details and click the scratch row.
2. Download the file.

**Expected:** the folder and the file are listed and the file downloads. A path outside the folder answers 403.

### @LETTER@5. Cull

1. Cull the card.

**Expected:** the scratch folder atrium made is gone. A folder atrium did not make is left alone.
