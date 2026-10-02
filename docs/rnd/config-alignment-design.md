# Config alignment: one config for every session atrium starts, owned by the hub, rolled out to every room

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint through the orchestrator, 2026-10-02 late. His
question: could atrium own and start sessions with their own config, control them in one way, and roll that out
across rooms?

**The trigger.**
- @ui published a claude.ai artifact from m1mini.
- sg4's settings deny the Artifact tool. m1mini and sg3 had no deny, because each room has its own
  `~/.claude/settings.json`.
- On sg4 that file is a symlink into clint's dotfiles repo (`docs/backlog/fabric/f-006.md`). On the other rooms it
  is kept by hand.
- The orchestrator patched both rooms by hand that day: deny `Artifact` and `Artifact(*)`, `enableArtifact: false`,
  with a backup kept next to each file.

**Questions are held in section 8, and section 9 is the interview plan.** @fabric is asked to take the rollout and
drift half (sections 4 and 5).

Read for this design, 2026-10-02:
- **Launch.** `internal/daemon/launch.go`, where `runnerArgsWith` (252) builds args and `childEnvFrom` (1080-1152)
  builds env.
- **Runner rows.** `internal/store/harness.go`.
- **Lean workers and the keep-alive fork.**
  - lean: `internal/daemon/lean.go` 315-326 and 402, `lean_agents.go`;
  - keep-alive fork: `internal/daemon/keepalive.go` 820-872 (`forkCarried`).
- **Hooks.** `internal/daemon/a2a.go` 229-237 (the Stop hook), and `internal/claudeconf/hooks.go` 213 and
  `target.go` 161.
- **Room checks and provisioning.**
  - preflight: `internal/daemon/preflight.go`;
  - requirements: `atrium.requirements.yaml`, `internal/requirements`, `docs/fabric/room-requirements-design.md`;
  - provisioning: `scripts/provision-room.ps1` (its mcp, statusline and gate steps).
- **r-033.** `docs/backlog/runtime/r-033.md` (WON'T DO).
- **The vendors' docs.**
  - Claude Code: settings, managed settings, the CLI reference and artifacts, at code.claude.com/docs/en/;
  - codex: config, CODEX_HOME, `requirements.toml`;
  - opencode: its config layers and `OPENCODE_CONFIG_CONTENT`.

  The facts marked **(verify)** in this doc are taken from those docs and have not yet been run on a room.

## 0. The answer

- **One bundle, owned by the hub, versioned by git.**
  - The config lives in one hub repo, `atrium-config`, in the hub's git store (hub forge 3.1). Its `main` moves only
    by an operator push, under the hub's existing rule that a card cannot push `main`.
  - A revision is that repo's commit sha.
  - It holds everything clint listed:
    - permission deny, allow and ask lists;
    - hooks and the statusLine;
    - the org's CLAUDE.md, agents and skills;
    - MCP servers;
    - model defaults;
    - atrium's own standing gate rules.

  Each part is written once, with a per-room overlay only where a room really differs.
- **Layer it on at launch, never edit the user's files.** This is mechanism (b) from the brief, for all three
  runners:
  - **claude:** `--settings <bundle>/claude/settings.json`, `--plugin-dir <bundle>/claude/plugin` (agents, skills),
    `--append-system-prompt-file <bundle>/claude/CLAUDE.md`, `--mcp-config <bundle>/claude/mcp.json` with the
    existing `--strict-mcp-config`;
  - **codex:** a generated list of `-c key=value` overrides;
  - **opencode:** one env var, `OPENCODE_CONFIG_CONTENT`, set on every launch.

  The login is untouched and the dotfiles symlink is never written. A deny in the bundle holds even against a user
  allow: Claude Code evaluates deny before allow whatever the scope, and lists merge across scopes. A `false` for
  `enableArtifact` from `--settings` cannot be turned back on by any lower scope (verify).
- **Not mechanism (a), a config dir per runner.**
  - It moves the login: a login per dir, and on macOS a keychain entry per dir. codex keys its keyring entry by the
    home too.
  - It moves the transcripts that resume, usage backfill and the keep-alive fork read.
  - atrium's hooks, lean and statusline code hardcode `~/.claude` today (`claudeconf/hooks.go:213`,
    `lean_agents.go:49`), so they would all fall out of step.
  - The one case where (a) earns its cost is a **second account**: a runner row for another Claude login, which
    runner switch may want. That case is held as question 4.
- **Mechanism (c), the machine's managed settings, is an optional floor, not the channel.**
  - It needs admin once per machine.
  - It also binds clint's own hand-started sessions on that machine.
  - It is the only mechanism that also covers a session atrium did not start.

  So it holds only a short never-list (Artifact off, bypass mode off) and only where clint says so (question 2).
  Everything else stays in the layered bundle.
- **r-033 holds.** Every runner's env stays the same set. The bundle adds atrium-wide names to every launch, not
  per row:
  - `OPENCODE_CONFIG_CONTENT`;
  - `ATRIUM_CONFIG_REV`.

  The per-runner differences live in args, which are per row already.
- **Rollout is a pull, like upgrades.**
  1. The hub announces a new revision.
  2. Each room fetches it by the existing upload-pack route and checks the sha.
  3. The room writes it into a hash-named folder under its state, the way lean plugins are written today.
  4. New launches use it.
  5. A live card keeps the revision it started with until it restarts, because every runner reads its config at
     start, and its card shows the gap.

  A revision can go to one room first, and rolling back means pointing a room at an older sha.
- **Drift is reported, never silently fixed.** Each room reports:
  - its revision;
  - the cards running an older one;
  - what the user-scope files on that account add on top of the bundle: an extra allow, an extra hook, a user agent
    or skill, a user CLAUDE.md, an enabled plugin.

  The board shows this per room. A later stage can make atrium sessions ignore user settings entirely
  (`--setting-sources project,local`, which lean workers already use), once atrium's own hooks have moved into the
  bundle.

## 1. Why the three mechanisms cost what they cost

| | claude | codex | opencode | cost |
| --- | --- | --- | --- | --- |
| **(a) a config dir per runner** | `CLAUDE_CONFIG_DIR` moves `settings.json`, `.claude.json` (sign-in, MCP, folder trust), `projects/` transcripts, plugins, agents, skills and the user CLAUDE.md | `CODEX_HOME` moves `config.toml`, `auth.json`, `hooks.json` and AGENTS.md, and the keyring entry is keyed by the home | `OPENCODE_CONFIG_DIR` moves config only. Auth stays in `XDG_DATA_HOME/opencode/auth.json` | **A login per dir** for claude and codex, plus the macOS keychain. Transcripts split, so resume, backfill and keep-alive must learn the dir. atrium's own wiring must follow the env. A per-row env difference |
| **(b) a layer per launch** | `--settings` sits above user, project and local, and below managed. Lists merge, deny wins, and `enableArtifact: false` sticks. Plus `--plugin-dir`, `--agents`, `--append-system-prompt-file`, `--mcp-config` with `--strict-mcp-config`, and `--disallowedTools`. atrium already uses `--settings` three times (the Stop hook, lean, the keep-alive fork) | `-c key=value` is the highest layer. `codex exec --ignore-user-config` keeps auth and drops the user config (verify for interactive). AGENTS.md and hooks may not be settable by `-c` (verify) | `OPENCODE_CONFIG_CONTENT` is merged above the user's files. `OPENCODE_PERMISSION` is merged last (verify) | **Covers only sessions atrium starts.** The user's files still add their extras (section 5). Any flag that shapes the system prompt or tool list must join the keep-alive fork's carried set, or forks miss the cache |
| **(c) managed, admin-enforced** | `managed-settings.json` in the system path per OS, plus `managed-mcp.json`. Highest precedence. Managed-only keys: `allowManagedPermissionRulesOnly`, `allowManagedHooksOnly`, `allowManagedMcpServersOnly`, `disableSideloadFlags`. Also server-managed settings from the claude.ai console, polled hourly | `/etc/codex/requirements.toml` (allowed approval and sandbox modes, an MCP allowlist, managed hooks only) and `managed_config.toml` | `/etc/opencode` and the per-OS admin paths. Merge order only, with no requirements layer | **Admin once per machine. It binds every session on the machine, clint's own too.** `disableSideloadFlags` would break atrium's own `--plugin-dir`, `--agents` and `--mcp-config`, so it must never be set. On Windows codex's managed path is under the home (verify) |

## 2. The bundle

The bundle is a git repo on the hub, `atrium-config`, laid out like this:

```
base/
  claude/settings.json      permissions {deny, allow, ask}, hooks (from stage K6), statusLine, model, enableArtifact, env
  claude/CLAUDE.md          the org's rules for every atrium session
  claude/plugin/            .claude-plugin/plugin.json, agents/, skills/
  claude/mcp.json           atrium-control, plus any shared server
  codex/overrides.toml      keys turned into -c key=value at launch
  codex/AGENTS.md           (verify how to pass per launch; else skipped, section 7)
  opencode/config.json      becomes OPENCODE_CONFIG_CONTENT
  atrium/rules.json         standing always and never rules for atrium's own gate
rooms/<room>/               the same tree, only the files that differ; merged over base
bundle.yaml                 schema version, and which parts are on
```

- **Merging an overlay.** Lists append, and a scalar replaces. A room overlay may add a deny but can never remove
  one, because the merge refuses an overlay that drops a base deny entry. The same goes for `enableArtifact`.
- **Secrets.** None are kept. The repo is refused at push if a value looks like a secret. This reuses the
  requirements file's refusal of absolute paths and secret-shaped values. A shared MCP server that needs a token
  names an env var, and the room supplies the var.
- **Who changes it.**
  - The bundle widens or narrows what every agent may do, so a card may push a proposal branch and only clint lands
    it on `main` (hub forge 3.2: no card pushes `main`).
  - The hub's pre-receive for this repo checks the schema, runs the overlay merge for every room, and refuses a push
    whose result fails.
  - @review reads a proposal like any doc.
- **The first revision** is built from the three rooms' files today (stage K0). The Artifact deny is its first
  entry.

## 3. Launch

On the room, revision `R` is materialised to `<state>/config/<R>/<room>/`: base merged with that room's overlay. The
folder is hash-named and read-only, like `lean-plugins/`. At launch:
- **claude.**
  - The args get `--settings`, `--plugin-dir`, `--append-system-prompt-file` and `--mcp-config` pointing into that
    folder.
  - The Stop hook and lean's inline `--settings` stay separate, because `--settings` may be given more than once
    (verify). Otherwise atrium merges them into one generated file per launch.
  - Lean builds its filtered settings from the bundle instead of from `~/.claude/settings.json` (`lean.go:402`), so
    a room's drift no longer reaches lean workers.
- **codex.** One `-c` per key from `overrides.toml`: approval policy, sandbox mode, `mcp_servers`, model. These are
  generated at launch, so the row's args stay as the operator set them.
- **opencode.** `OPENCODE_CONFIG_CONTENT` is the bundle's `config.json`. It is set on every launch whatever the
  runner, so the env stays uniform (r-033). A runner that is not opencode ignores it.
- **Every launch.**
  - `ATRIUM_CONFIG_REV=<R>` is set.
  - The card records `R`.
  - The keep-alive fork's carried set (`forkCarried`) gains the new flags, so a fork reads the same prompt and
    tools.
- **atrium's gate.** The room imports `atrium/rules.json` as rules marked `source: bundle`. They are read-only on
  the room, and local rules stay beside them. A bundle never rule wins over a local always rule, as the gate's
  tie-to-block rule already says. So even a joined session that atrium did not launch, and that therefore has no
  layered settings, is still gated by the bundle's never list.

## 4. Rollout (for @fabric)

1. **Announce.** When `main` of `atrium-config` moves, the hub records revision `R` and tells each room that opted
   in. A room opts in with a room setting, `config_follow`: `latest`, `pinned <sha>`, or `off`. This is the same
   shape as `--accept-upgrades`.
2. **Fetch and check.** The room fetches by the hub's upload-pack route (the link's `git` kind) and checks the
   commit sha it was told. It then materialises the folder.
3. **Report.** The room reports `R` and its materialise result in check-in.
4. **Apply to new launches only.** A live card keeps its revision. The board offers "restart onto the same card"
   for cards on an older one. Nothing restarts by itself.
5. **One room first.** `atrium config rollout <R> --rooms sg3` moves only the named rooms. `--all` moves the rest.
   The default is all rooms, because the bundle is small and every room keeps the old folder.
6. **Roll back.** `atrium config pin <room> <older sha>`. The older folder is still on the room, so rolling back
   needs no fetch.
7. **An offline room** takes the revision at its next check-in, and its card shows "config behind".

## 5. Drift (for @fabric and @ui)

Drift is reported, never fixed silently. A fix is clint's, or a bundle change. The room answers a fixed probe, in
the shape of `/v1/preflight`: keys in, booleans and hashes out, and never a file's content.

| drift | how the room finds it | shown as |
| --- | --- | --- |
| room behind the hub | its `R` against the hub's | "config R-2" on the room tile |
| a card on an older revision | the card's recorded `R` | a chip on the card: "config behind, restart to apply" |
| the bundle failed to apply | the materialise result, or a launch that refused a flag | red on the room tile, with the error |
| user-scope extras on this account | it reads `~/.claude/settings.json` (and codex's and opencode's user config) and lists what the bundle does not hold: allow entries, hooks, enabled plugins, MCP servers. It also hashes `~/.claude/CLAUDE.md` and lists user agent and skill names | a count on the room tile, with names on open, never values |
| a hand patch | a user-file key the bundle also sets, with a different value (today's Artifact patch on m1mini and sg3) | "hand patch: enableArtifact", with a one-click "move into the bundle" that drafts a proposal branch |
| the managed floor missing where it was asked for | the managed file's presence and hash | warn |

- `atrium.requirements.yaml` gets a `config:` key (the revision a room must follow, and the floor it must have), so
  `room-check` reports drift with the rest.
- The account-scope rule from the room requirements design still holds: two rooms on one OS account share one user
  file, and are shown as sharing it.

## 6. The interaction with dotfiles

- atrium never writes the user's `~/.claude/settings.json` for config. It writes nothing into dotfiles.
- The hooks atrium installs today are the one write left. Stage K6 moves them into the bundle's `settings.json`, and
  then the hooks install can stop writing the user file.
- On sg4, then, the dotfiles file is clint's alone again.

## 7. Stages

All held by the pause.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| K0 | Read-only snapshot: the three rooms' `~/.claude/settings.json`, CLAUDE.md, agents and skills names, codex and opencode user config, and the managed paths. Hashes and keys, collected through the hub, compared, and written up as the first bundle proposal | @fabric | 0.5 day | a table of every key that differs across sg4, sg3 and m1mini. The Artifact patch shows on two rooms |
| K1 | The bundle format, the merge with its refusal to drop a deny, materialising on the room, and the claude launch flags. The card records `R`, `forkCarried` gains the flags, and lean builds from the bundle | @runtime | 2 days | a launch on m1mini with no user deny cannot call Artifact. A user allow of Artifact does not bring it back. A keep-alive fork of that card hits the cache. An overlay that drops a base deny is refused |
| K2 | The `atrium-config` repo on the hub, pushes to `main` from the operator only, a pre-receive running the schema and the merge for every room, the announce, room fetch and check, `config_follow`, rollout to named rooms, and pin | @fabric | 2 days | a push from clint reaches sg3 and m1mini within one check-in. A card's push to `main` is refused. `pin` rolls a room back with no fetch. An offline room catches up on its next check-in |
| K3 | The drift probe and its report, and the `config:` key in requirements | @fabric, @runtime | 1.5 days | m1mini's hand patch shows as "hand patch: enableArtifact". A user allow shows by name. No value is ever sent |
| K4 | The board: the revision on the room tile, the "config behind" chip, a Config view diffing rooms, and "move into the bundle" | @ui | 1.5 days | clint sees on the phone which rooms are behind and which cards need a restart |
| K5 | codex `-c` generation, opencode `OPENCODE_CONFIG_CONTENT`, and atrium gate rules from `atrium/rules.json` | @runtime | 1.5 days | a codex card on sg4 runs with the bundle's approval policy. An opencode card's permission matches the bundle. A bundle never rule blocks a joined session |
| K6 | atrium's hooks move into the bundle's `settings.json`, and the hooks install stops writing the user file. Then, behind a setting, atrium sessions ignore user settings (`--setting-sources project,local`) | @runtime, after K1 to K3 | 1.5 days | a fresh room with an empty user `settings.json` has its gate, its statusline and the Stop hook from the bundle alone. With the setting on, a user allow is not loaded at all. Nothing runs twice |
| K7 | The optional managed floor: a script clint runs as admin once per machine, writing a short `managed-settings.json` (Artifact off, bypass off) and codex's `requirements.toml` floor | @fabric, only if question 2 is yes | 0.5 day | a hand-started claude on that machine cannot call Artifact. atrium's `--plugin-dir` still works, because `disableSideloadFlags` is never set |

**Facts to prove before K1** (the "verify" marks):
- `--settings` given twice merges;
- `enableArtifact: false` from `--settings` sticks;
- deny in `--settings` beats a user allow;
- `--setting-sources project,local` also drops user agents, skills and CLAUDE.md;
- codex can take AGENTS.md and hooks per launch;
- `OPENCODE_CONFIG_CONTENT`'s precedence, against managed config.

Each is a 10-minute check on a test room.

## 8. Questions for clint, held until he asks

Phrased as `docs/rnd/interviewer-brief.md` section 3 asks. Question 1 tests the core picture: if the answer is no,
sections 2 to 5 change.

1. **One config for every agent, owned by the hub.** You change one file on the hub: "deny Artifact". Within a
   minute, every new session that atrium starts, on sg4, sg3 and m1mini, has that deny. Your own `settings.json`
   and your dotfiles are not touched. Sessions already running keep the old config until they restart, and their
   cards say so. Is that the picture? **Suggested: yes.**
2. **Your own sessions too.** You start claude by hand in a terminal on m1mini, outside atrium. Should the deny reach
   that session too? That needs an admin step once per machine, and binds everything on that machine. **Suggested:
   no for now. The atrium gate still applies its never list to a session you join to the board.**
3. **The user file.** m1mini's `~/.claude/settings.json` has an allow the hub's config does not have. Should atrium
   sessions on m1mini still get that allow, with the board showing it as drift? Or should they ignore the user file
   entirely? **Suggested: get it and show it for now. Ignore it once atrium's hooks have moved into the hub's
   config.**
4. **A second Claude account.** You buy a second Claude subscription for when the first runs out. A runner row
   "claude-2" starts sessions with their own config folder and their own login. Is that wanted, as the one place a
   separate config folder is used? **Suggested: yes, later, with runner switch.**
5. **Who may change the config.** A director wants a new MCP server for every agent. It pushes a proposal branch to
   the hub's config repo, @review reads it, and you land it. Or should directors never even propose? **Suggested:
   propose and review, you land.**

## 9. Interview plan

The orchestrator asked for clint to be walked through this in interview style once the model is reviewed. Following
`docs/rnd/interviewer-brief.md`:
- **Question 1 is the scenario in section 8, question 1, asked as his picture first.** Before question 2, the
  interviewer writes his picture back in five lines.
- **Then questions 3, 2, 5 and 4, in that order.** They are the user file, his own sessions, who may change the
  config, and a second account.
- **What the interviewer brings:** a ten-line summary of section 0, plus the table in section 1 for when he asks why
  not a config folder per runner.
- **The answers file** stays outside the repo, as the brief says.
- **Who runs it:** an interviewer card the orchestrator launches on the hub with the brief, or @rnd if clint opens
  this card. That is his choice.
