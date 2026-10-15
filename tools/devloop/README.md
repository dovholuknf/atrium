# devloop

The repeat orchestration chores around atrium, as a separate program. It lives under `tools/` for now and imports
nothing from atrium's `internal/`, so it can move to its own repo. It talks to atrium only over the hub HTTP API
(127.0.0.1:7778, `X-Atrium-Room`), git and ssh. It is not part of the `atrium` binary or `make build`.

```
go build -o build.claude/devloop ./tools/devloop
GOOS=windows go build -o build.claude/devloop.exe ./tools/devloop
go test ./tools/devloop/...
```

Each step prints one line. Any failure exits non-zero.

## land

```
devloop land claude/devloop --item t-devloop
```

In the main checkout (`--repo`, default the current directory, which must be on `claude/main`): refuses a dirty tree,
runs `git fetch hub`, fast-forwards `claude/main` to `hub/<branch>` or cherry-picks what it has that `claude/main`
lacks, prints the new SHA, runs `atrium-deploy.ps1 -Rooms hub` (`--deploy`, default
`C:\Users\claude\.atrium2\scripts\atrium-deploy.ps1`, `--no-deploy` to skip), then marks the backlog item done
(`POST /_hub/backlog/<id>`). A conflict aborts and resets `claude/main` to where it was.

## prune

```
devloop prune claude/devloop --room sg3 --card sg3~01HXYZ
```

Exits the card (`POST /v1/tasks/<id>/exit` with `X-Atrium-Room`), then over ssh removes the branch's worktree and
deletes the branch on the room, then deletes the local branch and the hub branch. A hub refusal is reported, not
retried. `--ssh` names the host when it differs from the room, `--remote-repo` the repository on the room.

## shots

```
devloop shots --room sg3 'C:\tmp\shots'
```

Copies the PNGs in a room's directory to `D:\tmp\shots\<room>\` (`--out` to change). The bytes travel as base64 over
ssh, since scp fails against a PowerShell default shell. `--shell sh` for a room whose shell is not PowerShell.

## usage

```
devloop usage --since 2026-10-15T02:30:00Z --src D:\usage\1015 --out D:\notes --landed landed.json
```

Port of the sg4 `build.ps1`. Reads `usage-<room>-*.jsonl` and `tasks-<room>-*.json` from `--src` (newest by name per
room, rooms from `--rooms`), pages the says from each room over the hub, and writes `says-`, `tokens-` and `cards-`
CSVs plus `summary-*.md` to `--out`. A say is paired with the receiver's first say-caused turn that begins within 10
minutes after it. `--until` closes the window, `--landed` is an optional JSON object of card title to landed sha.
