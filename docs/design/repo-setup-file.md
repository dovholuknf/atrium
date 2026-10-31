# The per-repo setup file and its atrium_resources cross-check

Status: design only, 2026-10-31. Nothing here is built. Origin: a card on claude-sg4 (repo ziti-hook) lost an hour
learning by failed builds that the room had no Docker daemon, no Linux toolchain and no autotools or ninja. Nothing
told it up front.

## 0. The answer, in one paragraph

Each repo carries `.atrium/setup.toml`, a short human-written list of what the repo needs to build and test: tools,
how to get each per OS, and a version only where it matters. The room checks the file against itself with a small set
of built-in probes (never a command the repo supplies). The check runs on the room, at launch, because only the room
has the checkout and the live state (a Docker daemon can stop between check-in and launch). A missing tool never
blocks a launch. It is written into the card's brief, and `atrium_resources` shows, per room, what the repo needs and
what that room lacks, so the launcher picks a room that has it. Cards read the file themselves too, since it holds the
install hints.

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
(to confirm in the build card). Size cap 16 KiB, like `resources.MaxBytes` in spirit.

## 3. Format

One table per need. The key is the tool's name. Every field but `detect` is optional.

```toml
# what ziti-hook needs. Each entry: how to detect it, and how to get it per OS.
[cmake]
version = ">=3.25"
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
os = ["windows"]
optional = true                         # warn only, never mentioned as blocking
```

Fields:

| Field | Meaning |
| --- | --- |
| `detect` | A built-in probe, section 4. Default is `{ bin = "<key>" }`. |
| `version` | A constraint (`>=3.25`, `~1.22`, `=2`). Omitted means any. Checked against the probe's version. |
| `install.<os>` | One line a person or card can run or read. `macos`, `linux`, `windows`. Text only, never run by atrium. |
| `os` | OS names the entry applies to. Omitted means all. |
| `optional` | The entry is a nice-to-have. Shown, but never counted as missing in the summary. |
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

Decision: the check runs ON THE ROOM AT LAUNCH, and its result is cached on the room by (repo, setup-file hash) for 60
s so a burst of launches and repeated `atrium_resources` calls do not re-run `docker version`. The check-in `Tools`
stays as it is, as the cheap always-there baseline. The room sends the result up in the launch reply and in a new
`Hello.Needs` only for repos it has already checked (so the hub can show last known state of an offline room, marked
stale). The launcher choosing a room BEFORE the launch has no checkout on the candidate rooms, so
`atrium_resources` takes a `repo` argument (section 7) and the hub asks each online room to check that repo's file
from its own clone or, if it has no clone, from the file's content the hub passes (the hub reads it from its git
mirror at the branch tip). The probes are the same code either way.

## 6. What a launch does with a missing need

Warn, never refuse, and say it where the card will see it.

1. The room runs the check. Missing non-optional needs are collected.
2. The card's brief gets one block appended (next to `withToolsLine`), only when there is something to say:

   ```
   Setup: this repo's .atrium/setup.toml needs things this room lacks.
   - docker: stopped (the daemon is not answering). why: integration tests start containers. install: ...
   - ninja: missing. install.windows: winget install Ninja-build.Ninja
   Read the file. Install what you can in your card directory or tell your launcher. Do not hand-build a toolchain
   for an hour before saying so.
   ```

3. The hub records the missing list on the card, so the board can show a small "setup: 2 missing" badge. No new store
   table is proposed here, it is a field on the card's launch result.
4. A launch that the LAUNCHER explicitly pins to a room is never moved. A launch that names no room (the hub picks)
   prefers an online room with no missing needs, among rooms otherwise eligible, and says so in the launch reply. It
   never blocks if every room lacks something.

Refusing was rejected: the file may be stale or wrong, a card may exist precisely to install the tool, and
atrium's rule is to inform, and not to stand between an operator and a launch. A `required = true` that refuses is
left as an open question.

## 7. What the card sees and how atrium_resources shows it

The card sees: the brief block above, and the file itself in its checkout, which it is told to read first by one
added framing line, `Read .atrium/setup.toml if the repo has one.` The line is added with the others in
`internal/daemon/launch.go` and only when the checkout has the file.

`atrium_resources` gains an optional `repo` input (a repo name the hub knows, default the caller's own repo). Output
gains `setup` beside `file` and `rooms`:

```
setup for ziti-hook (.atrium/setup.toml at claude/main, 4 needs):
- claude-sg4 (online): docker stopped, ninja missing, alpine missing, cmake 3.28 ok
- m1mini (online): all present
- m4-air (offline, last checked 2h ago): all present (stale)
```

and in the structured output each room gains `needs: [{name, state, version, install, why, optional}]`. The rooms'
existing `tools` map is unchanged. A launcher picking a room reads the `setup` section and sees which room has it. A
card with no `repo` and no file sees `setup: this repo has no .atrium/setup.toml`.

## 8. Staged plan

1. File format, parser and the `bin` and `file` and `env` probes, the brief block, the framing line. (room only)
2. `docker` and `wsl` probes, `atrium_resources` `repo` input and the `setup` section, the hub asking rooms.
3. Hub placement preference when no room is named, board badge, `Hello.Needs` for offline rooms.

Tests: probes run against a fake PATH and a fake `docker`/`wsl.exe` on PATH, as `toollocations` tests do. No flat waits.

## 9. Open questions, with a recommendation each

1. **Should a need be able to refuse a launch (`required = true`)?** Recommend no for now. Warn first and see whether
   cards ignore the warning before adding a way to block.
2. **TOML or the `## heading` markdown of resources.md?** Recommend TOML. Machine-checkable needs structure, and
   markdown prose is what `resources.md` is already for.
3. **Should atrium ever run the `install.*` line?** Recommend never. It is a hint for a person or a card, and running
   it would be the repo choosing what runs as the operator.
4. **Should a card be allowed to write a missing need back to the file?** Recommend yes, as an ordinary commit on its
   branch like any file in the repo, reviewed in the merge. It is the repo's file, unlike `resources.md`.
5. **Monorepos with a nested setup per subdirectory?** Recommend root only for v1, plus `[needs-in.<dir>]` later if a
   real repo asks.
6. **Version comparison syntax and parsing of odd `--version` output (Python 2 prints to stderr, `gh` is multi-line)?**
   Recommend a fixed small grammar (`>=`, `~`, `=`), first dotted number on stdout then stderr, and `unknown` rather
   than `old` when no number is found, so a parse failure warns and never blocks.
7. **Should `Hello.Tools` grow to cover the repo's names automatically?** Recommend no. The hello stays the fixed
   baseline and per-repo state is asked for, so the hello does not grow with every repo.
8. **Is the hub reading a repo's file from its git mirror reliable for a repo with no hub mirror?** Recommend falling
   back to "the room that holds the card's checkout checks it and the others are shown as unchecked", and saying so.
