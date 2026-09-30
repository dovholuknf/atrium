# An inventory of what an agent may use (f-003)

Status: design, ACCEPTED by @rnd on 2026-09-30 (section 4). Written by @fabric. Not built. LOW. The item is
`docs/backlog/fabric/f-003.md`.

## 0. The answer, in one paragraph

A hand-edited markdown file per room, `resources.md` next to the room's state dir (`~/.atrium/resources.md` for a
default room), and one control tool, `atrium_resources`, that returns
it together with the rooms the hub already knows. The rooms are inventory entries without anyone writing them down.
No table, no migration, no board panel, no probe in the first stage. Every atrium-launched card, lean or not, is told the tool
exists in one line of its launch framing, so a review worker finds `m1mini` and the FIPS network without being told in its prompt. The
credential line in CLAUDE.md holds: the file names hosts, identities and commands, and never holds a secret.

## 1. The five questions, decided

1. **Where it lives: a file, not a table.** A table needs a migration (@runtime's), a board editor (@ui's) and an API,
   for something clint writes a few times a month. A file is edited in any editor, can be kept in a dotfiles repo,
   and costs nothing when empty. Format: one `## <name>` heading per entry, free prose under it, because the reader is
   a model. A short header comment in the file, written by `atrium resources init`, says what may go in and that no
   token, password or key goes in.
2. **How an agent reads it: a tool, and one line that names the tool.** `atrium_resources` is in the WORKER tool set
   (`ctlclass.go`), since workers are the ones who need a build machine. It returns this room's file, then the rooms
   list from the hub (name, OS and arch, build, online or not). The launch framing gains one line, "atrium_resources
   lists the machines and environments you may use", rather than the file's content, so briefs stay small and the
   answer is current when it is asked.
3. **Who keeps it current: people, first.** clint and the directors edit it. A worker that finds something (vcpkg at
   `/opt/vcpkg`, cmake off the default PATH) says it to its director, who decides whether it goes in. No agent writes
   the file, because an inventory that any session can append to is one nobody trusts. A probe is stage 2.
4. **Scope across rooms: per room, shown with where it came from.** Reachability is per machine, so each room has its
   own file. Stage 1 returns only the caller's room's file plus the hub's room list. Stage 2 has the tool also ask
   the hub for every room's file, each labelled "from room X, may not be reachable from here". A room is itself an
   entry, and its file describes what is reachable FROM it.
5. **Reservations: a convention, not a lock.** A lock needs expiry, a holder and a store, and a crashed agent leaves
   it held. Instead an entry names its working-directory rule, and the default the init header suggests is one
   directory per card, `~/work/<card alias>`. Two agents on m1mini then build in different directories. A machine
   that truly takes one build at a time says so in its entry, and the agent asks its director.

## 2. What the file looks like

```
<!-- atrium resources for this room. Names and commands only. Never a token, password or key. -->

## m1mini
ssh m1mini. macOS arm64. Apple frameworks, iOS builds, leaks. cmake, ninja and vcpkg are in /opt/homebrew/bin and
/opt/vcpkg, not on the default PATH. Build in ~/work/<your alias>.

## ziti-fips
A self-hosted FIPS OpenZiti network. Controller https://<host>:1280. Identity: the file named fips-dev in ~/.ziti on
m1mini. Ask clint before enrolling anything new.
```

## 3. Staged plan

- **Stage 1 (@fabric).** `atrium_resources`, reading `resources.md` next to the room's state dir and the hub rooms
  list, in the worker tool set. f-021's worker-set test goes from six tools to seven. `atrium resources init` writes the header. The one line in the launch framing. clint seeds the file for
  sg4 with m1mini and the FIPS network, since those two are the incidents that raised this.
- **Stage 2 (@fabric, only if stage 1 gets used).** The tool fans out to every room's file through the hub, and a
  `probe` that checks each `ssh <host>` entry with BatchMode and reports reachable and last-seen BESIDE the entry,
  never written into the file.

## 4. Decided by @rnd, 2026-09-30

1. Every atrium-launched card gets the framing line, lean or not. It is one line, and directors need build machines
   too.
2. The file sits next to the room's state dir, so an isolated or pinned room has its own. For a default room that is
   `~/.atrium` anyway.
