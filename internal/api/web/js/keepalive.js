// ── the cache keep-alive, on the board ──────────────────────────────────────
//
// An idle Claude card's prompt cache is refreshed shortly before it expires, so
// coming back to the card costs a cache read rather than a write of its whole
// context. The daemon does the work. This file draws the switch, the chip and
// the toast. See docs/runtime/cache-keepalive-design.md.

// What each stopped state means, for the chip's tooltip.
const KEEPALIVE_STOPPED = {
  "stopped:break-even": "keep-alive stopped at break-even",
  "stopped:miss": "keep-alive stopped: a refresh missed the cache",
  "stopped:failing": "keep-alive stopped: two refreshes in a row failed",
  "stopped:acted": "keep-alive stopped: a refresh tried to use a tool. turn it back on by hand",
};

// The daemon's last skip reason, in words for the watching chip's tooltip. A
// reason not listed here is shown as the daemon wrote it.
const KEEPALIVE_WHY = {
  "": "not looked at yet",
  "not due": "idle, and not due for a refresh yet",
  "not idle": "working, so there is nothing to refresh",
  "cache already cold": "its cache has already gone cold, so there is nothing to keep warm",
  "context under 50k": "its context is under 50k, too small to be worth keeping warm",
  "a permission dialog is open": "a permission dialog is open",
  "no session id yet": "it has no session yet",
};

function keepaliveTime(at) {
  return new Date(at).toLocaleTimeString();
}

// ── the cache chip ───────────────────────────────────────────────────────────
//
// ONE chip on every Claude card, drawn by `keepaliveChip` on the board, the stack
// row, the terminals list, the attached terminal's header and the phone tray. It is
// a coloured dot and nothing else, because words there squeezed the title to one
// letter. The words (is it warm, who keeps it so, until when, and if not, why) are
// in the card's details, `peekCache`, and the tooltip. A card with no `keepalive` is
// not Claude and draws nothing. See docs/backlog/ui/u-032.md.
//
// `kaModel` is the ONE function that decides what a card is. The chip and the
// summary line both read it, so the two cannot disagree.

// What the daemon's `why` means, in one word after "won't refresh:". A why not
// listed here draws no word and stays in the tooltip.
const KA_WONT = {
  "not idle": "busy",
  "context under 50k": "small",
  "not on the 1h cache": "5m cache",
  "local hooks": "local hooks",
  "a permission dialog is open": "dialog open",
  "budget spent": "budget spent",
  "parked": "parked",
  "fast mode": "fast mode",
  "no transcript": "no transcript"
};

// A stopped card in words. The raw state goes in the tooltip.
const KA_STOP_WORD = {
  "stopped:break-even": "not worth it",
  "stopped:miss": "cache missed",
  "stopped:failing": "refresh failing",
  "stopped:acted": "refresh used a tool"
};

// The daemon's `keepaliveMargin`, used only until it sends `next_refresh_at`.
const KA_MARGIN_MS = 5 * 60 * 1000;

// The cache lives an hour and the daemon refreshes KA_MARGIN_MS before it ends, so a pie with no refresh to
// start from began its fill this long before the refresh.
const KA_CYCLE_MS = 3600000;
// The pie is drawn in steps of this many degrees, so a repaint changes the chip only when the wedge moves.
const KA_PIE_STEP = 15;
// A pie this full says it is about to fire.
const KA_DUE_AT = 0.92;

// Every card a chip or a line has drawn, by id, so a tick can redraw a chip from
// what it was drawn from. Cold ones are dropped when the timer is re-armed.
const KA_SEEN = new Map();
const KA_LINES = new Map();
const KA_LINE_IDS = new Set();

function kaNow() { return Date.now(); }

function kaPad(n) { return String(n).padStart(2, "0"); }

// Local HH:MM, with the day when it is not today.
function kaClock(ms, now) {
  const d = new Date(ms), n = new Date(now);
  const hm = kaPad(d.getHours()) + ":" + kaPad(d.getMinutes());
  const day = x => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(d) - day(n)) / 86400000);
  if (diff === 0) return hm;
  if (diff === 1) return "tomorrow " + hm;
  if (diff === -1) return "yesterday " + hm;
  return d.toLocaleDateString([], { month: "short", day: "numeric" }) + " " + hm;
}

function kaWhen(v) {
  const ms = v ? Date.parse(v) : NaN;
  return isNaN(ms) ? 0 : ms;
}

// The room's suspension in words. It stays until a person clears it.
function kaSuspendedTip(reason) {
  return `keep-alive is suspended for the whole room (${reason}), so no card is refreshed. ` +
    "clear it with the clear it button under the keep-alive setting";
}

// What a card's cache is at `now`, or null for a card with no keepalive.
// `bucket` is what the summary line counts: "kept", "warm" or "cold".
// `flip` is when its words next change on their own, 0 for never.
function kaModel(t, now) {
  const k = t && t.keepalive;
  if (!k) return null;
  now = now || kaNow();
  const wu = kaWhen(k.warm_until);
  const warm = wu > now;
  const n = k.refreshes || 0;
  const why = k.why || "";
  const on = k.state === "on";
  const m = {
    bucket: warm ? (on && n > 0 ? "kept" : "warm") : "cold",
    warm, wu, flip: warm ? wu : 0, cls: "", full: "", short: "", tip: [], pie: -1, due: false
  };
  const rawWhy = why ? "the daemon's last reason: " + why : "";
  if (KA_STOP_WORD[k.state]) {
    m.cls = "stopped";
    m.full = "⊘ stopped · " + KA_STOP_WORD[k.state];
    m.short = "⊘ " + KA_STOP_WORD[k.state];
    m.tip.push(KEEPALIVE_STOPPED[k.state]);
    if (n || k.missed) {
      m.tip.push(`${n} refresh${n === 1 ? "" : "es"}` +
        (k.missed ? `, ${k.missed} miss${k.missed === 1 ? "" : "es"}` : ""));
    }
    if (k.missed) m.tip.push("a miss writes the whole context again, about eight times the budget");
    if (wu) m.tip.push((warm ? "cache warm until " : "cache went cold at ") + kaClock(wu, now));
    // A suspended room refreshes nothing, so say it before the card is blamed.
    if (k.suspended) m.tip.push(kaSuspendedTip(k.suspended));
    m.tip.push("it starts again on this card's next turn" +
      (k.state === "stopped:acted" ? " only if you turn it back on" : ", or when you turn it on by hand"));
    m.tip.push("state: " + k.state);
    return m;
  }
  if (!on) {
    if (k.state !== "off") return null;
    m.cls = warm ? "off" : "off cold";
    m.full = warm ? "○ off · warm → " + kaClock(wu, now) : "○ off · cold";
    m.short = "○ off";
    m.tip.push("keep-alive is off for this card, so nothing refreshes its cache");
    m.tip.push(warm ? "its cache is still warm until " + kaClock(wu, now) : "its cache is cold or unknown");
    m.tip.push("turn it on in the card's menu: keep its cache warm");
    return m;
  }
  if (!wu) {
    m.cls = "none";
    m.full = "❄ no cache yet";
    m.short = "❄ no cache";
    m.tip.push("keep-alive is on, but this card has no cache to keep yet");
    if (why) m.tip.push(KEEPALIVE_WHY[why] || why);
    return m;
  }
  if (!warm) {
    m.cls = "cold";
    m.full = "❄ cold since " + kaClock(wu, now);
    m.short = "❄ cold";
    m.tip.push("its cache has expired, so the next turn rewrites the whole context");
    if (why) m.tip.push(KEEPALIVE_WHY[why] || why);
    if (rawWhy) m.tip.push(rawWhy);
    return m;
  }
  // Warm and on. A refresh fires only when every gate in the daemon's `decide`
  // passes, so "next" is promised only for a card that is just waiting for its
  // time. Any other reason is said as a word instead. The tick is once a minute,
  // hence the tilde on the inferred time.
  const exact = kaWhen(k.next_refresh_at);
  const next = exact || (why === "not due" ? wu - KA_MARGIN_MS : 0);
  const waiting = next > now && (exact || why === "not due");
  const wont = !waiting && why && why !== "not due" ? (KA_WONT[why] || "") : "";
  const base = n > 0 ? `❄ kept warm ${n}×` : "❄ warm";
  const until = kaClock(wu, now);
  // The phone's short text is HH:MM alone: "tomorrow" near midnight overflowed its 16 characters, and the full
  // text in the aria-label and tooltip still carries the day.
  const hm = ms => { const d = new Date(ms); return kaPad(d.getHours()) + ":" + kaPad(d.getMinutes()); };
  const tilde = exact ? "" : "~";
  if (waiting && n > 0) {
    m.full = `${base} · next ${tilde}${kaClock(next, now)}`;
    m.short = `❄ ${n}× next ${tilde}${hm(next)}`;
  } else {
    m.full = `${base} → ${until}` + (wont ? ` · won't refresh: ${wont}` : "");
    m.short = `❄ → ${hm(wu)}`;
  }
  m.cls = n > 0 ? "kept" : "warm";
  m.next = waiting ? next : 0;
  if (waiting) {
    // From the last refresh, or from when the cache was written, to the next refresh.
    let start = kaWhen(k.last_refresh_at) || wu - KA_CYCLE_MS;
    if (start >= next) start = next - (KA_CYCLE_MS - KA_MARGIN_MS);
    const f = Math.min(1, Math.max(0, (now - start) / (next - start)));
    m.pie = Math.floor(f * 360 / KA_PIE_STEP) * KA_PIE_STEP;
    m.due = f >= KA_DUE_AT;
    m.cls += m.due ? " pie due" : " pie";
  }
  m.tip.push(n > 0
    ? `atrium has refreshed this card's cache ${n} time${n === 1 ? "" : "s"} this idle stretch`
    : "its cache is warm, so the next turn reads it instead of rewriting it");
  m.tip.push("warm until " + until);
  if (m.due) m.tip.push("about to refresh");
  if (waiting) {
    m.tip.push("next refresh " + (exact ? "at " : "about ") + kaClock(next, now) +
      (exact ? "" : ", checked once a minute"));
  }
  if (k.suspended) m.tip.push(kaSuspendedTip(k.suspended));
  if (why) m.tip.push(KEEPALIVE_WHY[why] || why);
  if (wont) m.tip.push("it will not refresh while: " + wont);
  if (rawWhy) m.tip.push(rawWhy);
  return m;
}

// The chip: a dot. The words ride in a hidden span, so a repaint still compares
// them and a screen reader gets them from aria-label.
function keepaliveChip(t) {
  if (!t || !t.keepalive || over(t) || t.archived_at) return "";
  KA_SEEN.set(t.id, t);
  kaSchedule();
  const m = kaModel(t, kaNow());
  if (!m) return "";
  const tip = m.tip.join(". ");
  const pie = m.pie >= 0 ? ` style="--pie:${m.pie}deg"` : "";
  return `<span class="chip keepalive cache ${m.cls}" data-cid="${esc(t.id)}" data-bucket="${m.bucket}"${pie}
    data-state="${esc(t.keepalive.state)}" role="img" aria-label="${esc(m.full + ". " + tip)}"
    data-tip="${esc(m.full + ". " + tip)}"><span class="cfull">${esc(m.full)}</span></span>`;
}

// The words, for the card's details. Drawn from the same `kaModel` as the dot.
function peekCache(t) {
  const m = t && t.keepalive && !t.archived_at ? kaModel(t, kaNow()) : null;
  if (!m) return "";
  return `<div class="peek-cache ${m.cls}"><b>cache: ${esc(m.full)}</b>` +
    m.tip.map(s => `<span>${esc(s)}.</span> `).join("") + `</div>`;
}

// ── keeping the words true without a fetch ───────────────────────────────────
//
// ONE timeout, for the soonest moment a drawn card's words change, re-armed after
// every repaint. It redraws chips and lines from what they already hold: a label
// tick, no request. Never an interval. A background tab throttles timers, so a tab
// that becomes visible repaints too, and so does every card list.

let kaTimer = 0;
let kaQueued = false;

// Arm after the paint in progress, once however many chips it drew.
function kaSchedule() {
  if (kaQueued) return;
  kaQueued = true;
  queueMicrotask(() => { kaQueued = false; kaArm(); });
}

function kaArm() {
  if (kaTimer) { clearTimeout(kaTimer); kaTimer = 0; }
  const now = kaNow();
  let soonest = 0;
  const soon = ms => { if (ms > now && (!soonest || ms < soonest)) soonest = ms; };
  for (const [id, t] of KA_SEEN) {
    const m = kaModel(t, now);
    if (!m || !m.flip) {
      if (!KA_LINE_IDS.has(id)) KA_SEEN.delete(id);
      continue;
    }
    soon(m.flip);
    soon(m.next);
    // A filling pie moves with the clock: one coarse look a minute, never a CSS animation.
    if (m.pie >= 0 && !m.due) soon(now + 60000);
  }
  if (!soonest) return;
  kaTimer = setTimeout(kaTick, Math.min(Math.max(soonest - now + 50, 250), 2147483000));
}

// Redraw every chip and line in place from the cards they were drawn from.
function kaRepaint() {
  document.querySelectorAll(".chip.cache[data-cid]").forEach(el => {
    const t = KA_SEEN.get(el.dataset.cid);
    if (!t) return;
    const html = keepaliveChip(t);
    if (!html) { el.remove(); return; }
    const tpl = document.createElement("template");
    tpl.innerHTML = html.trim();
    const fresh = tpl.content.firstElementChild;
    if (fresh && fresh.outerHTML !== el.outerHTML) el.replaceWith(fresh);
  });
  for (const [id, list] of KA_LINES) paintCacheLine(id, list, true);
}

function kaTick() {
  kaTimer = 0;
  kaRepaint();
  kaArm();
}

// A card list arrived: remember it for the header chip and repaint what is drawn.
function cacheRefresh(cards) {
  for (const t of cards || []) if (t && t.keepalive) KA_SEEN.set(t.id, t);
  // The attached terminal's header was drawn from the card as it was when it
  // attached, which may have had no switch yet.
  const head = document.getElementById("t-chips");
  const mine = typeof termTask !== "undefined" && termTask && KA_SEEN.get(termTask.id);
  if (head && mine && !head.querySelector(".chip.cache") && !document.getElementById("term-pane")?.classList.contains("dead")) {
    head.insertAdjacentHTML("afterbegin", keepaliveChip(mine));
  }
  kaRepaint();
  kaArm();
}

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) kaTick();
});

// ── the summary line ─────────────────────────────────────────────────────────
//
// `cache: 5 warm · 2 kept warm · 9 cold · keep-alive 83 refreshes this week`, over
// the Claude cards a list shows, not archived. Counted from `kaModel`'s bucket, the
// same value the chip carries, so the two cannot disagree. The week figure is the
// daemon's setting. The spend today is drawn only when the daemon sends it as
// `cache_keepalive_today_tokens`: the board holds no usage for today without a fetch.

function cacheLineText(list) {
  const c = { warm: 0, kept: 0, cold: 0 };
  let any = 0;
  const now = kaNow();
  for (const t of list || []) {
    if (!t || !t.keepalive || over(t) || t.archived_at || t.offline) continue;
    const m = kaModel(t, now);
    if (!m) continue;
    any++;
    c[m.bucket]++;
  }
  if (!any) return "";
  const s = typeof pastePrefs !== "undefined" && pastePrefs ? pastePrefs : {};
  const parts = [`${c.warm} warm`, `${c.kept} kept warm`, `${c.cold} cold`];
  const wk = s.cache_keepalive_week_refreshes;
  if (typeof wk === "number") parts.push(`keep-alive ${wk} refresh${wk === 1 ? "" : "es"} this week`);
  const today = s.cache_keepalive_today_tokens;
  if (typeof today === "number") {
    parts.push(`${typeof usageTokens === "function" ? usageTokens(today) : today} tokens today`);
  }
  return "cache: " + parts.join(" · ");
}

// Writes the line into `#id`, which may not exist yet: the terminals list is
// rebuilt on every paint, so the same call is made after it.
function paintCacheLine(id, list, quiet) {
  KA_LINES.set(id, list);
  for (const t of list || []) {
    if (t && t.keepalive) { KA_SEEN.set(t.id, t); KA_LINE_IDS.add(t.id); }
  }
  // The week figure lives in the settings, which nothing loads at boot. Asked once, cached by
  // `pasteSettings`, then the lines are painted again with it.
  if (typeof pastePrefs !== "undefined" && !pastePrefs && typeof pasteSettings === "function" && !quiet) {
    pasteSettings().then(() => kaRepaint());
  }
  const el = document.getElementById(id);
  if (el) {
    const txt = cacheLineText(list);
    if (el.textContent !== txt) el.textContent = txt;
    el.hidden = !txt;
  }
  if (!quiet) kaSchedule();
}

// The line opens the gear on the keep-alive setting.
function openKeepaliveSetting() {
  const gear = document.getElementById("gear");
  if (gear) gear.click();
  const field = document.getElementById("s-keepalive");
  const pane = field && field.closest(".pane");
  if (pane && typeof showSettingsPane === "function") showSettingsPane(pane.dataset.name);
  if (!field) return;
  const box = field.closest(".field") || field;
  box.scrollIntoView({ block: "center" });
  box.classList.remove("flash");
  void box.offsetWidth;
  box.classList.add("flash");
  field.focus();
}

// The card menu's switch. Null for a card with no switch, which is every card
// that is not Claude.
function keepaliveMenuItem(t, refresh) {
  const k = t.keepalive;
  if (!k) return null;
  const on = k.state === "on";
  return {
    label: "keep its cache warm", on,
    help: "When this card has been idle for nearly an hour, atrium refreshes its prompt cache. It " +
      "sends one word from a copy of the conversation and throws the copy away. The card's own " +
      "conversation is never touched. Coming back costs a cache read instead of rewriting the whole " +
      "context. It stops by itself once the refreshes have cost an eighth of one rewrite." +
      (KEEPALIVE_STOPPED[k.state] ? " Now: " + KEEPALIVE_STOPPED[k.state] + "." : ""),
    act: () => setCardKeepalive(t.id, !on).then(refresh)
  };
}

async function setCardKeepalive(id, on) {
  try {
    await api(`/v1/tasks/${id}/keepalive`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ on })
    });
  } catch (e) {
    toast("keep-alive did not change", e.message);
    return;
  }
  toast("keep-alive " + (on ? "on" : "off"), on
    ? "this card's cache is kept warm while it is idle, with a fresh budget"
    : "this card's cache goes cold an hour after its last turn");
}

// The settings screen: the default for new cards, and the suspension.
function paintKeepaliveSettings(s) {
  const el = document.getElementById("s-keepalive");
  if (el) el.checked = s.cache_keepalive_default !== false;
  const spend = document.getElementById("s-keepalive-spend");
  if (spend) {
    const n = s.cache_keepalive_week_refreshes || 0;
    spend.textContent = n
      ? `last 7 days: ${n} refresh${n === 1 ? "" : "es"}`
      : "last 7 days: no refreshes";
  }
  const sus = document.getElementById("s-keepalive-suspended");
  if (sus) {
    sus.hidden = !s.cache_keepalive_suspended;
    const why = document.getElementById("s-keepalive-why");
    if (why) why.textContent = s.cache_keepalive_suspended || "";
  }
}

async function saveKeepaliveDefault() {
  const el = document.getElementById("s-keepalive");
  if (!el) return;
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ cache_keepalive_default: !!el.checked })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  if (typeof afterMachineSave === "function") afterMachineSave();
  toast("saved", el.checked
    ? "new Claude cards start with their cache kept warm. cards already open keep their own switch"
    : "new Claude cards start with keep-alive off. cards already open keep their own switch");
}

async function clearKeepaliveSuspension() {
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ cache_keepalive_suspended: false })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  paintKeepaliveSettings(pastePrefs || {});
  toast("keep-alive resumed", "cards with the switch on are refreshed again");
}

// The daemon's `keepalive` event. A stop at break-even and a suspension carry a
// toast, which goes through `toast` and so into the toast log like every other.
function onKeepaliveEvent(e) {
  let d;
  try { d = JSON.parse(e.data) || {}; } catch (err) { return; }
  if (d.toast) {
    const title = d.suspended ? "keep-alive suspended" : "keep-alive stopped";
    // Clicking it lands on the card, the same way a card's own alerts do.
    toast(title, d.toast, "", "", d.task_id || "");
  }
  if (d.suspended && typeof loadHousekeeping === "function") loadHousekeeping();
  // A refresh finished, so the week's count moved. Asked for on the event, never
  // on a timer, and the cards themselves come from the `tasksSoon` beside this.
  if (d.outcome && typeof api === "function") {
    api("/v1/settings").then(s => {
      if (s && typeof pastePrefs !== "undefined") { pastePrefs = s; kaRepaint(); }
    }).catch(() => {});
  }
}
