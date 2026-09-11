// ── reaching the board from elsewhere ───────────────────
//
// Two overlays, configured and driven the same way. Everything about one of
// them lives in OVERLAY_UI below, so adding a third is an entry rather than a
// branch through the rendering.

let overlays = [];

// The projects' own marks, traced from their published SVGs: the zrok rocket
// and the OpenZiti ziggurat. Each keeps its own colors rather than taking
// the board's, since a logo recolored is a logo somebody has to look twice at.
//
// Both carry their own viewBox, so they are held in an <svg> of their own
// rather than being pasted into a shared one.
const OVERLAY_MARKS = {
  zrok: `<svg viewBox="41.62 64.03 125.8 166.27" aria-hidden="true">
    <path fill="#9BF316" d="m 104.52059,64.027974 c 0,0 -12.300998,16.684095 -17.163248,24.964306 -4.8623,8.28022 -7.86765,12.84754 -10.59501,26.3339 -2.72736,13.48636 -1.27406,42.21319 -1.27406,42.21319 l -18.58752,20.04004 -15.280273,49.9615 40.422833,-6.79427 a 22.714797,11.567473 0 0 0 22.320548,9.54731 22.714797,11.567473 0 0 0 22.32712,-9.55945 l 40.72649,6.84277 -15.28027,-49.95979 -18.58916,-20.04001 c 0,0 1.45496,-28.72857 -1.2724,-42.21493 -2.72736,-13.48636 -5.73437,-18.05368 -10.59667,-26.3339 -4.8608,-8.277646 -17.14905,-24.947062 -17.15668,-24.957378 z m -0.0151,14.741718 c 0.52421,0.860497 14.76063,18.300498 18.34492,32.586238 3.65093,14.55125 3.25036,30.16797 2.29067,46.75483 -0.68395,11.82081 -5.48912,37.13724 -8.2384,50.97445 a 22.714797,11.567473 0 0 0 -12.54073,-1.9254 22.714797,11.567473 0 0 0 -12.237098,1.84921 c -2.75047,-13.84452 -7.55003,-39.12307 -8.23343,-50.93462 -0.95969,-16.58687 -1.36026,-32.20531 2.29067,-46.75656 3.57561,-14.25109 17.733538,-31.579359 18.323398,-32.548148 z m -27.999198,95.069708 6.36686,35.53319 -30.30472,9.08328 10.94985,-35.7998 z m 56.021568,0.0351 12.988,8.81667 10.9482,35.80155 -30.30311,-9.08504 z"/>
  </svg>`,
  ziti: `<svg viewBox="0 0 201.4 201.4" aria-hidden="true">
    <defs><linearGradient id="zitiGrad" gradientUnits="userSpaceOnUse"
      x1="154.4149" y1="206.1329" x2="43.0052" y2="-12.521">
      <stop offset="0.004" stop-color="#FC0147"/>
      <stop offset="0.039" stop-color="#F4044D"/>
      <stop offset="0.9" stop-color="#0068F9"/>
    </linearGradient></defs>
    <circle fill="url(#zitiGrad)" cx="100.7" cy="100.7" r="100.7"/>
    <path fill="#FFFFFF" d="M101.1,10.6L44.7,96.4l32,20.9l5.3-8.1L98.7,189l55.5-86.3l-31.1-20.4l-5.3,8.2L101.1,10.6z M126.4,96.7 l13.7,9.2l-36.5,55.9L86.8,82l-13.6,20.8L59.8,94l36.7-56.1l16.6,79.2L126.4,96.7z"/>
  </svg>`
};

// The fields each overlay takes. Rendered from this, so the form and what the
// daemon stores cannot drift apart in one place and not the other.
const OVERLAY_UI = {
  // A stable address is a different flag per mode in zrok v2, so both fields
  // are here and each says which mode it belongs to.
  zrok: [
    // TWO CHECKBOXES, NOT ONE SELECT, and both can be on.
    //
    // The select was a leftover from when a share was one thing. A machine may
    // reasonably want a public link for the board and a private share for a
    // lent session, and the select could not express that: it made two
    // independent capabilities look like one exclusive choice.
    { key: "public", label: "share publicly", type: "check",
      check: "a link anyone who has it can open",
      hint: "A public share is a link with no login in front of it. Whoever opens it " +
            "sees every card and can answer permission requests." },
    { key: "private", label: "share privately", type: "check",
      check: "they need zrok on the other end too",
      hint: "The safer one. Reaching it needs zrok and the share token, not just a URL." },
    { key: "share_token", label: "share token (private)", type: "text",
      placeholder: "optional",
      hint: "Reuses a reserved private share so the address survives a restart." },
    // NO RESERVE BUTTON ANY MORE. Reserving used to be three presses: type a
    // name, press `reserve it`, press `start sharing`, and skipping any of
    // them meant zrok invented an address, the board handed it out, and the
    // controller deleted it at the next stop. Starting a public share now
    // reserves whatever is here, or invents and remembers one, so this field
    // is only for choosing the hostname rather than for making it durable.
    { key: "name", label: "what share name would you like to use?", type: "text",
      placeholder: "atrium-xxxxxxxx, picked for you",
      hint: "Public shares only, and it becomes the hostname somebody types. Left empty, " +
            "atrium invents one and keeps it. Either way it is reserved, so the link you " +
            "hand out survives a restart." }
  ],
  ziti: [
    { key: "identity", label: "identity file", type: "text",
      placeholder: "C:/path/to/identity.json",
      hint: "Filled in by enrolling. The SDK opens it and owns the key inside." },
    // Whether a service exists and whether this identity may BIND it are both
    // facts on the controller, and both failures look the same from here: the
    // listener refuses. Asking is the only way to tell them apart without
    // pressing start.
    { key: "service", label: "service", type: "text", placeholder: "atrium-board",
      actions: [{ label: "what can I host?", fn: "zitiServices" }],
      hint: "The service this board answers. It has to already exist, with a bind policy " +
            "this identity satisfies." }
  ]
};

async function loadOverlays() {
  try { overlays = (await api("/v1/overlays")).overlays || []; } catch (e) { return; }
  paintOverlays();
  // A count of nothing is noise on a panel that is still a box to paste a
  // token into.
  if (overlays.some(o => o.kind === "zrok" && o.ready)) loadZrokAccount(false);
}

// WHAT THE ZROK ACCOUNT IS ALREADY USING.
//
// NO DENOMINATOR. zrok does not give an account token its own ceilings: they
// live on a limit class only an admin may read, and an account with no class
// applied is measured against the controller's configuration, which no
// endpoint exposes at all. So the block counts and never divides, and a "3 of
// 5" added here would be a limit atrium made up. internal/daemon/
// overlay_limits.go has the endpoints that were checked.
//
// Cached and not polled, because it is a call to somebody else's instance.
// A minute is stale by design: another machine on the same account can open a
// share atrium never sees, so `check again` is the button rather than a timer
// nobody would want fired on every paint.
let zrokAccount = null;
let zrokAccountAt = 0;
const ZROK_ACCOUNT_FRESH = 60000;

async function loadZrokAccount(force) {
  if (!force && zrokAccount && Date.now() - zrokAccountAt < ZROK_ACCOUNT_FRESH) return;
  zrokAccountAt = Date.now();
  zrokAccount = { checking: true };
  paintOverlays();
  try {
    zrokAccount = await api("/v1/overlays/zrok/account");
  } catch (e) {
    zrokAccount = { err: "could not ask the daemon: " + (e && e.message ? e.message : e) };
  }
  zrokAccountAt = Date.now();
  paintOverlays();
}

function zrokAccountBlock(o) {
  if (o.kind !== "zrok" || !o.ready) return "";
  const a = zrokAccount;
  if (!a) return "";
  if (a.checking) {
    return `<div class="ov-acct"><span class="hintline"><span class="shspin"></span>
      reading what this zrok account is using</span></div>`;
  }
  const again = `<button onclick="loadZrokAccount(true)">check again</button>`;
  if (a.err) {
    return `<div class="ov-acct">
      <div class="ov-acct-row"><span class="hintline">${esc(a.err)}</span>${again}</div>
    </div>`;
  }

  // Reserved names get a cell of their own because atrium grows that one by
  // itself, one per board address and one per lent session, so an account
  // filling up with them is usually atrium's doing.
  const cell = (n, what) =>
    `<span class="ov-acct-n">${n} <span>${esc(what)}</span></span>`;
  const counts = [
    cell(a.environments, a.environments === 1 ? "environment" : "environments"),
    cell(a.shares, a.shares === 1 ? "share open" : "shares open"),
    cell(a.reserved_names, a.reserved_names === 1 ? "reserved name" : "reserved names")
  ].join("");

  const mine = a.here
    ? `${a.here_shares} of those shares ${a.here_shares === 1 ? "is" : "are"} this machine's`
    : "this machine's environment is not listed on that account, which happens when the " +
      "token is for a different zrok instance or the environment was disabled from the console";
  const ours = a.atrium_names
    ? ` ${a.atrium_names} of the reserved names ${a.atrium_names === 1 ? "is" : "are"} atrium's.`
    : "";

  // The transfer allowance and nothing else. zrok sets this flag off the
  // bandwidth journal alone, so naming a count here would send somebody to
  // delete shares over a block that deleting shares does not lift.
  if (a.limited) {
    return `<div class="ov-acct stop">
      <div class="ov-acct-row">${counts}${again}</div>
      <span class="hintline"><b>zrok has this account over its transfer allowance,</b> so
        starting a share will fail until the period rolls over. The counts above are not the
        cause. Check the account at ${esc(a.endpoint || "your zrok instance")}.</span>
    </div>`;
  }
  return `<div class="ov-acct">
    <div class="ov-acct-row">${counts}${again}</div>
    <span class="hintline">On the account behind this machine, everywhere, not just here:
      ${esc(mine)}.${esc(ours)} zrok does not tell an account token where its ceilings are,
      so these are counts and not fractions. Every one of them is a thing a share can be
      refused for.</span>
  </div>`;
}

// A SHARE THAT STOPS ON ITS OWN HAS TO SAY SO.
//
// The one state change in the overlay panel that nobody pressed a button for.
// Starting and stopping are visible in the panel because you were looking at
// it. A share ending by itself is not: the listener drops, `err` is set, and
// the gear is not open.
//
// It matters because the address is the thing you handed to somebody else. A
// dead public share is a link that has stopped working for a person who is not
// in the room, and they have no way to tell you except by telling you.
//
// Only the running-to-stopped edge, and only with a reason attached. Pressing
// `stop sharing` clears the error, so this cannot fire for something you did.
function noticeSharesThatDied(before, after) {
  // Nothing to compare against on the first event of a page's life. A board
  // that opens onto a share that stopped an hour ago should not announce it as
  // news: the panel says so, which is the right weight for old information.
  if (!before || !before.length) return;
  (after || []).forEach(now => {
    const was = before.find(o => o.kind === now.kind);
    if (!was || !was.running || now.running) return;
    if (!now.err) return;
    const what = now.label || now.kind;
    // NO `goTo`. Every other alert on the board goes to a VIEW, and the gear
    // is a dialog rather than one, so handing `settings` to `switchView` would
    // be a click that appears to do nothing. The message says where to look
    // instead, which is one instruction rather than a broken promise.
    alerting.notify(`${what} stopped sharing`, now.err, "", "", "overlay:" + now.kind, "", "");
    toast(`${what} stopped sharing`, now.err + ". open the gear, expose the board, to see why");
  });
}

function paintOverlays() {
  const host = document.getElementById("overlays");
  if (!host) return;
  setHTML(host, overlays.map(overlayCard).join(""));
}

function overlayCard(o) {
  const mark = OVERLAY_MARKS[o.kind] || "";
  // The executable is NOT what shares. Both overlays are driven by an embedded
  // SDK, and the board answers the overlay listener itself rather than being
  // proxied to by a child process.
  //
  // For ZROK the binary is now needed for nothing at all: enabling and
  // disabling are the same API calls the command makes, done natively, because
  // the command can only ever reach the machine's environment and atrium may
  // be on its own. For ZITI it is still needed to enroll an identity.
  //
  // So a machine that is already set up shares perfectly well with no
  // executable anywhere. Treating a missing binary as "not installed" hid the
  // whole overlay from exactly that machine and offered a download instead of
  // a start button.
  const missing = !o.found;
  const stuck = missing && !o.ready && o.kind !== "zrok";

  // What it is doing, in the one place somebody looks to find out.
  const state = o.running
    ? `<span class="ov-live">sharing</span>`
    : o.ready
      ? `<span class="by">ready</span>`
      : stuck
        ? `<span class="by missing">not installed</span>`
        : `<span class="by missing">needs setting up</span>`;

  // Three states, in the order somebody hits them: nothing set up and no tool
  // to set it up with, tool but nothing set up, ready to share.
  const body = stuck
    ? `<div class="ov-missing">
         Nothing is set up here, and the executable that would set it up is not on this
         machine or not on the daemon's PATH. Sharing itself needs no executable: it is
         the embedded SDK. This is only needed once, to enable the account.
         <a href="${esc(o.install)}" target="_blank" rel="noreferrer">${esc(o.install)}</a>
       </div>`
    : !o.ready
      ? overlaySetup(o)
      : lockedNote(o) + zrokWhoseAccount(o) +
        (OVERLAY_UI[o.kind] || []).map(f => overlayField(o, f)).join("");

  // The address, once there is one. This is the thing you came for, so it gets
  // its own line and a copy button rather than being read out of the log.
  const addr = o.address ? `
    <div class="ov-addr">
      <code>${esc(o.address)}</code>
      <button onclick='copyText(this, ${JSON.stringify(o.address).replace(/'/g, "&#39;")})'
        >copy</button>
    </div>` : "";

  // WHAT THE ACCOUNT CALLS IT, which is not the address.
  //
  // A public share's address is a URL on a frontend and the share itself is a
  // token. So somebody looking at their zrok account to find what atrium made
  // had the address on this panel, the token in the console, and nothing
  // joining the two. The operator asked for it in those terms: show the share
  // name so it can be found.
  const share = o.share ? `
    <div class="ov-share">
      <span class="eyebrow">listed in your account as</span>
      <code>${esc(o.share)}</code>
      <button onclick='copyText(this, ${JSON.stringify(o.share).replace(/'/g, "&#39;")})'
        >copy</button>
    </div>` : "";

  // Whatever it said. A share that refuses explains itself here and nowhere
  // else, so it is shown rather than summarised.
  const log = (o.output || []).length ? `
    <details class="ov-log">
      <summary>what ${esc(o.label)} said</summary>
      <pre class="code">${esc(o.output.join("\n"))}</pre>
    </details>` : "";

  const err = o.err ? `<div class="ov-err">${esc(o.err)}</div>` : "";

  // Said here rather than left to be discovered when `atrium stop` refuses.
  // A tunneler terminates on this machine, so while a share is up every
  // request looks like it came from this keyboard, and the shutdown endpoint
  // stops trusting that.
  const note = o.running ? `
    <div class="ov-note">While this is up, <code>atrium stop</code> needs
      <code>--shutdown-token</code>: a share makes every request look local, so the
      loopback-only rule no longer means anything.</div>` : "";

  // Only once sharing is possible. Before that the setup block carries its
  // own button and a second one would be two things to press.
  // Ready is the whole condition. A missing executable does not stop a share,
  // only the disable button below, which says so for itself.
  // Starting takes seconds and used to show nothing for all of them, so the
  // button becomes the progress line while it runs rather than sitting there
  // looking unpressed. Replaced rather than disabled: a greyed-out button says
  // you may not, and what is true is that you already have.
  const busy = overlayBusy[o.kind];
  const actions = !o.ready ? "" : `
    <div class="ov-actions">
      ${busy
        ? `<span class="ov-busy"><span class="shspin"></span>${esc(busy)}</span>`
        : o.running
          ? `<button class="no" onclick="stopOverlay('${esc(o.kind)}')">stop sharing</button>`
          : `<button class="go" onclick="startOverlay('${esc(o.kind)}')">start sharing</button>`}
      <!-- Disabling zrok no longer needs the executable. It is the same two
           API calls the zrok command makes, so it works on a machine that has
           never had zrok installed. What the button MEANS still differs: on
           atrium's own environment it costs atrium's shares, and on the
           machine's it costs every tool on the machine.
           NO BACKTICKS IN HERE. This comment sits inside a template literal,
           so a backtick ends the string and the rest of the file becomes
           nonsense. See web/CLAUDE.md, and check-board.sh, which caught
           exactly that when this comment was first written. -->
      <button onclick="teardownOverlay('${esc(o.kind)}')"
        title="${o.kind === "zrok"
          ? ((o.setup || {}).own
              ? "remove atrium's own zrok environment. nothing outside atrium is affected."
              : "remove THIS MACHINE's zrok environment, which every other tool here shares")
          : "stop using this identity"}">${
        o.kind === "zrok" ? "disable" : "forget identity"}</button>
      <span class="grow"></span>
      <!-- Where the executable is, when there is one, and what shares when
           there is not. The SDK is the answer in both cases. -->
      <span class="by" title="${missing
        ? "sharing is the embedded SDK, so no executable is needed for it"
        : esc(o.found)}">${missing ? "embedded SDK" : esc(o.found)}</span>
    </div>`;

  // What is set up, once it is. One line, because the point is that there is
  // nothing left to do here.
  const ready = o.ready ? `<div class="ov-ready">${esc(readyLine(o))}</div>` : "";

  const acct = zrokAccountBlock(o);

  return `<div class="ov ${o.running ? "live" : ""}" data-kind="${esc(o.kind)}">
    <div class="ov-head">
      <span class="ov-mark" data-kind="${esc(o.kind)}">${mark}</span>
      <div class="grow">
        <b>${esc(o.label)}</b>
        <span>${esc(o.blurb)}</span>
      </div>
      ${state}
    </div>
    ${addr}${share}${err}${note}${ready}${acct}
    ${overlayBody(o, body)}
    ${actions}${log}
  </div>`;
}

// The configuration, folded away once there is nothing to do in it.
//
// Two overlays with every field showing is most of a screen spent on values
// nobody is changing, and it gets worse with each option added. What has to
// stay visible is the state and the address, and those live above this.
//
// Open when there IS something to do: no tool, or nothing set up yet. Closed
// once it is ready, because at that point the section is settings rather than
// steps. Held per overlay in localStorage so opening one to change a field and
// coming back does not fold it again mid-edit.
function overlayBody(o, body) {
  // OPEN UNLESS IT WAS CLOSED. It defaulted the other way and that was wrong:
  // the choice of which zrok account and which instance lives in here, so a
  // machine that was already enabled opened the gear onto a summary with no
  // visible way to change any of it.
  const open = !o.found || !o.ready || overlayOpen(o.kind);
  return `<details class="ov-fold"${open ? " open" : ""}
      ontoggle="setOverlayOpen('${esc(o.kind)}', this.open)">
    <summary>configure ${esc(o.label)}</summary>
    <div class="ov-body">${body}</div>
  </details>`;
}

const OV_OPEN_KEY = "atrium.overlays.open";
function overlayOpen(kind) {
  try {
    const v = JSON.parse(localStorage.getItem(OV_OPEN_KEY) || "{}")[kind];
    // Undefined is "never said", which is open. Only an explicit close closes
    // it, so the settings are there the first time somebody looks.
    return v === undefined ? true : !!v;
  } catch (e) { return true; }
}
function setOverlayOpen(kind, open) {
  let all = {};
  try { all = JSON.parse(localStorage.getItem(OV_OPEN_KEY) || "{}"); } catch (e) {}
  all[kind] = open;
  localStorage.setItem(OV_OPEN_KEY, JSON.stringify(all));
}

// What is already set up, in one line.
//
// Which zrok instance, or which network, because a token from one does not
// work on another and that is otherwise only discovered by failing.
function readyLine(o) {
  const s = o.setup || {};
  if (o.kind === "zrok") {
    return s.api_endpoint ? `enabled against ${s.api_endpoint}` : "enabled on this machine";
  }
  return s.controller ? `identity enrolled with ${s.controller}` : "identity ready";
}

// The step before sharing is possible at all.
//
// zrok needs an environment, which comes from an account token. OpenZiti needs
// an enrolled identity, which comes from a one-use JWT. Different words, same
// shape: paste the thing you were given, press the button.
const OVERLAY_SETUP = {
  zrok: {
    title: "this machine is not enabled yet",
    what: "zrok needs an environment before it can share anything. That comes from your " +
          "account token, which you get once and reuse on every machine.",
    label: "account token",
    placeholder: "paste your zrok account token",
    name: "what to call this machine",
    namePlaceholder: "left to zrok's default when empty",
    action: "enable this machine",
    link: "get a token"
  },
  ziti: {
    title: "no identity yet",
    what: "An identity is what proves this machine to your network. It comes from a one-use " +
          "enrollment token, which your network administrator issues.",
    label: "enrollment token",
    placeholder: "paste the enrollment JWT",
    name: "call this identity",
    namePlaceholder: "atrium",
    action: "enroll this machine",
    link: "how to get one"
  }
};

// Why every field below is greyed out, said once at the top.
//
// A row of disabled inputs with no explanation reads as broken. The rule is
// simple and worth one line: a running share is serving what it was started
// with, so changing the settings means stopping it first.
function lockedNote(o) {
  if (!o.running) return "";
  return `<div class="ov-note">These are what the share running right now was started
    with, so they cannot be changed while it is up. Press
    <b>stop sharing</b> below to edit them.</div>`;
}

// WHOSE ZROK ACCOUNT, and which instance it talks to.
//
// Drawn in the panel rather than in the setup block, and that placement is the
// whole point. The setup block only appears while the overlay is NOT ready, so
// on a machine with zrok already enabled these controls did not exist, and
// that machine is precisely the one that needs them: somebody with a working
// public account who wants atrium on a different instance.
//
// Which environment sits above which instance because the second only makes
// sense once the first is decided. Pointing the machine's environment
// somewhere else means disabling the one every other tool on the machine
// shares. Atrium having its own means nothing outside atrium changes at all.
// That is too large a difference to leave implied by a text box.
//
// Both are shown even while enabled, where the daemon will refuse to move
// them. The refusal names the instance and says to disable first, which is a
// better answer than a control that is not there.
// The address of the zrok everybody gets by default.
//
// Named rather than left as a placeholder, because the instance toggle below
// has to be able to SAY "the public one" and mean a specific host. An empty
// endpoint and this string are the same choice, and the daemon stores the empty
// one so zrok's own resolution keeps working.
const ZROK_PUBLIC = "https://api.zrok.io";

// Which side of the instance toggle is showing, while it is being changed.
//
// `null` means "whatever the daemon says", which is the state after every
// repaint. It is only set by pressing a side, so choosing `somewhere else`
// reveals the box BEFORE anything is saved: saving first would mean pressing a
// button, watching nothing appear, and having no idea what to do next.
let zrokInstanceCustom = null;

function zrokWhoseAccount(o) {
  if (o.kind !== "zrok") return "";
  const setup = o.setup || {};
  // Locked while a share is up, for the reason `lockedNote` gives. Moving an
  // environment out from under a running share is the one edit here that could
  // break something already working.
  const lock = o.running ? " disabled" : "";
  const own = !!setup.own;

  // TWO WAYS TO USE ZROK, and the difference is who owns the credential.
  //
  // Written as a question with two answers rather than as "whose account",
  // which named the mechanism to somebody who already understood it. What
  // anybody arriving here actually wants to decide is whether atrium reuses
  // what `zrok enable` already did on this machine, or keeps a second
  // environment of its own that nothing else touches.
  //
  // The long version of each is a tooltip. A settings row cannot afford two
  // paragraphs, and the paragraphs are what make the choice answerable, so
  // they go where they can be asked for.
  const tipMachine =
    "Atrium shares from the environment `zrok enable` already made on this machine, the same " +
    "one the zrok command uses. Nothing to paste and nothing new to manage. The instance is " +
    "whatever that environment was enabled against, so atrium cannot point somewhere else " +
    "without disabling the environment every other tool here shares.";
  const tipOwn =
    "Atrium keeps a second zrok environment in its own directory and manages it: you paste an " +
    "enable token once, atrium enables it, reserves its own share names and releases them. The " +
    "zrok command and everything else on this machine are untouched, and this is the only way " +
    "to run atrium against a different zrok instance than the rest of the machine.";

  // ONLY A CHOICE WHEN THERE IS ONE. On a machine with no zrok environment the
  // left-hand answer is a button whose only outcome is a refusal, so the toggle
  // collapses to the statement of what is going to happen.
  const whose = setup.machine_enabled
    ? `<div class="seg" id="ovs-zrok-whose">
         <button class="${own ? "" : "on"}"${lock} onclick="pickZrokAccount(false)"
           >use this machine's zrok<span class="help" tabindex="0"
             onclick="event.stopPropagation()" data-tip="${esc(tipMachine)}">?</span></button>
         <button class="${own ? "on" : ""}"${lock} onclick="pickZrokAccount(true)"
           >give atrium its own<span class="help" tabindex="0"
             onclick="event.stopPropagation()" data-tip="${esc(tipOwn)}">?</span></button>
       </div>
       <span class="hintline">${own
         ? esc("Atrium's own, kept at " + (setup.root || "its own directory") + ".")
         : "Atrium shares from the same account as the zrok command."}</span>`
    : `<div class="hintline">This machine has no zrok environment, so atrium keeps its
         own. Paste an enable token below and atrium manages it from there.</div>`;

  return `
    <div class="ov-field">
      <label class="eyebrow">how do you want to use zrok?</label>
      ${whose}
    </div>
    ${zrokInstanceField(o, own, lock)}`;
}

// WHICH ZROK, as a toggle rather than as a text box that is usually right.
//
// The box asked everybody to know an address to leave alone. Almost every
// machine wants the public zrok and should press nothing; the ones that do not
// want a specific private instance and know its URL. So the common answer is a
// button and the box only exists once the uncommon one is chosen.
//
// NOT OFFERED FOR THE MACHINE'S OWN ENVIRONMENT. That environment was enabled
// against an instance and moving it means disabling the one every other tool
// on this machine shares, which is not a thing to offer as a toggle. It is
// reported instead, with the sentence that says how to change it.
function zrokInstanceField(o, own, lock) {
  const setup = o.setup || {};
  const at = (setup.api_endpoint || "").trim();

  // AN ENABLED ENVIRONMENT IS NOT A TOGGLE, and offering one is what made this
  // look broken. Enabling issues a token against one instance and sending it
  // to another fails as if the token were revoked, so the daemon refuses to
  // move an enabled environment. The board offered two buttons anyway: one was
  // already selected and did nothing, the other was refused, and the refusal
  // was a toast that had already gone by the time anybody looked.
  //
  // So while it is enabled this states what is true and what the way out is.
  // Both environments end up here, by different routes: the machine's belongs
  // to every other tool on it, and atrium's own has to be disabled first.
  if (setup.enabled) {
    return `
      <div class="ov-field">
        <label class="eyebrow">which zrok</label>
        <div class="hintline"><b>${at ? esc(at) : "zrok's own default"}</b>, which is what
          ${own ? "atrium's own environment" : "this machine"} was enabled against. An enable
          token belongs to one instance, so this cannot move while it is enabled:
          ${own
            ? "press <b>disable</b> below, set the address, then enable again."
            : "give atrium its own environment above, which leaves this machine alone."}</div>
      </div>`;
  }

  const custom = zrokInstanceCustom === null
    ? !!at && at !== ZROK_PUBLIC
    : zrokInstanceCustom;

  return `
    <div class="ov-field">
      <label class="eyebrow">which zrok</label>
      <div class="seg" id="ovs-zrok-where">
        <button class="${custom ? "" : "on"}"${lock} onclick="pickZrokInstance(false)"
          >the public zrok</button>
        <button class="${custom ? "on" : ""}"${lock} onclick="pickZrokInstance(true)"
          >somewhere else</button>
      </div>
      ${custom ? `
        <div class="picker">
          <input type="text" id="ovs-zrok-api" spellcheck="false"${lock}
            value="${esc(at === ZROK_PUBLIC ? "" : at)}"
            placeholder="https://zrok.example.com">
          <button onclick="saveZrokEndpoint()"${lock}>use this one</button>
        </div>` : ""}
      <span class="hintline" id="ovs-zrok-api-said">${custom
        ? "The API address of your own zrok. An enable token from one instance does not work " +
          "on another, so set this before enabling."
        : esc(ZROK_PUBLIC + ", which is what an account from zrok.io uses.")}</span>
    </div>`;
}

// Picking one side of the how-do-you-want-to-use-zrok toggle.
//
// It SAVES rather than only painting, because the two sides are different
// environments and everything below reads from whichever is selected: the
// instance, whether anything is enabled, and which token the box wants. A
// toggle that only changed its own colour would leave the rest of the panel
// describing the other one.
//
// The endpoint travels with it, unchanged, because the daemon treats the pair
// as one decision and would otherwise be told about a switch with no address.
async function pickZrokAccount(own) {
  const el = document.getElementById("ovs-zrok-api");
  zrokInstanceCustom = null;
  try {
    await api("/v1/overlays/zrok/endpoint", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ own: !!own, endpoint: ((el && el.value) || "").trim() })
    });
  } catch (e) {
    toast("could not switch account", e.message);
  }
  loadOverlays();
}

// Picking the public zrok, or revealing the box for anything else.
//
// The two sides are not symmetrical and that is on purpose. `the public zrok`
// is a complete answer, so it saves at once. `somewhere else` is half an
// answer: nothing is saved until an address is typed and `use this one` is
// pressed, because saving an empty custom endpoint would silently mean the
// public one again.
async function pickZrokInstance(custom) {
  zrokInstanceCustom = !!custom;
  if (custom) { paintOverlays(); return; }
  try {
    await api("/v1/overlays/zrok/endpoint", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ own: zrokIsOwn(), endpoint: "" })
    });
  } catch (e) {
    toast("could not switch instance", e.message);
  }
  zrokInstanceCustom = null;
  loadOverlays();
}

// Whether atrium is on its own environment, read from the last view rather
// than from the DOM.
//
// It used to be read off a hidden input, which has no `checked`, so it was
// always false: saving an endpoint on a machine with no zrok of its own moved
// atrium OFF its own environment and back onto one that does not exist.
function zrokIsOwn() {
  const o = (overlays || []).find(v => v.kind === "zrok");
  return !!((o && o.setup && o.setup.own) || !(o && o.setup && o.setup.machine_enabled));
}

function overlaySetup(o) {
  const s = OVERLAY_SETUP[o.kind];
  if (!s) return "";
  const setup = o.setup || {};

  // A path that points at nothing is worth saying out loud. Configured and
  // missing looks identical to never configured otherwise, and the fix is
  // different.
  const gone = setup.path && !setup.present
    ? `<div class="ov-err">The identity atrium was pointed at is not there any more:
         <code>${esc(setup.path)}</code></div>`
    : "";
  const bad = setup.err ? `<div class="ov-err">${esc(setup.err)}</div>` : "";

  // Which zrok you are talking to, above the token that only works on one of
  // them. It has to be set BEFORE enabling, because enabling talks to whatever
  // this names, so it is here rather than in the settings below: those are for
  // a machine that is already set up.
  // Which environment and which instance are drawn by `zrokWhoseAccount`, in
  // the panel itself rather than here. They were here first and it was wrong:
  // this block is only rendered while the overlay is NOT ready, so on a machine
  // that already had zrok enabled the controls for pointing atrium somewhere
  // else were invisible, which is exactly the machine that needs them.
  const where = zrokWhoseAccount(o);

  return `${gone}${bad}
    <div class="ov-setup">
      <b>${esc(s.title)}</b>
      <p>${esc(s.what)}</p>
      ${where}
      <div class="ov-field">
        <label class="eyebrow" for="ovs-${esc(o.kind)}-token">${esc(s.label)}${
          // Said in the label, where somebody looking at an empty box looks
          // first, rather than only in a refusal after they press the button.
          o.kind === "zrok" ? ` <span class="req">required</span>` : ""}</label>
        ${o.kind === "zrok"
          // A PASSWORD FIELD, because that is what it is. A zrok account token
          // is reusable on every machine and grants everything that account
          // can do, and it was sitting in a plain textarea on a board that gets
          // screenshotted, shared and popped out into windows on other
          // monitors.
          //
          // An input rather than a textarea for the same reason: a token is one
          // line, and a textarea cannot be masked.
          ? `<input type="password" id="ovs-zrok-token" spellcheck="false"
               autocomplete="off"
               placeholder="${esc(s.placeholder)}"
               oninput="tokenTyped('zrok')">`
          // The ziti side stays a textarea. An enrollment JWT is three long
          // base64 segments that people paste and want to see, and it is
          // single use rather than an account credential.
          : `<textarea id="ovs-${esc(o.kind)}-token" rows="3" spellcheck="false"
               placeholder="${esc(s.placeholder)}"
               oninput="tokenTyped('${esc(o.kind)}')"></textarea>`}
        <span class="hintline" id="ovs-${esc(o.kind)}-claims"></span>
      </div>
      <div class="ov-field">
        <label class="eyebrow" for="ovs-${esc(o.kind)}-name">${esc(s.name)}</label>
        <input type="text" id="ovs-${esc(o.kind)}-name" spellcheck="false"
          placeholder="${esc(s.namePlaceholder)}">
      </div>
      <div class="ov-actions">
        <button class="go" onclick="setupOverlay('${esc(o.kind)}')">${esc(s.action)}</button>
        <a class="ov-link" href="${esc(o.signup || o.install)}"
          target="_blank" rel="noreferrer">${esc(s.link)}</a>
        <span class="grow"></span>
      </div>
    </div>`;
}

// Reads a pasted enrollment token and says what it is for.
//
// Only for ziti: a zrok account token is opaque, and there is nothing to read
// out of it. Debounced, because this fires on every keystroke of a paste.
let tokenTimer = null;
function tokenTyped(kind) {
  if (kind !== "ziti") return;
  clearTimeout(tokenTimer);
  tokenTimer = setTimeout(() => inspectToken(kind), 350);
}

async function inspectToken(kind) {
  const el = document.getElementById(`ovs-${kind}-token`);
  const out = document.getElementById(`ovs-${kind}-claims`);
  if (!el || !out) return;
  const token = el.value.trim();
  if (!token) { out.textContent = ""; out.className = "hintline"; return; }

  let c;
  try {
    c = await api("/v1/overlays/inspect-token", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token })
    });
  } catch (e) {
    out.textContent = e.message;
    out.className = "hintline bad";
    return;
  }
  // Said before the token is spent, since it is one use and a controller
  // refusing an expired one is a worse way to find out.
  if (c.expired) {
    out.textContent = `this token expired at ${c.expires}. ask for a new one.`;
    out.className = "hintline bad";
    return;
  }
  const parts = [];
  if (c.issuer) parts.push(`for ${c.issuer}`);
  if (c.expires) parts.push(`good until ${c.expires}`);
  out.textContent = parts.join(", ") || "readable, but it says nothing about itself";
  out.className = "hintline ok";
}

// Runs the setup step and shows what the tool said, either way.
async function setupOverlay(kind) {
  const token = (document.getElementById(`ovs-${kind}-token`) || {}).value || "";
  const name = (document.getElementById(`ovs-${kind}-name`) || {}).value || "";
  if (!token.trim()) {
    toast("nothing to use", "paste the token first");
    return;
  }
  const btn = event && event.target;
  if (btn) { btn.disabled = true; btn.textContent = "working..."; }

  let res;
  try {
    res = await api(`/v1/overlays/${kind}/setup`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token.trim(), name: name.trim() })
    });
  } catch (e) {
    // A refusal comes back as a body, not a throw, so this is the transport
    // having failed rather than the command.
    toast("could not run it", e.message);
    if (btn) { btn.disabled = false; }
    loadOverlays();
    return;
  }

  overlays = res.overlays || overlays;
  paintOverlays();
  if (res.ok) {
    toast(kind === "zrok" ? "this machine is enabled" : "enrolled",
      res.output ? String(res.output).split("\n").slice(-2).join(" ") : "");
  } else {
    // Its own words, in full. The summary is what fits in a toast, and the
    // detail belongs where it can be read.
    tellUser("that did not work", `<pre class="code">${esc(res.error || "")}${
      res.output ? "\n\n" + esc(res.output) : ""}</pre>`);
  }
}

// Undoing setup. Confirmed, because for zrok this asks the account side to
// forget this machine and there is no undo that does not need the token again.
async function teardownOverlay(kind) {
  const what = kind === "zrok"
    ? "This removes the zrok environment from this machine. Enabling it again needs your " +
      "account token, and any reserved share belonging to it goes."
    : "Atrium forgets which identity to use. The file stays on disk, so nothing is lost, " +
      "but you will have to point atrium at it again.";
  if (!await confirmUser(kind === "zrok" ? "disable zrok here?" : "forget this identity?",
    what, kind === "zrok" ? "disable it" : "forget it")) return;

  try {
    const res = await api(`/v1/overlays/${kind}/teardown`, { method: "POST" });
    overlays = res.overlays || overlays;
    paintOverlays();
    toast(res.ok ? "done" : "that did not work", res.error || res.output || "");
  } catch (e) {
    toast("could not run it", e.message);
  }
}

function overlayField(o, f) {
  const v = (o.config || {})[f.key] || "";
  const id = `ov-${o.kind}-${f.key}`;
  // LOCKED WHILE THE SHARE IS UP. The daemon refuses the write, and a form that
  // takes an edit and then reports a refusal has taught the operator nothing
  // twice: once when they typed it and once when it came back. What a running
  // share was started with is what it is, so the fields say so.
  const lock = o.running ? " disabled" : "";
  const input = f.type === "check"
    ? `<label class="check"><input type="checkbox" id="${id}"${lock}
         ${v ? "checked" : ""} onchange="saveOverlay('${esc(o.kind)}')">
       <span>${esc(f.check || f.label)}</span></label>`
    : f.type === "select"
      ? `<select id="${id}"${lock} onchange="saveOverlay('${esc(o.kind)}')">${
          f.options.map(([val, label]) =>
            `<option value="${esc(val)}" ${val === v ? "selected" : ""}>${esc(label)}</option>`
          ).join("")}</select>`
      // Saved on blur rather than on every keystroke, so a half-typed path is
      // not stored and read back under the cursor.
      : `<input type="text" id="${id}"${lock} spellcheck="false" value="${esc(v)}"
          placeholder="${esc(f.placeholder || "")}" onchange="saveOverlay('${esc(o.kind)}')">`;
  // A field can carry actions that act on what is typed in it. The buttons sit
  // beside the input rather than under the section, because what they do is
  // about that one value and a button further away would be about the overlay.
  //
  // A LIST rather than one: reserving a name, and reserving it and publishing
  // it, are different intentions and both are worth offering. `when` keeps an
  // action out of the way in the states where it cannot mean anything.
  const act = (f.actions || []).filter(a => !a.when || a.when(o)).map(a =>
    `<button class="${a.go ? "go" : ""}"
       onclick="${esc(a.fn)}('${esc(o.kind)}','${esc(f.key)}')"
       ${a.title ? `title="${esc(a.title)}"` : ""}>${esc(a.label)}</button>`).join("");
  return `<div class="ov-field">
    <label class="eyebrow" for="${id}">${esc(f.label)}</label>
    ${act ? `<div class="picker">${input}${act}</div>` : input}
    ${f.hint ? `<span class="hintline">${esc(f.hint)}</span>` : ""}
    <span class="hintline" id="${id}-said"></span>
  </div>`;
}

// Atrium's configuration, out to a file and back.
//
// A DOWNLOAD RATHER THAN A FETCH, so the browser's own save dialog puts it
// where the operator wants it. The daemon already names it and dates it, and a
// file called `export` in a repository is one nobody can identify in six
// months.
//
// A refusal comes back as a 400 with a sentence, because the export stops when
// something in the configuration looks like a credential. That sentence is the
// whole product of the failure, so it is shown rather than summarised.
async function exportConfig() {
  const said = document.getElementById("config-said");
  said.className = "hintline";
  said.textContent = "asking…";
  try {
    // Fetched rather than navigated to, so a refusal can be read. A plain link
    // would show the operator a page of JSON error text in a new tab.
    const res = await fetch("/v1/config/export");
    if (!res.ok) {
      let why = await res.text();
      try { why = (JSON.parse(why) || {}).error || why; } catch (e) {}
      said.textContent = why;
      said.className = "hintline bad";
      return;
    }
    const blob = await res.blob();
    const named = (res.headers.get("Content-Disposition") || "").split("filename=")[1];
    const name = named ? named.replace(/"/g, "") : "atrium-config.json";
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    a.click();
    URL.revokeObjectURL(a.href);
    said.textContent = "saved as " + name + ". no credentials are in it.";
    said.className = "hintline ok";
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
  }
}

// Reading one back. A DRY RUN FIRST, always.
//
// The list of what would change is the question somebody restoring a machine
// actually has, and answering it by doing it is not an answer. Applying is a
// second press, made after reading it.
async function importConfig(input) {
  const said = document.getElementById("config-said");
  const file = input.files && input.files[0];
  // Cleared so choosing the same file twice fires the event both times.
  input.value = "";
  if (!file) return;

  let text;
  try {
    text = await file.text();
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
    return;
  }

  let dry;
  try {
    dry = await api("/v1/config/import", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: text
    });
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
    return;
  }

  const changes = dry.changes || [];
  const doing = changes.filter(c => c.action !== "keep");
  if (!doing.length) {
    tellUser("nothing to change", esc(dry.note || "this machine already matches that file."));
    said.textContent = "nothing to change.";
    said.className = "hintline";
    return;
  }
  const rows = changes.map(c =>
    `<div class="sharerow"><div class="grow"><b>${esc(c.action)}</b> ${esc(c.kind)}
       <code>${esc(c.name)}</code>${c.why ? `<span class="hintline">${esc(c.why)}</span>` : ""}
     </div></div>`).join("");

  const go = await askUser({
    title: "apply this configuration?",
    body: `${doing.length} thing(s) would change. Anything already set here is kept unless you
      choose to overwrite it.<br><br>${esc(dry.note || "")}${rows}`,
    choices: [
      { value: "safe", label: "add what is missing, keep what is here" },
      { value: "force", label: "overwrite what is already set too" }
    ],
    buttons: [{ label: "cancel", value: null }, { label: "apply", value: true, style: "go" }]
  });
  if (!go) return;

  try {
    const res = await api("/v1/config/import?apply=1" + (go === "force" ? "&force=1" : ""), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: text
    });
    said.textContent = (res.changes || []).filter(c => c.action !== "keep").length +
      " applied. " + (res.note || "");
    said.className = "hintline ok";
    // Everything on this screen may have moved.
    loadOverlays();
    paintSettings();
    refresh();
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
  }
}

// The login in front of the published board.
//
// Read on open and written on every change, like the rest of the gear. The
// SECRET is never read back: the daemon reports whether one is set and nothing
// more, so an empty box means "keep what is there" rather than "clear it".
// This board gets screenshotted and popped out into windows, and a secret that
// round-trips through a form is a secret in all of those.
async function loadAuth() {
  let a;
  try {
    a = await api("/v1/auth");
  } catch (e) {
    // An older daemon has no login support. Nothing to draw and not an error.
    return;
  }
  const set = (id, v) => {
    const el = document.getElementById(id);
    if (el) el.value = v || "";
  };
  const on = document.getElementById("auth-on");
  if (on) on.checked = !!a.enabled;
  set("auth-issuer", a.issuer);
  set("auth-client", a.client_id);
  set("auth-redirect", a.redirect);
  set("auth-allow", (a.allow || []).join(", "));
  const said = document.getElementById("auth-secret-said");
  if (said) {
    said.textContent = a.has_client_secret
      ? "a secret is set. typing here replaces it, and leaving it empty keeps it."
      : "no secret set. leave it empty if your provider registered a public client.";
  }
  // The name and password half. Same rule as the secret: the password is never
  // read back, only whether there is one, so an empty box keeps what is there.
  const basic = document.getElementById("auth-basic");
  if (basic) basic.checked = !!a.basic;
  set("auth-user", a.user);
  const pwSaid = document.getElementById("auth-pass-said");
  if (pwSaid) {
    pwSaid.textContent = a.has_password
      ? "a password is set. typing here replaces it, and leaving it empty keeps it."
      : "no password set yet.";
  }
}

async function saveAuth() {
  const val = id => (document.getElementById(id) || {}).value || "";
  const said = document.getElementById("auth-said");
  const body = {
    enabled: !!(document.getElementById("auth-on") || {}).checked,
    issuer: val("auth-issuer").trim(),
    client_id: val("auth-client").trim(),
    redirect: val("auth-redirect").trim(),
    allow: val("auth-allow").split(",").map(s => s.trim()).filter(Boolean),
    client_secret: val("auth-secret"),
    basic: !!(document.getElementById("auth-basic") || {}).checked,
    user: val("auth-user").trim(),
    // Sent in plain and hashed by the daemon, which is the only place it ever
    // exists. Empty means keep the one that is there, for the same reason the
    // secret above works that way.
    password: val("auth-pass")
  };
  try {
    await api("/v1/auth", {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    if (said) { said.textContent = "saved."; said.className = "hintline ok"; }
    // Cleared after a successful save, so the secret is not sitting in the DOM
    // for the rest of the session.
    const s = document.getElementById("auth-secret");
    if (s) s.value = "";
    loadAuth();
  } catch (e) {
    // The daemon refuses a configuration that could not let anybody in, and
    // that refusal is the useful sentence.
    if (said) { said.textContent = e.message; said.className = "hintline bad"; }
  }
}

// Chooses whose zrok account atrium uses, and points it at an instance.
//
// One request for both, because the daemon treats them as one decision and
// two requests would let the board reach a state the daemon refuses: atrium's
// own environment saved, the address not, so it enables against the public
// service and reports the address somebody typed.
async function saveZrokEndpoint() {
  const el = document.getElementById("ovs-zrok-api");
  const said = document.getElementById("ovs-zrok-api-said");
  const endpoint = (el.value || "").trim();
  if (!endpoint) {
    said.textContent = "type the address, or press `the public zrok` above.";
    return;
  }
  try {
    await api("/v1/overlays/zrok/endpoint", {
      // WHOSE ENVIRONMENT COMES FROM THE VIEW, not from the DOM. This read a
      // hidden input's `checked`, which a hidden input does not have, so it
      // was always false: saving an address on a machine with no zrok of its
      // own moved atrium off its own environment every time.
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ endpoint, own: zrokIsOwn() })
    });
    said.textContent = "saved. enabling will talk to that one.";
    zrokInstanceCustom = null;
    loadOverlays();
  } catch (e) {
    // The daemon refuses to move an environment that is already enabled, and
    // that refusal is the useful sentence rather than an error to summarise.
    said.textContent = e.message || String(e);
  }
}

// Asks the network what this identity may host, and offers the answers.
//
// The bindable ones are clickable, because the next thing anybody does with
// this list is type one of those names into the box above it, and a list you
// have to transcribe from is a list with a typo in it.
async function zitiServices(kind, key) {
  const said = document.getElementById(`ov-${kind}-${key}-said`);
  said.textContent = "asking the network...";
  let cap;
  try {
    cap = await api(`/v1/overlays/${kind}/services`);
  } catch (e) {
    said.textContent = e.message || String(e);
    return;
  }
  if (cap.err) { said.textContent = cap.err; return; }

  const svcs = cap.services || [];
  if (!svcs.length) {
    said.textContent = cap.identity
      ? `${cap.identity} can see no services at all. that is a policy on the network.`
      : "this identity can see no services.";
    return;
  }
  const who = cap.identity ? `${esc(cap.identity)}: ` : "";
  // A service this identity can only dial is the common mistake and reads as
  // "no such service" from the listener, so it is listed and marked rather
  // than filtered out.
  const rows = svcs.map(s => s.bind
    ? `<span class="chip attach" title="click to use this one"
         onclick="useZitiService('${esc(kind)}','${esc(key)}','${esc(s.name)}')"
         >${esc(s.name)}</span>`
    : `<span class="chip" title="this identity can reach this service but not host it"
         >${esc(s.name)} (dial only)</span>`).join(" ");
  said.innerHTML = `${who}${cap.bindable} of ${svcs.length} can be hosted.<br>${rows}`;
}

function useZitiService(kind, key, name) {
  const el = document.getElementById(`ov-${kind}-${key}`);
  el.value = name;
  saveOverlay(kind);
  document.getElementById(`ov-${kind}-${key}-said`).textContent =
    `using ${name}.`;
}

// Reads the whole form back rather than patching one field, so what is stored
// is always what is on screen.
async function saveOverlay(kind) {
  const body = {};
  (OVERLAY_UI[kind] || []).forEach(f => {
    const el = document.getElementById(`ov-${kind}-${f.key}`);
    if (!el) return;
    // A checkbox's `value` is the string "on" whether or not it is ticked, so
    // reading it the same way as a text field stores every checkbox as true
    // forever. `checked` is the one that means anything.
    body[f.key] = f.type === "check" ? el.checked : el.value;
  });
  try {
    await api(`/v1/overlays/${kind}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    // Read back rather than assumed. The daemon reconciles the two share
    // checkboxes with the single mode the start path uses, and refuses the
    // whole write while a share is up, so what is on screen after a save
    // should be what was stored and not what was typed.
    loadOverlays();
  } catch (e) {
    // The refusal is the useful sentence. Repainting puts the fields back to
    // what is actually stored, so a rejected edit does not sit on screen
    // looking saved.
    toast("that did not stick", e.message);
    loadOverlays();
  }
}

// Turning on a public share is the one option here that puts a board with no
// login on the open internet, so it says so before it does it.
// What the panel shows instead of its start button while one is running.
//
// Keyed by overlay, because the two are independent and starting one must not
// make the other look busy. The value is the text of the step it is on, which
// arrives over the event stream from `overlayStep`.
const overlayBusy = {};

async function startOverlay(kind) {
  const o = overlays.find(x => x.kind === kind);
  const publicShare = kind === "zrok" && o && (o.config || {}).mode === "public";
  if (publicShare && !await confirmUser("share this publicly?",
    "A public zrok share is a link anyone who has it can open, and this board has no login. " +
    "Whoever opens it can see every card, read every command, and answer permission requests." +
    "<br><br>A private share needs zrok on the other end and is the safer choice.",
    "share it publicly")) return;

  // Set before the request leaves, not on the first event. The events may
  // never arrive, and the one thing that must be true the instant the button
  // is pressed is that the board looks like it heard.
  overlayBusy[kind] = "starting";
  paintOverlays();
  await saveOverlay(kind);
  try {
    overlays = (await api(`/v1/overlays/${kind}/start`, { method: "POST" })).overlays || overlays;
  } catch (e) {
    toast("could not start it", e.message);
  }
  delete overlayBusy[kind];
  paintOverlays();
  // The address arrives on the share's own output a moment after it starts.
  setTimeout(loadOverlays, 1500);
  setTimeout(loadOverlays, 4000);
}

async function stopOverlay(kind) {
  try {
    overlays = (await api(`/v1/overlays/${kind}/stop`, { method: "POST" })).overlays || overlays;
  } catch (e) {
    toast("could not stop it", e.message);
  }
  paintOverlays();
}

function setHookNag(on) {
  skipConfirm(HOOK_NAG, on);
  renderHooks();
  renderRunners();
}

function setGroupMode(mode) {
  const p = groupingPrefs();
  const now = p.on ? (p.mode || "project") : "off";
  if (now === mode) return;
  setGrouping(mode === "off" ? { on: false } : { on: true, mode });
  paintGrouping();
  paintGroupSegs();
}

function resetGrouping() {
  const wasOff = !groupingPrefs().on;
  setGrouping({ by: "", order: "" });
  paintGrouping();
  // Said differently when grouping is switched off, because otherwise this
  // button reports success for something with no visible effect and reads as
  // broken. It DID reset the rule. There is just nothing using it.
  toast("grouping rule reset",
    wasOff
      ? "back to reading a project out of the worktree path. grouping is off, so nothing moved"
      : "back to reading a project out of the worktree path");
}

// Saved on blur rather than on every keystroke, so a half-typed function is
// not compiled and reported as broken while it is being written.
document.getElementById("s-group-by").addEventListener("change", e => {
  setGrouping({ by: e.target.value === DEFAULT_GROUP_BY ? "" : e.target.value });
});
document.getElementById("s-group-order").addEventListener("change", e => {
  setGrouping({ order: e.target.value === DEFAULT_GROUP_ORDER ? "" : e.target.value });
});

