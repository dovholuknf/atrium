# Launch options: model, effort, extra args and env (backlog-2 item 48)

Status: built, 2026-09-28, sa48, on `claude/launch-model-effort`. See CHANGELOG and `docs/test-plan.md` section BZ.

## The ask

`atrium_launch` (the hub's atrium-control MCP tool), `atrium launch` on the CLI and the board's launch dialog take
what a launch passes to the runner. Two layers:

1. A free-form pass-through. `args` is extra argv for the runner, `env` is extra environment. Used as given. Nothing
   checks them against a list of models, flags or values.
2. Two convenience fields, `model` and `effort`. Each runner's harness row maps them to its own flag or env var. A
   runner with no mapping refuses the launch with the reason. Atrium holds no list of model names or effort levels.

Left out, every one of them means what a launch means today: the runner's own defaults.

Why: an interviewer agent (asks clint questions, sends the answers back, exits) should run on a small model at low
effort. `lean: true` already strips the context. This is the other half of the cost.

## What exists already

- `harness.model_args` (migration 0047) is the model mapping as an argv template with `{model}` in it. The claude
  and codex rows have `["--model", "{model}"]`. A launch naming a model for a runner with no model args is refused,
  and so is a template with no `{model}` in it. See `runnerArgs` in `internal/daemon/launch.go`.
- `task.model` records what a card was launched on. `reopenSaved`, `restartSession` and a relaunch fall back to it,
  so a restart does not move a session back to the default.
- `LaunchRequest.Model` is on `/v1/launch`, the CLI has `--model`, the board dialog has a model field.
- The hub's `atrium_launch` does NOT pass a model. That, and effort, args and env everywhere, is the gap.

## How each runner takes them (checked against the installed binaries, not guessed)

| runner | model | effort |
|---|---|---|
| claude code 2.1.284 | `--model <name>` | `--effort <level>` (`claude --help` lists low, medium, high, xhigh, max) |
| codex-cli 0.156.1 | `--model <name>` (`-m`) | `-c model_reasoning_effort=<level>` (a config override, the key is in the binary) |
| ollama | positional, already in `args` (`run llama3`) | none |

Gemini CLI has `-m` and no effort flag, and has no seeded row. An operator who adds one can map `-m {model}` and
leave effort empty, and an effort asked of it is refused.

## The harness row

Two new mapping fields beside `model_args`, same shape and same refusal rules:

- `effort_args`, an argv template with `{effort}`. Seeded `["--effort", "{effort}"]` for claude and
  `["-c", "model_reasoning_effort={effort}"]` for codex. Empty for ollama.
- `model_env` and `effort_env`, the NAME of an environment variable that carries the value, for a runner that takes
  it that way. Empty on every seeded row. A runner may map a field by args, by env, or both.

A field is mapped when its args template carries the placeholder or its env name is set. Otherwise asking for it is
refused: "`<runner>` has no way to be given an effort. set effort arguments on the runner, using {effort} where the
level goes, or an effort env var". A template set but missing its placeholder is refused as `model_args` is today.

The values are not checked. `effort: "turbo"` reaches claude as `--effort turbo`. Checked on 2.1.284: claude does
not refuse it. It prints "Unknown --effort value 'turbo', ignoring it and using the default effort. Valid values:
low, medium, high, xhigh, max" and runs at the default, so the warning is at the top of the card's terminal and the
card says `turbo`. Atrium still does not keep a list, because the list is claude's and changes with claude.

## The request

`LaunchRequest` gains `Effort string`, `Args []string`, `Env map[string]string`. The hub's `launchInput`, the CLI
(`--effort`, `--arg` repeatable, `--env KEY=VALUE` repeatable) and the older stdio control MCP in
`internal/cli/control_peers.go` pass them through. The board dialog gets an effort text box beside model, and a
harness editor field for effort args. No dropdown of levels.

## Argv order

`base or resume args, model args, effort args, extra args, prompt args`. The same slot model args take now, before
the prompt because a prompt is a bare positional that a flag after it would be read into. Extra args last before the
prompt so they can override an earlier flag where the runner takes the last one.

On a stale resume `Launch` retries with a fresh start built from `h.Args` only. Today that drops the model. The fresh
spec gets model, effort and extra args too.

## Env order

`os.Environ (or prepare), harness env, launch env, mapped model and effort vars, atrium's own vars`. Later layers win.
Three collisions are refused rather than resolved by order, because each would run the session on a value nobody
can see was chosen:

- a launch env key starting `ATRIUM_`, because the atrium block would silently win over it and those vars are how
  the session is identified
- a launch env key equal to the runner's `model_env` or `effort_env` when that field is also asked for, because two
  values were given for one variable
- `model_env` and `effort_env` naming the same variable when a launch asks for both

A launch env key equal to `model_env` with no `model` asked for is allowed: that is the pass-through doing its job.

## On the card

Sticky with respect to the card, one time with respect to the harness, exactly as `task.model` is:

- `task.effort TEXT`, `task.launch_args TEXT` (JSON array), `task.launch_env TEXT` (JSON object).
- `Launch` falls back to the card's values when the request names none, and `reopenSaved` and `restartSession` pass
  them, so a restart brings the session back at the same effort with the same extras.
- An explicitly empty value on a relaunch cannot clear them. That matches model today, and clearing is a later ask if
  anyone makes it.

The card JSON carries `effort`, `launch_args` and `launch_env_keys`. The env VALUES are stored on the room (a restart
needs them) but never sent to the board or the hub: a token passed as env must not land on a screen. The card
details show model, effort, extra args and the env key names. The launched event records the same, keys only. The
room's card JSON is the one the hub's aggregate board and the control MCP both read, so the same three fields are
what the skew check below looks for.

Design review: mercurius session `s_UXDl6haMDE4h` round 1. C1 (env precedence for the mapped vars) folded in above
as the three refusals. A1 (say the aggregate card carries the fields) folded in here.

## Lean

Lean builds `--settings` from the operator's settings.json and keeps its `effortLevel` and `model`. The `--model`
and `--effort` flags are on the command line and override settings in claude, so a lean launch at low effort runs at
low effort. Lean does not touch extra args or env.

## Version skew

The hub's MCP tool may talk to a room older than this change. That room decodes `/v1/launch` into a struct and drops
`effort`, `args` and `env` without a word, which is the silent ignore this design refuses everywhere else. The hub
checks the card the room returns: asked for effort, args or env and the card does not echo them back, the tool
result says so plainly ("the room is older than launch options: effort, args and env were NOT applied, the session
is running on the runner's defaults"). The session is not killed, because an exit is the caller's call.

`model` has been on `/v1/launch` since 0047, so it works against any room of that age.

## Schema

Migration `0065_launch_options`, at the end of the list:

- `ALTER TABLE harness ADD COLUMN effort_args TEXT NOT NULL DEFAULT '[]'`
- `ALTER TABLE harness ADD COLUMN model_env TEXT NOT NULL DEFAULT ''`
- `ALTER TABLE harness ADD COLUMN effort_env TEXT NOT NULL DEFAULT ''`
- `ALTER TABLE task ADD COLUMN effort`, `launch_args`, `launch_env`, defaults `''`, `'[]'`, `'{}'`
- backfill `effort_args` for the `claude` and `codex` rows only where it is still `'[]'`, as 0047 does for model

A duplicate column is already tolerated by `migrate`.

## Room-side and hub-side

- ROOM-SIDE: store, migration, `LaunchRequest`, `runnerArgs`, env assembly, reopen and restart, card JSON, the board
  dialog and harness editor, the CLI's `atrium launch` (it posts to the local room).
- HUB-SIDE: `atrium_launch` in `internal/link/control_mcp.go`, the tool description, the skew check.

Neither half needs the other to be safe: an old room with a new hub gets the skew note, a new room with an old hub
simply never sees the fields.

## Not doing

- No list of models or effort values anywhere, and no validation of either.
- No clearing of a card's sticky effort or extras from a relaunch.
- No per-runner defaults for effort (that is the harness `args` already).
