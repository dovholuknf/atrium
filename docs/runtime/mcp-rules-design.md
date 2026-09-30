# Standing rules that name MCP tools (r-034)

Status: BUILT 2026-09-30 to this design, with the answers @rnd gave to the open questions below (see
`docs/backlog/runtime/r-034.md`). One correction is marked in section 4. Author: the r-034 worker for @runtime.

The gate already sees every `mcp__` call and the store can already hold a rule for one, but nothing can create a
useful one. The importer skips every `mcp__` entry, and the rule editor would build a rule from compacted JSON. This
document settles what such a rule matches, how the importer reads `settings.json`, and what a deny does against a
broad allow. It changes no rule of the permission chain.

## What is true today

- `runPermissionHook` (`internal/cli/hook_permission.go`) posts `tool` as the raw tool name and `command` as
  `permSummary`. For an MCP call there is no `command`, `file_path`, `url` or `pattern` field, so the summary falls
  through to the compacted `tool_input` JSON.
- `MatchRule` selects `WHERE tool = ?`, so an exact-name MCP rule already fires. A glob over the name cannot.
- `claudeconf.convert` looks the tool up in `toolsWeGate`, which has no `mcp__` entry, so every MCP entry becomes a
  `Skipped` with "atrium does not gate mcp__...". `TestConvert` and `TestLoadFromSettingsFile` assert that.
- The board's "always" and "never" on an MCP card go through `DefaultPrefix`, which splits the JSON on whitespace and
  keeps up to two fields. The result is a prefix like `{"query":"ziti` that matches almost nothing later.
- `specificity` ties are broken by row order, which SQLite does not define. Nothing has depended on that until now.

## 1. What an MCP rule matches: the tool name, and only the tool name

**Decision.** An MCP rule matches on `tool` alone. Its `prefix` is fixed at `*`, meaning any input. The `tool`
column holds either an exact name or a glob over the name.

Why not a pattern over the arguments:

- The summary is `json.Compact` of what the model wrote. Key order and spacing are the model's choice, and a run
  of `{"query":"x","limit":5}` and `{"limit":5,"query":"x"}` are the same call with different text. A prefix rule
  would approve one and ask about the other, with no visible reason. A rule that fails silently is the failure
  `store/rules.go` already goes out of its way to avoid.
- Claude Code does not offer it either. Its docs say "When Claude Code loads a settings file, it skips any `mcp__`
  rule that has parentheses", and that an allow for one parameter value "wouldn't establish that the call is safe
  overall". An importer that invented argument matching would create rules the operator never wrote.
- The dangerous distinction in a gateway is between tools, not between arguments. `discourse_discourse_search` reads
  and `discourse_discourse_create_user` writes, and both take a free-form object. The tool name carries that.

Why not a stable summary of chosen arguments (say, the first string field): it needs a per-tool table of which
argument matters, for servers atrium has never heard of. That is `permSummary` growing a registry, and a rule
against a chosen argument is still an allow on one argument value. Not now. If a real case appears the extension
point is a second kind of rule, and section 6 says what that would cost.

**Accepted names**, which follow Claude Code's MCP syntax (`code.claude.com/docs/en/permissions`, "MCP"):

| Rule text | Stored `tool` | Meaning |
| --- | --- | --- |
| `mcp__mercurius__discourse_discourse_search` | same | that one tool |
| `mcp__mercurius__discourse_discourse_*` | same | a glob, after the literal `mcp__<server>__` |
| `mcp__mercurius__*` | same | every tool of that server |
| `mcp__mercurius` | `mcp__mercurius__*` | server-wide, spelled without the wildcard, normalised |
| `mcp__*`, `mcp__m*__x`, `*` | refused | see below |

Claude Code's own rule for allow globs is that the glob may appear "only after a literal `mcp__<server>__` prefix"
and the server segment "must be glob-free". Atrium takes the same rule for allow AND deny. The deny side is stricter
than Claude Code, which accepts a bare `mcp__*` deny, on purpose: a standing deny of every MCP tool would also deny
`mcp__atrium-control__atrium_report`, the call that ends a worker's turn. The importer reports that one (section 2)
and does not silently store it.

Normalising `mcp__mercurius` to `mcp__mercurius__*` and never keeping the bare form matters. A bare form matched by
prefix would also cover a server called `mcp__mercurius-staging__...`. With the trailing `__` the server segment has
to end where the operator's did.

The names are the real ones. From the mercurius server: `mcp__mercurius__discourse_discourse_search`,
`..._read_topic`, `..._create_topic`, `..._create_user`, `..._update_user`, `..._upload_file`, and the session tools
`mcp__mercurius__mercurius_open_session`, `..._collect_round`. The doubled `discourse_discourse` is how that server
names them, which is why a glob such as `mcp__mercurius__discourse_discourse_get_*` is the useful shape for "the
readers". From atrium: `mcp__atrium-control__atrium_report`, `atrium_say`, `atrium_peers`, `atrium_status`.

**Matching mechanics** (for the implementer, not code):

- `MatchRule` widens its candidate query from `tool = ?` to `tool = ? OR tool LIKE 'mcp\_\_%\*%' ESCAPE '\'`, so
  glob rules are candidates only for names that start with `mcp__`. Every other tool keeps exactly today's query.
- A candidate whose `tool` contains `*` is matched by `globRE` against the request's tool name. The existing regexp
  cache is reused. A glob that does not compile matches nothing.
- The candidate's `prefix` (`*`) then goes through `matchPattern` as today, and matches any summary, including the
  empty one.
- Ranking becomes `specificity(tool) + specificity(prefix)`. For every existing rule all candidates share one `tool`,
  so the first term is a constant and no existing ordering changes. For MCP rules it is what puts an exact name above
  a glob above a server-wide rule.
- A rule row written before this, with an exact `mcp__` tool and a real prefix, keeps working as it did.

## 2. The importer

`convert` gains a branch for names starting `mcp__`, ahead of the `toolsWeGate` check. It turns each entry into an
`Entry` with `Tool` set to the normalised name and `Pattern` set to `*`.

| `settings.json` entry | Allow becomes | Deny becomes |
| --- | --- | --- |
| `mcp__s__t` | approve, tool `mcp__s__t` | block, same |
| `mcp__s__pre_*` | approve, glob | block, glob |
| `mcp__s__*` or `mcp__s` | approve, `mcp__s__*` | block, `mcp__s__*` |
| `mcp__s__t(anything)` | skipped: "MCP rules match the tool name, not its arguments" | skipped, same reason |
| `mcp__*` | skipped: "the server name must be spelled out" | Broad, imported only with `include_broad` |
| `mcp__*__t`, `mcp__s*` | skipped: "the server name must be spelled out" | skipped, same |
| `mcp__` alone, or an empty server | skipped: "no usable pattern" | skipped, same |

Three points behind the table:

- **Parentheses are skipped, not guessed.** Claude Code skips them too, so the operator's settings never meant them.
  Skipping reports the entry on the import dialog instead of dropping it.
- **A deny `mcp__*` is Broad.** `Entry.Broad` today means "every request for this tool". Here it means "every MCP
  tool". It needs `include_broad`, and the dialog should say it includes atrium's own tools. An unanchored ALLOW glob
  is refused outright, because Claude Code refuses it ("skipped with a warning and doesn't auto-approve anything")
  and atrium approving it would be more permissive than the file it was read from.
- **A server-wide allow is not Broad.** The name carries the narrowness, so `mcp__mercurius` and `mcp__mercurius__*`
  import by default. If it were Broad, the common case would be skipped, which is the bug this task exists to fix.

**Deny-first, restored at import.** Claude Code evaluates deny, then ask, then allow, and "rule specificity doesn't
change the order". Atrium's store lets a narrow rule beat a broad one in either direction. So this pair means
different things in the two systems:

```
deny:  mcp__mercurius__*
allow: mcp__mercurius__discourse_discourse_search
```

Claude Code blocks the search. Atrium, imported literally, would approve it, because the exact name is more specific.
The importer therefore drops an MCP allow that a deny in the same import covers, across all four settings files, and
reports it as `Skipped` with "shadowed by deny mcp__mercurius__*, which Claude Code applies first". The reverse pair
(broad allow, narrow deny) imports both and the store gets it right by specificity, which is section 5.

This is done for MCP entries only. The same mismatch already exists for `Bash` and is left alone, see Open Questions.

`ask` entries in `settings.json` are not read by the importer today and stay unread. An `mcp__` ask is not converted
into anything, because a rule can only approve or block.

The tests that change: `TestConvert` line 68 (`mcp__ziti-mcp` is now an entry, `mcp__ziti-mcp__*`, approve, not
Broad) and `TestLoadFromSettingsFile` (the "at least 3 skipped" count loses one, and the entry is asserted).

## 3. The API, and the board (@ui, not code here)

**API.** `POST /v1/rules` (`addRule`, `internal/api/api.go`) and the import endpoint need:

- `tool` beginning `mcp__` is accepted with `prefix` empty or `*`. Any other prefix is `400`, saying that an MCP rule
  matches the tool name and not its arguments. That keeps the fragile JSON-prefix rule from being creatable by hand.
- `kind` stays `command`, `path` is refused for an `mcp__` tool.
- The tool name is validated by one store function (`AddMCPRule`, name to be settled by the implementer) that applies
  the section 1 table. It refuses with `400` and the reason, as `addRule` already does for a bad glob.
- The approve-forever and never paths (`api.go` near line 325) call the same function for an `mcp__` tool, with the
  exact tool name, instead of `DefaultPrefix`. Widening to the server is a second call to `POST /v1/rules` with the
  `mcp__s__*` name, so no new field is needed.
- `Rule.Prefix` stays `*` on the wire, so the rules list needs no new field. The board can tell an MCP rule by its
  tool.

**Flag for @ui.** Three board changes, none of which the API forces before the store change lands:

1. The "always" and "never" buttons on a card whose tool starts `mcp__` should read "always allow this tool" and
   "never allow this tool", with a choice to cover the whole server (`mcp__s__*`) beside it.
2. The rule list shows `prefix` as the match. For an MCP rule it should show the tool name, and show "any input" where
   it shows `*`.
3. The import dialog groups skipped entries by reason. The new reasons above need their own wording, and the deny
   `mcp__*` case should say it includes atrium's own tools.

**Audit text.** `daemon.go:809` records `rule.Prefix` as the decider. For an MCP rule that would record `*`, which
tells nobody anything. It should record the tool pattern for a tool rule. This is one line for the implementer and
part of the same change, not @ui's.

## 4. Where it sits in the permission chain: step 4, and nothing moves

An MCP rule is a standing rule, step 4 of `onPermRequest`, and nothing is added, removed or reordered.

```
1 replayed decision   2 queued message   3 shelved card   4 STANDING RULE   5 auto mode   6 ask the human
                                                              ^ an MCP rule is here, beside Bash and Edit rules
```

The consequences are the ones the chain already promises, restated for MCP because this is the first time it applies:

- A queued message (2) and a shelved card (3) still win. A shelved card blocks `mcp__atrium-control__atrium_report`,
  as it blocks everything else.
- An MCP allow beats nothing above it. It cannot approve a call while a message is queued for the session.
- Auto mode (5) is below the rule. `auto` approving every call of a gateway session does not undo an MCP deny, so a
  deny on `discourse_discourse_create_user` holds under board-wide auto, exactly as `Bash(git push*)` does.
- `drain.go:58` calls `MatchRule` with the same arguments, so turning auto mode on approves the queued MCP calls
  that no rule answers or an approve rule covers, and leaves one that a block rule covers in the queue. (An earlier
  draft of this line said the drain answers a rule's calls with the rule. It does not: it never blocks, it only
  declines to approve. Corrected when the test for it was written.)

Nothing in `internal/cli` changes either. `permSkipTools` and the `mcp__` gating question in cr48 cli finding 01 are
independent: skipping the atrium-agent tools in the hook is one answer to that finding, and a standing rule
`mcp__atrium-agent__*` approve is another that needs no hook change and keeps the audit row. This design does not
choose between them, see Open Questions.

## 5. Safety: a deny for a write tool beats a broad allow

The store's rule is "most specific wins", by literal characters, wildcards not counted. With the section 1 ranking:

| Rules present | Request | Answer | Because |
| --- | --- | --- | --- |
| allow `mcp__mercurius__*`, deny `mcp__mercurius__discourse_discourse_create_user` | `..._create_user` | **block** | exact name, more literal characters |
| same | `..._search` | approve | only the allow matches |
| allow `mcp__mercurius__*`, deny `mcp__mercurius__discourse_discourse_create_*` | `..._create_user` | **block** | the deny glob has more literal characters |
| allow `mcp__mercurius__discourse_discourse_*`, deny `mcp__mercurius__*` | `..._search` | approve | narrow beats broad, in the allow's favour |
| allow `mcp__mercurius__discourse_discourse_create_user*`, deny `mcp__mercurius__discourse_discourse_create_user` | `..._create_user` | **block** | equal literal length, see below |

The fourth row is the one that differs from Claude Code, and it is why the importer drops a shadowed allow (section 2).
Hand-written rules on the board keep the store's meaning, which is "the narrower answer is the one I thought about".

**A tie goes to block.** Two matching rules with equal specificity must not be broken by row order, and today they are.
The fix is in `MatchRule`: on equal specificity, a `block` replaces an `approve`. It applies to every rule, not only MCP
ones, because there is no case where an operator who wrote both an approve and a block of equal reach meant the approve.
The last row above is the case that reaches it, since a glob whose literal part is the whole name ties with the exact
rule.

**Scope does not change precedence.** A scoped rule and an unscoped one compete on specificity alone, as today. A scoped
allow does not outrank an unscoped deny of the same reach, because the tie goes to block.

**What a deny cannot promise.** It matches the name the runner reports. A server that exposes the same operation under
a second tool name is not covered, and a deny is a standing answer, not a sandbox. The design says so on the board next
to the rule list, and does no more.

## 6. Migration: none

No new rule kind is needed, so nothing is added to the migration slice.

`perm_rule.tool` is `TEXT NOT NULL` with no `CHECK`, so a glob in it needs no schema change. `kind` stays `command`, and
its `CHECK (kind IN ('command','path'))` is untouched. Widening that `CHECK` costs a rebuild of `perm_rule`, which is
what `0014` did, and this design avoids it deliberately: `store/CLAUDE.md` calls that "a fair price once and a bad
habit".

The `UNIQUE (tool, prefix, scope, kind)` key already gives the right identity. `mcp__s__*` with prefix `*` and scope
`''` is one row, and adding it twice updates it, as `addRule` does now.

If argument matching is ever wanted, that is when a kind is named. It would be added at the END of the slice as
`00NN_rule_kind_tool` with a table rebuild in the `0014` pattern, and it should be written against the first real tool
that needs it rather than in advance.

Existing databases: a rule with an exact `mcp__` tool and a junk JSON prefix, if one exists, keeps matching as before.
Nothing rewrites it. The rules list will show it with its prefix, which is the honest display.

## Test list

Store (`internal/store/rules_mcp_test.go`, new):

1. Exact rule: approve `mcp__mercurius__discourse_discourse_search` answers that tool and no other.
2. Server glob: approve `mcp__mercurius__*` answers both `..._search` and `mercurius_open_session`, and does not
   answer `mcp__mercurius-staging__x` or `mcp__atrium-control__atrium_say`.
3. Prefix glob: approve `mcp__mercurius__discourse_discourse_get_*` answers `..._get_user`, not `..._create_user`.
4. Specificity: the five rows of the section 5 table, each as one assertion.
5. Tie: two rules of equal specificity, one approve one block, return the block whichever was inserted first. Run with
   both insertion orders.
6. The tie fix does not disturb an existing case: a Bash approve and a Bash block of equal specificity, block wins.
7. A glob rule is not a candidate for a non-`mcp__` tool: a request for `Bash` never runs the glob query path.
8. Validation: `mcp__*`, `mcp__m*__x`, `mcp__`, `*`, `mcp__m__t(x)` each refused with a reason. `mcp__m` stored as
   `mcp__m__*`.
9. Scope: a scoped MCP rule answers only its worktree, an unscoped one answers all.
10. Idempotence: adding `mcp__s__*` twice leaves one row and updates the decision.
11. A pre-existing exact `mcp__` rule with a real prefix still matches by prefix.

Claudeconf (`claudeconf_test.go`, two existing tests change):

12. `mcp__ziti-mcp` allow becomes tool `mcp__ziti-mcp__*`, pattern `*`, approve, not Broad. This replaces the assertion
    at line 68.
13. `mcp__a__t`, `mcp__a__t_*`, `mcp__a__*` in allow and in deny each become the right decision.
14. `mcp__a__t(x)` is skipped with the argument reason, in allow and in deny.
15. Allow `mcp__*` is skipped. Deny `mcp__*` is an entry with `Broad` true.
16. Shadowing: deny `mcp__s__*` with allow `mcp__s__t` in one file, and with them in two different files, drops the
    allow and reports it. Allow `mcp__s__*` with deny `mcp__s__t` keeps both.
17. `TestLoadFromSettingsFile` asserts the MCP entry is imported and that the skipped count is one lower.

API (`internal/api`):

18. `POST /v1/rules` with tool `mcp__s__*` and empty prefix is `200`. With prefix `foo` is `400`. With kind `path` is
    `400`.
19. Import with `include_broad` false reports deny `mcp__*` as skipped, and with it true stores it.
20. Approve-forever on an MCP permission creates an exact tool rule, not a JSON-prefix rule.

Daemon (`internal/daemon`):

21. Chain order with an MCP call: an approve rule answers, and a queued message, a shelved card and a block rule each
    still answer first or as the rule says. Auto mode on with a block rule still blocks.
22. The audit row for an MCP rule decision names the tool pattern, not `*`.
23. `drain` approves the queued MCP call no rule covers, and leaves one a block rule covers in the queue.

## Open Questions for @rnd

1. **Extend deny-first at import to Bash, Edit and the rest?** The mismatch with Claude Code is the same and exists
   today. This design fixes it for MCP only, because the tests and the cost of being wrong are small there. Doing it
   everywhere changes what an existing import produces.
2. **The tie rule changes every rule, not only MCP ones.** It is one comparison in `MatchRule` and no stored rule
   changes. Say if you would rather it be scoped to MCP rules and leave the undefined order elsewhere.
3. **Settle cr48 cli finding 01 first or together?** Skipping `mcp__atrium-agent__*` in the hook removes the
   Mode A stall at the source, and a standing rule does the same with an audit trail. They do not conflict. The order
   only matters for whether a Mode A operator needs the importer change to be quiet.
4. **A bare deny `mcp__*` is stricter here than in Claude Code.** It imports only with `include_broad`. Is that the
   right line, or should it be refused outright because it blocks atrium's own tools?
5. **`ask` entries.** A rule cannot say "ask", and Claude Code lets an `ask` beat an `allow`. Import ignores them, so
   an operator's `ask mcp__mercurius__discourse_discourse_create_topic` is silently lost beside an
   `allow mcp__mercurius__*`. Should an `ask` shadow an allow the way a deny does (drop it and report), or is a
   report enough?
6. **Where the record of who decided goes.** The audit change at `daemon.go:809` touches the `by` value that the board
   already reads. Confirm nothing keys on `by` equalling a prefix before it changes.
7. **Claude Code's docs are the source of truth for the syntax, and they move.** The table in section 1 was read on
   2026-09-29. A test that fails when a documented form stops importing would need the docs in the repo, which they
   are not. Accepting that the table can drift is the alternative.
