Screenshots: `docs/changes/img/f-share-doc-zrok-stage1/reader-shared.png` (the reader with the box open) and
`shared-page.png` (what a shared link opens). The live check needs a room with `zrok enable` done and was not run
here. It belongs to claude-sg4.

## Test plan

## @LETTER@. A document can be shared publicly from the reader

### @LETTER@1. Share, copy, stop

1. On a room with a zrok environment, open a markdown file with two relative images in the reader.
2. Click "Share publicly".
3. Click Copy, open the address in a private window or on a phone off the LAN, then click Stop in the reader.

**Expected:** the box shows `https://atrium-<12 characters>.<zrok domain>/` and "public, anyone with this link,
expires in 7 days". The private window shows the document rendered like the reader, with both images. After Stop the
button reads "Share publicly" again and the address stops answering.

### @LETTER@2. One share per document

1. Share a document, reload the reader.
2. Click "Shared".

**Expected:** the button reads "Shared" after the reload, and clicking it opens the box with the same address. Sharing
the same document again never makes a second name.

### @LETTER@3. Nothing else is reachable

1. With a document shared, request `/v1/health`, `/read.html`, `/index.html`, `/f/9` and `/map.json` on its address.

**Expected:** each answers 404. `/doc.md` and `/f/0` answer with the snapshot, with `nosniff`, a CSP of
`default-src 'none'` and `Referrer-Policy: no-referrer`.

### @LETTER@4. What is refused

1. Share a file that holds `-----BEGIN RSA PRIVATE KEY-----`.
2. Share a markdown file that is a symlink to a file outside the card.

**Expected:** both are refused with 403. The first says it is a speed bump, not a guarantee. No folder, row or
reserved name is left behind.

### @LETTER@5. It is a snapshot, and it outlives the card

1. Share a document, then edit the file in the card.
2. Reload the shared address.
3. Delete the card.

**Expected:** the shared page still shows the text as it was when shared, and it keeps answering after the card is
gone. `GET /v1/shares` lists it under `docs` with `card_gone` true.

### @LETTER@6. Restart and expiry

1. Share a document and restart the room.
2. Stop the room, set the row's `expires_at` to the past in `doc_share`, and start it again.

**Expected:** after the first restart the same address answers. After the second the share is gone, its folder under
`docshares/` is deleted and its name is released.

### @LETTER@7. The cap

1. Share 20 documents on one room, then a 21st.

**Expected:** the 21st is refused with a sentence naming the cap of 20.
