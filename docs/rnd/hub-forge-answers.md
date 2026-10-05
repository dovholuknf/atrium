# Hub forge interview answers (clint)

## Q1. Mirror every repo any card works in automatically?
Answer (2026-10-02): No, wasteful. Not auto, and not a copy made when someone asks either. Clint's model: m1mini is
told "pull sg4's fix/x" and git-fetches from the hub at sg4's name (e.g. sg4.atrium:...). The hub sees the request is
for sg4 and PROXIES it to sg4. The hub is a router, not a store.

Clarification (clint, same day): the hub holds its own `main`. It is where sg4 and m1mini PUSH when a feature is done,
so that the orchestrator or clint can pull, merge, re-sign and push from there. So the hub does store pushed work.
What it does not do is mirror work in progress: that stays on the room and is proxied live.

## Q2. Hub keeps a copy for when the room is offline?
Answer (2026-10-02): No. Room off, asleep or disconnected means the fetch fails ("you're fucked"). No fallback copy.

## Q3. Build order, push or proxy first?
Answer (2026-10-02): Push is core. Rule from clint: the hub is the only place a "finished" feature can be. If it is
only on a room, it is NOT finished. So push to the hub is first-class, not "later".

## Q4. Where does a pushed branch land on the hub (rooms/<room>/<branch>)?
Answer (2026-10-02): Not answered directly. Clint: this should be a configuration option, picked (forced) during hub
install. And the clone model, in his words:
- The operator sets an "scm folder" (his is d:\git). Layout is <scm>/<host>/<owner>/<repo>.
- "Go work on <url>": atrium tries to `git clone` it into the scm folder.
- Private repo and atrium has no credential: the clone FAILS, and the agent is told "that repo doesn't exist, check
  it, and if it is private have the operator clone it, atrium can't". If the operator gave atrium a real key, it works.
- Public repo (e.g. netfoundry/omnigent): the clone just works, into d:\git\github\netfoundry\omnigent.
- The clone has a remote named `hub` next to the usual one. Open: `origin` AND `hub`, or `hub` only. Clint can be
  convinced either way.

## Q5. origin as well as hub?
Answer (2026-10-02): Taken as `origin` AND `hub` (clint said "origin AND hub I suppose", and called a re-ask a repeat).

## Q6. Two rooms push the same branch name?
Answer (2026-10-02): Plain git rules, so B: no force push, a non-fast-forward push is refused. Branches live under
their normal names, not under per-room namespaces.

## Q7. What may a room push to the hub?
Answer (2026-10-02): Must match the operator's preferences. Clint normally allows no pushes at all (does not trust the
agent not to do dumb things), but would allow pushes to the hub repo only. Taken as: a push permission that is a
setting, with the hub as the one allowed target. (Confirming whether "never origin" is the rule.)

## Q8. Rule: cards may push to `hub`, never to `origin`?
Answer (2026-10-02): Yes ("a safe and smart design"). The dotfiles hook keeps refusing everything else.

Note for @rnd: this reverses the design's section 1a rule "the hub never forwards the reader's request to the room"
(it does a fetch-through into its own mirror instead). Whether a pass-through is acceptable given sg4's served-set
rules is still to be settled.
