# LangChain and OpenWiki: does either fit atrium today? (SPIKE)

Status: spike by @rnd, 2026-10-02, revised for @review's HOLD (854c4ba1, `docs/backlog/rnd/rd-new-review-131bf5b1.md`).
Design only: no code, no upstream PR, nothing posted anywhere. Asked by clint after
geowa4 sent "You should fix this https://github.com/langchain-ai/openwiki/blob/main/src/cli/commands.ts#L181" and
"openwiki needs to be zitified". clint: "it points to ngrok but adding zrok would be easy."

Sources, all read 2026-10-02:
- OpenWiki at commit `2b63e88fa1c60286430347aa6b1adbcaa08e0f64` (main): `README.md`, `package.json`, `LICENSE`,
  `src/cli/commands.ts`, `src/auth/ngrok.ts`, `src/auth/oauth.ts`, `src/cli/runners.ts` and `src/cli/cli.tsx`, through
  raw.githubusercontent.com and the GitHub API.
- zrok at `0c9d346464a7990391d26f35ecf08944c69caf95`: `cmd/zrok2/sharePublic.go`, `environment/env_v0_4/dirs.go`,
  and `sdk/nodejs`.
- The npm and PyPI registries.
- docs.langchain.com: deepagents code overview, quickstart and configuration; LangSmith self-hosted; Agent Server;
  trace-with-opentelemetry.

Nothing was cloned and run. Anything a source did not say is marked **unverified**.

## 0. The answer

- **OpenWiki fits as a tool a card runs, not as something atrium is built around.** It writes and keeps current an
  `openwiki/` folder of agent docs and a root `AGENTS.md`, and serves them to a coding agent through read-only MCP
  tools. Atrium gains repo context for its PR reviewers and workers. The trial worth doing is one repo, in a scratch
  worktree, in the mode that rides on Claude's own login, so no new key lands on a room.
- **The ngrok line is a Slack OAuth tunnel, and a zrok option is small**: about a day, one module, and an upstream PR
  for clint to decide on. It must be a zrok **public** share with a reserved name, because Slack redirects the user's
  browser to an HTTPS URL. "Zitified" in the private-share sense does not fit this use.
- **LangChain: plug in, do not run a second orchestrator.** Deep Agents Code (`dcode`) could be one more runner row.
  LangSmith could be one sink for traces, if atrium ever exports OpenTelemetry to a collector the operator runs, which
  holds the key. LangGraph and Open SWE are orchestrators, so no, as the factory landscape concluded
  (`docs/rnd/factory-landscape.md` section 5).

## 1. OpenWiki

**What it is.** The repo description is "OpenWiki is a CLI that writes and maintains agent documentation for your
codebase". package.json: "uses a DeepAgents documentation agent to generate and maintain an OpenWiki for a
codebase". License **MIT** (LICENSE, package.json, the GitHub API). Status as read:
- 16,920 stars, created 2026-06-22, 95 commits since 2026-09-01.
- Release v0.6.1 on 2026-09-28, the same version as npm `openwiki`.
- Install with `npm install -g openwiki`, which needs Node 22.22 or later.

**How it works.**
- It is built on LangChain's **deepagents** JS (pinned at 1.13.2), on LangGraph. Provider packages cover Anthropic,
  OpenAI, Google, AWS and OpenRouter. It also uses `langsmith`, the MCP SDK, `ink` for its terminal UI, and
  `posthog-node`.
- **Two ways to run, and they are not the same path.**
  - *Inside a coding agent:* `openwiki integrations install claude` (or codex, opencode, cursor and others) installs a
    skill and an MCP server. The host agent is then prompted ("Initialize this repository's OpenWiki...") and drives
    the write lifecycle through MCP tools: `openwiki_begin`, `submit_plan`, `next_page`, `submit_page` and `finish`
    (README:102-108). That runs on the host's own authenticated model session, so no provider key is configured
    (README:460). The install also adds four read-only, model-free tools: `openwiki_search`, `openwiki_read`,
    `openwiki_list_workspaces` and `openwiki_list_wikis`. **It installs at user level by default** (README:86), and
    `--project <dir>` scopes it to one directory.
  - *Standalone:* `openwiki --init` or `--update` is the native CLI. It has 13 providers, OpenAI by default, with keys
    in `~/.openwiki/.env`. Provider settings "apply when running OpenWiki directly" (README:460), so this path
    needs a key.
- **Code mode** reads the repo through `.openwikiignore`, plus a hand-written `openwiki/INSTRUCTIONS.md`, and optional
  LangSmith traces.
- **It writes:**
  - pages under `openwiki/`;
  - evidence for each fact under `openwiki/.claims/*.json`;
  - a manifest and a resume checkpoint (`.run.json`);
  - **a root `AGENTS.md`, which it maintains, and, when the repo already has a `CLAUDE.md`, a rewrite of its own
    `OPENWIKI:START`/`OPENWIKI:END` block in that file only** (README:317).
- *Personal mode* reads Slack, Gmail, X, Notion, Tavily and Hacker News into `~/.openwiki/wiki`.
- **Keeping docs current:** the native `openwiki --update` works from the git changes since the last run plus the claims that
  went stale, and a clean run makes no model calls. It ships example CI workflows (GitHub Actions, including an
  auto-merge variant with an `OPENWIKI_PR_TOKEN`, GitLab and Bitbucket) and `openwiki cron` commands. There is no
  watch mode.
- **Telemetry: PostHog, on by default.** `OPENWIKI_TELEMETRY_DISABLED=1` or `DO_NOT_TRACK=1` turns it off.

**The ngrok code.** `commands.ts` line 181 is `kind: "ngrok"`, the `ngrok` member of the CLI's command union
(lines 169-228). It is not runtime code. The command behind it works like this:

- **The command** is `openwiki ngrok start [url] [--port <p>]`, parsed at `commands.ts:337-398`, with the port
  defaulting to 53682. It runs through `cli.tsx:87` to `runNgrokCommand` (`runners.ts:48`), then `startNgrokTunnel` in
  `src/auth/ngrok.ts`. The help text says: "Start an ngrok tunnel for Slack OAuth, optionally using a fixed HTTPS URL."
- **What it exposes is the OAuth callback receiver**, and only that. `oauth.ts:384-442` listens on `127.0.0.1:53682`
  while `openwiki auth slack` runs, in a separate process. It serves `/callback?code&state` and gives 404 for
  everything else.
- **Why a tunnel is needed:** Slack requires an HTTPS redirect URI. `providerUsesHttpsRedirectOverride` is true for
  Slack only.
- **Who reaches whom:** Slack sends the user's own browser to a public ngrok HTTPS URL, which forwards to localhost.
  The tunnel has no auth of its own. The only guard is the OAuth `state` check (`waitForCode(expectedState)`).
- **How ngrok is run:** `spawn("ngrok", ["http", port] or [..., "--url", base])`, with no shell and with
  `stdio: "inherit"`. There is no ngrok npm package.
  - With a fixed URL, it validates it: https only, no port, no credentials or query, and a path of empty or
    `/callback`.
  - Without one, it polls ngrok's local API, `http://127.0.0.1:4040/api/tunnels`, every 500 ms for up to 15 s.
  - It writes `OPENWIKI_OAUTH_CALLBACK_PORT` and `OPENWIKI_HTTPS_OAUTH_REDIRECT_URI` to `~/.openwiki/.env` and prints
    the redirect URL to register in Slack.
- **The authtoken: OpenWiki handles none.** The ngrok CLI uses its own config (`ngrok config add-authtoken`).
- **When it fails:**
  - A missing binary gives `Could not start ngrok: <error>` and exit 1. The exact text is **unverified**.
  - An unauthenticated ngrok exits non-zero, giving `ngrok exited with code=...` and exit 1.
  - A discovery timeout prints the manual steps.

## 2. Fit with atrium today

**What atrium already does for agent docs.** Each repo's `CLAUDE.md` is written by hand, and in atrium's own repo it
holds the architecture rules. @review keeps reviewer files per repo and persona in dotagents, with the commit each
fact was true at (`docs/review/review-memory-design.md`). Designs live in `docs/<dept>/`. **Nothing in atrium writes or
refreshes docs on a schedule.** OpenWiki's `.claims/` (one fact, its evidence, re-checked on update) is close in
spirit to the reviewer files' "each entry carries the commit it was true at", but it is generated, not curated.

**How a card or a source could run it.**
- **A source row: no.** A source (`internal/store/sources.go`) runs a command on an interval and turns its output
  into intake items. `openwiki --update` changes files in a checkout, and that is work, which belongs on a card with a
  worktree and a review, not in an intake poll.
- **A card a director launches: yes, in integration mode.** It runs in a worktree on `claude/openwiki-<repo>`, with
  the integration installed for that worktree only (`openwiki integrations install claude --project <worktree>`). The
  card is prompted to initialize or update the repo's OpenWiki, and it drives `openwiki_begin` through `finish` on its
  own login. It commits, and the result goes to @review like any doc change. It could be scheduled later, once there
  is a scheduled launch. **The card must not run `openwiki --update`.** That is the native CLI, which needs a provider
  key (OpenAI by default) in `~/.openwiki/.env` on the room. Atrium would not hold that key, but it would be on the
  room. The integration mode keeps atrium's rule: atrium holds no third-party account credential it acts through,
  and no new key lands on the room.
- **A runner row: not needed.** OpenWiki is a batch CLI and a set of MCP tools, not an interactive agent.
- **The read-only MCP tools for any card**: `openwiki_search` and `openwiki_read` use no model, so a worker or a PR
  reviewer could look up repo context without reading the tree again. That needs the MCP server scoped to that
  card's directory, not the user-level install.

**What atrium would gain.** Repo context that keeps itself current, for the cards that start cold. The PR runner's
prime reads no reviewer file today, so each PR starts from zero (gap G7 of the PR review story, `docs/rnd/pr-review-story.md` on the orchestrator's branch `claude/pr-review-story`,
not landed). An
`openwiki/` page set for the touched area could be part of `bundle.md`.

**Risks for atrium's use.**
- **A generated block lands in `CLAUDE.md`.** OpenWiki rewrites only its own marked block (README:317), but that
  block lands in a file agents load as rules. A trial reverts that block, or runs in a tree with no `CLAUDE.md`.
- **Telemetry is on by default.** `OPENWIKI_TELEMETRY_DISABLED=1` goes in the card's launch env, and the first run
  checks it once with `--telemetry-file` to see that nothing is sent.
- **The default install is user level** (README:86): a skill and an MCP server in m1mini's `~/.claude`, which every
  non-lean card there loads, the directors included. Every atrium use installs with `--project <worktree>`.
- **For the PR runner, generate from the base branch only, never the PR head.** Otherwise the PR's author writes the
  reviewers' context.
- **Cost.** The first initialization of a large repo (openziti/ziti) is a full model run on the card's login. Updates are cheap.

**Concrete uses for clint's repos.**
1. **tlsuv first.** It is small, it is where 378 was reviewed, and it has reviewer files to compare against. In a
   scratch worktree on m1mini, install with `--project <worktree>`, and have a claude card initialize the OpenWiki
   through the integration, with telemetry off. Then compare `openwiki/` with @review's tlsuv reviewer files: what
   does each know that the other misses?
2. **openziti/ziti, ziti-sdk-c and ziti-tunnel-sdk-c**, for the PR runner. Keep a local `openwiki/` in each review
   room's clone, never committed upstream. A claude card refreshes it from the base branch, never the PR head,
   through the integration before a PR run, and the pages are fed to the prime.
3. **atrium itself, `openwiki/` only**, as an index for new directors, leaving `CLAUDE.md` hand-written.

## 3. The zrok angle, for an upstream PR clint decides on

**The shape.** Generalise the command to `openwiki tunnel start [url] [--provider ngrok|zrok] [--port <p>]`, and keep
`openwiki ngrok start` as an alias so nothing breaks. Only `src/auth/ngrok.ts` and the command parser change. The
callback server, the `.env` keys and the Slack instructions stay as they are.

**The zrok path.**
- **Sharing mode: public.** The redirect is followed by the user's own browser, and the tunnel exists only because
  Slack demands an HTTPS redirect URI. A private share is reached through a local `zrok access`, which gives plain
  http on localhost, and that fails Slack's rule, so a private share cannot work here. A public share with
  OAuth or basic auth in front would break the redirect, so the guard stays the OAuth `state` check, as it is with
  ngrok today. **The share runs only for the auth window**: started for `openwiki auth slack`, stopped when the
  callback arrives or the command exits, so the callback server is not left on the internet.
- **A stable URL.** Slack wants the redirect registered once, so the share uses a reserved name (zrok2
  `--name-selection`/`-n`, the same reservation atrium makes in `internal/daemon/overlay_reserve.go`). The exact name
  syntax in v2 is **unverified**. A run without a name gets a random URL, as ngrok's does, and the user registers it
  each time.
- **The run:** `spawn("zrok2", ["share", "public", "http://127.0.0.1:" + port, "--subordinate", ...nameArgs])`.
  `--subordinate` prints a JSON boot message with `frontend_endpoints`, which replaces the poll of ngrok's port 4040.
  Take the https endpoint and add `/callback`. Use the CLI, not the SDK: the v2 Node SDK, `@openziti/zrok2`, is in the
  zrok repo but returned "Not found" on npm today, and the v1 SDK `@openziti/zrok` targets `~/.zrok`.
- **What replaces the ngrok authtoken: nothing in OpenWiki.** The user runs `zrok2 enable <account token>` once, which
  writes `~/.zrok2`. zrok reads it, and OpenWiki never sees a token, which is also how it treats ngrok today.
- **When zrok is missing or not enabled:**
  - A missing binary (`ENOENT`) says `zrok2 not found: install zrok v2, or use --provider ngrok`.
  - An environment that is not enabled makes `zrok2` exit non-zero. OpenWiki prints its stderr plus
    `run 'zrok2 enable <token>' first`, and exits 1.
  - No endpoint within 15 s prints the same manual steps as today.
  - Optionally, fall back to v1 `zrok` when `zrok2` is absent, behind a flag.

**OpenZiti.** `@openziti/ziti-sdk-nodejs` (0.29.1, Apache-2.0) needs an enrolled identity and gives no public HTTPS
URL, so it cannot serve a Slack redirect. For this use, zrok public is the "zitified" option, since it runs on
OpenZiti. A true private OpenZiti path would need the user's browser on the ziti network, through BrowZer or a
tunneler with an HTTPS intercept and a certificate Slack accepts. That is out of scope for one CLI flag.

**Size.** About 150 to 250 lines: the provider switch, the zrok spawn and parse, the errors, and tests mirroring
`test/auth/ngrok.test.ts`. Plus a README paragraph and the CLI reference. **About one day**, and an upstream PR to
langchain-ai/openwiki, with 161 open issues and PRs, active daily. This spike opens nothing. It is clint's to open
or hand to someone.

## 4. LangChain broadly

| Piece | Current, read 2026-10-02 | License | Credentials it needs | Fit |
| --- | --- | --- | --- | --- |
| LangChain | `langchain` 1.4.3 (PyPI), 1.5.15 (npm) | MIT | provider keys | a library. Nothing for atrium |
| LangGraph | 1.2.12 (PyPI), `@langchain/langgraph` 1.4.18 | MIT | provider keys | a workflow engine. **No**: a second orchestrator beside atrium's recipes and runner |
| Agent Server ("LangSmith Deployment", was LangGraph Platform) | `langgraph-api` 0.15.1 | **Elastic-2.0** | a license key (`LANGGRAPH_CLOUD_LICENSE_KEY`) for production self-hosting, Postgres, Redis | **No**: paid, and an orchestrator |
| LangSmith | SDKs `langsmith` 0.14.3 and 0.10.7, MIT. The platform is proprietary. Self-hosting is an Enterprise add-on | `LANGSMITH_API_KEY` | **Later, maybe**: one sink for traces. It accepts OTLP at `/otel` with an `x-api-key` header |
| deepagents | 0.7.21 (PyPI), 1.14.1 (npm) | MIT | provider keys | a library, "the batteries-included agent harness". OpenWiki's base. Nothing for atrium directly |
| Deep Agents Code (`dcode`) | `deepagents-code` 0.1.80, 2026-10-01 | MIT | provider keys in `~/.deepagents/.env`. LangSmith optional | **Possible runner row.** A terminal coding agent with headless `dcode -n`, MCP (`.mcp.json`), `hooks.json` and `dcode doctor` |
| Open SWE | 10.8k stars, active | MIT | a GitHub App, LLM keys, MCP credentials. Its production Agent Server needs a license key | **No**: "an open-source software factory built on Deep Agents", atrium's own layer |

**Against the factory landscape's rule (plug in, do not run a second orchestrator):**
- **A runner, yes in principle.** `dcode` would be a harness row like codex or opencode. Its keys live on the room,
  in its own file, not in atrium. Its `hooks.json` would need an adapter for atrium's permission gate and tool hooks,
  as opencode's plugin does. The value is low to medium: one more harness, when Claude Code and codex already cover
  the work.
- **A workflow engine, no.** LangGraph would run atrium's recipes in a second engine. Agent Server adds a license
  key and a database.
- **Tracing, later and only through a collector.** Atrium exports no OpenTelemetry today (the otel modules in `go.mod` are indirect only). If it ever does, it exports
  to an OTLP collector the operator runs, and the collector holds the LangSmith (or any other) key. A key in atrium's
  own exporter headers would be a third-party credential atrium holds, which the rule forbids.

## 5. Recommendation

1. **OpenWiki: one trial, then decide.** tlsuv, in a scratch worktree on m1mini. Install with `--project`, never at
   user level. A claude card drives the integration's write tools on its own login, never `openwiki --update`, so no
   new key is needed. Telemetry is off in the launch env and checked once with `--telemetry-file`. The `CLAUDE.md`
   block is reverted, and nothing is committed upstream. Compare it with @review's tlsuv reviewer
   files. If it adds context they lack, the next step is a PR-runner item: feed `openwiki/` pages to the prime.
2. **The zrok PR: worth it, small, and clint's.** Section 3 is the sketch. A public share with a reserved name, the CLI
   with `--subordinate`, no token in OpenWiki, and ngrok kept as the default.
3. **LangChain: nothing now.** `dcode` as a runner row and OTel export to a collector are both named for later.

## 6. Questions for clint

1. **The upstream zrok PR.** Do you open it yourself from section 3, or hand it to geowa4? It would add
   `--provider zrok` beside ngrok, using a public share with a reserved name. **Default: you open it, ngrok stays the
   default provider.**
2. **The OpenWiki trial.** One run on tlsuv as in section 5 item 1, spending one initialization of model time on
   the card's own login? **Default:
   yes, after the pause, as a worker on m1mini.**
3. **`dcode` as a runner row.** File it now, or not? **Default: not now. File it low.**
4. **OpenTelemetry export** from atrium to a collector you run, with LangSmith as one possible sink and the key held
   by the collector? **Default: later, filed as a design item, not now.**
