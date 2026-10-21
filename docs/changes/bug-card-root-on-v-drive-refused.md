## Test plan

## @LETTER@. A card root behind a denied ancestor

### @LETTER@1. Launch with the card root on a drive the account cannot list

1. Set `card_root` to a folder whose parent the room's account gets access denied on (sg3: `V:/work/localai/atrium/cards`).
2. Launch a worker with `repo` set to a clone on the room.

**Expected:** the card's home is made under the card root and the worker starts. No "not inside the card root" refusal.

### @LETTER@2. Still contained

1. With the same card root, ask for a file path with `..` out of the root, or in a sibling such as `cards-evil`.

**Expected:** answered 403 as before.

### @LETTER@3. Denied under the root

1. Under the card, make a folder the room's account cannot list and put a link in it to a folder outside the card.

**Expected:** a download or upload through that link is refused 403. Only the card root and its ancestors are walked past when denied.

