# The restart that never came: a room is never idle for ten seconds

Series: The daemon and the halt. Status: idea. Audience: people running agents unattended.

**Hook.** A HIGH fix waited seven hours because the safe-restart rule needed a quiet room, and a room running ten agents is never quiet.

**Angle.** A rule that is right for safety can be unattainable in practice; the fix was to make the quiet happen on purpose.

**Rests on:** restart_atrium, room deploy hold, hub restart gate, rolling restart design. See `docs/blog/inventory.md`.

## Story beats

1. The rule: restart a room only after ten seconds fully idle.
2. The night of 09-29: directors and ten workers, never idle; the store-halt fix sat waiting.
3. Then a Windows Update reboot took the machine for six hours and one director did not come back.
4. The answer: a deploy hold. One call asks, every agent is told to end its turn, the room redeploys, one line wakes them.
5. Next: a pty host that outlives the room, so a restart need not stop agents at all.

## Screenshots and demos

- the 'end your turn and wait' refusal an agent sees
- the deploy countdown on the board

## Sources

- docs/rnd/room-deploy-hold-design.md
- changelog/runtime/2026-09-30-r-deploy-hold.md
- docs/fabric/hub-restart-gate.md
- docs/rnd/rolling-restart-design.md
- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
- **Check before drafting.** These figures come only from off-repo sources (the factory status, evaluations or log). Confirm each against those files first: seven hours unshipped; six hours lost to the reboot; one director not coming back.
