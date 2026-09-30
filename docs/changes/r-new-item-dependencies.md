## Test plan

## @LETTER@. Work items that wait on other work

Run against a hub whose `git_repos` names the atrium checkout on claude/main. Every step is on the hub machine.

### @LETTER@1. A gate on an item that has not landed holds a launch

1. From a director session, call `atrium_deps` with `action: add`, `item: r-900`, `waits_on: ["r-901"]`,
   `why: "test"`.
2. Call `atrium_launch` with a title `r-900: test` and any existing directory.

**Expected:** the add answers one gate, `state: open`, with a reason naming `changelog/*/*-r-901.md` and the claude/main
tip. The launch is refused with `r-900 waits on: r-901 (not landed ...)` and no card appears.

### @LETTER@2. The gate clears when the work lands, and the waiter hears once

1. On claude/main in the hub's checkout, commit `changelog/runtime/2026-09-30-r-901.md`.
2. Wait a little over a minute.
3. Call `atrium_deps` with `action: check`, `item: r-900`.

**Expected:** the director that added the gate gets ONE say from `atrium-hub`: `r-900 is ready: r-901 landed (...)`. The
check answers `ready: true`. The launch from @LETTER@1 now starts. Nothing more arrives on later ticks.

### @LETTER@3. A loop is refused

1. Add `r-902` waits on `r-903`, then `r-903` waits on `r-902`.

**Expected:** the second add is refused with `r-903 waits on r-902, which waits on r-903` and nothing is written.

### @LETTER@4. Only a human clears a gate

1. Add `r-904` waits on `f-006 migration on m1mini`.
2. `curl -X POST http://127.0.0.1:<board>/_hub/deps/clear -H "X-Atrium-Agent: sa1" -d '{"id":"<id>","why":"x"}'`.
3. The same without the header and without `why`.
4. The same without the header, with `why`.
5. Call `atrium_deps` with `action: clear`.

**Expected:** 2 answers 403 and 3 answers 400, and the gate stays open. 4 answers 200, the gate is `met` by `human`,
and the audit pane shows a `deps-clear` line with the reason and `127.0.0.1`. 5 is refused: there is no clear.

### @LETTER@5. What is stuck, and what is ready

1. Call `atrium_deps` with `action: list`.
2. Call `atrium_deps` with `action: ready`, `dept: runtime`.

**Expected:** the list holds every open gate with its reason. Ready lists docs/backlog/runtime items on claude/main
that have no changelog entry and no open gate, with their title and Status line.

### @LETTER@6. A worker does not see the tool

1. From a lean worker (tagged `atrium:subagent`), list the atrium-control tools.

**Expected:** `atrium_deps` is not among them.
