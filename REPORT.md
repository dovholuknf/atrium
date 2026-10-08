# f-paste-least-busy-scm

Design and test plan are in docs/changes/f-paste-least-busy-scm.md.

- Room: `no_scm_root` 422 from open, pr-worktree and scratch (internal/api/openkinds.go, prworktree.go, open.go).
- Hub: `retryDeaf` (internal/link/recogniseroute.go) hands such a paste to the next least busy room and, when none is
  left, answers one message naming each room. pr-worktree is now a placed paste too, and its claim moves with it.
- Default: `gitsync.EffectiveSCMRoot`, derived on each read (scm clone, scratch root, sweep), logged once.
- Provisioning: `provision-room.ps1` sets scm_root, `atrium room set|get scm_root` added.
- Twice: busyWhile in js/core.js left the old refusal line outside the scope it cleared. Fixed. Not tested in the
  headless board suite (not run here).
- Tests: internal/link/noscm_test.go, internal/api/noscm_test.go, internal/gitsync/scmroot_test.go. link, api, gitsync
  and cli pass 3 times with -count=1.
- internal/daemon: TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP fails (a worker prompt in the fork's argv, not
  touched here). Everything else there passed.
- An existing test (TestOpenATicketWithNoRepoAndNoSCMRootSaysWhy) wrote into the real ~/git/scratch once the default
  existed. It now sets HOME to a temp folder. I removed the empty folders it left on this machine.
