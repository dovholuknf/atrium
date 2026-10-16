Pictures: `docs/changes/img/u-chat-image-thumbnails/` (`before.png` shows the paths as plain text, `after.png` the
thumbnails). The desktop chat tab is the phone page in an iframe, so one change covers both.

Replies already drew a thumbnail for an absolute path under the card. This adds relative paths with a folder in them, and
the operator's own messages (`MD.own` in `m/js/md.js`), which were plain escaped text. Every fetch goes through
`GET /v1/tasks/{id}/files?path=` with `X-Atrium-Room`, so it works through the hub and over the ziti board. The page
decides "inside the card" by path alone and never probes.

## Test plan

## @LETTER@. Image paths in chat

### @LETTER@1. A path inside the card

1. Open a card's chat on the phone or the desktop chat tab.
2. Send `see <card folder>/.atrium/incoming/<stamp>-image.png`, then `see .atrium/incoming/<stamp>-image.png`.

**Expected:** each message shows the picture under its text, at most 240px wide. The path stays as a link. Tapping the
thumbnail opens it full size.

### @LETTER@2. A path outside the card

1. Send a message naming `D:/somewhere/else.png`.

**Expected:** plain text, no thumbnail and no request for the file.
