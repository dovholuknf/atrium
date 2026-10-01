# Live phone test 412x891, tester live412-d, items 6 and 7

Target http://127.0.0.1:7778 (the running hub), Playwright Chromium, 412x891 at 2.625, isMobile, hasTouch, Android
Chrome UA. Taps are real touchscreen taps. Scripts are kept in D:/tmp/live412/live412-d/. Screenshots are in
docs/backlog/ui/img/live-412/.

## Item 6, hub docs

PASS 6a docs list: /m/docs shows the three published docs (Factory eval 2026-09-30, 2026-09-29, Usage 2026-09-29 to 30)
and "33 KB of 2048 MB used". No horizontal overflow (scrollWidth 412). img/live-412/60-docs-list.jpg

FAIL 6b title filter is blocked by the "tap to enable sound" hint: see finding 1. Once the input is tapped at its left
edge, "usage" leaves one row (61-docs-filter-usage.jpg), "zzzz" says "no document has that in its title"
(62-docs-filter-none.jpg), and clearing restores all three. The filter itself works.

PASS 6c the three docs render: each opens at /d/<slug>, markdown renders, tables included (5 tables in the usage doc, 1 in
each eval). Wide tables (748, 720, 1012, 425, 643 px) sit in a wrapper with overflow-x auto, so the page never widens
(docW stays 412). Screenshots 63-doc-usage-2026-09-29-to-30.jpg, 63-doc-factory-eval-2026-09-29.jpg,
63-doc-factory-eval-2026-09-30.jpg. In the eval doc the table rows are tall with empty space under the short cells
(about 105 px rows), cosmetic only.

PASS 6d upload (multipart): title "test-d upload", file test-d-doc.md through the real file chooser. Status "uploaded open
it", slug test-d-upload appears in GET /_hub/docs. 64-upload-done.jpg, 65-test-doc-v1.jpg

PASS 6e edit, history and text diff: "new version" with a changed file made v2. History lists v2 (96 B) over v1 (72 B).
"compare" v1 to v2 shows `-line two`, `+line two CHANGED`, `+line four ADDED` in red and green and a strip "2 unchanged
lines". Picking v1 in history opens /d/test-d-upload@1. 66-test-doc-v2.jpg, 67-history.jpg, 68-compare-diff.jpg,
69-version1-view.jpg

PASS 6f delete: delete asks "delete this document?", "yes, delete" shows the banner "deleted on ... by operator" with
restore. 70-delete-confirm.jpg, 71-deleted-banner.jpg. Note: this is a tombstone. The docs list without the "deleted"
chip is clean of test- slugs (checked after each delete). The two test docs still show under the "deleted" chip
(82-deleted-view.jpg) and cannot be purged from the board (purge is operator only). They are test-d-upload and
test-d-card-doc.

FAIL 6g "published N documents" on a card: see finding 2. The scratch card published test-d-card-doc (hub confirms
origin card, `claude-sg4~01a0f722-...`) and the card page never shows the line. 78-card-published-line.jpg
After the failure I opened the doc from /m/docs: its origin line reads "live412-scratch-scratch-card-for-phone-t@claude-sg4"
and the doc deleted cleanly (81-card-doc-in-docs.jpg).

PASS 6h touch targets on /m/docs: input 392x40, chips and upload 40 px high, rows 56 to 73 px high.

## Item 7, desktop board at 412x891

Opened `/` with sessionStorage atrium.m.desktop=1 (the desktop opt-out). Body overflow none (scrollWidth 412).

PASS 7a header does not overflow: the header is one 55 px row (stack, board, terminals, perms, bell, toggle). With the
toggle open it is 311 px high and every control has right <= 412. 72-desktop-412.jpg, 73-header-open.jpg

PASS 7b filters button: the "filters" button (65x40) sits next to the search box with "27 of 195". It opens a panel with
status chips (all 195, ready 27, running 5, finished 162, shelved 0), sort chips, scope chips and group chips, all inside
the 412 px width, nothing clipped (every scrollWidth equals clientWidth). 74-filters-open.jpg

FAIL 7c touch size of the filter chips: see finding 3 (low).

FAIL 7d deploy-ready pill never appears: see finding 4 (high-ish). 75-header-pill.jpg
SKIPPED 7e reading the live deploy report in the dialog: there is no report because the GET never answers, so the
real dialog content (blocking commits, notes) could not be reached. Deploy was never clicked, 0 POSTs to
/_hub/deploy-ready/deploy were sent.

PASS 7f deploy dialog with no report, opened by calling openDeployReady() because the pill is hidden: the dialog is 366
px wide inside the 412 viewport, says "no report: not read yet", the Deploy button (78x33) is greyed with the reason
"no report", close (62x31) closes it. 76-deploy-dialog-forced.jpg. See finding 5 for the missing title.

## Findings

1. FAIL, medium, "tap to enable sound" hint covers the docs filter. Repro: open /m/docs on a fresh load. The hint
   (#m-sound-hint, 145x36 at x 133 y 56) sits over the filter input (392x40 at x 10 y 53), so a tap in the middle of
   the field hits the hint. Playwright's tap on `.d-q` timed out with "m-sound-hint intercepts pointer events". Tapping the
   field at x+20 works. Guess: the hint's z-index and position in m/css against the docs sheet in m/docs.css.
2. FAIL, medium, "published N documents" never shows. Repro: have a card publish a doc, open /m/alias/<card>. The
   board asks GET /_hub/docs?card=01a0f722-373b-7bb4-a427-08762974d6ab (the bare id, `openId`) and gets `{"docs":[]}`.
   The doc is stored with card `claude-sg4~01a0f722-373b-7bb4-a427-08762974d6ab` and the same query with that id returns
   it. So #m-card-docs stays hidden. Guess: paintCard in m/js/docs.js (called from card.js:634) must send the
   room-qualified id, or the hub's card filter in internal/link/docs_api.go must match the id after `~`.
3. FAIL, low, filter chips are 28 to 29 px high on the desktop board at phone size (all, ready, running, finished, status,
   name, by project, off). They are tappable but under the 40 to 44 px the header buttons use. Guess: css/ filter chip rule.
4. FAIL, high, GET /_hub/deploy-ready never answers. Repro: `curl -m 100 http://127.0.0.1:7778/_hub/deploy-ready`
   returned nothing for 100 s, again for 60 s, and an earlier call ran past 120 s. /_hub/docs on the same hub answers in
   3 ms. The board waited 170 s and the pill stayed hidden, so the deploy pill and the real report are unreachable.
   Reproduced 4 times, not flaky. Guess: readyReport in internal/link/deployready.go holds st.mu across the whole git
   pass (bound 60 s) so every board and request queues behind one slow pass, and several boards plus testers on a
   loaded machine keep it busy. The board has no timeout on plainFetch in js/deployready.js either.
5. FAIL, low, the deploy dialog header shows only the close button. The "the hub" eyebrow and the "deploy" title are not
   in the rendered text (dialog innerText is "close, no report: not read yet, Deploy, no report"). Seen once, in the
   no-report state, not re-run. Guess: .dlg-head in the dialogs css at this width, or the h2 hidden.
