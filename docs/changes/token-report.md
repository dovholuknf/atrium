## Test plan

## @LETTER@. The token report rebuilds the baseline

### @LETTER@1. Build over the saved inputs

1. Put the 2026-10-15 `tasks-*.json` and `usage-*.jsonl` exports in a folder.
2. Run `pwsh scripts/token-report/build.ps1 -Start 2026-10-15T02:30:00Z -End 2026-10-15T10:45:00Z -InDir <folder>
   -OutDir <out>`.
3. The three CSVs match `docs/reports/token-efficiency-2026-10-15/` byte for byte, apart from says from a room that
   no longer answers `/v1/says`.

### @LETTER@2. Fetch and build in one command

1. With a hub on 127.0.0.1:7778, run `pwsh scripts/token-report/run.ps1` with a window, an `-InDir` and an `-OutDir`.
2. `tasks-<room>-*.json` and `usage-<room>-*.jsonl` appear for every online room, and the CSVs are written.
