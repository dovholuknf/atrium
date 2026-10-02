# Before you contribute: the policy gate

Series: Intake and PR review. Status: idea. Audience: people contributing to open source with agents.

**Hook.** A day of work was done before anyone checked whether the upstream accepts AI contributions or needs a CLA.

**Angle.** Read the project's rules before the clone, not after the build.

**Rests on:** outside-repo contribution gate, change lifecycle stage 0. See `docs/blog/inventory.md`.

## Story beats

1. The test case: a day of building on someone else's repo.
2. What was not checked: license, CLA or DCO, AI policy, toolchains, test OSes.
3. The gate: read them from the forge and tell the human what he signs up for.
4. Stage 0 of the lifecycle; nothing cloned if the policy says no.
5. Signing and DCO at the end.

## Screenshots and demos

- the gate's summary card (mockup)

## Sources

- docs/rnd/change-lifecycle-design.md section 5
- docs/rnd/factory-refactor.md section 3 (held on claude/rnd)
- the factory log of 2026-10-01 and 10-02 (held, off-repo for now; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
- **Check before drafting.** These figures come only from off-repo sources (the factory status, evaluations or log). Confirm each against those files first: a day of work before the check.
