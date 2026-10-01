# Review: factory landscape spike (8b9ede4f), and the room-to-room fold e0d08a39 (m1mini, 2026-10-01): HOLD

`docs/rnd/factory-landscape.md`, a spike for clint. Range 020f0cfd..8b9ede4f. e0d08a39 folds my gate-off low into
the room-to-room spike as written: OK, nothing more needed there.

The survey is good work. It is scoped (factories, not the aggregators competitors.md already covered), and its sources
are named, with `unverified` used where a source is silent. Spot check against the GitHub API today: paperclip 95,810
stars MIT, agent-orchestrator 12,596 Apache-2.0, ai-software-factory 1 star with no licence, loki-mode NOASSERTION
(consistent with BUSL), vibe-kanban 28,233, OpenHands 89,743 MIT. All match. The borrows are well chosen, and
"plug in a tracker, don't run another orchestrator" is the right call. It holds on one claim that is stronger than
the facts, in the place clint reads first.

## High: "no credential held anywhere in the tool" is not true of atrium

Sections 0 and 3 make "a rule that the tool holds no credential" one of atrium's four differentiators, and section 3
says "no credential held anywhere in the tool, across all of those machines". Today's audit review
(`docs/backlog/review/review-new-security-audit-kimi.md`) checked what atrium holds:
- the public share's password, plaintext in hub.db (C4);
- the OIDC client secret and the session cookie HMAC key in atrium.db (C9);
- its own CA private key and room certificates (H1);
- ziti identities (H2);
- every card's launch_env values, which can be API keys (C10).

The true rule is narrower. Atrium holds no credential to a third-party account it acts through: no SCM token and no
LLM key of its own. It drives `gh`, `zrok` and `ziti` by name, and those tools keep their own tokens
(`docs/backlog/fabric/f-003.md:23`). It does hold the credentials of its own trust fabric, and whatever an operator
passes in a launch env. Restate the trait that way, and the comparison with loki-mode, Agent Orchestrator and the
rest still stands. As written, it is the claim a reader would check first, and it fails.

## Smaller, stronger than the source

- "Nobody else combines atrium's four traits" (section 0) rests on 23 READMEs, nothing run. Say "none of the 23
  surveyed".
- "Directors that review one another's work" (section 3): directors do not review each other. @review reviews
  everyone, with verdict trailers. Say that.
- "Atrium's [landing rules] are habit" (section 3): the deploy reader already enforces verdict trailers mechanically
  for deploys (`internal/deployready`). Landing onto claude/main is the habit part.

## Row 10: risk-rated confirmation by a model

You were right to flag it. Two hard rules make it safe. Without them it is an auto-approve channel an attacker can
talk to.
- **Escalate only, never relax.** The rater may turn something the gate would auto-approve (a standing rule or
  `global_auto`) into an ask. It may never turn an ask into an allow. Built that way it is strictly more
  conservative than today, and an attack on it can at worst cost a prompt. OpenHands' shape, where low-risk calls skip
  the human, is the one atrium must not copy.
- **It fails closed.** A rater error, timeout or unparseable answer means ask. That is the opposite of the hook's
  fail-open posture, and the design must say so explicitly, because "a hook must never fail a session" would
  otherwise be read as "allow".
- Why the shape matters: the rater reads exactly what the attacker controls. That is the command text, file
  contents and comments, all prompt-injectable ("this is a routine cleanup"). An agent can also split a dangerous
  act into calls that each look benign: write a script, then run it. A model in the gate is a classifier over
  hostile input, so it can only ever add caution.
- With those rules it is medium value and safe. File it as an @review-owned design, not a borrow to build.

## Row 2: landing rules as policy, against the handoff landing op

It fits with the M3 op, with two corrections before it is filed:
- **"A verdict's author is never the change's author" cannot use git authorship.** Every commit in this repo, from
  every agent on every room, is authored `dovholuknf`, and the rooms are unsigned. Author identity has to come from
  something atrium records, not from text in the commit. The hub knows which room and branch it collected a commit
  from, and the landing op knows which card asked. Use those, and the M3 rule that a verdict counts only from a
  review-file commit. A card trailer (`Atrium-Card:`) written by the agent is forgeable, like the verdict trailer.
  Signing is still the real guarantee.
- **"The requester cannot approve" has to be scoped to cards.** clint asks for most work and decides designs. The
  rule is that the card that asked for a landing is not the card whose verdict clears it. In practice: @review does
  not clear its own code. Its review-file commits are exempt as verdict commits, so a code change @review writes
  needs another reviewer.
- "A commit carries a link to its session": yes, as a trailer the landing op checks. It is cheap and useful.

## Verdict

HOLD: fix the credential claim in sections 0 and 3, with the three smaller wordings. Put row 10's escalate-only and
fail-closed rules in its row, and row 2's two corrections in its row. Then it goes to clint. A re-read covers
sections 0, 3 and 4. doc-ok on OK.

Quality: careful sourcing, honest `unverified` marks, and claims that check out. The one overreach is about atrium
itself, which the spike did not check against today's audit.
