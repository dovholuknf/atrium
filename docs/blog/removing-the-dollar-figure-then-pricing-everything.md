# Removing the dollar figure, then pricing everything

Series: Cards, not agents. Status: idea. Audience: people tracking LLM spend.

**Hook.** The board stopped showing cost the day before a cost study became the factory's main design input.

**Angle.** Cost on every card was noise to the operator; cost per landed item was the number that changed decisions.

**Rests on:** usage tab, usage backfill, factory-shape cost study. See `docs/blog/inventory.md`.

## Story beats

1. Usage rows priced and shown per card.
2. 09-29: the dollar figure judged not worth having, and hidden.
3. 09-30: a study of $1,202 over 41 hours, written in dollars.
4. What each view is for: burn and limits live, cost per item after the fact.
5. Cache reads hidden by default, because they swamp the chart.

## Screenshots and demos

- the usage tab's burn chart
- the flameout projection

## Sources

- commit 0bbd2caf
- docs/rnd/usage-tab-design.md
- docs/rnd/factory-shape.md
- changelog/runtime/2026-09-29-r-015-backfill.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
