// ── the look page ───────────────────────────────────────
//
// One settings page for how a card looks, the one place for themes and colours. Nothing here is a draft: a choice is applied as it is made, and the
// preview at the top shows whatever is being edited.
//
// Where each choice lives: a theme override for a repo is the daemon's card colours, for a card it is the card's own
// theme, and the default is the card colours default. A FONT is taste and stays in this browser (js/terminal-links.js).

let lookFocus = { kind: "default", key: "" };
let lookTry = "";
let lookEdit = null;
let lookPending = [];
let lookFilter = "";

const lookEl = (id) => document.getElementById(id);
const lookTasks = () => (typeof lastTasks !== "undefined" ? lastTasks : []);
const lookTask = (id) => lookTasks().find(t => t.id === id) || null;
const lookCardName = (id) => { const t = lookTask(id); return t ? (t.display_title || id) : id; };

// The repos and cards that have an override, and those added a moment ago and not given a choice yet.
function lookRows() {
  const seen = new Set(), out = [];
  const add = (kind, key) => { const k = kind + " " + key; if (!seen.has(k)) { seen.add(k); out.push({ kind, key }); } };
  const repos = new Set(Object.keys(repoColors.get().repos).concat(Object.keys(readTermFontMap(TERM_FONT_REPOS_KEY))));
  Array.from(repos).sort().forEach(k => add("repo", k));
  const cards = new Set(Object.keys(readTermFontMap(TERM_FONT_CARDS_KEY)));
  lookTasks().forEach(t => { if (t.theme) cards.add(t.id); });
  Array.from(cards).sort((a, b) => lookCardName(a).localeCompare(lookCardName(b))).forEach(k => add("card", k));
  lookPending.forEach(p => add(p.kind, p.key));
  return out;
}

const lookThemeOf = (kind, key) => kind === "repo" ? (repoColors.get().repos[key] || "")
  : kind === "card" ? ((lookTask(key) || {}).theme || "") : repoColors.get().default;
const lookFontOf = (kind, key) => kind === "default" ? readTermFontFamily()
  : (readTermFontMap(kind === "card" ? TERM_FONT_CARDS_KEY : TERM_FONT_REPOS_KEY)[key] || "");

// What the preview wears: the theme and font the focused row ends up with, with the tile or the editor over the theme.
function lookShown() {
  const { kind, key } = lookFocus;
  const def = repoColors.get();
  let theme = def.default, font = readTermFontFamily(), task = null, who = "the default";
  if (kind === "repo") {
    theme = def.repos[key] || theme;
    font = lookFontOf("repo", key) || font;
    task = lookTasks().find(t => repoKeyOf(t) === key) || null;
    who = key;
  } else if (kind === "card") {
    const t = lookTask(key);
    task = t;
    who = lookCardName(key);
    theme = (t && (t.theme || def.repos[repoKeyOf(t)])) || theme;
    font = termFontFor(t || { id: key });
  }
  if (lookTry) theme = lookTry;
  return { theme, font, task, who, kind };
}

function lookFontOptions(cur, none) {
  const opt = f => `<option value="${esc(f.name)}" style="font-family:${esc(termFontStack(f.name))}"` +
    `${f.name === cur ? " selected" : ""}>${esc(f.name)}</option>`;
  const group = (label, prop) => {
    const list = TERM_FONT_FACES.filter(f => !!f.prop === prop && (termFontInstalled(f) || f.name === cur));
    return list.length ? `<optgroup label="${label}">${list.map(opt).join("")}</optgroup>` : "";
  };
  return `<option value=""${cur ? "" : " selected"}>${esc(none)}</option>` + group("monospace", false) +
    group("proportional", true);
}

function lookPaintPreview() {
  const host = lookEl("look-pv");
  const s = lookShown();
  const palette = lookEdit ? lookDraftPalette() : themeNamed(s.theme);
  const who = s.kind === "default" ? "the default" : s.who;
  host.innerHTML = `<div class="look-cap"><span>previewing <b>${esc(lookEdit ? lookEdit.name || "a new theme" : s.theme || "plain card")}</b>` +
    ` in <b>${esc(s.font || "the board font")}</b> for <b>${esc(who)}</b></span>` +
    (lookTry ? `<span><button type="button" class="go" data-look="try-use">use for ${esc(who)}</button> ` +
      `<button type="button" data-look="try-stop">stop previewing</button></span>` : "") +
    `</div><div class="tshow" id="look-show"></div>`;
  const task = Object.assign({}, s.task || SHOWCASE_TASK, s.task ? {} : { display_title: "a card with this look" });
  paintShowcase(lookEl("look-show"), palette, task);
  const term = host.querySelector(".tshow-term");
  if (term) term.style.fontFamily = termFontStack(s.font);
}

function lookPaintDefault() {
  const d = repoColors.get().default, f = readTermFontFamily();
  lookEl("look-def").innerHTML =
    `<select data-look="d-theme" aria-label="default theme">${themeOptionsWith(d, "plain card")}</select>` +
    `<select data-look="d-font" aria-label="default font">${lookFontOptions(f, "board font")}</select>` +
    `<button type="button" data-look="d-edit">edit theme</button>` +
    (lookFocus.kind === "default" && !lookTry ? "" : `<button type="button" data-look="d-show">preview</button>`);
}

function lookPaintOverrides() {
  const rows = lookRows();
  lookEl("look-ovr").innerHTML = rows.length ? rows.map(r => {
    const hot = lookFocus.kind === r.kind && lookFocus.key === r.key;
    return `<div class="look-row look-ovr${hot ? " hot" : ""}" data-kind="${r.kind}" data-key="${esc(r.key)}">` +
      `<span class="look-who"><small>${r.kind}</small>${esc(r.kind === "card" ? lookCardName(r.key) : r.key)}</span>` +
      `<select data-look="o-theme" aria-label="theme">${themeOptionsWith(lookThemeOf(r.kind, r.key), "same as default")}</select>` +
      `<select data-look="o-font" aria-label="font">${lookFontOptions(lookFontOf(r.kind, r.key), "same as default")}</select>` +
      `<button type="button" data-look="o-show">preview</button>` +
      `<button type="button" data-look="o-drop">remove</button></div>`;
  }).join("") : `<div class="look-empty">No overrides. Every card wears the default.</div>`;
}

// Searchable: the repos and cards atrium knows that have no row yet.
function lookPaintAddList() {
  const list = lookEl("look-addlist"), q = lookEl("look-add").value.trim().toLowerCase();
  const taken = new Set(lookRows().map(r => r.kind + " " + r.key));
  const repos = new Set();
  lookTasks().forEach(t => { const k = repoKeyOf(t); if (k) repos.add(k); });
  const hit = (s) => !q || s.toLowerCase().includes(q);
  const rep = Array.from(repos).sort().filter(k => !taken.has("repo " + k) && hit(k));
  const cards = lookTasks().filter(t => !taken.has("card " + t.id) && hit((t.display_title || "") + " " + repoKeyOf(t)))
    .slice(0, 40);
  const item = (kind, key, name, sub) => `<div class="look-item" data-look="o-add" data-kind="${kind}" data-key="${esc(key)}">` +
    `${esc(name)}<small>${esc(sub)}</small></div>`;
  list.innerHTML = (rep.length ? `<div class="look-head">repos</div>` + rep.map(k => item("repo", k, k, "")).join("") : "") +
    (cards.length ? `<div class="look-head">cards</div>` +
      cards.map(t => item("card", t.id, t.display_title || t.id, repoKeyOf(t))).join("") : "") +
    (rep.length || cards.length ? "" : `<div class="look-head">nothing matches</div>`);
}

function lookPaintGallery() {
  const names = Object.keys(allThemes()).sort().filter(n => n.includes(lookFilter.toLowerCase()));
  const def = repoColors.get().default;
  lookEl("look-gal").innerHTML = names.map(n => {
    const t = allThemes()[n], c = (k, d) => okColour(t[k], d);
    return `<div class="look-tile${lookTry === n ? " on" : ""}" data-look="tile" data-name="${esc(n)}">` +
      `<div class="look-sw" style="background:${c("background", "#000000")};color:${c("foreground", "#cccccc")}">` +
      `<span style="color:${c("green", "#88cc88")}">$</span> ls <span style="color:${c("blue", "#8888cc")}">src</span><br>` +
      `<span style="color:${c("red", "#cc8888")}">err</span> <span style="color:${c("yellow", "#cccc88")}">warn</span></div>` +
      `<div class="look-nm"><span>${esc(n)}</span><span class="look-use">${n === def ? "default" : ""}</span></div>` +
      `<div class="look-acts"><button type="button" data-look="t-edit" data-name="${esc(n)}">edit</button>` +
      `<button type="button" data-look="t-dup" data-name="${esc(n)}">copy</button>` +
      `<button type="button" data-look="t-export" data-name="${esc(n)}">export</button></div></div>`;
  }).join("");
}

// The editor, drawn from the daemon's slots with the colour boxes the old editor uses.
function lookPaintEdit() {
  const host = lookEl("look-edit");
  if (!lookEdit) { host.innerHTML = ""; return; }
  const slots = THEME_SLOTS.length ? THEME_SLOTS
    : Object.keys(TERM_THEMES.atrium).map(n => ({ name: n, required: false }));
  const brought = lookEdit.from in BROUGHT_THEMES;
  host.innerHTML = `<div class="look-editor"><div class="look-row">` +
    `<input type="text" id="look-name" value="${esc(lookEdit.name)}" aria-label="theme name" spellcheck="false" placeholder="name">` +
    `<button type="button" class="go" data-look="e-save">save</button>` +
    `<button type="button" data-look="e-revert">revert</button>` +
    (brought ? `<button type="button" data-look="e-delete">delete</button>` : "") +
    `<button type="button" data-look="e-close">close</button></div><div class="thgrid">` +
    slots.map(s => {
      const v = String(lookEdit.draft[s.name] || "");
      return `<div class="thslot"><label>${esc(s.name)}${s.required ? "" : " ?"}</label>` +
        `<input type="color" data-slot="${esc(s.name)}" value="${okColour(v, s.name === "background" ? "#000000" : "#cccccc")}">` +
        `<input type="text" data-slot="${esc(s.name)}" maxlength="7" spellcheck="false" value="${esc(v)}"` +
        ` placeholder="${s.required ? "#1a2b3c" : "optional"}"></div>`;
    }).join("") + `</div></div>`;
}

function lookDraftPalette() {
  const p = {};
  for (const k in lookEdit.draft) { const c = okColour(lookEdit.draft[k], ""); if (c) p[k] = c; }
  return p.background && p.foreground ? p : null;
}

function lookPaint() {
  if (!lookEl("look-host")) return;
  lookPaintPreview();
  lookPaintDefault();
  lookPaintOverrides();
  lookPaintGallery();
  if (!lookEdit || !lookEl("look-name")) lookPaintEdit();
}

async function lookSetTheme(kind, key, name) {
  try {
    if (kind === "card") {
      await patchTask(key, { theme: name });
      const t = lookTask(key);
      if (t) t.theme = name;
      const card = typeof cardRows !== "undefined" && cardRows.get ? cardRows.get(key) : null;
      if (card) card.theme = name;
      if (typeof runRefresh === "function") runRefresh();
    } else {
      const next = repoColors.get();
      if (kind === "default") next.default = name;
      else if (name) next.repos[key] = name;
      else delete next.repos[key];
      await repoColors.set(next);
    }
  } catch (e) {
    toast("that did not stick", e.message);
  }
  lookTry = "";
  lookPaint();
}

async function lookSetFont(kind, key, name) {
  if (kind === "default") await setTermFontFamily(name);
  else await setTermFontOverride(kind, key, name);
  lookPaint();
}

function lookFocusOn(kind, key) { lookFocus = { kind, key }; lookTry = ""; }

function lookAdd(kind, key) {
  if (!lookRows().some(r => r.kind === kind && r.key === key)) lookPending.push({ kind, key });
  lookFocusOn(kind, key);
  lookEl("look-add").value = "";
  lookEl("look-addlist").hidden = true;
  lookPaint();
}

async function lookDrop(kind, key) {
  lookPending = lookPending.filter(p => !(p.kind === kind && p.key === key));
  if (lookThemeOf(kind, key)) await lookSetTheme(kind, key, "");
  if (lookFontOf(kind, key)) await setTermFontOverride(kind, key, "");
  if (lookFocus.kind === kind && lookFocus.key === key) lookFocusOn("default", "");
  lookPaint();
}

function lookOpenEdit(name) {
  const p = themeNamed(name) || {};
  lookEdit = { from: themeNamed(name) ? name : "", name: name || "", orig: Object.assign({}, p), draft: Object.assign({}, p) };
  lookTry = "";
  lookPaintEdit();
  lookPaintPreview();
  const box = lookEl("look-edit");
  if (box.scrollIntoView) box.scrollIntoView({ block: "nearest" });
}

// From atrium's own colours, so it is legible from the first change.
function lookNewTheme() {
  let n = "new-theme";
  while (themeNamed(n)) n += "-2";
  lookOpenEdit("");
  lookEdit.name = n;
  lookEdit.draft = Object.assign({}, TERM_THEMES.atrium);
  lookPaintEdit();
  lookPaintPreview();
}

async function lookSaveEdit() {
  const name = String(lookEl("look-name").value || "").trim().toLowerCase();
  if (!name) { toast("give it a name first", "that is how a card asks for it"); return; }
  const body = {};
  for (const k in lookEdit.draft) if (lookEdit.draft[k]) body[k] = lookEdit.draft[k];
  let saved;
  try {
    saved = await api("/v1/themes/" + encodeURIComponent(name), {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body)
    });
  } catch (e) { toast("that did not save", e.message); return; }
  await loadThemes();
  lookEdit = Object.assign(lookEdit, { from: saved.name, name: saved.name, orig: Object.assign({}, themeNamed(saved.name)) });
  lookPaint();
  lookPaintEdit();
  toast("theme saved", saved.name);
}

async function lookDeleteEdit() {
  const name = lookEdit.from;
  const sure = await askUser({
    title: "delete " + name,
    body: (name in TERM_THEMES)
      ? "Yours goes and atrium's own " + name + " comes back. Cards set to it keep the name."
      : "Cards set to it keep the name and fall back to their project's colour.",
    buttons: [{ label: "keep it", value: null }, { label: "delete", value: true, style: "no" }]
  });
  if (!sure) return;
  try { await api("/v1/themes/" + encodeURIComponent(name), { method: "DELETE" }); }
  catch (e) { toast("that did not delete", e.message); return; }
  await loadThemes();
  lookEdit = null;
  lookPaint();
  toast("theme deleted", name);
}

async function lookDuplicate(name) {
  let n = name + "-copy";
  while (themeNamed(n)) n += "-2";
  try {
    await api("/v1/themes/" + encodeURIComponent(n), {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(themeNamed(name))
    });
  } catch (e) { toast("that did not copy", e.message); return; }
  await loadThemes();
  lookTry = n;
  lookPaint();
  toast("copied", n);
}

// A Windows Terminal scheme, the one shape the daemon's import reads back: its own names for the cursor and for magenta.
// WT has no selection foreground, so that slot does not travel.
const LOOK_WT_NAMES = { cursor: "cursorColor", magenta: "purple", brightMagenta: "brightPurple" };
function themeExportScheme(name) {
  const p = themeNamed(name) || {};
  const out = { name };
  for (const k of Object.keys(p)) if (k !== "selectionForeground" && p[k]) out[LOOK_WT_NAMES[k] || k] = p[k];
  return out;
}

function lookExport(name) {
  const blob = new Blob([JSON.stringify(themeExportScheme(name), null, 2) + "\n"], { type: "application/json" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name + ".json";
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  toast("exported", name + ".json");
}

async function lookImport(text) {
  if (!String(text || "").trim()) { toast("nothing to import", "paste a theme, or pick the file"); return; }
  let out;
  try {
    out = await api("/v1/themes/import", { method: "POST", headers: { "Content-Type": "application/json" }, body: text });
  } catch (e) { toast("that did not import", e.message); return; }
  await loadThemes();
  const got = out.imported || [], kept = out.kept || [];
  if (!got.length) {
    toast("nothing was brought in", kept.map(k => k.name + ": " + k.why).join(". ") || "no scheme in that was usable");
    return;
  }
  lookEl("look-imptext").value = "";
  lookEl("look-imp").hidden = true;
  lookTry = got[0];
  lookPaint();
  toast(got.length + " theme" + (got.length === 1 ? "" : "s") + " brought in",
    got.slice(0, 4).join(", ") + (got.length > 4 ? " and more" : "") + (kept.length ? ". " + kept.length + " skipped" : ""));
}

// Opened from a card's menu: the page on that card's own override, whether it has one yet or not.
function openLookFor(id) {
  if (!lookRows().some(r => r.kind === "card" && r.key === id)) lookPending.push({ kind: "card", key: id });
  lookFocusOn("card", id);
  if (!settingsDlg.open) document.getElementById("gear").onclick();
  showPane(settingsBody(), "look", SETTINGS_PANE);
  lookPaint();
  const row = Array.from(document.querySelectorAll("#look-ovr .look-ovr")).find(r => r.dataset.key === id);
  if (row && row.scrollIntoView) row.scrollIntoView({ block: "center" });
}

(() => {
  const host = lookEl("look-host");
  if (!host) return;
  const rowOf = (el) => el.closest(".look-ovr");
  host.addEventListener("change", (e) => {
    const el = e.target, act = el.dataset && el.dataset.look;
    if (el.id === "look-file") {
      const file = el.files && el.files[0];
      el.value = "";
      if (!file) return;
      const r = new FileReader();
      r.onload = () => lookImport(String(r.result || ""));
      r.readAsText(file);
    } else if (act === "d-theme") { lookFocusOn("default", ""); lookSetTheme("default", "", el.value); }
    else if (act === "d-font") { lookFocusOn("default", ""); lookSetFont("default", "", el.value); }
    else if (act === "o-theme" || act === "o-font") {
      const r = rowOf(el);
      lookFocusOn(r.dataset.kind, r.dataset.key);
      (act === "o-theme" ? lookSetTheme : lookSetFont)(r.dataset.kind, r.dataset.key, el.value);
    }
  });
  host.addEventListener("input", (e) => {
    const el = e.target;
    if (el.id === "look-add") { lookEl("look-addlist").hidden = false; lookPaintAddList(); }
    else if (el.id === "look-filter") { lookFilter = el.value; lookPaintGallery(); }
    else if (el.id === "look-name" && lookEdit) lookEdit.name = el.value;
    else if (lookEdit && el.dataset && el.dataset.slot && el.closest("#look-edit")) {
      const v = String(el.value || "").trim().toLowerCase();
      if (el.type === "text" && v && !/^#[0-9a-f]{6}$/.test(v)) { el.classList.add("bad"); return; }
      el.classList.remove("bad");
      lookEdit.draft[el.dataset.slot] = v;
      if (v) {
        el.parentNode.querySelectorAll("input").forEach(o => { if (o !== el) o.value = v; });
      }
      lookPaintPreview();
    }
  });
  host.addEventListener("focusin", (e) => {
    if (e.target.id === "look-add") { lookEl("look-addlist").hidden = false; lookPaintAddList(); }
  });
  document.addEventListener("click", (e) => {
    if (!e.target.closest(".look-combo")) lookEl("look-addlist").hidden = true;
  });
  host.addEventListener("click", (e) => {
    const el = e.target.closest("[data-look]");
    if (!el || el.matches("select, input")) return;
    const act = el.dataset.look, name = el.dataset.name;
    const r = rowOf(el);
    if (act === "tile") { lookTry = name; lookPaint(); }
    else if (act === "t-edit") lookOpenEdit(name);
    else if (act === "t-dup") lookDuplicate(name);
    else if (act === "t-export") lookExport(name);
    else if (act === "t-new") lookNewTheme();
    else if (act === "t-import") { const b = lookEl("look-imp"); b.hidden = !b.hidden; }
    else if (act === "imp-go") lookImport(lookEl("look-imptext").value);
    else if (act === "imp-file") lookEl("look-file").click();
    else if (act === "try-stop") { lookTry = ""; lookPaint(); }
    else if (act === "try-use") lookSetTheme(lookFocus.kind, lookFocus.key, lookTry);
    else if (act === "d-edit") { const d = lookThemeOf("default", ""); if (d) lookOpenEdit(d); else lookNewTheme(); }
    else if (act === "d-show") { lookFocusOn("default", ""); lookPaint(); }
    else if (act === "o-show") { lookFocusOn(r.dataset.kind, r.dataset.key); lookPaint(); }
    else if (act === "o-drop") lookDrop(r.dataset.kind, r.dataset.key);
    else if (act === "o-add") lookAdd(el.dataset.kind, el.dataset.key);
    else if (act === "e-save") lookSaveEdit();
    else if (act === "e-revert") { lookEdit.draft = Object.assign({}, lookEdit.orig); lookPaintEdit(); lookPaintPreview(); }
    else if (act === "e-delete") lookDeleteEdit();
    else if (act === "e-close") { lookEdit = null; lookPaintEdit(); lookPaintPreview(); }
  });
})();
