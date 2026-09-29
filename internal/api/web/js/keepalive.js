// ── the cache keep-alive, on the board ──────────────────────────────────────
//
// An idle Claude card's prompt cache is refreshed shortly before it expires, so
// coming back to the card costs a cache read rather than a write of its whole
// context. The daemon does the work. This file draws the switch, the chip and
// the toast. See docs/cache-keepalive-design.md.

// What each stopped state means, for the chip's tooltip.
const KEEPALIVE_STOPPED = {
  "stopped:break-even": "keep-alive stopped at break-even",
  "stopped:miss": "keep-alive stopped: a refresh missed the cache",
  "stopped:failing": "keep-alive stopped: two refreshes in a row failed",
  "stopped:acted": "keep-alive stopped: a refresh tried to use a tool. turn it back on by hand",
};

function keepaliveMoney(n) {
  return "$" + (Number(n) || 0).toFixed(2);
}

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

// The chip on a card, on every card keep-alive watches. Three looks, so a
// glance tells them apart:
//   - watching: the switch is on and nothing has been refreshed yet. The
//     tooltip says why not, and until when the cache is warm.
//   - warm: it has refreshed this card in its current idle stretch.
//   - cold: it stopped, and the tooltip says why and what that cost.
// A card with the switch off, or with no switch, draws nothing.
function keepaliveChip(t) {
  const k = t.keepalive;
  if (!k || over(t)) return "";
  if (KEEPALIVE_STOPPED[k.state]) {
    const parts = [KEEPALIVE_STOPPED[k.state]];
    if (k.refreshes || k.missed || k.spent) {
      parts.push(`${k.refreshes} refresh${k.refreshes === 1 ? "" : "es"}` +
        (k.missed ? `, ${k.missed} miss${k.missed === 1 ? "" : "es"}` : "") +
        `, ${keepaliveMoney(k.spent)}` + (k.budget ? ` of a ${keepaliveMoney(k.budget)} budget` : ""));
    }
    if (k.missed) parts.push("a miss writes the whole context again, about eight times the budget");
    if (k.warm_until) parts.push("cache went cold at " + keepaliveTime(k.warm_until));
    parts.push("it starts again on this card's next turn" +
      (k.state === "stopped:acted" ? " only if you turn it back on" : ", or when you turn it on by hand"));
    return `<span class="chip keepalive stopped" data-state="${esc(k.state)}"
      data-tip="${esc(parts.join(". "))}">&#10052; cold</span>`;
  }
  if (k.state === "on" && k.refreshes > 0) {
    return `<span class="chip keepalive" data-tip="${esc(
      `kept warm ${k.refreshes}x, ${keepaliveMoney(k.spent)} of ${keepaliveMoney(k.budget)}` +
      (k.warm_until ? ". warm until " + keepaliveTime(k.warm_until) : ""))}"
      >&#10052; warm</span>`;
  }
  if (k.state === "on") {
    const why = k.why || "";
    const parts = ["keep-alive is watching this card", KEEPALIVE_WHY[why] || why];
    if (k.warm_until) {
      parts.push((new Date(k.warm_until) > new Date() ? "warm until " : "cache went cold at ") +
        keepaliveTime(k.warm_until));
    }
    if (k.budget) parts.push("refreshes about 5 minutes before expiry, up to a " + keepaliveMoney(k.budget) + " budget");
    return `<span class="chip keepalive watching" data-why="${esc(why)}"
      data-tip="${esc(parts.join(". "))}">&#9678; watching</span>`;
  }
  return "";
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
      ? `last 7 days: ${n} refresh${n === 1 ? "" : "es"}, ${keepaliveMoney(s.cache_keepalive_week_usd)}`
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
}
