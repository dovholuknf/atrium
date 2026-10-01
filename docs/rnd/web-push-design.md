# Web Push for the phone

Written by @rnd on 2026-09-30, at clint's request through the orchestrator. Design only. Nothing here is built.

## The answer

- **Yes, but run ntfy first.** Every tool in this space buzzes a locked phone. Atrium does not, and that gap is
  real.
- **This week: ntfy through the notify command, which is already built.** It needs no atrium code. clint spends
  about 15 minutes on it: install the ntfy app, write a 10-line script, and switch notify on. A week of that shows
  whether he wants buzzes at all, and how many is too many, before anybody builds anything.
- **Then Web Push, if he keeps ntfy on.** Web Push needs no third-party app or account, the payload is encrypted
  end to end, and a tap lands on `/m` in the browser he already uses. It costs one hub worker and one board worker.
  It needs no new Go dependency.
- **No email and no chat bot.** Email is slow and noisy. A Slack or Telegram bot reads every word in plaintext, and
  its token is one more secret to keep.

## What is there today

- **The trigger is built.** `internal/link/notify.go` decides, after each room announcement, which cards want a
  human and why: permission, question, input or finished. It sends only changes. It stays quiet while a desktop
  board tab is visible. It runs fire and forget on one goroutine with a bounded queue.
- **One sink is built: an operator's command.** The command gets four fields: the card's name, the reason, the card
  id and the room. They arrive in the environment and on stdin, never in argv. The command is set and tested only
  from loopback.
- **The sink is an interface, waiting for Web Push.** The comment in `notify.go` says so: "Web Push is meant to
  arrive later as a second one behind the same trigger."
- **Notify is off, and no command is set.** `GET /_hub/notify` on sg4 at 2026-09-30 21:00 answered
  `enabled: false, command: []`.
- **`sw.js` shows notifications only when an open page hands it one.** It has no `push` handler. A locked phone, or
  a browser in the background, gets nothing.
- **`docs/rnd/mobile-research.md` R4** already recommended Web Push first and the command as the fallback. This
  design is that recommendation worked out.

## How Web Push would work

1. **The hub makes its own key.** On first enable it generates a VAPID key pair (ECDSA P-256) and keeps it in the
   hub store. The private key never leaves the hub. The public key is served at `GET /_hub/push/key`.
2. **The phone subscribes.** In `/m`, the gear has a "phone alerts" switch. A tap on it asks for notification
   permission, which needs a tap: no browser lets a page ask on its own. It then calls
   `pushManager.subscribe({userVisibleOnly: true, applicationServerKey})`. The browser returns an endpoint URL at
   its push service and two keys, `p256dh` and `auth`.
3. **The hub stores the subscription.** `POST /_hub/push/subscriptions` carries `{endpoint, keys, label}`. The label
   is a name for the device, which the page fills in from the platform and the person can edit. The hub refuses an
   endpoint that is not HTTPS on a known push service host (see "Rate, noise and failure"). It then sends one test
   push.
4. **The hub sends.** A notice from the trigger goes to every subscription. Each send is an HTTPS POST to the
   endpoint. The body is encrypted to that browser (RFC 8291, `aes128gcm`), and a VAPID JWT signed by the hub's key
   rides in the header (RFC 8292). The browser's push service delivers it to the phone:
   - Brave and Chrome on Android: Google FCM (`fcm.googleapis.com`).
   - iOS Safari, for a web app on the home screen: Apple (`web.push.apple.com`).
   - Firefox: Mozilla (`updates.push.services.mozilla.com`).
   - Edge: Microsoft (`*.notify.windows.com`).
5. **The phone shows it.** The `push` handler in `sw.js` decrypts nothing: the browser has already done that. It
   shows the notification. A tap goes through the existing `openBoard` path to the card on `/m`.

**Size:** the encryption is about 150 lines of Go on the standard library: `crypto/ecdh`, `crypto/hkdf`,
`crypto/aes` and `crypto/ecdsa`. `SherClockHolmes/webpush-go` does the same work, but this does not need a new
dependency.

## What an alert says

- **Title:** the card's name, cut to 60 characters. It is plain text, because a notification renders no HTML.
- **Body,** one phrase per reason:
  - permission: "wants permission"
  - question: "asked you something"
  - input: "is waiting for you"
  - finished: "finished its turn"
- **Tag:** the card id. A newer alert for the same card replaces the older one on the phone, so one card never
  stacks up five alerts.
- **Data:** the card's `/m` path, relative. The service worker adds its own origin to it.
- **Never in the payload:** the command a permission wants to run, the question's text, any reply text, the recap,
  or a path on the machine. These are the same four fields the command sink gets today, and no more.
- **No approve or block buttons on a push.** An approval from a lock screen would be given without seeing the
  command, because the command is not in the payload, and must not be. iOS shows no action buttons on web push
  anyway. A tap opens the card, and the answer is given there.

## What the push service still learns

The payload is encrypted, and Google or Apple cannot read the card name or the reason. They do learn:

- **That this phone gets pushes from this atrium.** The endpoint is tied to one browser install, and the VAPID
  public key is a stable id for this hub.
- **When, and how often.** The timing and the volume of pushes follow the work: a burst means a lot is waiting. The
  timing alone shows when the factory runs.
- **The hub's public IP address,** from the outbound POST.
- **The size of each push.** Every payload is padded to 256 bytes, so a long card name does not show.
- **The VAPID `sub` contact.** Apple requires one. The default is the project URL, not an email address. The
  operator can change it.

ntfy.sh, a Telegram bot or Slack learn all of that and also the text itself.

## Subscribing over the zrok share

- **A service worker and push need a secure context.** The zrok share is HTTPS, so it qualifies. Loopback
  (`127.0.0.1`) qualifies as well. A ziti overlay served over plain HTTP on a non-loopback name does not, and push
  is then not offered. The gear says why.
- **A subscription is tied to the page's origin.** If the share's name changes, the push still arrives, because the
  endpoint does not care about the origin. The tap opens the origin the service worker was registered at, so an old
  name lands on a dead page until the phone subscribes again from the new one. The gear shows the origin each
  subscription came from.
- **Unsubscribing:**
  - The switch in `/m` calls `subscription.unsubscribe()` and `DELETE /_hub/push/subscriptions/<id>`.
  - The desktop gear lists every subscription with a remove button.
  - A 404 or 410 from the push service deletes that subscription by itself. That is how a phone that uninstalled
    the web app goes away.

## LP1 and the overlay rule

**What is operator-only** (`edge.LocalOperator`, `docs/rnd/local-proxy-trust-design.md`):

- Turning push on or off for the hub.
- Generating or rotating the VAPID keys.
- Changing the `sub` contact.
- The test push to every subscription.

These sit next to the notify command's PUT and test, which are loopback-only today.

**What the share may do:**

- Subscribe and unsubscribe its own browser, while push is on.
- Anybody past the share's password can already approve permissions, and a subscription is far less power than
  that. The hub POSTs only to an allowlisted push host, and the payload is four plain fields.

**Making a stranger visible:**

- At most 8 subscriptions.
- Every new one puts a line in the desktop growler: "a new device subscribed to alerts: <label>, from <origin>".
  A stranger who has the share's password and subscribes is then seen.

**The overlay rule holds:** atrium holds no one else's credential.

- The VAPID key is atrium's own.
- The subscription and its `auth` secret are the browser's, handed over by the browser for exactly this use.
- Nothing about Google's or Apple's accounts is stored.

The one new thing is an outbound call from the hub to a third party, and mobile-research R4 already named it. It is
off until the operator turns it on, and each phone opts in on its own.

## iOS limits

- **iOS 16.4 or later, and only for a web app added to the home screen.** A Safari tab gets no push. Since iOS 26,
  a site added to the home screen opens as a web app by default.
- **There is no install prompt.** The person has to go through Share, then Add to Home Screen. `/m` shows how, on
  iOS, when the page is not yet installed.
- **No silent pushes.** Every push must show a notification, or iOS revokes the subscription. The design never sends
  a silent push.
- **No action buttons,** which the design does not use anyway.
- **Deleting the web app drops the subscription.** The 410 cleans it up.
- **Apple requires the VAPID `sub`,** and it rejects a JWT that does not have one.

## Brave on Android

Brave sends web push only when "Use Google services for push messaging" is on, in Settings, then Privacy. It is off
by default. `/m` checks this: when `subscribe` fails or the test push does not arrive in 30 seconds, it says so and
names the setting. clint uses Brave, so this is the first thing to check on his phone.

## Rate, noise and failure

- **The trigger already dedupes.** A card sends one alert per waiting spell. It is quiet while a desktop tab is
  visible, and on enable it seeds itself, so whatever is already waiting does not flood the phone.
- **Topic and TTL.** Each push carries `Topic: <card id>`, so the push service replaces an undelivered alert for
  the same card instead of queuing both. `TTL` is 1 hour: an alert that could not be delivered for an hour is stale.
  `Urgency` is `high` for a permission and `normal` for the rest.
- **The burst cap.** More than 5 pushes in 2 minutes to one subscription become a single "N cards want you"
  summary, tagged `atrium-summary`, until 2 quiet minutes pass.
- **Quiet hours:** stage 3, if the week of ntfy shows they are needed.
- **Failure is per subscription.** Three failures in a row switch off that one subscription, with the reason shown
  in the gear, and not the whole feature. The command sink keeps its own count of three.
- **The allowlist.** An endpoint must be HTTPS on `fcm.googleapis.com`, `*.push.apple.com`,
  `updates.push.services.mozilla.com` or `*.notify.windows.com`. Redirects are not followed, and the response is
  read bounded. Without the allowlist, a subscription would make the hub POST to any URL somebody typed.
- **It never delays anything.** It runs on the same queue as the command sink, with the drop-oldest bound and a
  10-second timeout per send.

## What others do

- **Claude Code Remote Control:** push through the Claude app for permission prompts and questions, and optionally
  when a task is done. It skips the push while you are at the terminal. It cannot show atrium's gate, which blocks
  in the hook before Claude Code asks anything.
- **Codex in the ChatGPT app** (iOS and Android, since 2026-05): push for approvals and for a finished task. Its
  issue tracker has open reports of pushes not delivered on iOS.
- **Happy:** a native app with push on permission requests and on errors, over an end-to-end encrypted relay.
- **Cursor cloud agents:** a web app that installs as a PWA. A finished task notifies you in Slack.
- **Devin:** Slack first, for both the tasks and the notifications.
- **GitHub mobile:** native push for review requests, mentions and Actions results.
- **ntfy:** HTTP POST to a topic, with apps for Android and iOS. Self-hosting is possible, but iOS still relays
  through ntfy.sh to reach Apple. ntfy.sh sees plaintext.
- **Pushover:** a one-time fee per platform, and an app token that lives in the sender's script.
- **Gotify:** self-hosted, Android only, and it holds an open socket.

**The pattern:** every one of these buzzes a locked phone. The ones that have a native app use native push. The
web-first tools lean on Slack. None relies on an open page, which is what atrium does today.

## The cheaper alternatives

**ntfy through the notify command.** It works today and needs no atrium code.

- The script is 10 lines. It reads `ATRIUM_NOTIFY_NAME` and `_REASON`, and POSTs them to
  `https://ntfy.sh/<topic>` with a `Click:` header set to the card's `/m` URL on the share.
- The topic name is the secret. It lives in clint's script, so atrium never holds it.
- It works on iOS with no home-screen install.
- The cost: ntfy.sh sees the card name and the reason. To avoid that, send only "atrium: a card wants you".

**A Telegram or Signal bot through the same command.**

- It also works today, and it can carry more text.
- The cost: everything sent is readable by the bot's operator, and the token is one more secret to keep.

**Email.** Slow, filtered as spam, and noisy. It is not worth it.

**Claude Code Remote Control on claude cards (R5).** This is not an alternative for alerts: it cannot see atrium's
gate, which is the alert that matters most.

## Benefits and pitfalls

**Benefits:**

- A locked phone buzzes when a card wants clint. That is the gap.
- No third-party app or account.
- The payload is unreadable in transit.
- A tap lands on the card in the browser that already holds the share's login.

**Pitfalls:**

- Google or Apple still see timing and volume.
- Brave needs a privacy setting turned on.
- iOS needs a home-screen install.
- A change to the share's name breaks taps until the phone subscribes again.
- A subscription endpoint is an outbound target, which is why the allowlist exists.
- Push trains you to ignore it if it is noisy. That is why ntfy goes first, to learn the right volume cheaply.
- One more hub table and one more key to rotate.

## Stages

**WP0: ntfy through the notify command.**

- Owner: clint, with a sample script in `docs/user-guide.md` from @fabric.
- Size: tiny.
- Acceptance: a permission on a card buzzes his locked phone, and a tap opens `/m` on the card.

**WP1: the hub half.**

- Owner: @fabric.
- Size: one worker.
- Contents: VAPID keys in the hub store, a `push_subscription` table in a new hub migration at the end of the
  slice, the sink with RFC 8291 and RFC 8292, the allowlist, per-subscription failures, Topic, TTL and Urgency,
  padding, and the burst cap.
- Acceptance:
  - The RFC 8291 test vector decrypts.
  - A fake push service, given a browser key pair, decrypts what the hub sends and checks the JWT.
  - A 410 deletes the subscription.
  - An endpoint that is not on the allowlist is refused.
  - With `X-Forwarded-For` set, `PUT /_hub/push` is 403.

**WP2: the board half.**

- Owner: @ui.
- Size: one worker.
- Contents: the `push` handler in `sw.js`, the "phone alerts" switch in the `/m` gear with test and unsubscribe,
  the subscription list with remove in the desktop gear, and the iOS install hint and the Brave hint.
- Acceptance:
  - The headless suite passes with `PushManager` mocked.
  - By hand: Brave on Android, and an iOS web app on the home screen, each get the test push and a real
    permission alert while locked.

**WP3: quiet hours and the summary text.**

- Owner: @ui and @fabric.
- Size: small.
- Only if WP0 shows that they are needed.

Both WP1 and WP2 go through @review, because WP1 adds an outbound call and WP2 adds a new handler that a push
service can wake.

## Questions for later

These are parked in `notes/rnd-queue-2026-09-30.md`:

1. Should the alert carry the question's text, now that the payload is encrypted end to end? The default here is
   no.
2. Does ntfy first suit clint, or does he want to go straight to WP1 and WP2?
