# Review: LangChain and OpenWiki spike (131bf5b1, m1mini, 2026-10-02): HOLD

`docs/rnd/langchain-openwiki-spike.md`, for clint. Range claude/main bd190b28..131bf5b1. The queue commit cdf7a708
is exempt and fine. Checked against OpenWiki's README at the cited commit 2b63e88f (fetched today) and the spike's
own sources.

The conclusions are right: OpenWiki as a tool a card runs, a zrok public share for the Slack callback, and LangChain
nothing now, with OTel only through a collector that holds the key. It holds on section 2. The trial it prescribes
names the wrong command and the wrong install scope. A trial run as written either asks for a provider key or
changes every card on the room.

## High: section 2's "no new key" names the keyed mode

- **The card would run the wrong command.** Section 2 says the card "runs `openwiki --update`… Run it in the
  integration mode, inside a claude card, so it rides on that card's own login". But `openwiki --update` is the
  native CLI. The README says its provider settings "apply when running OpenWiki directly. Coding-agent integrations
  use the host's authenticated model session" (README:460). In integration mode nothing runs `--update`. The claude
  card is asked ("Initialize this repository's OpenWiki…", README:66-72) and drives generation itself, through MCP
  lifecycle tools: `openwiki_begin → openwiki_submit_plan → openwiki_next_page → openwiki_submit_page → … →
  openwiki_finish` (README:102-108). A card that runs `openwiki --update` as written falls to the native CLI with
  OpenAI as the default provider. That is the key the spike says never lands. Fix section 2, the concrete uses
  (items 1 and 2: "`--init`", "refreshed by `--update`") and section 5 item 1 to say "the card is prompted to
  initialize or update, through the integration's lifecycle tools".
- **The tools are not all read-only.** Section 1 lists four read-only MCP tools. The integration also installs the
  write lifecycle above, which writes `openwiki/`, `.claims/`, `AGENTS.md` and the CLAUDE.md block. State that.
- **Install scope.** "Integrations install at user level by default" (README:86). For claude that is the room's
  `~/.claude`: a skill and an MCP server that every non-lean card on m1mini then loads, all the directors included.
  Lean workers drop them (`--strict-mcp-config`, skills by name only). "Install it on a scratch setup first" is not
  enough. The trial uses `openwiki integrations install claude --project <scratch worktree>`, and never the user
  scope on a room.

## Smaller

- **The CLAUDE.md risk is narrower than written.** "Whether that can be turned off is unverified": the README says
  OpenWiki "only rewrites its own `<!-- OPENWIKI:START -->…<!-- OPENWIKI:END -->` block and leaves the rest of each
  file untouched" (README:317). The real risk is that block: generated instructions placed in a file every agent
  loads as rules. For atrium's own repo, revert it, or run with no CLAUDE.md in the worktree. For a review clone, the
  same.
- **The PR runner use (concrete use 2).** Generate the wiki from the base branch only, never the PR head. A wiki
  refreshed on the head would put text the PR author controls into every reviewer's prime as "context", next to the
  bundle already treated as untrusted.
- **Telemetry:** say that `OPENWIKI_TELEMETRY_DISABLED=1` must be set in the card's launch env, not after the fact.
  Check once with `--telemetry-file=<path>` (README:705) that nothing is sent.

## The three questions

1. **Section 2's credential reasoning.** The conclusion holds (the host's login, no provider key on the room), but
   only with the integration lifecycle, a project-scoped install and no `--update`. As written it prescribes the
   keyed path. That is the High.
2. **Section 3, can a private share serve a Slack redirect? No, and the reasoning is right.** One sharpening: the
   redirect is followed by the user's own browser, not by Slack's servers. The tunnel exists only because Slack
   requires the registered redirect URI to be HTTPS. A private share's `zrok access` gives plain http on localhost, so
   it fails that requirement, as the spike says. The public URL is reachable by anyone while the share runs, with the
   `state` check as the only guard. So add: the share runs only for the `openwiki auth slack` window and stops after,
   not as a long-running tunnel on a reserved name.
3. **Does anything imply we open the upstream PR? No.** The status line ("no upstream PR, nothing posted"), section 3
   ("This spike opens nothing. It is clint's to open or hand to someone"), section 5 item 2 ("clint's") and Q1 all
   leave it to clint. Nothing to change.

## Verdict

HOLD on section 2: the integration lifecycle instead of `--update`, the write tools named, and `--project` install
scope. Fold the smaller points into sections 2 and 3. A re-read covers sections 1, 2, 3 and 5. doc-ok on OK.

Quality: well sourced, with line-level reads of OpenWiki and zrok and honest `unverified` marks. Section 3 is a clean
upstream sketch. The miss is the mechanism of the mode it recommends: one more README section would have caught it.
