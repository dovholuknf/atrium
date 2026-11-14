# heldStrip "an answered row stayed" on macOS CI

The headless unit heldStrip failed twice on macOS CI in PR #102's PR-event run. It did not reproduce here.

The unit failed message now names the rows still drawn, what `permsLocal` and `remoteLocal` hold and the decide POSTs,
so the next failure shows its own cause. No product code and no timeout changed.
