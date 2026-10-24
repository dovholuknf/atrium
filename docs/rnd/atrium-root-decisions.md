# One atrium folder per machine: decisions

Decisions from clint's interview on 2026-10-23 about the one-root design (`.uncommitted/atrium-root.md` in clint's
checkout, backlog items `o-atrium-dir-layout` and `o-atrium-dir-layout-git`). Several answers change the design's shape,
not just its open questions, so the design needs rewriting before it is built. Each section names the design's open
decision it answers.

## 1. A config file, not a root variable (decisions 1 and 2)

- atrium reads a **config file**, JSON, like `room.json` and `daemon.json`. No `ATRIUM_ROOT` and no `--root`.
- The file is found in this order, each overriding the one before:
  1. the default, `~/.atrium/config.json`, in the home of the account atrium runs as
  2. `ATRIUM_CONFIG`, naming a config file elsewhere
  3. `--config <path>` on any subcommand
- The folder setting inside it is **`data_dir`**. "Root" was too generic. `workspace_root`, `home`, `atrium_dir` and
  `worktree_root` are taken in the code. In the docs it is "the data dir".
- With no config file anywhere, atrium behaves exactly as today.
- The `root.json` marker beside the binary is **dropped**. A hook from `settings.json` runs as `claude`, so it finds
  `~/.atrium/config.json` in claude's home with no environment and no guessing from the binary's location.
- A flag that names one path (`--db`, `--dir`, `--location-file`) still wins for that path.
- Ports are settings in the config file like anything else.

## 2. One config per process, all its data in its own data dir (decision 3)

- The hub and each room have **their own config file**, and each names its own `data_dir`. Everything that process keeps
  goes under that folder, laid out as it likes. The design's fixed tree (`<root>\hub`, `<root>\room\<name>`) is a
  convention an operator chooses when writing configs, for example `D:\atrium\hub` and
  `D:\atrium\room\claude-sg4`. atrium does not impose it.
- No `--room` flag and no picking a room inside a shared tree. A second room on a machine is started with `--config`
  or `ATRIUM_CONFIG` pointing at its own file.
- A session a room launched keeps finding its room through `ATRIUM_LOCATION`, as today. A session nobody launched goes
  to the room `~/.atrium/config.json` describes.

## 3. Room names and rename (decision 4, plus a new decision)

- The room on sg4 is **`claude-sg4`** going forward, old and new alike. While both run, the old one is
  **`claude-sg4-old`** and the new one is `claude-sg4`.
- atrium cannot rename a room today: the name is the certificate's common name (`internal/link/certs.go:548`) and the
  hub keys cards, reports, the rooms list and ssh lines by it.
- New feature, **built before the trial**: the certificate carries a **stable room id** the hub chooses at join, and
  the human name is one field in the hub's store attached to that id. `atrium rooms rename <old> <new>` changes that
  field with no new certificate, rejoin or restart. clint wants it because every future upgrade runs old beside new
  and hits the same clash. SANs were considered and are not needed, since names in the certificate would mean a new
  certificate per rename.

## 4. Fully fresh hub and rooms (decisions 5 and 6)

- The new atrium is **fully fresh**: a new hub with a new CA and an empty store, and new rooms. Nothing is carried, not
  the backlog, reports, published files, rooms list or settings. The design's phase 2 (moving the old hub's store, CA
  and git mirror into the root) is dropped, and so is the trial hub that phase 1 joined first.
- Remote rooms (m1mini, sg3, sg4-wsl) each rejoin the new hub with `atrium rooms add` and `atrium room join`.
- The old room is **drained**, not moved: its cards finish or hand off, then the old hub and rooms are retired.

## 5. Ports (decision 7)

No policy. Ports are configuration, set in each config file. Whatever is free during the overlap, whatever is wanted
after.

## 6. Build tooling under the room's data dir (decision 8)

The room steers **all the build tooling it can** into its own data dir for the agents it launches: `GOCACHE` and
`GOTMPDIR` under `<data dir>\go`, and any other tool cache it knows how to redirect, such as npm's. Nothing
account-wide, so the old atrium's agents see no change during the overlap.

## 7. Firewall popup stays on (decision 9, reversed)

- `NotifyOnListen` stays **on**. The popup is machine-wide only (there is no per-user setting), and it does not block
  anything: the program runs and its listen succeeds, and only other machines are kept out until Allow.
- Agents behave instead: tests and agents bind `127.0.0.1`, a fixed test port comes from 17800 to 17899, and
  `scripts/check-test-listen.go` run by `scripts/test-touched.sh` refuses a wide listen in a test. A popup means an
  agent broke the rule, and the agent says so on its card and fixes the bind.
- The hub's link port gets an explicit allow rule so remote rooms connect.

## 8. Defender exclusions (decision 10)

- Each data dir is excluded **by path**.
- Process exclusions for `atrium.exe` and `atrium-pty.exe` only.
- **No** process exclusion for test binaries. Their files are already under the path exclusion, and a process exclusion
  would skip everything agent-written code opens anywhere on the disk.

## 9. Permissions on the data dirs (decision 11)

`claude` **and clint's own account both get full control**. clint is the user, and wants to fix or delete things by hand
without switching accounts.

## Follow-ups

- clint wants to review the parameter sprawl personally: flags, environment variables and settings that name paths
  overlap too much. The config file is the start of that, not the end.
- Rewrite `.uncommitted/atrium-root.md` against these answers: config file, per-process data dir, no marker, no trial
  hub, fresh hub, rename feature first, popup on.
- File the room id and rename feature as its own backlog item. The trial waits for it.
- A fresh hub carries no backlog, so open items on the old hub (including this one) need re-filing by hand or by an
  agent before the old hub is retired.
