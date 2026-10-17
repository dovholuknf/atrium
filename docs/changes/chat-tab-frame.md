Docs: none, a test-only change. Nothing a reader names changed.

## What and why

`chatTab` failed on the windows-latest board-visual job in five of the last six main runs ("no phone page frame"),
twice both in the shard and alone, and reproduced here in one of three runs under 16 CPU burners. The unit waited for
the `#chat-frame-host iframe[data-card="chat-a"]` element and then polled `page.frames()` for a frame whose URL
matched `/m/?chat=1`. A dump at the failure showed the frame present and loaded, at `/m/`: the phone page's card code
calls `history.replaceState` once the card named in its address is open, and the chat flag is read before that. The
poll only won when it looked between the frame's navigation committing and that rewrite.

The unit now takes the frame from the element with `contentFrame()`, which is the frame whatever its URL, and the
checks that follow wait on its selectors as before. Six of six under 16 burners after, against one failure in three
before. No timeout changed.

## Test plan

## @LETTER@. The chat tab's phone frame is found by its element

### @LETTER@1. Under load

1. Start a dozen busy `node -e 'for(;;){}'` processes.
2. Run `HEADLESS_UNITS=chatTab node scripts/test-board-headless.js` six times.

**Expected:** every run passes.
