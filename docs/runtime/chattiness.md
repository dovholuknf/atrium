# Chattiness: what atrium's messages cost a session

Atrium and other agents send sessions peer says, held notices, Stop-hook nudges, `[atrium] X says:` lines and queued
messages delivered through the permission hook. This is a tool to say, with numbers, whether that is a problem. It
reads the Claude Code transcripts and changes nothing.

## Run it

```
scripts/chattiness/chattiness.sh --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00
scripts/chattiness/collect.sh    --since 2026-10-09T22:00:00-04:00 --until 2026-10-10T08:00:00-04:00
scripts/chattiness/test.sh
```

- `chattiness.sh` is bash and jq only and runs on macOS, Linux and git-bash on Windows. `--root` is a Claude projects
  directory (default `~/.claude/projects`). `--since` and `--until` are RFC3339 and optional. `--tsv FILE` names the
  per-session rows (default `./chattiness.tsv`, `-` prints them and skips the summary). The markdown summary goes to
  stdout. `--from-tsv FILE` summarizes rows already written.
- `collect.sh` runs `chattiness.sh` locally for the room named like this machine (default rooms `m1mini,sg4,sg3`, so it
  works from sg4 too) and over `ssh <room> bash -s` for the others (sg3 and sg4 read `C:/Users/claude/.claude/projects`), then merges the rows and prints one summary. A room that does not answer is
  named on stderr with the exact error and listed as not covered. `--rooms m1mini,sg4` picks rooms.
- `test.sh` checks the counts on `testdata/`, a synthetic transcript. A change in the transcript format shows up as a
  failing test. The fixture holds no real text.

Only `<root>/<project>/<session>.jsonl` is read. Subagent files sit deeper and are left out, and sidechain entries are
skipped, so a session's figures are its own conversation.

## What a turn is

A turn starts at a user entry that is not a tool result, or at a Stop hook that carried a message. It runs to the next
start. Each turn is named for what started it. Assistant usage is summed per message id, because Claude Code writes one
entry per content block and repeats the usage.

## Markers

A turn start, by source. The first match wins.

| source | marker |
|---|---|
| nudge | user entry whose text starts `[atrium] atrium says:` (for example "Your turn ended without telling your launcher anything") |
| say | user entry starting `[atrium] <name> says:`, or `Another Claude session sent a message` |
| notice | user entry starting `[atrium]` with no `says:` (`[atrium] new context:`, `[atrium] restart wake:`, `[atrium] room deploy:`) |
| stop | `stop_hook_summary` system entry whose `hookErrors` hold `Message from`, `open items from your workers` or `[atrium]` |
| human | any other user entry with text. A turn that is not typed, such as `This session is being continued`, the `Read BRIEF.md` launch prompt, `<...>` notifications and `[Request interrupted`, is `other` and is not counted as human |

A mid-turn delivery starts no turn. It lands inside a turn that is already running.

| source | marker |
|---|---|
| perm | tool result with `is_error` starting `PreToolUse:<tool> hook error:` then `Message from` or `[atrium]` |
| say, notice, nudge | `queued_command` attachment whose prompt matches the same text rules as the turn starts |
| nudge | `silent_turn_reminder` attachment (the "The user hasn't heard from you" line). Claude Code writes it, not atrium |

A queued message carries `origin.kind: human` and `humanTurn: true` in the transcript, so the origin field cannot tell
atrium from a person. The text does.

## Columns

One row per session. Token columns sum input plus cache write, output, and cache read for the assistant messages.

| column | meaning |
|---|---|
| session, project, first | session id, the project directory name, time of the first turn in the window |
| turns | turns that began in the window, not counting work before the first start |
| human | turns typed by a person |
| a_turns | turns atrium started: say + notice + stop + nudge |
| mid_perm, mid_say, mid_notice, mid_nudge | mid-turn deliveries by marker |
| tok_in, tok_out, tok_cread | all tokens in the session: input plus cache write, output, cache read |
| a_in, a_out, a_cread | the same, inside atrium-started turns only |
| a_wall_s, a_wall_max_s | seconds from an atrium message to the last assistant entry of the turn it started: sum and longest |
| a_noact | atrium-started turns with no action |
| repeats | deliveries of a text a session has already been given, counted past the first (whitespace and paste wrappers removed, first 200 characters compared) |
| say_*, notice_*, stop_*, nudge_* | per source: `_turns`, `_tok` (in + out + cache read), `_noact`, `_wall_s` |

No action is the waste signal. A turn has no action when its only tool calls are `atrium_say`, `atrium_report`,
`atrium_done` or `atrium_blocked`, or it has none. That is a reply that only acknowledges.

A turn is the whole span until the next start, so its tokens include work the message did not ask for. The tokens of a
turn are an upper bound on what the message cost. Mid-turn deliveries have no tokens of their own.

## The verdict, fixed before the first run

- A session is chatty when atrium-started turns are over 25 percent of its turns, or over 20 percent of its tokens,
  and more than half of those turns end with no action.
- A source is a PROBLEM when more than half of its turns end with no action and it holds 5 percent or more of all turns
  or of all tokens. "wasteful but small" is the same waste below that size. Otherwise it is fine.
- Repeats are worth a fix when they are over 10 percent of deliveries.

The summary prints each source's verdict from these numbers. Change the thresholds in `chattiness.sh` and here
together.

## Reading the summary

Totals give the share of turns and tokens atrium started and the share of those that ended in no action. By source
gives the verdict per marker. Worst sessions lists the ten with the most atrium-started tokens and whether each is over
the threshold. A high share with a low no-action count means a coordinator doing its job, not waste. Look at the
no-action count first, then the share.

## Limits

- Wall time stops at the last assistant entry, so a turn that ends on a long tool call is read short.
- Transcripts of a session that was cleared or compacted start over, so one card can be several sessions.
- Only rooms `collect.sh` can reach are covered. It says which.
