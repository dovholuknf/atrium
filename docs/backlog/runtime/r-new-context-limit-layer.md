# r-new-context-limit-layer. The per-runner context limit layer

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @runtime.

## What is missing

A file for the @runtime half of `u-new-context-bar-on-rows`: a per-runner context limit layer (store, API and the
three editors). It is named only inside the ui item and has no file of its own.

## Why it is needed

The context figure on the row needs a limit to measure against, and the limit differs per runner.

## Depends on it

- `u-new-context-bar-on-rows`

## Done looks like

- A limit per runner, stored, readable through the API and editable in the three places the board edits runner
  settings.
- A default per runner kind, overridden by the layer.
- The row reads the limit from the API, not a constant in the page.
- Go tests for the store and the API.
