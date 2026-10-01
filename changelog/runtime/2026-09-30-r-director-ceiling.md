- **A card tagged `atrium:context-ceiling` is cycled at `context_ceiling_k` (default 150), mid-turn included.** The
  global `auto_new_context` mode and `auto_new_context_k` do not matter to it, and a card with both is cycled at the
  lower line. Crossing the ceiling on a running card starts the cycle, which asks the card to finish its step, commit
  and end its turn (once at a minute, once at half the limit) before the capture is typed. Pending permission, an open
  dialog, subagents, background work, held messages, the same-directory check and the minimum gap still hold it back,
  and `atrium:no-auto-new-context` still excludes. The wake says the context was cycled at the ceiling and the
  launcher's notice says the card passed it. The ceiling never sits below `context_threshold_k`. Room side.
  (r-director-ceiling)
