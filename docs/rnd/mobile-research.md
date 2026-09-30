# Atrium on a phone: research for the mobile workshop

Status: research, @rnd, 2026-09-29. Input to the mobile design workshop @ui leads, whose output is one design doc
(@ui's draft is `docs/backlog/ui/mobile-design.md` on claude/ui). This is not that design. It is what other tools
do, what a phone is for, and the constraints and recommendations the design should start from.

Method, honestly: product docs and READMEs read on 2026-09-29, not source reads. Claude Code's Remote Control page
was read in full. Happy's and Omnara's pages gave feature lists and little mechanism. A source read in the `bb.md`
style is follow-up work, if the workshop wants one.

## 1. What the other tools do

| Tool | What the phone shows | How you answer | Notifications | Notes |
| --- | --- | --- | --- | --- |
| Claude Code Remote Control (`/rc`, `claude --remote-control`, Claude app and claude.ai/code) | The conversation as chat, synced with the terminal. Subagent progress too | A native composer. Photos and files attach from the phone | Push through the Claude app. "Push when actions required" covers permission prompts and questions, and "push when Claude decides" covers task done. Skipped while you are at the terminal, and `CLAUDE_CLIENT_PRESENCE_FILE` widens that to "at the machine" | Subscription only, one remote per process, the terminal must stay open, about 10 minutes of network loss ends it. Can be on for every session (`/config`) |
| Happy (slopus/happy, 23.9k stars, Expo app plus web) | Chat, from a wrapper (`happy` instead of `claude`) | Composer, voice | Push on permission needed and on errors | End-to-end encrypted relay. Taking the phone "restarts the session in remote mode", and a key on the desktop takes it back |
| Omnara (omnara-ai/omnara, 2.9k stars) | Chat plus diffs | "Approve with one tap" | Push "when agents need you" | Has since pivoted to an agent platform. The mobile app is still listed |
| VibeTunnel, Termius, Blink | A real terminal | Keyboard plus a key toolbar | None for agent events | Terminal-first. VibeTunnel's mobile history is a run of keyboard and paste fixes, including a separate input box for paste |

**The finding that matters.** Every tool built for agents shows the phone a CONVERSATION, not a terminal. The
terminal-first tools are for shells, and their phone story is a long fight with the soft keyboard. Nobody who
started from agents put a terminal on the phone as the main view.

## 2. What a phone is for, with atrium

In the order clint does them, from his reports today and the widened ask:

1. **Triage: which cards want me.** Needs-input, pending permissions, open questions, reports from workers, held
   messages. That is most visits, and it needs no terminal.
2. **Read the latest reply or report on one card.** Text, readable at phone width, scrollable back a few turns.
3. **Answer.** A permission (approve or deny with a reason), a question (u-004, open questions), a choices dialog.
4. **Say something short.** A prompt to a card, or a say, with voice dictation.
5. **Glance at status.** What is running, what is stuck, how long.
6. **Drop into the terminal**, rarely: a TUI dialog nothing else can answer, or something visibly wrong.

## 3. Recommendations for the design

### R1. A dedicated phone page, not the board squeezed. Serve it at `/m`

A separate lightweight page served by the same handler (the room and the hub already serve `index.html`), on the
same JSON API and SSE stream, sharing the skins' CSS variables. It does NOT load xterm.js until the terminal is
asked for, and the terminal it opens is the existing popped-out view. The home-screen manifest's `start_url` on a
phone is `/m`.

Why not a phone layout inside `index.html`: the board is one large page with many modules, so every desktop change
becomes a phone regression risk and the reverse, and the phone pays to load and run everything to show four lists.
It is still a board file under `internal/api/web/`, so @ui builds it either way.

### R2. The card view is a conversation built from the transcript, with the live screen only as a fallback

The main view of a card is its last replies as text, read from the runner's transcript on disk (a bounded tail),
plus the card's recap and reports, plus a native composer. That is t-003's option (a), the text reading view,
promoted from a mode to the default. It sidesteps the pty constraint entirely: text reflows to any width, and the
pty is never resized, which t-003b already pins.

Reuse: `internal/daemon/keepalive.go` already reads a card's transcript for its last main reply, with a bounded
cache (`readLastReply`, `lastReplyCache`). Returning the reply TEXT and the last N turns is a small @runtime
endpoint. `internal/api/sessions.go` already finds the conversation on disk. Codex and other runners without a
readable transcript fall back to the screen's bottom rows as text.

A TUI dialog (AskUserQuestion, the trust prompt) shows as the live bottom rows of the screen with its options as
buttons. The daemon already knows a dialog is open (`act.dialogOpen`), and the answer is the key the option names,
sent through the existing input path.

### R3. The keyboard, from the start: native input, never typing into the terminal

- **The composer is a native `<textarea>`.** Tap-to-position-cursor, the long-press magnifier, selection, paste,
  autocorrect and voice dictation are then the operating system's, and all of them work. Arrow keys synthesised
  from a tap into claude's TUI input (the first version @ui is building) is fragile: wrapped lines, wide
  characters, a multi-line input, and a TUI that redraws under it. The dedicated view does it better by never
  editing inside the terminal. On send, the text goes through the existing prompt and say paths, with bracketed
  paste and the typing gate, as one message.
- **The visual viewport.** Put `interactive-widget=resizes-content` in the page's viewport meta. On Chromium
  Android (clint's Brave) the layout viewport then shrinks with the keyboard, so a bottom-anchored composer and the
  key bar stay above it with plain CSS. iOS Safari ignores it, so also listen to `window.visualViewport` `resize`
  and `scroll` and pin the composer to `visualViewport.height + offsetTop`. The only other place the board reads
  `visualViewport` today is `terminal-links.js`.
- **Nothing takes focus on its own.** The focus bounce clint reported comes from focus moving on a redraw. On `/m`
  focus changes only on a tap, and a list refreshed by SSE never steals it.
- **One header row, and it hides.** His screenshots show 25 to 30 percent of the screen spent on headers. `/m`
  has one row, hidden on scroll down and shown on scroll up, and none in landscape.

### R4. Notifications: web push, primary. An operator's notify command, the fallback. Presence-aware

**Web push does not cross the `docs/fabric/overlays.md` line, with three conditions.** The line is that atrium never
holds somebody else's credential and never becomes the thing deciding who connects. Web push holds atrium's OWN
key: the VAPID key pair is generated by the hub and never leaves it. The subscription is the browser's, handed to
atrium by the browser. The payload is encrypted to that browser (RFC 8291), so the push service (FCM for Brave)
carries a blob it cannot read. What is genuinely new is an outbound call from the hub to a third party's service.
The conditions:
1. Off until turned on, per browser, by the person holding the phone.
2. Payload: the card name and the reason (needs-input, permission, question, report) only. Never reply text, and
   never a command.
3. Fire and forget, bounded, on its own goroutine. A push that fails is logged and dropped, and never delays the
   card, the permission chain or a hook.

**ntfy as a built-in is worse as the primary.** ntfy.sh sees plaintext unless self-hosted, and the topic name is a
bearer secret atrium would have to store, which is the credential shape the line forbids. The better fallback, for
iOS without a home-screen install or for anyone who wants ntfy or Pushover, is a **notify command**: an operator-
written command line, run with the card name and reason as arguments, bounded like a source. atrium holds the name
of a command, never the credential in it, which is the rule `overlay_reserve.go` and `scm-design.md` already follow.

**Presence.** Do not push while clint is at the desktop board. The seen design already knows a turn was seen after a
full dwell in front of somebody, and the board knows it is visible. Claude Code does the same (it skips pushes
while you are at the terminal). A push nobody needed teaches people to turn pushes off.

### R5. Claude Code's Remote Control is a cheap complement, not a replacement

A per-harness launch option could add `--remote-control` for claude cards. The card then carries "open in Claude app",
and clint gets Anthropic's native chat, voice and file attach for free. It does not replace `/m`:
- claude only, and subscription only
- one remote per process
- the relay goes through claude.ai
- **atrium's permission gate still runs first.** A tool call atrium is asking about blocks in the PreToolUse hook,
  and the Claude app will not show that question. So permissions and atrium's own questions stay on `/m`.

Worth one line in the design as an option, and a question for clint.

## 4. Cost, as I see it (@ui owns the real estimate)

- `/m` stage 1, with the attention list, the card view (reply text, recap, permission and question answers) and the
  composer: one @ui worker. One small @runtime endpoint for the transcript tail as text.
- Web push, with the VAPID keys and the sender on the hub, plus `sw.js` subscribe and a settings switch: one @fabric
  worker for the hub half and one @ui worker for the service worker. The notify command: small, @runtime.
- Dialog buttons from the live screen: small, @ui, on top of `act.dialogOpen`.
- Remote Control launch option: small, @runtime, if clint wants it.

## 5. Open questions worth putting to clint

1. `/m` as its own page, or a phone layout inside the board?
2. Web push on the hub, with its outbound call to the push service?
3. Remote Control on claude cards as an extra, with atrium's gate still first?
4. How far back the card view reads: the last reply only, or the last N turns?

Sources: [Claude Code Remote Control](https://code.claude.com/docs/en/remote-control),
[Claude Code on mobile](https://code.claude.com/docs/en/mobile), [slopus/happy](https://github.com/slopus/happy),
[Happy features](https://happy.engineering/docs/features/), [omnara-ai/omnara](https://github.com/omnara-ai/omnara),
[Omnara](https://www.omnara.com/), [amantus-ai/vibetunnel](https://github.com/amantus-ai/vibetunnel).
