# Re-read: change lifecycle 398c5454 (HOLD 44dd83df)

Range 45687063^..398c5454, read by @review on m1mini. Only docs/rnd/change-lifecycle-design.md changes. The commits
are unsigned, like every m1mini commit.

## Closed

- **H1:** new section 1.2. Walk facts, rejected-by-clint, the stage-0 go-ahead and red n/a are operator facts. Only
  the daemon writes them, from the operator-authenticated board action, carrying his identity, and `atrium_record`
  refuses those kinds. The folder's files are renders, never read back. L3's acceptance now says a line written into
  walkthrough.txt changes nothing.
- **M1:** tests are listed from every test-file hunk by the language's naming. The session can add to the list,
  never remove from it. It's in L2's acceptance.
- **M2:** the hub matches by sha or patch-id under any ref, and tells card from operator by credential (forge 3.4,
  which exists).
- **M3:** a new wall 2. Rooms hold no forge write credential, `atrium doctor` reports one, and the push setting stays
  off while one is present. It's in L4's acceptance.
- **M4:** the stage is computed at the fetched head, merges are refused, and the facts carry over through tree
  identity plus per-commit patch-id. It's in L5's acceptance.
- **M5:** only `<hub tip>..HEAD` is signed, so a later round is a fast-forward. When clint pushed unsigned first,
  `<branch>-signed` is the default, or an operator delete. Q4 is updated and still held.
- **M6:** red means the named test ran and failed. Passed-on-red, did-not-build and did-not-apply each block and are
  named. The way through is a test that builds against base, or clint's n/a as an operator fact.
- **L1, L2, L3:** new section 2.1, with a per-hunk hash on `--stable` rules, grouped steps reopening whole, and a
  rebase reopen expected.
- **Wall 4:** blocks once per change per stage, recorded on the change.

## New lows (none block)

- **L4:** wall 1 cites "the hub's change index (C5)", but C5 in the change record is one record per change across
  rooms. The commit and patch-id index the pre-receive reads is new work, so name it in L4's row.
- **L5:** a card that squashes the change into one commit makes a new sha and a new patch-id. The match misses it.
  Matching the pushed tree against the open change's head tree would catch it.
  - The residue is small. Wall 2 means a card push to the hub reaches nothing upstream, and an unsigned branch is
    never finished.

Closed: H1, M1, M2, M3, M4, M5, M6, L1, L2, L3 / Open: L4, L5

Verdict: OK 45687063^..398c5454 (doc-ok). It lands by cherry-pick alone.

Quality: every point folded in where it belongs, each with an acceptance that would catch a regression. The
evidence-writer table in 1.2 is the piece the design needed.

## Follow-up e865491b (398c5454..e865491b)

- **L4:** the L4 row now names the hub's open-change index (commits, patch-ids and head tree) as new work, fed from
  the C5 records.
- **L5:** wall 1 also refuses a pushed commit whose tree equals an open change's head tree, which catches the squash.
  The residue, an edit after a squash, is stated, and wall 2 confines it to the hub. L4's acceptance includes the
  squash.

Closed: L4, L5 / Open: none

Verdict: OK 45687063^..e865491b (doc-ok). It lands by cherry-pick alone.
