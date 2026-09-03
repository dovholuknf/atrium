# Does atrium fit the rest of the stack

Four projects were named: `llm-gateway`, `mcp-gateway`, `agora`, and `sterling`. This is an assessment of whether
atrium belongs next to any of them, where it would touch, and which one is worth building.

All four were read. They are all checked out on this machine:

| Project | Path | Read | Last commit |
| --- | --- | --- | --- |
| llm-gateway | `D:/git/github/openziti/llm-gateway` | yes | 2026-08-12 |
| mcp-gateway | `D:/git/github/openziti/mcp-gateway` | yes | 2026-08-13 |
| agora | `D:/git/github/openziti/agora` | yes | 2026-07-30 |
| sterling | `D:/git/github/netfoundry/sterling` | yes | 2026-07-30 |

Two corrections to the premise before anything else.

**"sterling, also called mint" is not confirmed.** There is no product called mint in that repo. `mint` occurs five
times and every one is the ordinary verb, as in "the ledger can only refuse, never mint trust"
(`D:/git/github/netfoundry/sterling/demo/PKI.md:67`). The only prior name in its history is **silver**, renamed to
sterling on 2026-07-02. If mint is a real name it lives somewhere this machine cannot see, and nothing below
depends on the answer.

**The `mcp-gateway` MCP server configured in this session is the same project.** It is not an inference from the
tool names. `C:/Users/claude/.claude.json` configures it as `{"type":"http","url":"http://127.0.0.1:8088"}`, and
the repo serves MCP over HTTP with a namespaced tool list built by
`D:/git/github/openziti/mcp-gateway/aggregator/namespace.go:48`, which is literally
`backendID + n.separator + tool.Name` with `_` as the default separator. That is where `discourse_`, `zendesk_`
and `mercurius_` come from.

## What atrium actually is, stated so the comparisons mean something

Atrium is one operator's supervision surface for coding agents. Its distinguishing asset is not the board and not
the terminals. It is this: **a synchronous gate that can pause a tool call indefinitely until a human answers,
plus a durable record of every answer and who gave it.** Everything else in the repo exists to make that gate
usable, which is what the standing rules, the diff rendering, the notifications and the review are for.

Nothing in the other four projects has that. This is the whole basis of the assessment.

## Ranking

1. **sterling.** Real fit, exact shape match, small change on both sides. Build this one.
2. **mcp-gateway.** Higher ceiling, one hard blocker. Worth a spike, not a commitment.
3. **agora.** No fit as a substrate. A fifty-line fit as a third overlay kind.
4. **llm-gateway.** No fit. The integration people would imagine is already a harness row.

---

## 1. sterling

### Verdict

Sterling already has the exact hole atrium fills, and it knows it. A signed recipe classifies every tool method as
`auto`, `prompt`, or `deny`, and the comment on the type says why the default is `prompt`: "an unclassified method
fails toward a human" (`D:/git/github/netfoundry/sterling/internal/recipe/approval.go:3-6`). But the only human
sterling can reach is one sitting at a terminal. `approvalGate.Allow` writes `approve %s.%s %q? [y/N]` to stderr
and does a blocking `ReadString('\n')` (`internal/harness/approvalGate.go:53-56`). If the run is unattended there
is no human at all, and the gate fails closed with a message telling you to pass `--auto-approve`, which converts
every `prompt` in the recipe to `auto` at once (`approvalGate.go:47-48`). So sterling's governance model has three
dispositions and, in practice, two working ones. Atrium is a human that is not a terminal, that a run can reach
from anywhere, that remembers what it answered. This is the one integration where both sides are better for it and
neither has to become something it is not.

### The concrete seam

`internal/harness/harness.go:224`, where `newApprovalGate` is constructed, and `harness.go:297`, the single call
site `approval.Allow(tool, binding.Method, arguments)`. `harness.Options` already carries what its own comment
calls "the narrow injection seam used by tests" for `ToolClient`, `ToolBindings` and `ModelClient`
(`harness.go:66-70`). An `Approver` interface added beside them is in-idiom and roughly a day's work.

On atrium's side the seam is `POST /permission` on the agent listener
(`internal/daemon/daemon.go:470`, handled by `internal/hub/hub.go:407`). That endpoint already takes exactly what
sterling has: `agent`, `tool`, `command`, `cwd`, `pid`, `details`, `dedup_key`. It already returns
`{decision, reason}` and already blocks with no timeout. Sterling's `tool.Name` maps to `tool`, the method to
`command`, and the JSON arguments to `details`, which is the field the board renders as "what this would actually
do".

**The dependency points sterling to atrium, in one direction, over loopback HTTP.** Atrium learns nothing about
recipes, signatures, OCI, or the fabric. Sterling gains an optional flag. Neither imports the other as a module.

### The strongest argument against

Sterling's entire premise is that policy is signed and immutable, and that loosening it means re-signing the
recipe, which changes the digest, which the registry treats as a different artifact. Atrium's standing rules are
the exact opposite: unsigned, local, mutable, and imported wholesale from Claude Code's settings file, which on a
working setup means "134 rules on the first run". Wiring atrium into the `prompt` path means a signed artifact
delegates a decision to a human and an unsigned SQLite file answers on that human's behalf. Auto mode is worse,
because it is `--auto-approve` by another name, arrived at through a checkbox nobody has to justify. If that is
allowed, sterling's third disposition is decorative and the signature is proving something that no longer
constrains the run.

That argument is correct, and it does not kill the integration. It constrains it, and the constraint is stage 4
below. `prompt` means a human decides. Atrium may present the decision and record it. Atrium may not answer it
from a rule.

### What atrium would have to give up

One thing, and it is a documented invariant, so it has to be written down rather than slipped in.

**Resilience guarantee 2, "a hook must never fail a session", and guarantee 5, "the permission hook fails open when
atrium is unreachable", are both wrong for this caller.** Atrium fails open because a coding session on the
operator's own machine must not break when the board is down. A governed run must do the opposite: an unreachable
approver means nobody approved, and the call must fail closed. Sterling already fails closed in exactly this case,
and taking that away would be worse than not integrating.

So `/permission` acquires a caller class whose failure posture is inverted. Atrium's side of that is nothing at all
(a closed listener already yields connection refused). Sterling's side is a two-line decision. But the invariant
list in `CLAUDE.md` and `docs/architecture-v2.md` says "the permission hook fails open" without qualification, and
after this it must say "the Claude Code hook fails open, a governed caller fails closed, and here is why they
differ".

Nothing else is given up. No auth is invented, because sterling reaches loopback or comes over the overlay atrium
already drives (`docs/overlays.md`). No multi-tenancy, no accounts, no schema change beyond what already exists.

---

## 2. mcp-gateway

### Verdict

This is the higher prize and the lower certainty. mcp-gateway is where tool calls actually get dispatched for
every MCP client on the machine, and atrium's gate currently only reaches Claude Code, because it rides Claude
Code's `PreToolUse` hook. Gating at the gateway instead would make the permission surface harness-agnostic in a way
nothing in atrium's backlog achieves. The gateway also already has the right instinct: a per-call policy check that
runs before dispatch, `backend.policy.Prepare(...)` at `gateway/session.go:331`, returning a tool-level error the
model reads rather than a transport failure (`aggregator/policy.go:303-310`). But that check is synchronous, pure
static config, and structurally incapable of pausing or escalating. There is no hook parameter on `SessionFactory`
(`gateway/session_factory.go:26-38`), no middleware chain, no plugin interface. Adding a human gate is not
extending a seam, it is creating one. And there is a harder problem underneath, below.

### The concrete seam

Same atrium endpoint, `POST /permission`. On the gateway side the insertion point is `gateway/session.go:331`,
immediately beside the existing policy call, with the namespaced tool name as `tool` and the settled arguments as
`details`. The gateway would need a new config block naming the atrium address, and it would need to become
capable of blocking a tool call on a network round trip of unbounded duration, which today it never does.

Dependency again points mcp-gateway to atrium. Atrium learns nothing about MCP.

### The strongest argument against

**Atrium could not name the caller, and naming the caller is the point.** The README says it plainly: "Every request
says which agent is asking, because with several running the same command means different things from different
sessions." mcp-gateway has no application-layer caller identity at all. Its `ClientContext` is
`{RemoteAddr, UserAgent, Headers}` (`gateway/session.go:22-27`), it authenticates nobody, and over zrok or Agora
the remote address is an overlay address rather than a user. Its per-call audit lines carry a `session_id` and no
principal (`gateway/session.go:372-379`). So every gated call would arrive at atrium as the same anonymous caller
and the board would show one card labelled `mcp-gateway`, holding requests from every agent at once, with shelving
and messaging and auto mode all meaningless because they are per-card and there is one card.

A second argument, weaker but real: mcp-gateway's identity story is deliberately delegated to the network. Its
zrok share is created with `sdk.OpenPermissionMode` (`gateway/share.go:38-42`) precisely because "who may connect"
is an overlay policy, which is the same reasoning `docs/overlays.md` uses for atrium. Asking it to grow a
per-caller approval gate asks it to grow a per-caller identity first, and that is a project, not an integration.

### What atrium would have to give up

Same inverted failure posture as sterling, plus one thing it can afford less. It would have to accept a card that
is not a session, or grow a second identity concept for callers that have no session. `wire_name` and the observed
bucket assume a process with a pid and a working directory, and a gateway request has neither. That is a real
change to the domain model, not a new endpoint.

**Verdict: spike it, do not commit.** The spike is one question and it belongs to mcp-gateway, not atrium: can a
caller be named? If the gateway grows even a weak caller label, propagated from a header the client sets, this
becomes the best integration of the four. Until then it produces a board with one card on it.

---

## 3. agora

### Verdict

No fit as a substrate, and the reasons are already written in atrium's own non-goals. Agora is a controller with
PostgreSQL, organizations, accounts, workgroups, contracts, cross-org invitations, an admin token plane and a
browser dashboard with its own audit log. Atrium's second non-goal is "no multi tenancy, no accounts, no billing,
no public deployment story" and its first is "not a product, single user, single operator". Adopting agora means
adopting the noun `organization`, and there is no version of that which leaves atrium as the thing it is. Note too
that both `CLAUDE.md` references to agora are already satisfied by something else: "if we ever needed auth" is
still hypothetical because atrium still has no auth and still does not want any, and cross-machine access shipped
as `docs/overlays.md`, which drives `ziti tunnel host` and zrok directly and never opens an identity file.

There is one real, small thing. Agora's Layer 1 tunnel is a third transport of the same kind atrium already
supports, and `docs/overlays.md` closes by saying a third overlay is "an entry in each, not a branch through the
rendering": one entry in `OVERLAY_UI`, one in `overlayViews`, one `*Args` method. `agora tunnel` binding the
board's address is a genuine fifty-line change with a visible payoff, and it is the correct size for an agora
integration.

### The concrete seam

`internal/daemon/overlay_config.go`, alongside `ZrokConfig` and `ZitiConfig`. Atrium would shell out to the `agora`
CLI the same way it shells out to `ziti` and `zrok`, and hold no identity. The dependency points atrium to agora,
as a child process, which is the only direction `docs/overlays.md` allows.

The other seam people will propose is atrium publishing itself as an agora advertisement so agents can discover it
in the catalog. Do not build that. It would make atrium an agent-to-agent participant, and atrium is a human
surface. An operator who wants the board from their phone wants an address, not a catalog entry.

### The strongest argument against

Even the small version is speculative in the direction that matters. Agora requires an external PostgreSQL, an
OpenZiti controller and an enrolled edge router before a tunnel exists at all, and Layer 1 is described by its own
README as "minimum-working". Atrium's ziti overlay already reaches the board over the same fabric with an enrolled
identity file and one command. Adding agora buys a second path to a destination atrium can already reach. The
honest reason to do it is that the team standardises on agora and atrium should not be the holdout, and that is a
political reason rather than a technical one. It is still a fine reason. It is just not worth pretending it is
about capability.

### What atrium would have to give up

For the overlay entry: nothing. It is the extension point working as designed. For anything larger: accounts, which
is not on the table.

---

## 4. llm-gateway

### Verdict

No fit, and the reason is already in `docs/architecture-v2.md`: "claude-code is the agent... It also holds the
subscription credentials, so bypassing it in favor of direct API calls would mean paying per token as well as
rebuilding everything it does." llm-gateway is an OpenAI-compatible proxy with four routes, and claude-code does
not talk to one. Everything atrium supervises either holds its own credentials or is a bare shell. Beyond that,
llm-gateway has no interception point, no approval concept, no audit log at all (its logging is `dl.Infof` lines
and an optional request tracer), and its policy surface is two allowlists on a bearer key: model globs and route
names (`keys/record.go:24-47`, enforced at `gateway/handler.go:149` and `:219`). There is nothing here for a
permission gate to attach to and nothing atrium wants to read.

### The concrete seam

There is one, and it is already available with no code on either side. A harness row in atrium is a command, args,
a working directory and an environment. An ollama or codex runner pointed at an llm-gateway is an environment
variable in that row. That is the integration, it works today, and calling it an integration overstates it.

The seam people will propose instead is cost attribution: mint an `sk-gw-*` key per atrium card, and let the board
show what each agent spent. It does not work yet on llm-gateway's side and it says so. Its own roadmap file
`docs/future/roadmap/per-key-metrics.md` is in state `researching` and states "the token counters aren't keyed, and
the streaming path meters no usage at all". So the feature depends on unbuilt work in a repo nobody here owns, to
report spend for runners that do not route through it.

### The strongest argument against

Atrium would become a cost dashboard. That is a different product with a different reason to exist, and it competes
for the one thing this project is short of, which is the operator's attention on the gate. Note also that sterling
has already solved the part of this that matters and solved it better: a recipe cannot be signed without a spend
ceiling, and the ceiling is enforced by the harness rather than reported after the fact
(`internal/harness/budget.go`). A bound that stops a run beats a number on a card.

### What atrium would have to give up

Focus, and the "answer every question that does not need a model" goal, which is about facts the operating system
already knows. Token spend is not one of those. It is a number from a third party that atrium would have to poll,
cache and reconcile.

---

## Recommendation

**Build the sterling integration. Spike the mcp-gateway caller-identity question and decide afterwards. Do not
build the other two**, beyond the agora overlay entry if and when the team standardises on it.

Sterling wins on four counts that the others do not share. The hole is already named in its own source comments.
The wire shapes match without translation. The dependency points one way and neither project imports the other.
And atrium changes almost not at all, which means the integration cannot rot the thing it is attached to.

### Stage 1: prove `/permission` answers a caller that is not Claude Code

Nothing is built. A `curl` POST to `http://localhost:7777/permission` with `agent`, `tool`, `command`, `cwd` and
`details` set to a blob of JSON arguments.

**How you know it works.** A card appears in `needs-permission` named after whatever `agent` you sent, the board
renders the arguments in the details pane, and `curl` stays blocked until you click. Approve, and `curl` prints
`{"decision":"approve"}`. Block with a reason, and the reason comes back in the body. If the details pane renders
raw JSON badly, that is discovered here, for free, before anyone writes Go.

### Stage 2: extract an `Approver` interface in sterling, changing no behavior

`Approver` is one method with `approvalGate`'s current signature. `harness.Options` gains an `Approver` field beside
`ToolClient` and `ModelClient`. When it is nil, `Run` constructs the terminal gate exactly as it does today.

**How you know it works.** Every existing sterling test passes untouched. Attended `y/N` behaves identically,
`--auto-approve` still converts prompts and still leaves signed denies denied, and an unattended run with a `prompt`
disposition still fails closed with the same message. This stage ships on its own merits: it is the same seam
sterling already built twice for its clients.

### Stage 3: an atrium approver, failing closed

A new `Approver` that POSTs to atrium and blocks. `tool.Name` to `tool`, the method to `command`, the arguments to
`details`, the run's working directory to `cwd`, and a hash of the three to `dedup_key` so a retry is the same
question. A block returns atrium's `reason` as the error text, which is what puts the operator's actual guidance in
front of the model instead of a bare refusal.

**Unreachable atrium is a denial, not an approval.** This is the inverted posture, and it is the one line of this
stage that matters most. Write it into `docs/architecture-v2.md` under "Resilience guarantees (daemon)" in the same
commit, or the invariant list becomes a lie.

**How you know it works.** Run the `code-reviewer` example with `write_file` left at `prompt`. The harness stalls,
the board shows the file and the content it would write, you approve, and the file appears. Repeat with a block and
a typed reason, and the reason is in the harness error. Then stop the daemon mid-run and confirm the harness stops
rather than proceeding.

### Stage 4: standing rules and auto mode do not answer a signed prompt

The stage that keeps the signature meaningful. A governed caller is marked as such, and for it the permission chain
skips steps 4 and 5, standing rules and auto mode. Steps 2 and 3, the queued message and the shelved card, stay,
because both are the operator acting rather than a stored answer.

**How you know it works.** Write a rule that would obviously match the command. Run the recipe. The rule does not
fire, the request reaches the board, and the permission history records the decision as yours. Turn auto mode on
for that card and confirm the request still stops.

### Stage 5: one record, and it names the artifact

Sterling writes decisions into its run history and atrium writes them into `permission`. Two records of the same
event will drift. Cheapest resolution: atrium's row carries the recipe digest the run was materialized from, so
`GET /v1/tasks/{id}/review` can be read against a specific signed artifact rather than against a session.

**How you know it works.** The review page for a sterling run lists every decision, folds the identical ones the way
it already does, and names the digest they were made under. Pull a different version of the same recipe and the two
runs are distinguishable in the history without reading the commands.

---

## What I could not verify

- **The mcp-gateway spike's central question.** Whether a caller can be named is a decision by that repo's
  maintainer, not something readable from the code. Michael Quigley authored almost all of mcp-gateway, agora and
  sterling. Every conclusion here about what those projects would accept is my reading of their code and comments,
  not their maintainer's intent, and that is the largest gap in this document.
- **Whether "mint" is real.** No trace on this machine. If it is a separate project it was not assessed.
- **Whether the sterling approval seam is wanted.** Sterling has 27 commits, its last is 2026-07-30, and atrium
  appears nowhere in it. mcp-gateway's `aggregator/policy.go:66` and `aggregator/environment.go:15` name sterling
  as a consumer, so that direction of integration is live between them. Nothing suggests anyone has considered
  atrium. Stage 2 is deliberately written to be worth shipping to sterling on its own, so that the answer to that
  question can arrive late.
- **Live behavior of any of the four.** Nothing was run. mcp-gateway on `127.0.0.1:8088` did not answer a direct
  MCP `initialize` probe within five seconds, so its live tool list was not enumerated. Every claim here is read
  from source, and none is from execution.
- **The agora overlay estimate.** "Fifty lines" comes from `docs/overlays.md` describing its own extension points.
  Whether `agora tunnel` has a subcommand shaped like `ziti tunnel host`, that binds an address and prints
  something atrium can match, was not checked against the CLI.
- **Anything the four projects have not written down.** All four keep `docs/future/` roadmap directories. Those
  were read only where they answered a question asked here, so a planned feature that changes one of these verdicts
  could exist and be missed.
