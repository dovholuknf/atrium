# "N owed" chip on the launcher's card

From @runtime (owed answers, claude/r-owed-answers 6b8ae15f). Task rows now carry `owed`, `owed_since` and
`owed_no_launcher`. The board shows a chip on the launcher's card: "N owed", amber once the oldest has been open 10
minutes. No JS from the room side.

State: filed, NOT started. Held by the pause (not scm); starts when the orchestrator releases it or asks. Lands through
@review once built, desktop card and /m card row, 4.5:1 on every skin, headless section with mutants (count, the 10
minute amber edge, owed_no_launcher shown on the right card or not at all, zero hides the chip).

Also from @runtime: a reporting agent-launched worker with no launcher is now held for the orchestrator (source
report-no-launcher), and reported_at is stamped only when a notice was queued. The /m "report waiting" label for an
agent-launched card should stop after the launcherFor fallback follow-up; nothing for the board to do yet.
