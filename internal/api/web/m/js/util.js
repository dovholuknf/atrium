// Small pure helpers for the phone page. Copied in, not shared, so the board can change without touching this page.
(function () {
  "use strict";

  const ENT = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, c => ENT[c]);
  }

  // Milliseconds since something, as a short quiet age. Compound past a minute is noise on a phone.
  function ago(ms) {
    if (ms == null || isNaN(ms)) return "";
    const s = Math.max(0, Math.floor(ms / 1000));
    if (s < 45) return "now";
    const m = Math.floor(s / 60);
    if (m < 60) return Math.max(1, m) + "m";
    const h = Math.floor(m / 60);
    if (h < 24) return h + "h";
    return Math.floor(h / 24) + "d";
  }

  function ts(x) {
    if (!x) return 0;
    const n = Date.parse(x);
    return isNaN(n) ? 0 : n;
  }

  // The alias first, since that is what the operator calls a card, then the title it wears on the board.
  function cardName(t) {
    const title = String((t && (t.display_title || t.title)) || "untitled");
    const alias = String((t && t.alias) || "");
    return { alias, title, main: alias || title, sub: alias && alias !== title ? title : "" };
  }

  // The board's names for the stored statuses (STATUS_LABEL in js/core.js).
  const STATUS = {
    "needs-input": "ready",
    "needs-permission": "needs permission",
    running: "running",
    done: "done",
    dead: "gone",
    shelved: "shelved",
    backlog: "offered",
  };
  function statusLabel(t) { return STATUS[t && t.status] || String((t && t.status) || ""); }

  // What the runner is doing right now, in words. Nothing when it is not doing anything worth saying.
  function activityText(t) {
    const a = t && t.activity;
    if (!t || t.status === "dead" || t.status === "done" || t.status === "shelved") return "";
    if (t.status === "needs-permission") return "waiting on a permission";
    if (t.status === "needs-input") return "waiting for you";
    if (!a || !a.what) return t.status === "running" ? "working" : "";
    let w = a.what === "tool" ? (a.tool ? "running " + a.tool : "running a tool") : String(a.what);
    if (a.seconds > 5) {
      const m = Math.floor(a.seconds / 60);
      w += " for " + (m >= 1 ? m + "m" : Math.floor(a.seconds) + "s");
    }
    if (a.dialog) w = "a dialog is open on its screen";
    return w;
  }

  window.mUtil = { esc, ago, ts, cardName, statusLabel, activityText };
})();
