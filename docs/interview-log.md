# Interview log

The canonical record of questions clint has been asked and how he answered, across every design interview. Its
job is dedup and organisation: before any doer (or the orchestrator) interviews clint, it reads this file so it
does not re-ask a settled question, and after it interviews him it appends the new questions and answers here.

Rules for anyone adding to this file:

- One entry per interview, newest topics can be appended under a dated heading.
- Record the QUESTION and clint's ANSWER, compressed to the decision. If the answer is captured in more detail in
  a design doc, link the doc rather than duplicating it.
- If a later answer overrides an earlier one, do not delete the old line. Add the new one and mark the old with
  `(SUPERSEDED YYYY-MM-DD, see below)`.
- Keep it skimmable. This is an index of decisions, not a transcript.

## 2026-09-19: OpenZiti and zrok flow (design interview)

Full record: `docs/ziti-zrok-flow-design.md` (and `docs/ziti-zrok-flow-design.mercurius-synopsis.md`). The decisions
below are settled - do NOT re-ask them.

- **What matters most:** reach the hub board from clint's phone and drive any agent. Off-machine reach is overlay
  only. (design doc, "the one distinction")
- **Board stays loopback, always.** No wide board bind, no mTLS on the board. Off-machine reach terminates on this
  machine and hands to the loopback board.
- **One "expose the board" panel: one section per transport, each disabled by default with an enable toggle,**
  styled succinct so the common case is one click. (This is the UX the "expose the board redo" is now building.)
- **Board login is one shared config (OIDC), delegated, verified by atrium.** Guards every enabled transport at
  once. The board has no accounts of its own.
- **OIDC is REQUIRED only for zrok public** (a public URL is reachable by anyone). For zrok private and openziti
  the overlay is the gate, so login is OPTIONAL there and its absence does not refuse the share.
- **Per-transport inputs:** zrok private = enable zrok once (account token) then `zrok access private <token>` on
  the far side; zrok public = same enablement, URL anyone can open, OIDC required; openziti = paste a JWT / path to
  JWT / an enrolled `.json`, plus "what service to bind" (default `atrium`).
- **A public zrok share reserves an `atrium-` name** so its URL and OIDC callback are stable across hub restarts.
  The reserved name is taken before the share starts. Atrium shows the exact redirect URI to register.
- **Any board exposure requires `--shutdown-token`** (a remote request presents as loopback, so loopback can no
  longer be trusted for `atrium stop`).
- **Several transports may be enabled at once**, each toggled independently.
- **Credential posture:** atrium MAY hold/use an identity to JOIN an overlay (it still administers no network); it
  otherwise holds the NAME of a credentialed command, never someone else's credential.

## 2026-09-20: board share auth (follow-up decisions, answered in chat, not a sit-down interview)

Implemented on `claude/board-auth` (merged). Settled - do NOT re-ask.

- **Public zrok share auth = OIDC OR basic (username/password).** The user/password backend is zrok "updb".
- **clint sets the share username/password on the SETTINGS SCREEN** (gear dialog), persisted hub-side, password
  never sent back to the board.
- **A hub public share gets its user/pass from zrok updb only** - the hub binary grows no login system. zrok
  private and openziti need no gate.
- **JWT-enroll-at-join: implement both** the ziti jwt/json enroll and the zrok-private access-token join.

## 2026-09-20: expose-the-board REDO (visual overhaul) - IN PROGRESS

Doer `expose-board-redo`. The functional design is settled above and in `docs/ziti-zrok-flow-design.md`; this
interview is about VISUAL/UX only. New Q&A appended here by that doer as it goes.
