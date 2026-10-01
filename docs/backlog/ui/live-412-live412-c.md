# Live phone test of /m, items 4 and 5 (live412-c)

Target http://127.0.0.1:7778 (hub e7bc886), Chromium emulating 412x891 at 2.625, mobile UA, touch. Card under test:
`live412-scratch`, reached at /m/alias/live412-scratch. Scripts and raw JSON are in D:/tmp/live412/live412-c/.
Screenshots are in docs/backlog/ui/img/live-412/.

Harness notes, not product findings:

- `Input.synthesizeScrollGesture` did not scroll anything in this Chromium, wheel did not either. Raw
  `Input.dispatchTouchEvent` sequences scroll fine and were used for every scroll below (lib.js `swipe`).
- The first tap after a programmatic scroll or a history.back is sometimes eaten by the emulation (touchstart and touchend
  fire, no click). It reproduces on a bare freshly loaded page with no viewer involved (fling.js, mode "none"), so it is
  not attributed to /m. The scripts retap once when no click fired.
- Back is `history.back()`, which is what the Android back button does.

## Item 4, file viewer

PASS 4a markdown: tapped the `long.md` link in a reply. The sheet opened with title long.md, 1 table and 1 code block
rendered, no horizontal overflow (scrollWidth 412 of 412). Touch scroll inside the sheet moved scrollTop to 585.
img/01-viewer-md.jpg, img/02-viewer-md-scrolled.jpg.

PASS 4b text: `notes.txt` opened as a `pre`, 30 lines, title notes.txt. img/03-viewer-txt.jpg.

PASS 4c json: `long.json` (392 lines) opened pretty printed in a `pre`, touch scrolled to 585.
The 1400 character string value does not wrap, the `pre` scrolls sideways inside its own box (scrollWidth 2002 in 380)
and the page itself stays 412 wide. img/04-viewer-json.jpg, img/05-viewer-json-scrolled.jpg.

PASS 4d image: `pic.png` (64x64) opened as an img with naturalWidth 64, clientWidth 64. img/06-viewer-img.jpg.
(The first run said "no such file" because the scratch card had `git rm`ed it for the item 5 delete case. After it was
restored the image loaded.)

PASS 4e unknown type asks first: `renamed.xyzunk` (9 B) showed "Can't preview renamed.xyzunk (9 B). Download?" with
Download and Cancel. No download event fired before the yes. Cancel closed the sheet, still no download. Reopen and
Download produced exactly one download, `renamed.xyzunk`. img/07-viewer-unknown.jpg.

PASS 4f Back keeps the thread scroll: the thread was touch scrolled so the link sat at y=330 (thread scrollTop 1385),
the file opened, the sheet was touch scrolled to 885, then Back (history.back) closed the sheet and the thread scrollTop
was 1385 again. The chevron in the sheet header gave the same result (1385 to 1385, sheet scrolled to 893). Reopening the
file starts at sheet scrollTop 0 both times. The same held for txt, json, image and unknown opened from the thread
(thread scrollTop unchanged). The md case in the opening loop printed a mismatch only because the script read scrollTop
before its own scroll-into-view, the touch scrolled runs above are the reliable ones. img/08-viewer-md-touch-scrolled-browser-back.jpg, img/09-viewer-after-browser-back.jpg,
img/10-viewer-md-touch-scrolled-chevron.jpg, img/11-viewer-after-chevron.jpg.

PASS 4g big text file: `big.txt` (3.0 MB) showed the first 1.0 MB with the note "showing the first 1.0 MB of 3.0 MB"
and a "download the rest" button. Page width stayed 412. img/12-viewer-big-txt-top.jpg,
img/13-viewer-big-txt-cut-note.jpg.

PASS 4h refusals: a path outside the card (C:/Windows/win.ini, opened through `mViewer.open`, read only) said "not in this
card's folder, or not readable" (403). A missing file said "no such file" (404). img/15-viewer-outside-folder.jpg,
img/16-viewer-missing-file.jpg.

FAIL 4i empty text file shows a blank sheet. Repro: tap the `empty2.txt` link (a 0 byte file). Title empty2.txt, body
`pre` with zero text and no note, nothing says the file is empty. The changes sheet does say "empty file, added" for the
same file, so the viewer is inconsistent with it. Severity low. Guess: internal/api/web/m/js/viewer.js `render`, the text
branch builds an empty `pre` and has no empty case. img/14-viewer-empty-txt.jpg.

## Item 5, code review

All against the scratch card. Its worktree held: a committed baseline of c-files, then uncommitted edits (a modified
notes.txt and long.md, a renamed and edited added.txt to added2.txt, a renamed binary, a new empty file, a new file, a
deleted changelog/README.md, a 3 MB big.txt).

PASS 5a chip on a reply: the reply after the Edit and Write tool turn carries the chip "3 files edited", no git numbers
on it, 1 chip in the thread, matches `edited: 3` in /replies. img/20-reply-with-chip.jpg.

FAIL 5b chip on a reply that edited nothing. Repro: the scratch card answered "edited three files" (3 Edit/Write
calls), then I sent a quote and it replied "ok" with no tool calls (the card confirmed "The 'ok' turn called no tools").
The "ok" reply also carries "3 files edited" in the thread (`edited: 3` in GET /replies) and
`GET /changes?turn=2026-10-01T11:24:23.088Z` returns the same 3 files (chip.txt, long.md, notes.txt). The next reply, "No.
The ok turn called no tools.", has no chip, so the carry-over lasted one turn. Not flaky, the data is still there. Severity
medium, a chip that claims edits that turn did not make. Guess: the turn window for the edit count in internal/daemon
(changes.go, the code that sets `edited` on /replies) starts at the last human message and so reaches back over the
previous turn, because my message was the first non-peer message after those edits. img/54-thread-after-send.jpg (the
"ok" reply with the chip).

PASS 5c turn sheet from the chip: one call, `GET /changes?turn=<at>`. Title "Turn 07:21" (device time), subline
"3 files edited +11 -6", rows chip.txt +5 -0 added, long.md +2 -2 modified, notes.txt +4 -4 modified. The API for that
turn gave the same paths, statuses and counts (5+2+4 = 11 added, 0+2+4 = 6 removed). Bar widths match the counts (chip.txt
62.5% add, notes.txt 50% / 50%). The sheet ends at y=840, exactly where the composer starts. img/21-turn-sheet-list.jpg.

FAIL 5d the turn sheet says the same thing twice. For this turn the API gave `partial: true` and `why: "changes made by a
shell command are not included"`. The sheet shows both lines "may miss changes made by commands (sed, generate,
checkout)" and "changes made by a shell command are not included". Severity low. Guess: changes.js `drawList`, the
`d.why !== PARTIAL` check compares against a string the daemon does not send, so it never suppresses.

PASS 5e card "changes" button: opens "Changes" with the toggle (uncommitted pressed, since base not). Uncommitted gave 8 files
and the rows equal the API (paths, status, old_path, +/-): added2.txt "renamed from c-files/added.txt" +1 -1, big.txt
+45000, chip.txt, empty2.txt, long.md, notes.txt, renamed.xyzunk "binary from c-files/blob.xyzunk", changelog/README.md
deleted +0 -7. Since base gave 11 files and again equals the API (the renamed files show as plain added because the
baseline commit was after the base). The toggle switches both ways. img/30-card-changes-uncommitted.jpg,
img/31-card-changes-since-base.jpg.

FAIL 5f bars are unreadable when one file dwarfs the rest. With big.txt at +45000, every other bar is 0.002% to 0.02% wide
(for example added2.txt 0.0022%, notes.txt 0.0089%), so seven of eight rows show no bar. Severity low. Guess: changes.js
`drawList`, widths are linear against the largest file, a log scale or a minimum width would keep them visible.
img/30-card-changes-uncommitted.jpg.

FAIL 5g the cut note reads badly. Text seen: "1 file show counts only (1 files show counts only, because a file over 256 KB or
hunks past 2 MB are not sent)". The first half is built by the sheet and the part in brackets is the daemon's whole `why`
sentence, so the count appears twice and one of them says "1 files". Severity low. Guess: changes.js `drawList` (the
`d.cut` block) plus the `why` text in internal/daemon/changes.go. img/30-card-changes-uncommitted.jpg.

PASS 5h file hunks with word highlights: notes.txt in the turn sheet showed 2 hunks, 4 added and 4 removed rows, with the
changed word marked on both sides ("Note" to "Remark", "line" to "row", "Note" to "Memo"). added2.txt (renamed and edited)
marked "5" and "five CHANGED". long.md marked "item" to "thing" and "filler" to "padding". Header and "‹ All files"
present. img/22-turn-file-hunks.jpg, img/40-file-renamed-edited.jpg, img/45-file-modified-md.jpg.

PASS 5i added, deleted, renamed, empty, binary:
- added with content: chip.txt in the turn list (+5 -0).
- deleted: changelog/README.md showed 1 hunk, 0 added, 7 removed rows, no open button. img/42-file-deleted.jpg.
- renamed: added2.txt "renamed from c-files/added.txt", hunk shown (see 5h).
- empty: empty2.txt says "empty file, added", no hunks. img/41-file-empty-added.jpg.
- binary: renamed.xyzunk says "binary, not shown" with an "open" button, which opens the file viewer over the sheet
  (viewer title renamed.xyzunk, the ask dialog). Back from the viewer returned to the file in the sheet, sheet still
  open. img/43-file-renamed-binary.jpg, img/43-file-renamed-binary-opened-in-viewer.jpg.
- big.txt (+45000, hunks past the cap) is "counts only" in the list, see 5g.

PASS 5j quoting a line: tapping a `-` and a `+` line made two chips in the composer, "notes.txt:5 removed" and
"notes.txt:5", and marked both rows. The sheet shrank to end at the composer (composerTop 788, sheetBottom 788 with the
two chips showing). After send the chips and the row marks were both cleared (chips 0, marked rows 0). img/23-quote-chips-in-composer.jpg, img/52-quote-ready-to-send.jpg.

PASS 5k the quote went to the scratch card only, with a typed note. The sent bubble read:
`review comment on c-files/long.md line 7, removed (uncommitted, head 05a3600):` then `    -| 3 | item3 | 21 |`, then
`review comment on c-files/long.md line 7 (uncommitted, head 05a3600):` then `    +| 3 | thing3 | 21 |`, then the note.
The card replied "ok". The paths are repo relative as designed. img/53-after-send.jpg, img/54-thread-after-send.jpg.

FAIL 5l tapping a quoted line a second time leaves the row marked and flips the next tap. Repro: open any file's hunks in
the changes sheet, tap one line (chip appears, row marked), tap it again. Result: the chip is gone but the row is still
marked (`.cg-row.on` count 1 where 0 is right). A third tap adds the chip back and the row is then unmarked (0 where 1 is
right). Seen twice (notes.txt and long.md, both runs), not flaky. Severity medium, the marks lie about what will be sent.
Guess: changes.js `comment`. `mCompose.addComment` removes the chip and fires `m-comment-removed`, whose handler already
deletes the key from `view.sel` and clears the class, then `comment` checks `view.sel.has(k)`, finds it gone, and adds it
back and marks the row. Fix: have `comment` read the state before calling `addComment`, or leave the row alone when the
event handler ran. img/50-untap-state.jpg (shows the state after the third tap).

PASS 5m back from the sheet: from a file's hunks, Back returned to the file list (title "Turn 07:21"), Back again closed the
sheet and showed the thread.

## Count

PASS 16, FAIL 6, SKIPPED 0.

PASS: 4a 4b 4c 4d 4e 4f 4g 4h, 5a 5c 5e 5h 5i 5j 5k 5m. FAIL: 4i 5b 5d 5f 5g 5l.
