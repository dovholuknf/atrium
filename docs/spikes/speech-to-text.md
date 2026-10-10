# Spike: speech to text for atrium

Goal: clint dictates answers (interviews especially) instead of typing them into a board terminal.

**What was and was not measured.** This ran on m1mini (Apple M1, 8 cores, 16 GB, no GPU), headless, with no Windows,
no browser with a mic, no phone, and no access to sg4. So:

- Measured: faster-whisper speed and accuracy on m1mini.
- Not measured, and stated from the specs and general knowledge: Win+H, macOS and phone dictation, the Web Speech API,
  hosted API latency. Each is marked *untested*. The first thing to do tonight is the two-minute Win+H test below.
- Test audio is macOS `say` (Samantha) reading a 20.5 s passage full of atrium words. Synthetic speech is easier than a
  person on a laptop mic, so read the error rates as a floor. Redo it with a recording of clint before trusting it.

## 1. Does an OS tool already do it?

Probably yes, with no atrium work. *Untested here.*

| Tool | Where | Expected to work | Expected to break |
|---|---|---|---|
| Win+H voice typing | clint's Windows browser | Types into whatever has text focus. xterm.js keeps a hidden `<textarea>` focused and reads its `input` events, so inserted text should arrive like typing. | Spoken "enter" or "new line" may insert a newline, and a newline in a terminal is Enter. Auto-punctuation and capitalisation will add noise to commands. Code words (`atrium_say`, `claude/main`) come out as prose ("atrium say"). It sends audio to Microsoft. |
| macOS dictation (Fn Fn) | a Mac browser | Same mechanism, same caveats. | Same. |
| iOS / Android keyboard mic | the phone view | Works in any text field, and the phone view has real inputs (compose). | The mic is on the keyboard, so it only works where the keyboard is up. |

Test (2 min): open a board terminal, focus it, press Win+H, say "hello world comma this is a test", and look at the
input line. Then say "new line" and see whether it submits. If text lands and Enter does not fire, the cheapest path is
done.

## 2. In the browser

- **Secure context.** `getUserMedia` and `SpeechRecognition` need a secure context. `http://127.0.0.1` and
  `localhost` count as potentially trustworthy, so the sg4 loopback board is allowed and prompts once per origin.
  zrok shares are https, so they are secure. A ziti URL is secure only if it is https or is reached as
  `127.0.0.1`/`localhost` through a local tunnel. A plain `http://<name>.ziti` is NOT secure and the mic is refused.
  Check which form the phone view uses.
- **Web Speech API.** Chrome and Edge only (Safari has a version, Firefox has none). Chrome sends audio to Google, Edge
  to Microsoft. Free, no key, streams interim results, and ends a continuous session after a pause (restart it). Brave
  ships the API but usually fails with `network`, because it does not include Google's key.
- **MediaRecorder** gives a webm/opus blob that has to go to a server (the hub, or an API). It works in every browser
  and on the phone. It is the path for local or hosted Whisper (sections 3 and 4).

## 3. Local model on the hub (measured, m1mini CPU, faster-whisper int8, 20.5 s clip, beam 5)

| Model | Transcribe time | Realtime factor | Result on the jargon passage |
|---|---|---|---|
| tiny.en | 1.5 s | 14x | "atrium say", "shawen", "the rock overlay", "city URL" (wrong) |
| base.en | 1.8 s | 11x | "Zrock", "the Shaw", "city URL", "rare eye commits" (wrong) |
| small.en | 4.6 s | 4.5x | "clod slash main", "the shaw", "Zrock", "city URL" |
| base.en + `initial_prompt` | 2.7 s | 7.6x | `claude slash main`, `atrium-say`, `atrium-done`, `sha`, `zrok`, `ziti` all right. Only "rewrite" came out as "rare eye". |
| small.en + `initial_prompt` | 4.9 s | 4.2x | Same, minus punctuation and capitals. Only "rewrite" wrong. |

- A 30 s answer is about 3 s on base.en and about 7 s on small.en, after a one-time 0.5 to 6 s model load. Model load
  can be kept warm in a daemon.
- Words that go wrong: "slash" is written out and never becomes `/`. Underscores are not produced ("atrium say", not
  `atrium_say`). Fix both with a small post-pass: "atrium say" to `atrium_say`, "slash" to `/`, plus the handle list.
- The `initial_prompt` (a list of the words you use) is the biggest win. It is free, and card handles can be injected
  from the live board.
- sg4 was not reachable, so it is not measured. Expect roughly the same or slower per core. If it has an NVIDIA GPU,
  faster-whisper with `large-v3` or `distil-large-v3` on CUDA runs far above realtime. `whisper.cpp` on m1mini with
  Metal would be faster than the CPU numbers above, not tried (no build tools installed).
- Cost of running it: `pip install faster-whisper` (done here, works), no key, nothing leaves the machine. It is batch,
  not streaming, so text appears after the user stops talking.

## 4. Hosted APIs (prices from third-party aggregators in the search, verify before relying on them; latency untested)

| Service | Cost per hour of speech |
|---|---|
| OpenAI gpt-4o-mini-transcribe | about $0.18 |
| OpenAI gpt-4o-transcribe / whisper | about $0.36 |
| Deepgram Nova-3 | about $0.26 batch, about $0.29 to $0.46 streaming (sources disagree) |
| Groq whisper-large-v3-turbo | not found, and it is advertised as the cheapest |

An hour of dictation a day costs under $10 a month. Latency is typically 1 to 3 s for a 30 s clip batch, and sub-second
for streaming (Deepgram). The costs are small, but the audio leaves the machine and a key has to be managed. For a
loopback tool that already sends audio to Google through Chrome, it is not a new kind of exposure.

## 5. Where the text lands

- Into the focused terminal as a **paste** (`sendPasteText`: bracketed paste, one frame, no Enter). That is what the
  proof does. The text waits on the input line so clint can fix it, then presses Enter. Never send `\r`, because a
  misheard word would run.
- **Toggle vs push-to-talk.** Toggle (Ctrl+Alt+D in the proof) for long interview answers. Push-to-talk (hold a key) is
  better for short commands and never leaves the mic open by accident. The proof has only toggle. Push-to-talk
  with MediaRecorder is simple (keydown starts, keyup stops and sends).
- The phone view should use the keyboard mic (section 1), because its compose box is a real input.

## Proof (separate commit, 43dd2e4)

`internal/api/web/js/dictate.js`, 55 lines, loaded from `index.html`. Off unless `localStorage["atrium.dictate"]="1"`.
Ctrl+Alt+D toggles Web Speech API recognition. Final phrases are pasted into the focused terminal with no Enter, and
interim text shows in a red badge at the bottom right. **Not exercised in a browser** (no mic and no display here), only
syntax-checked. Treat it as a sketch. To try it: set the flag in devtools, reload, focus a terminal, press Ctrl+Alt+D.

## 6. Recommendation

- **Tonight, 0 h:** Win+H in the board terminal (section 1 test). If it works, stop. If Enter leaks, add the flag and
  use the proof (about 0.25 h to try it in Chrome or Edge on the Windows box).
- **Worth building, about 6 to 8 h:** a push-to-talk and toggle mic button on the board (MediaRecorder), a hub endpoint
  that runs a warm faster-whisper (base.en or small.en) with `initial_prompt` built from live card handles, and a
  post-pass for `atrium_say`/`/`. It then works on the phone view and over the overlays too, keeps audio on the
  machine, and removes dependence on Chrome and Google. Breakdown: about 2 h frontend, about 2 h hub service and
  packaging (Python dep on the hub), about 1 h vocabulary and post-pass, about 1 to 3 h secure-context handling
  over ziti and testing on real audio. If sg4 has no GPU, use base.en (about 3 s per 30 s answer).
- **Skip** hosted APIs unless local accuracy on a real mic disappoints. Swapping one in is about 1 h behind the same
  endpoint.
