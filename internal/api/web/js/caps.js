// The capabilities view: a room-by-capability matrix, drawn from the hub's `/_hub/capabilities`. Hub only.
//
// The hub keeps the newest result of every probe and this file decides nothing. A cell is pass, fail, unknown or
// running. A failure on a capability marked optional, or not expected on that room, is shown neutral ("n/a") and counts
// as green for its group, so a group of only pass and n/a starts collapsed to one summary row. Any other group starts open.

const caps = { data: null, loading: false, open: {}, kind: "", result: "", find: "", picked: null, timer: 0, rerunNote: "" };

function capsStates(c, room, r, running) {
  if (running) return "run";
  const s = r ? r.state : "unknown";
  const off = c.optional || (c.not_expected || []).indexOf(room) >= 0;
  return s !== "pass" && off ? "na" : s;
}

function capsAgo(iso) {
  const t = iso ? Date.parse(iso) : NaN;
  if (isNaN(t)) return "never";
  const m = Math.max(0, Math.round((Date.now() - t) / 60000));
  return m < 1 ? "just now" : m < 60 ? m + " min ago" : m < 2880 ? Math.round(m / 60) + " h ago" : Math.round(m / 1440) + " d ago";
}

// The cells of the matrix, by capability then room, with each cell's drawn state.
function capsCells(d) {
  const got = {};
  for (const r of d.results || []) got[r.room.toLowerCase() + "\n" + r.cap] = r;
  const run = {};
  for (const rm of d.rooms || []) for (const id of rm.running || []) run[rm.name.toLowerCase() + "\n" + id] = true;
  return (d.caps || []).map(c => ({
    cap: c,
    cells: (d.rooms || []).map(rm => {
      const k = rm.name.toLowerCase() + "\n" + c.id, r = got[k];
      return { room: rm, row: r || null, state: capsStates(c, rm.name, r, !!run[k]) };
    })
  }));
}

// A group is green when every cell is pass or n/a. A cell still running or never read keeps it open.
function capsGroupGreen(rows) {
  return rows.every(x => x.cells.every(c => c.state === "pass" || c.state === "na"));
}

function capsGroupIsOpen(kind, rows) {
  return kind in caps.open ? caps.open[kind] : !capsGroupGreen(rows);
}

// What to do about a red cell: the capability's own fix when the list gives one, else one by its kind.
function capsFix(cap, room) {
  if (cap && cap.fix) return cap.fix + ", then press Re-run.";
  const id = cap ? cap.id : "it", on = room ? " on " + room : "";
  const k = cap && cap.kind;
  if (k === "toolchain") return "Install " + id + on + " and put it on the PATH the room starts with, then press Re-run.";
  if (k === "reach") return "Check that " + (room || "the room") + " can reach " + id.replace(/^\w+:/, "") + " (network, ssh or ziti config), then press Re-run.";
  return "Fix what the probe's reason names" + on + ", then press Re-run.";
}

function capsPill(state, stale) {
  const w = { pass: "pass", fail: "fail", unknown: "unknown", run: "running", na: "n/a" }[state] || state;
  return '<span class="cap-pill ' + state + (stale ? " stale" : "") + '"><i></i>' + w + "</span>";
}

function capsEsc(s) {
  return String(s == null ? "" : s).replace(/[&<>"]/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
}

function capsFilter(rows) {
  return rows.filter(x => {
    if (caps.kind && x.cap.kind !== caps.kind) return false;
    if (caps.find && x.cap.id.toLowerCase().indexOf(caps.find.toLowerCase()) < 0) return false;
    if (caps.result && !x.cells.some(c => c.state === caps.result)) return false;
    return true;
  });
}

function capsPaint() {
  const box = document.getElementById("caps-matrix");
  if (!box || !caps.data) return;
  const d = caps.data, all = capsCells(d), rows = capsFilter(all);
  const n = { pass: 0, fail: 0, unknown: 0, na: 0, run: 0 };
  rows.forEach(x => x.cells.forEach(c => n[c.state]++));
  document.getElementById("caps-sum").innerHTML =
    "<span>" + rows.length + " capabilities x " + (d.rooms || []).length + " rooms</span>" +
    ["pass", "fail", "unknown", "na", "run"].filter(k => n[k]).map(k => "<span>" + capsPill(k) + " " + n[k] + "</span>").join("");
  let h = '<thead><tr><th>capability <span class="cap-q" data-tip="Name is kind:target. A cell is the newest stored result for that room, and its age is when the probe last ran.">?</span></th>';
  for (const rm of d.rooms || []) {
    h += '<th><div class="cap-rh"><span class="cap-rn">' + capsEsc(rm.name) + '</span><span class="cap-rs' + (rm.online ? "" : " off") + '">' +
      (rm.online ? "online, read " + capsAgo(rm.checked) : "offline") + '</span><button type="button" class="cap-mini" data-room="' +
      capsEsc(rm.name) + '"' + (rm.online ? "" : " disabled") + ">Re-run</button></div></th>";
  }
  h += "</tr></thead><tbody>";
  const span = (d.rooms || []).length + 1;
  for (const kind of ["reach", "toolchain", "build", "access"]) {
    const grp = rows.filter(x => x.cap.kind === kind);
    if (!grp.length) continue;
    const open = capsGroupIsOpen(kind, grp);
    h += '<tr class="cap-kind" data-kind="' + kind + '" aria-expanded="' + open + '"><th colspan="' + span + '"><span class="cap-chev">' +
      (open ? "&#9662;" : "&#9656;") + "</span> " + kind + ' <span class="cap-count">' + grp.length + "</span>";
    if (!open) {
      h += '<span class="cap-roll">' + (d.rooms || []).map(rm => {
        const cs = grp.map(x => x.cells.find(c => c.room.name === rm.name));
        const p = cs.filter(c => c.state === "pass").length, na = cs.filter(c => c.state === "na").length;
        return capsEsc(rm.name) + " " + p + " pass" + (na ? ", " + na + " n/a" : "");
      }).join("  &middot;  ") + "</span>";
    }
    h += "</th></tr>";
    if (!open) continue;
    for (const x of grp) {
      h += '<tr><td class="cap-name">' + capsEsc(x.cap.id) + "</td>";
      for (const c of x.cells) {
        h += '<td class="cap-cell' + (c.room.online ? "" : " offline") + '" data-room="' + capsEsc(c.room.name) + '" data-cap="' + capsEsc(x.cap.id) + '"' +
          (c.state === "fail" ? ' data-tip="' + capsEsc(capsFix(x.cap, c.room.name)) + '"' : "") + ">" +
          capsPill(c.state, !c.room.online) + (c.row && c.row.version ? '<span class="cap-ver">' + capsEsc(c.row.version) + "</span>" : "") +
          '<span class="cap-when">' + (c.state === "run" ? "probing now" : c.row ? capsAgo(c.row.at) : "never") + "</span></td>";
      }
      h += "</tr>";
    }
  }
  box.innerHTML = h + "</tbody>";
  capsDetail();
  clearTimeout(caps.timer);
  if (n.run) caps.timer = setTimeout(loadCaps, 3000);
}

function capsDetail() {
  const el = document.getElementById("caps-detail");
  if (!el) return;
  const p = caps.picked;
  const hit = p && (caps.data.results || []).find(r => r.room === p.room && r.cap === p.cap);
  if (!p) { el.hidden = true; return; }
  el.hidden = false;
  el.innerHTML = "<h3>" + capsEsc(p.cap) + " on " + capsEsc(p.room) + "</h3><dl>" +
    "<dt>result</dt><dd>" + capsEsc(hit ? hit.state + (hit.version ? " " + hit.version : "") : "unknown") + "</dd>" +
    '<dt>reason <span class="cap-q" data-tip="The short reason the probe stored, cut to 200 characters and scrubbed of anything token shaped.">?</span></dt><dd>' +
    capsEsc(hit ? hit.reason : "never run") + "</dd><dt>checked</dt><dd>" + capsAgo(hit && hit.at) + "</dd>" +
    (hit && hit.state === "fail" ? '<dt>to fix</dt><dd class="cap-fix">' +
      capsEsc(capsFix((caps.data.caps || []).find(c => c.id === p.cap), p.room)) + "</dd>" : "") + "</dl>";
}

async function loadCaps() {
  if (caps.loading) return;
  caps.loading = true;
  try {
    const res = await fetch("/_hub/capabilities");
    if (res.ok) caps.data = await res.json();
    document.getElementById("caps-note").textContent = res.ok ? caps.rerunNote : "The hub is not answering for capabilities.";
  } catch (e) {
    document.getElementById("caps-note").textContent = "The hub is not answering for capabilities.";
  }
  caps.loading = false;
  capsPaint();
}

async function capsRerun(room) {
  caps.rerunNote = "";
  try {
    const res = await fetch("/_hub/capabilities?room=" + encodeURIComponent(room), { method: "POST" });
    if (!res.ok) caps.rerunNote = capsRerunFailed(res.status, await res.json().catch(() => ({})));
  } catch (e) {
    caps.rerunNote = "The re-run was not sent: the hub is not answering.";
  }
  loadCaps();
}

function capsRerunFailed(status, body) {
  const why = Object.entries(body.unreached || {}).map(([name, msg]) => {
    let m = msg;
    try { m = JSON.parse(msg).error || msg; } catch (e) { /* the room answered plain text */ }
    return name + ": " + m;
  });
  if (status === 409) return "Not re-run: " + Object.keys(body.unreached || {}).join(", ") + " is already probing. Results land as each probe finishes.";
  return "Not re-run. " + (why.length ? why.join(". ") : body.error || "The hub answered " + status + ".");
}

function paintCapsTab() {
  const tab = document.querySelector('.tab[data-view="caps"]');
  if (tab) tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
}

(function capsWire() {
  const root = document.getElementById("caps");
  if (!root) return;
  root.addEventListener("click", e => {
    const k = e.target.closest("tr.cap-kind");
    if (k) {
      const kind = k.dataset.kind;
      caps.open[kind] = k.getAttribute("aria-expanded") !== "true";
      capsPaint();
      return;
    }
    const b = e.target.closest("button[data-room]");
    if (b) { capsRerun(b.dataset.room); return; }
    if (e.target.id === "caps-all") { capsRerun("all"); return; }
    const td = e.target.closest("td.cap-cell");
    if (td) { caps.picked = { room: td.dataset.room, cap: td.dataset.cap }; capsDetail(); }
  });
  for (const [id, key] of [["caps-kind", "kind"], ["caps-result", "result"], ["caps-find", "find"]]) {
    const el = document.getElementById(id);
    if (el) el.addEventListener("input", () => { caps[key] = el.value.trim(); capsPaint(); });
  }
})();
