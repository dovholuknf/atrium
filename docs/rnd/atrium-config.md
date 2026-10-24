# How atrium is configured: inventory and decisions

clint's interview of 2026-10-23 on how atrium is configured as a whole. It follows the one-root data directory interview
(backlog `o-atrium-dir-layout-answers`, write-up `docs/rnd/atrium-root-decisions.md` on `claude/atrium-root-decisions`
at d9db5c801) and answers its follow-up: the parameter sprawl. Backlog item: `o-atrium-config-answers`.

Terms used below:

- **Boot config** is what a process needs before it can open its store: where its data lives, what it listens on, how
  it reaches the hub. It is read once at start.
- **Live setting** is a value kept in a store (the room's `setting` table or the hub's `hub_setting` table), changed on
  the board and applied without a restart.
- **Injected env var** is a variable atrium sets for a session it launches, so the session knows who and where it is.
- **Operator env var** is a variable a human sets to change atrium's behaviour.

## Inventory, as of 2026-10-23

### Config files

All under `~/.atrium` (or `<WORKTREE_ROOT>\hub` when that variable is set, `internal/cardroot/cardroot.go:33`).

| File | What it holds |
| --- | --- |
| `room/room.json` | the room's join: hub address, CA fingerprint, transport |
| `daemon.json` | the address file a hook reads to find its room |
| `transports.json` | a room's transport order, beside `room.json` |
| `remote_access.json`, `ssh_targets.json` | room ssh access |
| `lastdb.json`, `reopen.json` | which database was last opened, what to reopen |
| `room.yaml`, `room.lock` | the room setup spec: `work_root`, its layout, tool caches, packs |

### Environment variables

About 25 `ATRIUM_*` names are read outside tests, plus `WORKTREE_ROOT`.

- Injected: `ATRIUM_AGENT_NAME`, `ATRIUM_TASK_ID`, `ATRIUM_ROOM`, `ATRIUM_LOCATION`, `ATRIUM_HUB_URL`,
  `ATRIUM_BOARD_URL`, `ATRIUM_PERM_GATE`, `ATRIUM_RUNNER`, `ATRIUM_GUARD`, `ATRIUM_HUB_MCP`.
- Operator: `WORKTREE_ROOT` (moves the state dir and the card root), `ATRIUM_SHARED_LOCATION`, `ATRIUM_PERM_TIMEOUT`,
  `ATRIUM_PERM_PROBE_TIMEOUT`, `ATRIUM_ROLEBIN`, `ATRIUM_SHUTDOWN_TOKEN`.
- Test and debug, read by shipped code: `ATRIUM_LAUNCH_WAIT`, `ATRIUM_BACKLOG_BOARD`, `ATRIUM_PRIORITY`,
  `ATRIUM_TAP_DIR`. Many more `ATRIUM_TEST_*`, `ATRIUM_CONPTY_*` and `ATRIUM_DEBUG_*` are read only by tests and spikes.

Only `WORKTREE_ROOT` did real configuration work. The rest were test hooks, debug switches or legacy.

### Flags on the long-running commands

- `atrium run` (hub), about 20: `--atrium-dir`, `--atrium-db`, `--builds`, `--addr`, `--link`, `--link-advertise`,
  `--transport`, `--atrium-service`, `--atrium-identity`, `--board-transport`, `--board-service`, `--board-share`,
  `--ci-hook-service`, `--board`, `--board-views`, `--no-room`, `--isolated`, `--open`, and the room's `--dir`, `--db`,
  `--http`, `--agent` repeated.
- `atrium room`, about 15: `--dir`, `--db`, `--http`, `--agent`, `--identity`, `--mtls`, `--zrok-private`,
  `--openziti`, `--service`, `--no-run`, `--accept-upgrades`, `--detach`, `--isolated`, `--started-by`,
  `--restart-after`.
- `atrium ptyhost`, internal: `--state-dir`, `--build`, `--idle-exit`.
- `atrium daemon`, the old single process: `--addr`, `--http`, `--db`, `--long-poll`, `--shutdown-token`,
  `--location-file`, `--board-dir`.

Defaults are in `internal/cli/atrium_defaults.go`: board 7778, link 7779, room board 7781, agents 7777.

### Live settings

94 names are defined in code.

- Room (`internal/store`, `internal/api`, `internal/gitsync`, `internal/daemon`): `archive_workers_after`,
  `browse_roots`, `cache_cold_handoff`, `cache_keepalive_default`, `cache_keepalive_suspended`, `card_root`,
  `card_root_required`, `context_handoff_dir`, `context_limits`, `deploy_hold_max`, `editor_command`,
  `escalate_held_after`, `escalate_turn_after`, `event_cold_kinds`, `event_sink`, `event_window_bytes`,
  `git.credential_helper`, `git.credential_hosts`, `git.scm_root`, `git_push`, `git_root`, `global_auto`,
  `idle_park_after`, `input_lag_log`, `landed_reclaim_grace`, `lean_worker_gateway`, `merged_cull_grace`,
  `overlay_ziti`, `overlay_zrok`, `auth_oidc`, `paste_keep`, `paste_preamble`, `ports.card_range`, `prune_after`,
  `pty_host`, `replay_mode`, `restart_wrap_wait_s`, `reviews_root`, `room_hold`, `runner_priority`, `scratch_root`,
  `scrollback_lines`, `scrollback_mb`, `shared_location`, `shell_command`, `sweep_dead_after`, `tenant`,
  `terminal_command`, `terminal_min_cols`, `turn_cause_limits`, `typed_unread_after`, `unexpected_exit_wake`,
  `usage_cache_reads`, `usage_week_reset`, `worker_policy`, `worktree_hooks_dir`, `board_skin`.
- Hub (`internal/hubstore`, `internal/link`): `board_auto`, `board_skin`, `ci_hook.*`, `deploy_binary`, `deploy_owner`,
  `deploy_script`, `doc_debt.*`, `docs_settings`, `extra_hosts`, `files_settings`, `flow_repo`, `forge.entries`,
  `git.create_on_push`, `git.stash_retention_days`, `git.store`, `git_repos`, `launch_caps`, `overlay_legacy`,
  `recognisers.rows`, `room_transport_order`, `share_auth`, `share_oidc`, `share_pass`, `share_user`.
- Also in the same tables, but state, not settings: `auth_cookie_key`, `auth_session_gen`, `room_stopped_at`,
  `room_viewport`, `runner_latest`, `recognisers.seeded`, `recognisers.backfilled`, `work_ledger_backfilled_at`,
  `alias_default_backfill`, `card_actions_seeded`, and per-card rows `owed:`, `owed_closed:`, `owed_ask:`,
  `owed_reported:`, `park_handoff:`, `notices_read:`.

On sg4's room the `setting` table held 380 rows. About 350 were per-card state rows, about 30 were names from the list
above, and about 15 were values clint chose: `git.scm_root`, `global_auto`, `pty_host`, `board_skin`, `editor_command`,
`scrollback_lines`, `scrollback_mb`, `idle_park_after`, `merged_cull_grace`, `cache_cold_handoff`, `context_limits`,
`card_colors`, `replay_mode`, `overlay_ziti`, `overlay_zrok`, `auth_oidc`.

## Decisions

The guiding answer across all ten: remove as much configuration as possible, keep what has actually been used, and
make a fresh install just work with defaults that suit one room or three.

1. **Two kinds of setting.** Boot config lives in the config file and needs a restart. Live settings live in a store
   and change on the board. A setting never moves between the two.
2. **One operator env var: `ATRIUM_CONFIG`.** Removed: `WORKTREE_ROOT` (replaced by `data_dir`),
   `ATRIUM_SHARED_LOCATION`, `ATRIUM_PERM_TIMEOUT`, `ATRIUM_PERM_PROBE_TIMEOUT` (fixed internal values),
   `ATRIUM_ROLEBIN`, `ATRIUM_SHUTDOWN_TOKEN`, and the test and debug ones read by shipped code (`ATRIUM_LAUNCH_WAIT`,
   `ATRIUM_BACKLOG_BOARD`, `ATRIUM_PRIORITY`, `ATRIUM_TAP_DIR`). Tests use hooks in code instead. Injected vars stay:
   they are atrium talking to its own sessions.
3. **One config flag: `--config`.** Every path, address, transport and board flag becomes a config file key. Flags
   that describe one invocation stay (`--no-room`, `--detach`, `--open`, the join flags on `atrium room join`), and so
   do the internal ones a room passes to the pty host. `atrium run` stops repeating the room's flags. The one-root
   decision that path flags (`--db`, `--dir`, `--location-file`) still win is withdrawn.
4. **Every working folder derives from `data_dir`.** Card homes, clones, reviews, scratch, handoff and caches each
   default to a fixed subfolder with a clean name. The path settings already in use (`git.scm_root`, `browse_roots`)
   stay overridable. `room.yaml`'s `work_root` and layout fold into these defaults.
5. **Prune the live settings, by audit.** An agent lists each of the 94 names as keep, constant or default-flip (a
   default changes to what clint always sets, such as `pty_host` on or `idle_park_after` off). clint approves the list
   once before anything is deleted. State rows move out of the settings tables into their own tables, so "settings"
   means only what a human chose.
6. **The hub holds fleet defaults, a room may override.** A behaviour setting such as `global_auto`, `idle_park_after`
   or scrollback is set once on the hub and applies to every room unless the room sets its own.
7. **One chain: card > room > hub > default.** The most specific level with a value wins. A room caches the hub's
   values and keeps them while the hub is down. The board shows each value's source (default, hub, room or card).
   Clearing a value at any level falls back to the next level. Boot config is outside this chain.
8. **A fresh install needs no file and refuses nothing.** With no config file, `data_dir` is `~/.atrium` in the home of
   the account atrium runs as, on every OS, Windows and C: included. The hub uses `<data_dir>/hub` and the room uses
   `<data_dir>/room/<name>`. The card-home refusal on Windows (`internal/cardroot/cardroot.go:19`) goes. Keeping agent
   work off C: becomes a `data_dir` choice and is not enforced. `~/.atrium/config.json` may hold a `hub` and a `room`
   section, so one machine running both needs one file. A second room on a machine uses `--config`. A read-only
   `atrium config` prints effective values and where each came from.
9. **`atrium daemon` is retired**, with its flags and `--shutdown-token`.
10. **Who can change what stays open, as today.** The board, `atrium room settings` and the loopback API keep working
    for anyone on the machine, agents included. No new guard against an agent changing a setting.

## What this changes in the one-root design

- The config file (decisions 1 and 2 of the one-root write-up) stands, and it now carries every boot value, not only
  `data_dir` and ports. Its lookup order is unchanged: `~/.atrium/config.json`, then `ATRIUM_CONFIG`, then `--config`.
- "One config per process" becomes one default file with a `hub` and a `room` section. A separate file is still the
  way to run a second room, or a hub and a room as different accounts.
- "A flag that names one path still wins" is dropped. `--config` is the only flag that sets configuration.
- `WORKTREE_ROOT` is removed rather than kept beside `data_dir`, so the trial's new atrium sees nothing from the old
  one's environment.
- The fixed tree under the data dir becomes the default, not only an operator convention: `<data_dir>/hub` and
  `<data_dir>/room/<name>`, with each working folder derived from them.
- Build tooling under the room's data dir (one-root decision 8) is the `cache` folder of the derived layout.
- Settings carried nowhere: the fresh hub (one-root decisions 5 and 6) starts with the pruned settings list from
  decision 5 and the fleet defaults from decision 6, not a copy of the old hub's settings.

## Follow-ups

- The settings audit of decision 5, as its own backlog item, before the config work is built.
- Moving state rows out of `setting` and `hub_setting` into their own tables (a new migration, not an edit).
- Rewrite `.uncommitted/atrium-root.md` against both write-ups.
