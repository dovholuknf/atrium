# A keep-alive fork carries the card's launch args

Status: built. The keep-alive fork carries the card's launch args (`internal/daemon/keepalive.go`).

Backlog-2 item 73. `docs/runtime/cache-keepalive-design.md` is the argument for the fork and this is one gap in it.

## The problem

The fork is `claude -p ... --resume <id> --fork-session` with `--model`, `--setting-sources local`, atrium's
block-all-tools `--settings`, `--max-turns 1` and the card's effort. It carries the card's env and effort since item
70. It carries no other launch arg, so a card launched with a different system prompt, tool list or MCP set is forked
with the default ones, the prefix differs from the first token and the fork writes the whole context at the 1h price.
That is what happened to the two lean cards in the ledger (sa55's read 0 of 123,752 and wrote 127,952). `decide` skips
lean cards today for that reason.

## Which args the cache depends on

The evidence is the code, the probes in `cache-keepalive-design.md` and the ledger. The notes on
`claude/keepalive-visible` this item cites are gone.

The request is `system` + `tools` + `messages`. A resumed conversation keeps its history, and that history already
holds CLAUDE.md, memory and skills as messages. So what the fork has to reproduce is what shapes the first two:

| arg | in the cache key | evidence |
| --- | --- | --- |
| `--append-system-prompt`, `--system-prompt` (and `-file`) | yes, system | lean sets one. It is text in `system` |
| `--disallowedTools`, `--tools` | yes, tools | probe 3: dropping tool definitions rewrote the whole prefix |
| `--mcp-config`, `--strict-mcp-config` | yes, tools | MCP tool schemas are tool definitions |
| `--agents`, `--plugin-dir` | yes, tools and system | an agent list is in the Agent tool's description |
| `CLAUDE_CODE_DISABLE_AUTO_MEMORY` (env) | yes, system | the memory section of the system prompt is present or not |
| model, `[1m]`, effort, fast mode | yes | already handled, unchanged here |
| `--settings`, `--setting-sources` | no | probes 6 and 7: the fork read the full prefix with `local` only |
| hooks, permissions, `--permission-mode` | no | hooks are not part of the request |

The last two rows are why lean's `--settings` and `--setting-sources` are NOT carried. Lean's settings copy holds the
operator's permissions and hooks, and a fork must run only atrium's block-all hook.

## What the fork takes

The fork builds the card's restart command line with the same code a restart uses, then keeps only the cache
relevant flags from it. No second copy of the launch logic:

1. `runnerArgsWith(h, resumeID, "", launchOptions{Args: t.LaunchArgs})`. This is the harness's resume args, which
   replace its base args exactly as a restart does, then the card's stored extra args. Model and effort are left
   out on purpose, the fork names its own.
2. `leanArgs(...)` over the result when `leanOptions(LaunchRequest{}, t)` says the card is lean. That is the same
   call `launchLocked` makes, with the MCP names read off the card's `atrium:mcp:` tags.
3. `keepAliveCarried` filters the result to an allowlist: `--append-system-prompt`, `--system-prompt`,
   `--append-system-prompt-file`, `--system-prompt-file`, `--disallowedTools`, `--disallowed-tools`, `--tools`,
   `--mcp-config`, `--strict-mcp-config`, `--agents`, `--plugin-dir`. A flag's values are kept with it (a variadic
   flag takes the tokens up to the next flag, and `--flag=value` is one token). Arity is not encoded per flag:
   `--strict-mcp-config` is the only boolean and a bare token after it could only be a prompt, which step 1 never
   passes.

The fork's env gets `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` when the card is lean, from the same `leanEnv` a launch uses.
The card's own env, effort and the 1h pin are unchanged.

## What it drops on purpose

An allowlist, so an unknown flag is dropped and never carried. Dropping costs at worst a miss, which the receipt
catches and stops the card on. Carrying the wrong one could make a fork do work.

- The prompt. A restart passes none anyway, and the fork has `keepalivePrompt`.
- `--resume`, `-r`, `--continue`, `--session-id`, `--fork-session`, `--name`: the fork names its own.
- `--model` and `--effort`: from the reply and `forkEffortArgs`.
- `--settings`, `--setting-sources`: atrium's block-all hook file must be the only hook source (above).
- `--dangerously-skip-permissions`, `--permission-mode`, `--allowedTools`, `--add-dir`: nothing about the request,
  and each one moves the fork toward acting. `--max-turns 1` and the hook stay the guards.
- Anything that starts a UI or a different mode: `--print`, `--output-format`, `--input-format`, `--verbose`, `--ide`.

## No stored args

A card with no `LaunchArgs` is the common case and gets exactly what the harness row's resume args carry, which for
the seeded claude row is `--strict-mcp-config`. That is one flag more than the fork sent before, and it is the flag the
card was running with. A card that predates launch options has no args and no lean tag, and behaves as before plus that.
If the harness has no resume args, or a lean card names an MCP server its runner no longer has, the launch code returns
an error and the refresh is REFUSED with the reason on the card, as a bad effort already is. A fork on the wrong
prefix is a paid miss.

## Lean cards

The `lean card` skip in `decide` goes. Its reason is gone once the fork carries the lean set. What remains
unverified is that the carried set is byte identical to what the card sent. The receipt is the check: a lean fork
that misses scores `miss`, stops the card, and two misses on two cards suspend the room, as for any card. The first
live lean refresh is the test and it is in the test plan.

## Not done

Nothing here changes the ledger, the store or the board. No migration.
