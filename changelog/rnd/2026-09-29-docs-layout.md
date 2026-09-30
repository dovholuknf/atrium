- `docs/` is sorted into one folder per owning area (rnd, runtime, terminal, ui, fabric, review, release,
  orchestrator), and every path that named a moved doc is fixed. `docs/README.md` says where a new doc goes.
- The backlog is one file per item, `docs/backlog/<dept>/<id>.md`, so adding an item never conflicts.
  `pwsh scripts/backlog-index.ps1` prints the whole list. `docs/backlog-2.md` is now a pointer. docs-layout.
