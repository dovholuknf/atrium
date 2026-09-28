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

// The chip on a card. Drawn only when keep-alive has done something worth
// seeing: it is keeping this card warm, or it stopped. A card with the switch on
// and nothing spent yet draws nothing, since that is every idle card.
function keepaliveChip(t) {
  const k = t.keepalive;
  if (!k || over(t)) return "";
  if (KEEPALIVE_STOPPED[k.state]) {
    const parts = [KEEPALIVE_STOPPED[k.state]];
    if (k.refreshes || k.spent) {
      parts.push(`${k.refreshes} refresh${k.refreshes === 1 ? "" : "es"}, ${keepaliveMoney(k.spent)}` +
        (k.budget ? ` of a ${keepaliveMoney(k.budget)} budget` : ""));
    }
    if (k.warm_until) parts.push("cache went cold at " + new Date(k.warm_until).toLocaleTimeString());
    parts.push("it starts again on this card's next turn" +
      (k.state === "stopped:acted" ? " only if you turn it back on" : ", or when you turn it on by hand"));
    return `<span class="chip keepalive stopped" data-state="${esc(k.state)}"
      data-tip="${esc(parts.join(". "))}">&#10052; cold</span>`;
  }
  if (k.state === "on" && k.refreshes > 0) {
    return `<span class="chip keepalive" data-tip="${esc(
      `kept warm ${k.refreshes}x, ${keepaliveMoney(k.spent)} of ${keepaliveMoney(k.budget)}` +
      (k.warm_until ? ". warm until " + new Date(k.warm_until).toLocaleTimeString() : ""))}"
      >&#10052; warm</span>`;
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
    help: "When this card has been idle for nearly an hour, atrium refreshes its prompt cache with a " +
      "throwaway copy of the conversation that answers OK and is discarded, so coming back costs a " +
      "cache read instead of rewriting the whole context. It stops by itself once the refreshes have " +
      "cost an eighth of one rewrite." +
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
