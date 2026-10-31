# The per-repo setup file and its atrium_resources cross-check

Status: design, 2026-10-31, section 9 answered. Stage 1 (section 8) is being built. Origin: a card on claude-sg4 (repo ziti-hook) lost an hour
learning by failed builds that the room had no Docker daemon, no Linux toolchain and no autotools or ninja. Nothing
told it up front.

## 0. The answer, in one paragraph

Each repo carries `.atrium/setup.toml`, a short human-written list of what the repo needs to build and test: tools,
how to get each per OS, and a version only where it matters. The room checks the file against itself with a small set
of built-in probes (never a command the repo supplies). The check runs on the room, at launch, because only the room
has the checkout and the live state (a Docker daemon can stop between check-in and launch). A missing need marked
`required = true` refuses the launch, with the list, before the agent starts. Any other missing need is a warning
written into the card's brief. Atrium owns the needs list, a card never writes the file, and a card that finds a
missing need tells atrium (section 7). The hub learns needs and applies them with user input, never by reading a git
mirror. Cards read the file too, since it holds the install hints.

## 1. What exists today and is reused

- `internal/daemon/toollocations.go`: `toolNames` (go, node, python, git, gh), `findTool` (PATH lookup, with the
  Windows extensions) and `ToolLocations`, sent in the room's hello as `wire.Hello.Tools`
  (`internal/link/wire/protocol.go`). Observed, never trusted.
- `internal/link/resources_mcp.go`: `atrium_resources` returns `resources.md` plus `rooms`, each with OS, arch,
  online and `Tools`. `internal/base/resources` owns the hand-written, per-machine `resources.md`.
- `internal/daemon/launch.go`: every launched card gets `withResourcesLine` and `withToolsLine` appended to its brief.
- `docs/fabric/f-003-resources-design.md`: the rule that atrium holds names and commands and never a credential.

The setup file is the per-REPO half of what `resources.md` is per MACHINE. The two are different files because they
have different owners: the repo's maintainers write the first, the room's operator the second.

## 2. File name and location

`.atrium/setup.toml` at the repo root, committed. A directory rather than a root file because the repo root is not
atrium's, and `.atrium/` can later hold other repo-level atrium config. Read from the card's checkout (its cwd root),
so it is the version of the file at the commit the card works on. A repo without it behaves exactly as today.
TOML because it is small, has comments, needs no indentation care and has a parser the repo already depends on
(to confirm in the build card). The file is owned by atrium's operator side, not by cards (section 9, item 4). Size cap 16 KiB, like `resources.MaxBytes` in spirit.

## 3. Format

One table per need. The key is the tool's name. Every field but `detect` is optional.

```toml
# what ziti-hook needs. Each entry: how to detect it, and how to get it per OS.
[cmake]
version = ">=3.25"
required = true                         # a room without it refuses the launch
install.macos = "brew install cmake"
install.linux = "apt install cmake"
install.windows = "winget install Kitware.CMake"

[ninja]
detect = { bin = "ninja" }              # same as the default for a plain tool
install.windows = "winget install Ninja-build.Ninja"

[docker]
detect = { docker = true }              # a binary AND a daemon that answers
why = "integration tests start containers"
os = ["linux", "windows"]               # only needed there. Omitted means everywhere

[alpine]
detect = { wsl = "Alpine" }             # a WSL distro by name
os = ["windows"]                        # no required, so a missing one only warns
```

Fields:

| Field | Meaning |
| --- | --- |
| `detect` | A built-in probe, section 4. Default is `{ bin = "<key>" }`. |
| `version` | A constraint, `>=` only (`>=3.25`). Omitted means any. Checked against the probe's version. |
| `install.<os>` | One line a person or card can run or read. `macos`, `linux`, `windows`. Text only, never run by atrium. |
| `os` | OS names the entry applies to. Omitted means all. |
| `required` | `true` makes a missing or old need refuse the launch. Default false, which only warns. |
| `why` | Free text, shown to the card. |

No secrets, no hosts. Unknown fields are ignored with a note, so an older room reads a newer file.

## 4. How a need is detected

THE REPO NEVER SUPPLIES A COMMAND THAT ATRIUM RUNS. A file in a cloned repo is untrusted input, and a probe that
executes `detect = "sh -c ..."` on every room that launches a card is remote code execution by pull request. So
detection is a closed set of kinds implemented in the room, each with a fixed argv:

| Kind | Probe | Version from |
| --- | --- | --- |
| `bin = "name"` | `findTool` on the room's PATH (reuse), plus the extra dirs `withToolDirs` already adds | `name --version`, first `\d+(\.\d+)*` in the output, 3 s timeout, only when `version` is set |
| `docker = true` | `bin = docker`, then `docker version --format {{.Server.Version}}` | the server version. A binary with no daemon is "present, not running" |
| `wsl = "Distro"` | on Windows, `wsl.exe -l -q` (UTF-16 decoded), match the name case-insensitively | none. Other OSes: not applicable, never "missing" |
| `env = "NAME"` | the variable is set and non-empty. The value is never reported | none |
| `file = "path"` | the path exists, `~` and `$VAR` expanded by the room | none |

Version probes run `name --version` of a tool found by PATH, and nothing else, with no shell, a timeout and a minimal
environment. Every probe answers one of four states: `ok` (with version), `old` (found, below the constraint),
`stopped` (present but not usable, the Docker case), `missing`, plus `na` when the entry's `os` excludes this room.
A version that cannot be parsed from the output is `unknown`, which warns and never refuses, even when `required`.
New kinds are a code change in atrium, which is the point: a repo cannot invent one.

The probe's result is the room's own observation, in the spirit of `Hello.Tools`: observed, never trusted. A repo
needing something odd (a particular compiler flag, a toolchain in a vcpkg tree) lists it as `file = ...` or leaves it
to prose in the repo's own docs.

## 5. Where the check runs

Two places were considered.

- **Room inventory at check-in.** The room cannot know which repos cards will use, so it could only report the fixed
  `toolNames` set it reports now. Stale by the next minute (a daemon stops), and says nothing about a repo.
- **On launch, on the room.** The room has the checkout, so it reads `.atrium/setup.toml`, runs the probes and has the
  answer fresh at the moment it matters.

Decision: the check runs ON THE ROOM AT LAUNCH, before the agent starts, and its result is cached on the room by
(repo, setup-file hash) for 60 s so a burst of launches does not re-run `docker version`. The check-in `Tools` stays as
it is, as the cheap always-there baseline, and `Hello.Tools` stays fixed (section 9, item 7).

The hub never reads the file from a git mirror. It learns needs from the rooms: a room reports the needs it checked and
what it found, in the launch reply. The hub then applies them with user input. When a card or a room reports a need
the hub's inventory lacks, the hub asks the human ("ziti-hook needs docker, add it to the inventory?") and records the
answer. Design only in stage 1, nothing of this is built.

## 6. What a launch does with a missing need

A missing required need refuses. Every other missing need warns, in the brief where the card will see it.

1. The room runs the check before the agent starts. Needs are collected by state.
2. If any need with `required = true` is `missing`, `old` or `stopped`, the launch is refused and the reply lists them,
   one per line, with the `why` and the `install.<os>` hint for this room's OS. No agent starts, no card process
   exists. `unknown` never refuses.
3. Otherwise the card's brief gets one block appended (next to `withToolsLine`), only when something is not `ok`:

   ```
   Setup: this repo's .atrium/setup.toml needs things this room lacks.
   - docker: stopped (the daemon is not answering). why: integration tests start containers. install: ...
   - ninja: missing. install.windows: winget install Ninja-build.Ninja
   Read the file. Install what you can in your card directory or tell your launcher. Do not hand-build a toolchain
   for an hour before saying so.
   ```

4. The missing list is a field on the card's launch result, so a later stage can show a badge. No store table.
5. A launch the LAUNCHER pins to a room is never moved. Choosing among rooms by missing needs is a later stage.

Atrium never runs an `install.*` line, in either case. The refusal is only as good as the file, so a wrong
`required = true` is fixed by the operator editing the file, not by the card.

## 7. What the card sees, and how a card reports a need

The card sees: the brief block above, and the file itself in its checkout, which it is told to read first by one
added framing line, `Read .atrium/setup.toml if the repo has one.` The line is added with the others in
`internal/daemon/launch.go` and only when the checkout has the file.

A card never edits the file. When it finds a tool the file does not list, it calls a new tool, `atrium_need`, with
`{name, why, detect?, install?}` (shape only, built in a later stage). The room checks the claim with its own probe
before accepting it. The hub then asks the human whether to add it, and atrium writes the operator-approved entry. The
reply to the card says whether it was recorded, never that the file changed.

`atrium_resources` gains an optional `repo` input (default the caller's own repo) and a `setup` section beside `file`
and `rooms`, built from what rooms have reported for that repo, not from a hub-side read of the repo:

```
setup for ziti-hook (4 needs):
- claude-sg4 (online): docker stopped, ninja missing, alpine missing, cmake 3.28 ok
- m1mini (online): all present
- m4-air (offline, last checked 2h ago): all present (stale)
- m2-pro (online): not checked for this repo
```

and in the structured output each room gains `needs: [{name, state, version, install, why, required}]`. The rooms'
existing `tools` map is unchanged. A room that has not checked the repo is shown as unchecked, never as ok.

## 8. Staged plan

1. File format, parser, the `bin`, `file` and `env` probes with `>=` versions, the brief block, the framing line and
   the required-need refusal before the agent starts. (room only)
2. `docker` and `wsl` probes, `atrium_resources` `repo` input and the `setup` section, rooms reporting checked needs.
3. `atrium_need` and the hub asking the human to add a need, board badge, placement among rooms.

Tests: probes run against a fake PATH and a fake `docker`/`wsl.exe` on PATH, as `toollocations` tests do. No flat waits.

## 9. Decisions (answered by Clint)

1. **Can a need refuse a launch?** Yes, `required = true`. The room checks before the agent starts. A missing required
   need refuses the launch with the list. A non-required need stays a warning in the brief.
2. **TOML?** Yes.
3. **Does atrium run the `install.*` line?** Never.
4. **Does a card write the file?** No. Atrium owns the needs list. A card informs atrium of a missing need through
   `atrium_need` (section 7, built later).
5. **Monorepos?** Root only for v1.
6. **Version syntax?** `>=` only, no `~` and no `=`. An unparseable version is `unknown` and warns.
7. **Does `Hello.Tools` grow?** No, it stays fixed.
8. **Does the hub read the file from a git mirror?** No. The hub learns needs from rooms and applies them with user
   input, for example by asking the human "ziti-hook needs docker, add it to the inventory?". Design only for now.
