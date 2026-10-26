# Speech to text

Status: design, spike, 2026-10-25. Item `r-spike-speech-to-text`. No code. It builds on the earlier measured spike in
`docs/spikes/speech-to-text.md` (faster-whisper numbers on m1mini) and the opt-in proof in
`internal/api/web/js/dictate.js`, and does not repeat their tables.

Goal: clint says an answer instead of typing it into a card, at the desk and on the phone.

## 1. OS dictation first

The OS tools type into whatever text field has focus, so atrium gets them for free wherever the board has a real input.
None was exercised for this write-up (no mic or display on the machine that wrote it), so each row is expected
behavior, not measured.

| Tool | Where it lands on the board | What goes wrong |
| --- | --- | --- |
| Windows Win+H voice typing | Any focused field. In a terminal it types into xterm's hidden textarea. | Audio goes to Microsoft. Auto-punctuation and capitals add noise. "New line" may become Enter in a terminal and run a half-said command. |
| macOS dictation (Fn Fn) | Same as Win+H. | On Apple Silicon much of it runs on the device. Same Enter and punctuation risk. |
| Phone keyboard mic (Gboard, iOS) | The `/m` composer and the board's phone composer (`tcompose.js`), both real textareas. | None new. The composer already exists because xterm garbles OS composition, and it sends only on its button. |

The phone is already covered. `tcompose.js` gives the board's phone terminal a real textarea for exactly this reason,
and `/m` has one too. Dictation there is the keyboard's mic, needs nothing from atrium, and is reviewed before send.

The desk is nearly covered. The risk is xterm: dictated text lands as keystrokes, and a spoken or inferred newline is
Enter. The fix is the same one the phone got, a real textarea that sends on a button or Ctrl+Enter, not a speech
feature.

## 2. If atrium did its own: Web Speech vs local whisper

| | Browser Web Speech | Local whisper on the hub |
| --- | --- | --- |
| Browsers | Chrome, Edge, Safari. Not Firefox. Brave fails with `network`. | Any, through `MediaRecorder`. |
| Where audio goes | Google (Chrome) or Microsoft (Edge) | Nowhere. Stays on the hub. |
| Offline | No | Yes, which matches the board's offline rule |
| Latency | Streams words as you speak | Batch. About 3 s for a 30 s answer on base.en, CPU |
| Code words | No vocabulary hint. `claude/main` comes out as prose. | `initial_prompt` built from live card handles fixed almost every jargon word in the spike |
| Cost to build | Done, as the flagged proof | About 6 to 8 h: mic button, hub endpoint, warm model, a Python or whisper.cpp dependency |
| Secure context | Needed | Needed for `getUserMedia` too |

Secure context applies to both. `http://127.0.0.1`, `localhost` and a zrok share (https) allow the mic. A plain
`http://atrium.ziti` intercept and a LAN `http://192.168...` address do not, the same limit Web Push hits
(`docs/rnd/security-design.md` section 10). Over those, only OS dictation works, because it never asks the page.

Whisper wins on privacy, offline and code words. Web Speech wins on cost, since it already exists. Neither beats the
OS tool on the phone, and on the desk both only matter if OS dictation into a real textarea turns out not to be good
enough.

## 3. Recommendation

Build no speech feature now. Rely on OS dictation and give the desktop board the same real-textarea composer the phone
already has, so dictated text waits for review and never presses Enter on its own. Keep `dictate.js` as the opt-in
proof and do not promote it, since it sends audio to Google and gives nothing OS dictation does not. Revisit local
whisper on the hub only if a week of real use shows code words and repo names are the pain, because that is the one
thing it does that the OS cannot.

## Open questions for clint

1. Have you tried Win+H in a board terminal yet, and did a spoken "new line" press Enter?
2. Is the desk composer (a real textarea, send on Ctrl+Enter) wanted on its own, as the only change here?
3. Is sending audio to Microsoft or Google acceptable for this, or must speech stay on the machine?
4. How do you reach the board from the phone day to day: zrok share, ziti intercept or LAN? Only the first lets a page
   use the mic.
5. Do you mostly dictate long interview answers or short commands? It decides toggle vs push-to-talk if atrium ever
   owns the mic.
6. Does sg4 have an NVIDIA GPU? It decides whether whisper would run small.en or large-v3 if it is ever built.
7. Should `dictate.js` and `docs/spikes/speech-to-text.md` be removed once this is decided, or kept as the record?
