# Review of b3159548 + f47fc6c4 (@ui /m composer) and c82b960c (@ui sound hint off the phone terminal)

Reviewed by @review, 2026-09-30, read only. `git diff b3159548~1 f47fc6c4` and `git diff e96c1c10 c82b960c`. Hub side.
Board and headless checks are @ui's, and I ran none.

## What holds

- **c82b960c** is two CSS lines. `#sound-hint` is hidden while `body.term-phone` is set, so it no longer covers the
  tray handle. The unlock listeners do not change, so the first touch still turns sound on.
- **b3159548** removes the camera input. The picker keeps `image/*` and `multiple`, so a phone still offers its
  camera from the same sheet. The jump control is now an icon button with an `aria-label`. Nothing new reaches the
  DOM from data.
- **f47fc6c4: the pin is per-page state with clear exits.** `input` in `.mc-box` starts it. It ends on `focusout`
  from the box, on a pending send, and on the jump control. An operator scroll within 1.5 s of a touch moves the
  pin, and any other scroll is put back. `settle`, the ResizeObserver and the `visualViewport` resize all check
  `pinned()` before following. `grow` restores the thread's `scrollTop` in a `finally`, and it finds the thread by
  id, so the terminal's compact bar, which has no `m-card-scroll`, is untouched.

## Findings

### Nit

1. The pin also holds while the box is focused but empty. Type a word, delete it and keep the focus, and new replies
   do not follow the bottom until the box loses focus. Ending the pin when the box is empty would match "while a
   message is being typed".

HUB DEPLOY OK b3159548 f47fc6c4 c82b960c

## Re-read of c3787700 (@ui, nit 1)

`git diff c3787700~1 c3787700 -- internal/api/web`, read only. An input that leaves `.mc-box` empty now calls
`typingOff` and `settle`, so the thread follows again. The next non-empty input pins it again at the current scroll.
Closed. No findings.

Quality: after the Sonnet switch. One line plus the comment, and a test case for it. No drop seen.

HUB DEPLOY OK c3787700
