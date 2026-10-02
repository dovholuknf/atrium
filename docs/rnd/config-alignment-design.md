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

**Questions are held in section 8, and section 9 is the interview plan.** @fabric reviewed the rollout and drift half
(sections 4 and 5) against the forge stage 1 code, and its notes are folded in. The hub side is @fabric's, and the
room side is @runtime's.

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
- **Rollout is a pull, like upgrades, and every revision is signed by clint.**
  - A revision carries hooks and MCP server commands, so it is code that runs on every room at every launch.
  - Each room has clint's signing key in an allowed-signers file from provisioning. It runs `git verify-commit` before
    using a revision, and it refuses an unsigned one.
  - Then:
    1. The hub announces a new revision.
    2. Each room fetches it by the existing upload-pack route, verifies the signature, and writes it into a
       hash-named folder under its state, the way lean plugins are written today.
    3. New launches use it.
    4. A live card keeps the revision it started with until it restarts, because every runner reads its config at
       start, and its card shows the gap.
  - A revision that fails to verify or apply never blocks a launch. The room keeps using its last good folder and
    reports red.
  - A revision goes to a named canary list first when one is set. Rolling back means pinning a room to an older sha,
    and the last three folders are kept, so no fetch is needed.
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
| **(c) managed, admin-enforced** | `managed-settings.json` in the system path per OS, plus `managed-mcp.json`. Highest precedence. Managed-only keys: `allowManagedPermissionRulesOnly`, `allowManagedHooksOnly`, `allowManagedMcpServersOnly`, `disableSideloadFlags`. Also server-managed settings from the claude.ai console, polled hourly (verify) | `/etc/codex/requirements.toml` (allowed approval and sandbox modes, an MCP allowlist, managed hooks only) and `managed_config.toml` | `/etc/opencode` and the per-OS admin paths. Merge order only, with no requirements layer | **Admin once per machine. It binds every session on the machine, clint's own too.** `disableSideloadFlags` would break atrium's own `--plugin-dir`, `--agents` and `--mcp-config`, so it must never be set. On Windows codex's managed path is under the home (verify) |

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
  one, because the merge refuses an overlay that drops a base deny entry. The same goes for `enableArtifact`. An
  overlay **can** widen a room's allow list. That is deliberate: a room may need one more tool. It is visible in the
  bundle, signed by clint like the rest, and checked at review.
- **Size.** `opencode/config.json` travels as an env value, which reaches every tool subprocess. Windows caps an env
  value at 32K, so the hub refuses a push whose `config.json` is over 16K.
- **Secrets.** None are kept. The repo is refused at push if a value looks like a secret. This reuses the
  requirements file's refusal of absolute paths and secret-shaped values. A shared MCP server that needs a token
  names an env var, and the room supplies the var.
- **Nothing in it is private to a room.** Every room can read the whole repo, because upload-pack serves all of it.
  The secret-shape refusal is a heuristic, and it is a backstop, not a guarantee.
- **Who changes it.**
  - The bundle widens or narrows what every agent may do. So a card may push a `proposal/*` branch, which forge
    ownership already allows, and only clint lands it on `main` (hub forge 3.2: no card pushes `main`).
  - clint's landing commit is signed, as his commits already are, and rooms refuse an unsigned `main`.
  - The hub validates every push to this repo, a proposal included, as a lint with no announce. It checks the
    schema, runs the overlay merge for every room, and applies the size and secret-shape refusals. It refuses a push
    whose result fails.
    - This is the hub's own Go receive check (`f-new-hub-receive`). A repo's own hooks never run.
    - It needs the pushed objects. So the hub's check calls a hidden `atrium` subcommand with the repo and the old
      and new refs, which reads the quarantined tree with `cat-file` and uses no network.
    - It is chosen from a per-repo validator registry, so only `atrium-config` pays for it.
  - **A proposal's review depends on what it touches.**
    - Permission lists, the model, CLAUDE.md text and atrium rules are config, and get a doc-ok from @review.
    - Hooks, MCP server commands and plugin directories are code that runs on every room. They get a code verdict from
      @review, as a change to atrium's own code would, before clint lands them.
- **The repo has no forge to seed from.** An operator-only `atrium rooms git new <host>/<owner>/<repo>` makes an
  empty bare repo marked `made`. The suggested key is `hub/atrium/config`.
- **The first revision** is built from the three rooms' files today (stage K0). The Artifact deny is its first
  entry.

## 3. Launch

On the room, revision `R` is materialised to `<state>/config/<R>/<room>/`: base merged with that room's overlay.
- **The folder is checked before every launch.** It is writable by the room's own account, and making it read-only
  does not stop its owner. After K1 it is the only place the deny lives.
  - So before each launch, the room hashes the folder and compares it with a manifest it wrote at materialise time.
    The manifest is keyed to the revision's git tree sha.
  - A mismatch refuses that folder: the room rematerialises from git and reports red.
  - The same check applies to `lean-plugins/`.
- **A folder stays while a card uses it.** A folder is kept while any live card or its keep-alive fork records its
  `R`, even beyond the last three, because a fork uses its parent's recorded revision.

At launch:
- **claude.**
  - The args get `--settings`, `--plugin-dir`, `--append-system-prompt-file` and `--mcp-config` pointing into that
    folder.
  - atrium writes **one generated settings file per launch**: the bundle's `settings.json` merged with the Stop hook
    and, for lean, lean's filtered set. A second `--settings` may take the last value instead of merging, so atrium
    does not rely on it. The generated file is checked against the folder's manifest like the rest.
  - `--append-system-prompt-file` in interactive mode (verify): the `-file` prompt flags were once print-only. If it
    is not honoured, the CLAUDE.md text goes in through `--append-system-prompt`, which lean already uses.
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

## 4. Rollout

The hub side is @fabric's: the repo, the validator, the announce, the follow and pin state, the verbs and routes,
and the status endpoint. The room side is @runtime's: fetch, verify, materialise, fall back, and report.

1. **Announce.**
   - The trigger is a successful receive on `atrium-config`'s `main`, and it fires only after the push-log row is
     written.
   - It reuses git sync's mechanism: `tellRooms` for a moved ref, and `Attached()` for a room that comes back. There
     is no new transport and no polling.
   - The announce carries the sha a room should be on: latest, or its pin.
2. **Follow and pin, with no migration.**
   - `config_follow` (`latest` or `off`) is the room's own setting.
   - A pin is hub-held, as a hub setting mapping room to sha, set by `atrium config pin`.
   - The revisions rooms report live in the hub's in-memory room table, rebuilt at check-in and persisted nowhere.
3. **Fetch, verify and materialise (room).**
   - The room fetches as the room, using its certificate and no card. The forge fetch route allows that, and a card
     header is needed only for a push.
   - It checks that the commit is the announced sha, and that `git verify-commit` passes against the allowed-signers
     file from provisioning. Then it materialises the folder.
4. **Last known good (room).**
   - If verifying or materialising fails, the room keeps the previous folder for launches.
   - If a launch is refused for a flag the new revision added, that launch retries on the last good folder.
   - Either way the room reports red, with the error. A bad revision never blocks a launch.
   - The last three folders are kept.
5. **Apply to new launches only.** A live card keeps its revision. The board offers "restart onto the same card"
   for cards on an older one. Nothing restarts by itself.
6. **A canary first.**
   - When clint has set a canary list, a new revision goes to those rooms only.
   - `atrium config rollout <R> --all` sends it to the rest in one command. With no canary list set, it goes to all
     rooms.
7. **Roll back.** `atrium config pin <room> <older sha>`. The older folder is still on the room, so rolling back
   needs no fetch.
8. **An offline room** gets the announce through `Attached()` when it comes back, and its tile shows "config behind"
   until then.
9. **Report.** The room reports `R` and the result of verifying and materialising in check-in.

## 5. Drift (for @fabric and @ui)

Drift is reported, never fixed silently. A fix is clint's, or a bundle change.
- The room answers a fixed probe, in the shape of `/v1/preflight`: keys in, booleans and hashes out, and never a
  file's content.
- The probe is a room route that the hub calls through its existing loopback call per room, so it needs no new
  relay op.
- The hub's aggregated `GET /_hub/config/status` is operator-gated like `/_hub/git/settings`, because it names hooks
  and entries.
- An allow entry is a value, and can hold a secret, as in `Bash(curl https://...token...)`. So each entry is sent as
  three parts:
  - the tool name before the parenthesis;
  - a hash of the whole entry;
  - a count.

  The string itself is never sent.

| drift | how the room finds it | shown as |
| --- | --- | --- |
| room behind the hub | its `R` against the hub's | "config R-2" on the room tile |
| a card on an older revision | the card's recorded `R` | a chip on the card: "config behind, restart to apply" |
| the bundle failed to apply | the materialise result, or a launch that refused a flag | red on the room tile, with the error |
| user-scope extras on this account | it reads `~/.claude/settings.json` (and codex's and opencode's user config) and reports what the bundle does not hold. An allow entry is sent as its tool name, a hash and a count. Hooks, enabled plugins, MCP servers, user agents and skills are sent as hashes and counts, because a name or command can carry a host or path. `~/.claude/CLAUDE.md` is sent as a hash | a count on the room tile. On open it shows the tool names, plus the bundle's own name for any hash that matches a bundle entry. No rule text, command or name is sent or stored on the hub |
| a hand patch | a user-file key the bundle also sets, with a different value (today's Artifact patch on m1mini and sg3) | "hand patch: enableArtifact", with a one-click "move into the bundle" that drafts a proposal branch |
| the managed floor missing where it was asked for | the managed file's presence and hash | warn, as a needs-human item (`f-new-needs-human-tracked`, `f-room-accounts`) |

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
| K0 | Read-only snapshot: the three rooms' `~/.claude/settings.json`, CLAUDE.md, agents and skills names, codex and opencode user config, and the managed paths. Collected as hashes and keys by scripts over ssh (sg3 and sg4 from sg4, m1mini locally), compared, and written up as the first bundle proposal | @fabric | 0.5 day | a table of every key that differs across sg4, sg3 and m1mini. The Artifact patch shows on two rooms |
| K1 | The bundle format, the merge with its refusal to drop a deny, materialising on the room, and the claude launch flags. The card records `R`, `forkCarried` gains the flags, and lean builds from the bundle | @runtime | 2 days | a launch on m1mini with no user deny cannot call Artifact. A user allow of Artifact does not bring it back. A keep-alive fork of that card hits the cache. An overlay that drops a base deny is refused. An edited file in the materialised folder refuses that folder before the next launch |
| K2a | Hub side: `atrium rooms git new`, the `atrium-config` repo with operator-only and signed `main`, the validator hook calling back into Go through a per-repo registry (schema, the merge for every room, the secret-shape heuristic, a lint on `proposal/*`), the announce after the push-log row, the hub-held pin, the canary list, the rollout and pin verbs and routes, and an allowed-signers step in provision-room (a needs-human item if the key is not given) | @fabric, after forge stage 1's receive | 4.5 days | a signed push from clint is announced to the canary rooms only, and `--all` reaches the rest. A card's push to `main` is refused, and a card's `proposal/x` that drops a base deny is refused on push. A room coming back gets the announce with no polling. Nothing is added to the hubstore schema |
| K2b | Room side: fetch as the room, `git verify-commit` against the allowed signers, materialise, keep the last three folders, fall back to the last good folder on a verify, materialise or flag failure, and report in check-in | @runtime, with K1 | 1.5 days | an unsigned revision is refused and shown red, and launches carry on with the previous folder. A launch refused for a new flag retries on the last good folder. `pin` to one of the last three revisions needs no fetch |
| K3 | The drift probe as a room route (@runtime, 1 day), the hub's operator-gated `GET /_hub/config/status` aggregating it (@fabric, 0.5 day), and the `config:` key in requirements | @runtime, @fabric | 1.5 days | m1mini's hand patch shows as "hand patch: enableArtifact". A user allow shows by its tool name, with a hash and a count. No entry string is ever sent |
| K4 | The board: the revision on the room tile, the "config behind" chip, a Config view diffing rooms, and "move into the bundle" | @ui | 1.5 days | clint sees on the phone which rooms are behind and which cards need a restart |
| K5 | codex `-c` generation, opencode `OPENCODE_CONFIG_CONTENT`, and atrium gate rules from `atrium/rules.json` | @runtime | 1.5 days | a codex card on sg4 runs with the bundle's approval policy. An opencode card's permission matches the bundle. A bundle never rule blocks a joined session |
| K6 | atrium's hooks move into the bundle's `settings.json`, and the hooks install stops writing the user file. Then, behind a setting, atrium sessions ignore user settings (`--setting-sources project,local`) | @runtime, after K1 to K3 | 1.5 days | a fresh room with an empty user `settings.json` has its gate, its statusline and the Stop hook from the bundle alone. With the setting on, a user allow is not loaded at all. Nothing runs twice |
| K7 | The optional managed floor: a script clint runs as admin once per machine, writing a short `managed-settings.json` (Artifact off, bypass off) and codex's `requirements.toml` floor. It is a needs-human item, tied to `f-new-needs-human-tracked` and `f-room-accounts` | @fabric, only if question 2 is yes | 0.5 day | a hand-started claude on that machine cannot call Artifact. atrium's `--plugin-dir` still works, because `disableSideloadFlags` is never set |

**Facts to prove before K1** (the "verify" marks):
- `enableArtifact: false` from `--settings` sticks;
- deny in `--settings` beats a user allow;
- `--setting-sources project,local` also drops user agents, skills and CLAUDE.md;
- codex can take AGENTS.md and hooks per launch;
- `OPENCODE_CONFIG_CONTENT`'s precedence, against managed config;
- `--append-system-prompt-file` honoured in interactive mode;
- server-managed settings polled hourly;
- for K6, `--settings` still loading under `--setting-sources project,local`.

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
5. **Who may change the config.** A director wants a new MCP server for every agent. It pushes a `proposal/` branch to
   the hub's config repo, the hub checks it on push, @review reads it, and you land it with a signed commit. Or
   should directors never even propose? **Suggested: propose, the hub checks, @review reads, and you land.**
6. **A canary first.** You land a config change. Should it reach one room, say sg3, first, and the rest only when
   you say `--all`? Or should every room get it at once? **Suggested: one room first, once you have named which.**

## 9. Interview plan

The orchestrator asked for clint to be walked through this in interview style once the model is reviewed. Following
`docs/rnd/interviewer-brief.md`:
- **Question 1 is the scenario in section 8, question 1, asked as his picture first.** Before question 2, the
  interviewer writes his picture back in five lines.
- **Then questions 3, 2, 5, 6 and 4, in that order.** They are the user file, his own sessions, who may change the
  config, a canary room, and a second account.
- **What the interviewer brings:** a ten-line summary of section 0, plus the table in section 1 for when he asks why
  not a config folder per runner.
- **The answers file** stays outside the repo, as the brief says.
- **Who runs it:** an interviewer card the orchestrator launches on the hub with the brief, or @rnd if clint opens
  this card. That is his choice.
