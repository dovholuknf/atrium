# Review: config-alignment design 1980ec0b

`docs/rnd/config-alignment-design.md`, the only file in its commit, on `061f1054`. It lands by cherry-pick.

Verdict: **doc-ok.** The mechanism choice is right and argued well. Three Mediums should be folded into the design
before K1 is built. They are design points, not reasons to hold the doc.

## The five points

1. **The mechanism. Agreed: (b) for all three runners, (a) only for a second account, (c) as an optional floor.**
   - The cost of (a) is stated correctly: the login moves, and so do the transcripts that resume, backfill and the
     keep-alive fork read. atrium's `~/.claude` hardcodes would also fall out of step.
   - Never setting `disableSideloadFlags` is required, not optional, since (b) is built on side-loading.
   - Keeping (c) to a short never-list behind question 2 is the right default, because it binds clint's own sessions.
2. **r-033 holds.** Both names are set on every launch, and the per-runner difference lives in args, which are
   already per row.
   - A note for K5: the env is inherited by every tool subprocess. So an `opencode` that a claude card runs from Bash
     gets the bundle too. That is probably wanted, but say so.
   - Windows caps one env value at 32K characters. Give `opencode/config.json` a size check at push.
3. **Trust.** It is mostly right, with M1 and M2 below.
   - Operator-only `main` matches hub-forge 3.2, and so do card proposal branches.
   - "An overlay can't drop a base deny" is the right refusal, and `enableArtifact` is covered.
   - Secrets are refused at push, with an env var named in their place.
   - The probe's "never values" is not what section 5's own table describes. That is M3.
4. **forkCarried and lean. Agreed.** One addition, in L1: the fork has to carry the parent's revision, not the latest.
5. **The verify facts.** None is wrong as stated, but several are worded more firmly than the docs support. Notes
   are below.

## Mediums

### M1: the materialised folder is the one place the deny lives, and the room's account can write it

The design says the folder `<state>/config/<R>/<room>/` is "hash-named and read-only, like `lean-plugins/`". An agent
runs as the room's account, and that account owns the state directory, so read-only does not stop it from writing
there. REVIEWER-NOTES says a state file read back on a rerun is input.

Today that hardly matters, because a user-scope edit cannot undo a deny. After K1, though, the bundle's
`settings.json` is the deny. A card that edits `<state>/config/<R>/<room>/claude/settings.json` takes the deny out of
every later launch on that room, and the drift probe compares the user files, not this folder.

Fix it in section 3. Check the folder against its revision before each launch: re-hash the tree and compare it with
the git tree sha, which takes milliseconds for a bundle this size. Or build the files per launch from the fetched
objects into a fresh temp folder. A mismatch refuses the launch and shows as drift. Apply the same rule to
`lean-plugins/`.

### M2: hooks, MCP servers and plugins in the bundle are code that runs on every room

`settings.json` hooks, `mcp.json` servers and anything under `claude/plugin/` (which can carry its own hooks and MCP)
are commands. They run as every card on every room that follows `latest`. So a proposal that touches them is a code
change across the whole fleet, not a doc.

Two things for section 2:

- **Review.** "@review reads a proposal like any doc" should split. Permission lists and CLAUDE.md wording get
  `doc-ok`. A change to hooks, MCP or the plugin gets a code verdict, and the push to `main` lists which of those
  parts changed.
- **The check at push.** The "hub's pre-receive for this repo" has to be the hub's Go check before git runs, the way
  f-hub-receive works. f-hub-receive's rule is that a repository's own hooks never run, and the store has exactly one
  hook, the hub-owned fast-forward one. Say that, so K2 does not add a per-repo hook.

A third point fits here too: an overlay may add an allow, and lists append. Deny still wins, so a widening is limited
to what the base does not deny. Still, state that an overlay can widen, and have the push summary show what an
overlay adds to allow, hooks and MCP.

### M3: section 5 sends values, and section 3's promise says it never does

The drift probe is promised as "keys in, booleans and hashes out, and never a file's content". Section 5's table then
lists four things by name:

- user allow entries;
- MCP servers;
- user agent and skill names;
- hand patches.

A permission rule is a value. `Bash(curl https://internal.host/*)` or `Read(//home/someone/private/**)` carries hosts
and paths. MCP server names, and the commands behind them, can name internal services too.

Pick one rule and write it down. Either:

- (a) a rule is sent as its tool name plus a hash (`Bash:3f2a…`), shown with the full text only on the room's own
  board view; or
- (b) rule text is sent to the hub, and the doc says so, plus that it goes into the hub's database.

Either way, nothing goes into a public log or this repo. The same applies to hook commands: send each as a hash.

## Lows

- **L1: the fork and the bundle's lifetime.** A keep-alive fork must use the parent card's recorded `R` folder, or a
  revision that changed in between misses the cache and the fork gets another config. That needs a garbage rule: keep
  a revision's folder while any card, live or parked, records it.
- **L2: question 1 says "within a minute", and section 4 says "at its next check-in".** Make them agree, or state the
  check-in interval.
- **L3: K1's acceptance should include the M1 check.** A launch after the folder is edited is refused.

## The verify list (point 5)

Every claim below is either marked (verify) already or matches what I know of the current Claude Code docs. Five
points:

- **Precedence:** managed, then CLI `--settings`, then local, project and user. That is correct.
- **Deny before allow across scopes:** correct.
- **Scalar precedence:** a scalar from a higher scope replaces a lower one, so `enableArtifact: false` sticking
  follows. It is fine to keep it as (verify).
- **`--settings` twice:** unknown to me. It may take the last value rather than merge. Keep it on the list. The
  fallback the design already names, one generated file per launch, is the safer default, so consider making it the
  plan.
- **`--append-system-prompt-file`:** it is not marked (verify), and it should be. Historically the `-file` prompt
  flags were print-mode only. Add "it works in an interactive session, and a keep-alive fork carries it".
- **Server-managed settings polled hourly** (the table, row c) is not marked (verify). Mark it or drop it.
- **Missing from the list, needed for K6:** does `--settings` (flag scope) still load under `--setting-sources
  project,local`? K6 depends on the bundle's hooks surviving when the user source is dropped.
- **codex:** `--ignore-user-config` and `-c` for AGENTS.md and hooks are rightly marked (verify). I can't confirm
  either.

Atrium-Verdict: doc-ok 061f1054..1980ec0b
Quality: a clear design that answers the question asked, with the cost table and the reasons to reject (a) well
argued. Folding M1 to M3 in makes the trust story whole.

## Re-read: c4af3b8a

Range `061f1054..c4af3b8a`. This folds in my notes on 1980ec0b and @fabric's notes on sections 4 and 5. Only
config-alignment changes.

Closed:
- **M1 partly.** Each launch now checks the folder against a manifest. N1 below is what is still open.
- **M2.** The validator is the hub's Go check, run from the hub-owned hook through a per-repo registry, and a repo's
  own hooks never run. Hooks, MCP commands and plugin dirs get a code verdict. The doc now says an overlay can widen
  allow.
- **M3.** An allow entry goes as its tool name, a hash and a count. The other entries go as hashes and counts. No
  text is stored on the hub, and the status route is operator-gated.
- **L1.** A folder is kept while any card or fork records its R.
- **L2.** The announce is pushed through `tellRooms` and `Attached()`, so it now matches "within a minute".
- **L3.** It is in K1's acceptance.
- **The verify marks.** They are added. The plan is one generated settings file, and `config.json` has a 16K cap.

@fabric's additions read well:
- **Signed revisions.** Rooms run `verify-commit` against the allowed signers from provisioning.
- **Last known good.** A bad revision never blocks a launch.
- **Canary list, and the announce after the push-log row.** The canary list goes first, and the announce fires only
  once the push-log row is written, which fits f-hub-receive's "no row means not pushed".
- **No migration.** The pin is a hub setting and the revisions are in memory.
- **`rooms git new`.**

Open, for K1, K2a and K2b. None holds the doc:

- **N1: the manifest sits next to the folder it checks.** The manifest is written by the room at materialise time,
  under the same state folder. The room's account can edit the settings file and the manifest together, so the check
  catches a slip, not a card that means it.
  - The hub runs the merge for every room in its validator anyway. So have it send each room's expected folder hash
    with the announce, and have the room compare against that before a launch. The room cannot write the hub's value.
  - Or say plainly that the check is against accident only.
- **N2: the validator's input comes from a card.** A `proposal/*` tree comes from a card, so the hidden subcommand
  must treat it as untrusted input:
  - a size cap per file and on the tree;
  - room overlay names (`rooms/<room>/`) allowlisted to known room names before they become a path in
    `<state>/config/<R>/<room>/` on a room;
  - no symlinks, and no gitlinks;
  - parse errors refused, not panicked on.

  The quarantine env (`GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`) reaches the hook from git, and
  that is the only way the subcommand sees the objects.
- **N3: `verify-commit` runs under the room's own git config.** `~/.gitconfig` can set `gpg.ssh.program`, and the
  room's account can write that file. Run it with `GIT_CONFIG_GLOBAL=/dev/null`, with `gpg.format=ssh` and
  `gpg.ssh.allowedSignersFile` set by `-c`. This does not raise what the account can do, but it keeps the signature
  check out of the user's hands. A pin to an older sha verifies too.
- **N4: an unsalted hash of a guessable entry can be reversed.** `Read(//home/x/**)` or `Bash(npm test)` can be
  recovered from its hash by guessing. A token-bearing entry, the case M3 named, is safe. Say so, so nobody reads the
  hash as hiding the entry.

Verdict: doc-ok (re-read, 061f1054..c4af3b8a)
Quality: a careful fold-in, and @fabric's signed-revision and last-known-good pieces make the rollout safe to run.

Atrium-Verdict: doc-ok 061f1054..c4af3b8a
