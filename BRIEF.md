clint asked for this. You are on branch claude/r-keepalive-stale-badge, from claude/main ae80f76f, in a worktree on sg4.

BUG: the sg4-control orchestrator card shows "stopped · cache missed, state stopped:miss" since 03:25. It is actively taking turns, and the same tooltip says "cache warm until 09:09". clint: "you're actively working right now? how could the cache be cold?"

CAUSE, already found: sg4-control's setting `cache_keepalive_suspended` = "two refreshes in a row on two cards missed the cache". In internal/daemon/keepalive.go, `tick` returns at the top when `k.st.KeepaliveSuspended() != ""`, so it never reaches `clearOnRealTurn` (line ~896). That is what puts a stopped:miss / break-even / failing card back to on after a real turn. So while a room is suspended, every card's stopped badge is stuck.

FIX:
1. While suspended, still run `clearOnRealTurn` for each card, and do no refresh. Moving the check is fine, but keep the early return before any refresh.
2. Make the board say the room is suspended and why (the reason string), so a card does not blame itself. Look at internal/api/web/js/keepalive.js, which renders the tooltip.
3. Decide whether the suspension itself should clear. Today it looks permanent until a human clears it. If there is a reasonable self-clear (for example the next real turn on any card), do it and say why in a comment. If not, say on the badge how to clear it.

RULES: a Go test that a suspended room still clears a stopped card after a newer transcript reply. A headless board test only if you change keepalive.js, and then only the touched section plus bootClean. Clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG for go test. Add changelog/runtime/2026-10-05-r-keepalive-stale-badge.md. go builds go to build.claude/ (`go build -o build.claude/ ./...`). No co-author or attribution trailers. One-line commit subjects. Never push, pull or fetch origin. Do not touch CLAUDE.md. Before the final commit, run the atrium:codebase-steward agent on the diff and fix what it finds.

REPORT: write REPORT.md in the worktree, commit everything on your branch, then send one atrium_report: "done <sha>" or "incomplete <why>". Nothing else.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
