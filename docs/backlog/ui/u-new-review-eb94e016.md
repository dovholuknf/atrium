# Review of eb94e016 (u-growler, the growler on the desktop board, the phone board and the phone home)

Reviewed by @review, 2026-09-30, from reading `git diff eb94e016^1 eb94e016`. Board files only
(`internal/api/web/`), so this is a hub-only deploy with no room half. The headless tests are @ui's and were not run
here. 490d0233 (claude/ui-next) is test-script and flake notes only and was not reviewed.

## What holds

- **Everything the hub sends is escaped before it reaches `innerHTML`.** Desktop `growlFull`, `growlRow` and
  `growlDrawPhone` (`internal/api/web/js/growl.js:151-266`) pass `id`, `reason`, `title` and `body` through `esc`.
  The phone's `full` and `render` (`internal/api/web/m/js/growl.js:48-98`) do the same with `U.esc`. The lift
  confirmation escapes the room name inside its `<b>`. The rest is fixed strings and constants.
- **Writes name their room.** Approve, block and lift send `X-Atrium-Room` from the growler's row
  (`growlHeaders`, `growl.js:319`), so on the merged hub view a decision cannot land on another room's request.
  Reply goes through `/v1/tasks/{id}/message` with `when: "done"`, the operator path, keyed by the card id the list
  uses (`room~id` once two rooms are attached).
- **One ring across windows.** `growlSay` returns in a window that knows another has the focus, and the rest claim
  a key in `localStorage` that carries `raised_at` and the reminder count, so a raise or a reminder is said once
  (`growl.js:98-112, 488-503`). A popped-out card rings from its pop-out and not from the board, as @rnd's rules in
  the growler design section 7 ask.
- **The desktop notification still obeys the switches.** The `opts.growl` branch in `alerting.notify`
  (`notify.js:659-668`) asks `notifyHeld`, both focus checks, the mute and the desktop preference before it shows
  anything, and it never toasts, because the growler is already on the board. The ready-alert reminder backoff steps
  aside for a request with a growler (`notify.js:823`), so the two do not both ring.
- **Sticky means sticky.** A growler's notification has its own tag and takes no expiry (`notify.js:1297-1367`),
  and `growlReapNotes` closes it in each browser when the growler leaves.

## Findings

### Low

1. **The once-per-window claim is a read-modify-write on `localStorage`** (`growl.js:103-112`). Two unfocused
   windows handling the same `growls` event can both read the key as absent and both ring. It needs two board
   windows open, neither focused, inside the same few milliseconds. The effect is one extra tone and one extra
   notification that shares a tag, so the OS shows one. Noted, not a change.

## Verdict

**HUB DEPLOY OK eb94e016** on review grounds. No room half.
