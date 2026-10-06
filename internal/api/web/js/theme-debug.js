// A DEBUG TOOL for card colours: the "colours" block in the terminal details drawer's debug section, and a console
// trace behind `localStorage["atrium.debug.theme"] = "1"`. It reads the theme code and changes nothing in it.
// See js/peek-debug.js, js/themes.js.

let themeDbgOn = false;
try { themeDbgOn = localStorage.getItem("atrium.debug.theme") === "1"; } catch (e) {}
window.addEventListener("storage", e => { if (e.key === "atrium.debug.theme") themeDbgOn = e.newValue === "1"; });

function themeDbgName(th) {
  if (!th) return "plain";
  for (const [k, v] of Object.entries(TERM_THEMES)) if (v === th) return k;
  return "?";
}

function themeDbgSwatch(th) {
  return th ? `bg ${th.background} fg ${th.foreground} cursor ${th.cursor || "-"}` : "-";
}

// The whole block as text: one line per fact, so it pastes into a message as it is.
function themeDbgText(t) {
  if (!t) return "no card";
  const out = [];
  const add = (k, v) => out.push(k.padEnd(16) + v);
  const key = repoKeyOf(t), cc = repoColors.get();
  const cold = termCold(t);
  add("card", t.id + " " + (t.display_title || ""));
  add("card theme", JSON.stringify(t.theme || ""));
  add("repo key", JSON.stringify(key));
  add("repos[key]", JSON.stringify((key && cc.repos[key]) || ""));
  add("default", JSON.stringify(cc.default || ""));
  add("cardColors", String(cardColors));
  const src = themeSource(t), th = themeFor(t);
  add("themeSource", themeDbgName(src) + "  " + themeDbgSwatch(src));
  add("themeFor", themeDbgName(th) + "  " + themeDbgSwatch(th));
  add("cold", String(cold));
  for (const on of [false, true]) {
    const w = termWear(t, on, cold);
    add("wear " + (on ? "selected" : "unselected"), JSON.stringify(w.cls) + " " + (w.style || "(no style)"));
  }
  const row = document.querySelector(`#term-list .card.tab[data-id="${t.id}"]`);
  if (row) {
    const cs = getComputedStyle(row);
    add("row classes", row.className);
    add("row computed", `background ${cs.backgroundColor}; color ${cs.color}; border ${cs.borderTopColor} ${cs.borderTopWidth}`);
    add("row vars", `--tabbg ${cs.getPropertyValue("--tabbg").trim() || "-"}; --tabc ${cs.getPropertyValue("--tabc").trim() || "-"}`);
    add("row style attr", row.getAttribute("style") || "");
  } else add("row", "not in the list");
  const attached = termTask && termTask.id === t.id;
  const br = document.querySelector("#term-layout .tab-bridge, #term-layout [class*=bridge]");
  if (attached && br) {
    const cs = getComputedStyle(br);
    add("bridge", `${br.className}; background ${cs.backgroundColor}; hidden ${br.hidden}; style ${br.getAttribute("style") || ""}`);
  } else add("bridge", attached ? "none found" : "not attached");
  if (attached && term) {
    const x = term.options.theme || {};
    add("xterm theme", `bg ${x.background} fg ${x.foreground} cursor ${x.cursor}`);
    const pane = document.getElementById("term-pane");
    if (pane) add("pane --term-bg", getComputedStyle(pane).getPropertyValue("--term-bg").trim() || "-");
  } else add("xterm theme", "not attached");
  add("skin", (typeof currentSkin === "function" && currentSkin()) || document.documentElement.getAttribute("data-skin") || "default");
  return out.join("\n");
}

function themeDbgPaint() {
  const pre = document.getElementById("t-dbg-colours");
  if (!pre) return;
  const cp = document.getElementById("t-dbg-colours-copy");
  if (cp && !cp.firstChild) cp.innerHTML = copyIcon();
  const sel = window.getSelection && window.getSelection();
  if (sel && !sel.isCollapsed && pre.contains(sel.anchorNode)) return;
  try { pre.textContent = themeDbgText(termTask); } catch (e) { pre.textContent = "colours: " + e.message; }
}

async function themeDbgCopy(btn) {
  const pre = document.getElementById("t-dbg-colours");
  try { await navigator.clipboard.writeText(pre ? pre.textContent : ""); btn.dataset.copied = "1"; } catch (e) { btn.dataset.copied = "0"; }
  setTimeout(() => { delete btn.dataset.copied; }, 1200);
}

// One line per attach, detach and repaint of the attached row. A no-op while the switch is off.
function themeTrace(what, t) {
  if (!themeDbgOn || !t) return;
  try {
    const th = themeFor(t), on = termTask && termTask.id === t.id;
    const w = termWear(t, !!on, termCold(t));
    console.log("[theme] " + what, t.id, "theme=" + themeDbgName(th), "tabbg=" + th.background,
      "wear=" + (w.style || "-"), "xterm=" + (term && term.options.theme ? term.options.theme.background : "-"));
  } catch (e) { console.log("[theme] " + what, e.message); }
}
