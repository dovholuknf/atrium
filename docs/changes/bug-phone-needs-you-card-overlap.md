# bug-phone-needs-you-card-overlap

On the phone board (`/m/`), Needs you showed a hub notice ("The hub cannot use bb for bitbucket.org") with every
"asked a question" block drawn over it, text overprinting text.

## Cause

`#m-growl` is a flex column capped at half the window, scrolling past that. Its children, `.gm-full`, were
`flex: 0 1 auto; min-height: 0`, so with nine of them open the container squeezed every one below its own content
height, and the content overflowed onto the next. Nothing was positioned absolutely.

## Fix

`internal/api/web/m/m.css`: the growlers, the strip, the note and the undo line are `flex: 0 0 auto`. They keep their
height and the strip scrolls.

## Proof

Headless unit `mGrowlStack` (412x892): one hub notice and eight questions, expanded. It asserts no growler spills past
its own box and none starts above the bottom of the one before. It failed before the fix with all nine spilling.

Before and after: `docs/changes/img/bug-phone-needs-you-card-overlap/phone-before.png` and `phone-after.png`.

## Test plan

## @LETTER@. Phone growlers stack without overlapping

### @LETTER@1. A hub notice and eight questions

1. Run `HEADLESS_UNITS=mGrowlStack node scripts/test-board-headless.js`.
2. It prints `mGrowlStack ok`.
3. On a phone at `/m/` with a hub notice and several open questions, tap "+N more", and scroll the strip: every block
   reads cleanly, none overprints another.
