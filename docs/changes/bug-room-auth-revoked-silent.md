Docs: none, a stall fix whose one endpoint and chip are described below

# A room whose claude login is revoked says so, once, and stops launching

Pictures: `docs/changes/img/bug-room-auth-revoked-silent/`. Headless:
`HEADLESS_UNITS=bootClean,loginNeeded node scripts/test-board-headless.js`.

## What it does

Claude Code answers every prompt with `OAuth token revoked · Please run /login` once the account's token is gone, and
every card in the room then sits at that line until somebody looks (m1mini, 2026-10-15 22:30).

- **Detect.** On the reaper's tick the room reads each runner's last rendered screen, only when its pty has produced
  output since the last look. A line carrying `OAuth token revoked`, or `Please run /login` beside `revoked` or
  `expired`, on any card flags the ROOM. `internal/daemon/loginneeded.go`.
- **Refuse.** While flagged, a launch of a claude harness is refused in the room, with the reason and what to do. Other
  runners are untouched.
- **Show.** `/v1/health` and the `health` event carry `login_needed`. The room puts the reason on its beat, so the hub's
  `/_hub/rooms` and `/_hub/inventory` carry `login_needed` on that room, and the board draws a red `login needed` chip on
  its row and its dashboard tile.
- **Say once.** The hub rings the notifier (the command and Web Push) on the edge from fine to needed, reason `login`,
  not on the beats that repeat it. A room that recovers and loses it again rings again.
- **Clear.** Three ways: no screen shows the line any more while a runner is up, the line goes unseen for 15 minutes
  (a flag raised from a card that has since died must not hold launches for ever, and the launch that would prove the
  login is the very thing refused), or the operator presses the chip, which sends `DELETE /v1/login-needed` to that room.

Atrium observes only. It does not log in, touch a credential or end a session that shows the line.

## Why m1mini's token keeps being revoked

Not verified on m1mini (the brief says not to change any room's login, and nothing here read its keychain or
credentials). What the Claude Code issue tracker and docs say, ranked by fit for a machine running many sessions under
one account:

1. **Many processes refreshing one credential.** Every session on a machine reads and rewrites the same stored login
   (the macOS keychain item, or `~/.claude/.credentials.json`). An OAuth refresh rotates the refresh token, so two
   sessions refreshing near each other can leave one holding a refresh token the server has already replaced, which the
   server answers as revoked. A room runs a dozen or more sessions at once, which is the worst case for this. Reports:
   anthropics/claude-code issues 72006 and 72017.
2. **The macOS keychain.** A room started from ssh, launchd or a locked login keychain cannot always read or write the
   item, so a refreshed token is not persisted and the next process uses the stale one.
3. **The account.** A password change, signing out of claude.ai, an admin ending sessions or a plan change revokes the
   refresh token everywhere, and every machine on that account hits the line together. If m1mini goes at the same
   moment as other machines, this is it, and nothing local fixes it.
4. **An old client.** Changelog 2.1.118 reportedly fixed refresh failing when the server revokes before the local
   expiry.

Several logins sharing one account is not by itself a cause. A login on another machine does not revoke this one's
token. The failures come from refresh contention on one machine and from account-wide revocation.

## Would `claude setup-token` end it?

Probably for cause 1 and 2, not for 3. `claude setup-token` mints a long-lived token (about a year) for non-interactive
use, and Claude Code reads it from `CLAUDE_CODE_OAUTH_TOKEN`, which ranks above the stored login. A session using it
never refreshes, so there is no rotation to race and no keychain to read. Cautions:

- It is a credential. It would live in the room's environment, so atrium holds nothing: set it in the room's own service
  or shell environment, never in a harness row or a setting here.
- It is tied to the account, so cause 3 (password change, sign out of everything) still revokes it, and it then fails
  the same way with a year of nothing to refresh. The detection here catches that too.
- Unset `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` in that environment, which rank above it.
- Try it on m1mini only, watch a day, and keep this chip as the alarm. Not done: nobody's login was changed.

Sources: the Claude Code issues above, the authentication page of the Claude Code docs, and
https://claudeissues.com/issue/52202.

## Test plan

## @LETTER@. A revoked claude login stops the room and says so

### @LETTER@1. The flag

1. In a room, have a claude card show `OAuth token revoked · Please run /login` (an agent may print the line).
2. Wait one reaper tick.

**Expected:** `GET /v1/health` on the room carries `login_needed`, the hub's `/_hub/rooms` carries `login_needed` on the
room, the board shows a red `login needed` chip on its row and tile, and the notifier fired once.

### @LETTER@2. The refusal

1. With the flag up, launch a claude card in that room, from the board and with `atrium launch`.

**Expected:** both are refused with a sentence naming `/login`. A launch of a different runner is not.

### @LETTER@3. Clearing

1. Run `/login` in the card, or press the chip and confirm.

**Expected:** the chip goes within a beat and claude launches work. A screen that still shows the line raises it again.
