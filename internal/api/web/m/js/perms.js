// The phone page's permission rows, mounted into #m-perms for one card (or for every card when cardId is null).
//
// Each pending request from `mStore.perms()` is a row: the tool, the command or path in monospace, and three
// actions with 44px targets. approve, deny, and deny with a reason. The reason is how a block says "do X
// instead", so it opens a field in place and goes with the block.
//
// THE SAME CALL AS THE BOARD (`decide` in js/settings-spine.js): `POST /v1/permissions/{id}/decide` with
// `{decision: "approve" | "block", reason, forever, prefix, kind, command}`. A request that came from a room is
// answered by that room's own daemon, so the room (`room`, or the tag on the id) goes in `X-Atrium-Room` and the
// daemon's own id for it (`perm_id`, or the id without its tag) in the path.
//
// NOT HERE, ON PURPOSE: "always" and "never", and editing the command before approving. Both need the scope
// picker the board draws under its row, and a rule made by a thumb on a small screen without that scope in
// front of it is a rule wider than anyone agreed to. The phone answers one request at a time.
//
// Rows are kept by request id, so a reason someone is typing survives every update. A row answered somewhere
// else leaves with a short fade. Everything is set with textContent: a command is model output.
(function () {
  "use strict";

  let cur = null;

  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function reduced() {
    return !!(window.matchMedia && matchMedia("(prefers-reduced-motion: reduce)").matches);
  }

  function ago(iso) {
    const s = Math.max(0, Math.round((Date.now() - (Date.parse(iso) || Date.now())) / 1000));
    if (s < 60) return "just now";
    if (s < 3600) return Math.floor(s / 60) + " min";
    if (s < 86400) return Math.floor(s / 3600) + " h";
    return Math.floor(s / 86400) + " d";
  }

  function titleOf(id) {
    const c = window.mStore && window.mStore.card(id);
    return (c && (c.display_title || c.title || c.alias)) || "";
  }

  // What is changing, line by line, so an added line and a removed one read differently.
  function diffBlock(text) {
    const pre = el("pre", "mp-diff");
    for (const line of String(text).split("\n")) {
      const k = line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : "";
      pre.appendChild(el("span", "mp-dl" + (k ? " " + k : ""), line + "\n"));
    }
    return pre;
  }

  // Through `mNet.api`, which adds the room the page is scoped to. A request seen in the all-rooms view has a tagged
  // id (`room~id`) and a `room`, and the hub routes a permission path only by the `X-Atrium-Room` header, so the
  // room and the bare id are split off the tag and the room goes in that header.
  async function decide(p, decision, reason) {
    const net = window.mNet;
    const room = p.room || net.roomOf(p.id) || "";
    const perm = p.perm_id || net.bareId(p.id);
    await net.api("/v1/permissions/" + encodeURIComponent(perm) + "/decide", {
      method: "POST", headers: room ? { "X-Atrium-Room": room } : {},
      body: JSON.stringify({ decision, reason: reason || "", forever: false, prefix: "", kind: "command", command: "" })
    });
  }

  function build(p, showCard) {
    const row = el("article", "mp-row");
    row.dataset.id = p.id;

    const head = el("div", "mp-head");
    const tool = el("span", "mp-tool", p.tool || "tool");
    const meta = el("span", "mp-meta");
    const bits = [];
    if (showCard) bits.push(titleOf(p.task_id) || p.agent || "");
    else if (p.agent) bits.push(p.agent);
    if (p.room) bits.push("on " + p.room);
    bits.push(ago(p.requested_at));
    meta.textContent = bits.filter(Boolean).join(" · ");
    head.append(tool, meta);

    const cmd = el("pre", "mp-cmd");
    cmd.appendChild(el("code", "", p.command || ""));
    row.append(head, cmd);

    if (p.details) {
      const d = el("details", "mp-changes");
      d.appendChild(el("summary", "", "what changes"));
      d.appendChild(diffBlock(p.details));
      row.appendChild(d);
    }

    const acts = el("div", "mp-acts");
    const yes = el("button", "mp-btn approve", "Approve");
    const no = el("button", "mp-btn deny", "Deny");
    const why = el("button", "mp-btn why", "Deny with a reason");
    for (const b of [yes, no, why]) b.type = "button";
    acts.append(yes, no, why);

    const form = el("div", "mp-reason");
    form.hidden = true;
    const ta = el("textarea", "mp-reason-box");
    ta.rows = 2;
    ta.placeholder = "Say what to do instead";
    ta.setAttribute("aria-label", "reason for denying");
    const go = el("button", "mp-btn deny go", "Send denial");
    go.type = "button";
    go.disabled = true;
    form.append(ta, go);

    const err = el("div", "mp-err");
    err.setAttribute("role", "status");
    row.append(acts, form, err);

    let busy = false;
    const lock = on => {
      busy = on;
      row.classList.toggle("busy", on);
      row.querySelectorAll("button").forEach(b => { b.disabled = on || (b === go && !ta.value.trim()); });
    };
    const answer = async (decision, reason) => {
      if (busy) return;
      err.textContent = "";
      lock(true);
      try {
        await decide(p, decision, reason);
        // It leaves when the store drops it, which the `perms` event this answer raises does. Held off the
        // buttons meanwhile so it cannot be answered twice.
        row.classList.add("answered");
      } catch (e) {
        // Most often answered somewhere else a moment ago. The store's next update removes the row, and until
        // then it says so.
        err.textContent = "Not sent: " + (e && e.message ? e.message : e);
        lock(false);
      }
    };
    yes.addEventListener("click", () => answer("approve"));
    no.addEventListener("click", () => answer("block"));
    why.addEventListener("click", () => {
      form.hidden = !form.hidden;
      why.setAttribute("aria-expanded", String(!form.hidden));
      if (!form.hidden) ta.focus();
    });
    ta.addEventListener("input", () => { go.disabled = busy || !ta.value.trim(); });
    go.addEventListener("click", () => answer("block", ta.value.trim()));
    why.setAttribute("aria-expanded", "false");
    return row;
  }

  function leave(row) {
    if (row.classList.contains("leaving")) return;
    row.classList.add("leaving");
    const done = () => row.remove();
    if (reduced()) return done();
    row.style.height = row.offsetHeight + "px";
    // Forces the height to register before it collapses.
    void row.offsetHeight;
    row.classList.add("gone");
    setTimeout(done, 260);
  }

  function mount(host, cardId) {
    unmount();
    const root = el("div", "mp");
    host.replaceChildren(root);
    const all = cardId == null;
    const state = { host, root, offs: [] };
    cur = state;

    const empty = el("div", "mp-empty", "Nothing is waiting on you.");
    empty.hidden = true;

    const paint = () => {
      const list = ((window.mStore && window.mStore.perms()) || [])
        .filter(p => all || p.task_id === cardId)
        .sort((a, b) => (Date.parse(a.requested_at) || 0) - (Date.parse(b.requested_at) || 0));
      const want = new Set(list.map(p => p.id));
      root.querySelectorAll(".mp-row").forEach(r => { if (!want.has(r.dataset.id)) leave(r); });
      const have = new Set([...root.querySelectorAll(".mp-row:not(.leaving)")].map(r => r.dataset.id));
      for (const p of list) if (!have.has(p.id)) root.appendChild(build(p, all));
      // The empty line belongs to the all-cards view. On one card no rows means no section at all.
      empty.hidden = !all || list.length > 0;
      if (all && !empty.parentNode) root.appendChild(empty);
      host.classList.toggle("has-perms", list.length > 0);
    };
    if (window.mStore) state.offs.push(window.mStore.on("perms", paint));
    paint();
  }

  function unmount() {
    if (!cur) return;
    cur.offs.forEach(f => { try { f(); } catch (e) {} });
    cur.host.classList.remove("has-perms");
    cur.host.replaceChildren();
    cur = null;
  }

  window.mPerms = { mount, unmount };
})();
