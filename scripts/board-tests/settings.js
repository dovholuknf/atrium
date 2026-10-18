// settings: settings, themes and skins, providers, preferences.
//
// Units: skinScope, skinHeal, settingsOnce, websiteSkin, prefsEverywhere, themePreview, themeLab, alias, readTabSize, readWidthSkin, themePicker, themePickerShots, dropdownsInstant.
// `units` is what the harness runs, in suite order. `sections` is for HEADLESS_ONLY, which names a section and not a unit.
// Alone: `node scripts/board-tests/settings.js` runs just these units, the way the full run would.
const path = require("path");
const fs = require("fs");
const { slow } = require("../lib/browser.js");
const { mock, T1, landCard, resetSkins, SKINS, hubStreams } = require("../lib/mock.js");
const { fail } = require("../lib/results.js");
const { landContext } = require("../lib/helpers.js");

// The website skin is the one skin that carries rules beyond a palette: a
// gradient primary button, a frosted header and a glow behind the board. Every
// one is scoped to that skin, and the failure this guards is one leaking out, so
// harbour is measured before the skin is worn and again after, and must match
// both times, and a second skin must not pick any of it up either.
async function websiteSkinSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const wp = await ctx.newPage();
  const errors = [];
  wp.on("pageerror", e => errors.push(String(e)));
  try {
    await wp.goto(base, { waitUntil: "domcontentloaded" });
    await wp.waitForSelector("button.go.newagent", { state: "visible", timeout: slow(15000) });
    // Buttons carry `transition: all`, so a colour read straight after a skin
    // change is partway there.
    const look = skin => wp.evaluate(async skin => {
      applySkin(skin);
      getComputedStyle(document.body).color;
      await Promise.all(document.getAnimations().filter(a => a instanceof CSSTransition).map(a => a.finished.catch(() => {})));
      const go =getComputedStyle(document.querySelector("button.go.newagent"));
      const hd = getComputedStyle(document.querySelector("header"));
      const bd = getComputedStyle(document.body);
      const root = getComputedStyle(document.documentElement);
      const vars = ["--teal", "--blue", "--bg-0", "--bg-1", "--bg-2", "--card-0", "--stroke", "--head", "--body"];
      return {
        goBg: go.backgroundImage, goColor: go.color, goShadow: go.boxShadow, goBorder: go.borderTopColor,
        hdBg: hd.backgroundImage + " " + hd.backgroundColor, hdFilter: hd.backdropFilter,
        hdBorder: hd.borderBottomColor, bodyBg: bd.backgroundImage,
        palette: vars.map(v => v + "=" + root.getPropertyValue(v).trim()).join(" ")
      };
    }, skin);
    const harbour = await look("harbour");
    const site = await look("website");
    const noir = await look("noir");
    const harbourAgain = await look("harbour");

    if (JSON.stringify(harbourAgain) !== JSON.stringify(harbour)) {
      fail("wearing the website skin left harbour changed: " + JSON.stringify(harbour) + " then " +
        JSON.stringify(harbourAgain));
    }
    if (harbour.goBg !== "none" || /saturate/.test(harbour.hdFilter) || /radial/.test(harbour.bodyBg)) {
      fail("harbour wears a website effect: " + JSON.stringify(harbour));
    }
    if (noir.goBg !== "none" || /saturate/.test(noir.hdFilter) || /radial/.test(noir.bodyBg)) {
      fail("noir wears a website effect: " + JSON.stringify(noir));
    }
    // The skin itself: harbour's palette, plus the three effects.
    if (site.palette !== harbour.palette) {
      fail("the website skin's palette is not harbour's: " + site.palette + " vs " + harbour.palette);
    }
    if (!/linear-gradient/.test(site.goBg) || site.goColor !== "rgb(4, 18, 29)" || !/rgba\(0, 227, 176/.test(site.goShadow)) {
      fail("the website skin's primary button is not the gradient on its glow: " + JSON.stringify(site));
    }
    // The header went opaque on 2026-10-05 (u-new-board-gpu-cpu-fix): a blur over a moving board cost the GPU a
    // repaint of everything behind it. Only the walk drawer's header is still frosted.
    if (site.hdFilter !== "none" || !/^rgb\(/.test(site.hdBg.split(" ").slice(1).join(" "))) {
      fail("the website skin's header is not opaque and unfiltered: " + site.hdFilter + " " + site.hdBg);
    }
    if ((site.bodyBg.match(/radial-gradient/g) || []).length !== 2) {
      fail("the website skin has no glow behind the board: " + site.bodyBg);
    }
    // A disabled primary button still has to look like it cannot do anything.
    const disabled = await wp.evaluate(() => {
      applySkin("website");
      const b = document.createElement("button");
      b.className = "go"; b.disabled = true; b.textContent = "x";
      document.body.appendChild(b);
      const bg = getComputedStyle(b).backgroundImage;
      b.remove();
      return bg;
    });
    if (disabled !== "none") fail("a disabled primary button in the website skin keeps its gradient: " + disabled);

    // For checking by eye: WEBSITE_SHOTS=<dir> writes the board in the skin.
    if (process.env.WEBSITE_SHOTS) {
      for (const s of ["website", "harbour"]) {
        await wp.evaluate(s => applySkin(s), s);
        await wp.waitForTimeout(300);
        await wp.screenshot({ path: path.join(process.env.WEBSITE_SHOTS, "board-" + s + ".png") });
      }
    }
    if (errors.length) fail("the website skin page threw uncaught errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
}

// A load asks `/v1/settings` from several places, and on a hub those asks land
// inside the first one's round trip. They share it: one read per load, and every
// reader still gets the answer (the skin is worn).
async function settingsOnceSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const sp = await ctx.newPage();
  const wasHub = mock.hubMode;
  mock.hubMode = true;
  mock.settingsDelay = 600;   // wide on purpose: on a loaded runner the load's later reads land well after 135ms
  // This page's reads, not the server's. In the full run the main page is still
  // open and reads settings on its own clock, which the server count took for a
  // second read by this load.
  let settingsReads = 0;
  sp.on("request", r => { if (new URL(r.url()).pathname === "/v1/settings") settingsReads++; });
  try {
    await sp.goto(base, { waitUntil: "domcontentloaded" });
    await sp.waitForTimeout(2500);
    if (settingsReads !== 1) fail("a board load read /v1/settings " + settingsReads + " times, not once.");
    const skin = await sp.evaluate(() => document.documentElement.getAttribute("data-skin"));
    if (skin !== mock.skinFor[""]) {
      fail("with the settings read shared, the skin did not land (got " + skin + ", want " + mock.skinFor[""] + ").");
    }
    // A read made after the first has answered asks again: nothing finished is kept.
    const before = settingsReads;
    await sp.evaluate(() => api("/v1/settings"));
    if (settingsReads !== before + 1) fail("a settings read after the load did not reach the daemon.");
  } finally {
    mock.hubMode = wasHub;
    mock.settingsDelay = 0;
    await ctx.close();
  }
}

// TRYING A TERMINAL'S THEME OPENS THE BOARD'S OWN PANEL, the one trying a skin
// opens, rather than a copy of it: titled for the terminal, the same arrows,
// escape and enter, the project as its default answer and an edit button. A
// preview repaints the terminal as you step; nothing is saved until "use it";
// cancel and escape put back what was there; the skin lab still works through
// the same component.
async function themeLabSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  });
  const patches = [];
  p.on("request", r => {
    if (r.method() === "PATCH" && /\/v1\/tasks\//.test(r.url())) patches.push(r.url() + " " + r.postData());
  });
  const was = mock.tasksMode;
  try {
    mock.wornTasks = [Object.assign({}, T1, { id: "tl-a", display_title: "row tl-a", theme: "nord", supervised: true, pinned: true, worktree: "/tmp/tl/a" })];
    mock.tasksMode = "worn";
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="tl-a"]', { state: "attached", timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(async () => {
      termTask = lastTasks.find(t => t.id === "tl-a");
      term = { options: {}, dispose() {} };
    });
    const state = () => p.evaluate(() => {
      const lab = document.getElementById("skinlab");
      const vis = id => { const e = document.getElementById(id); return !!e && !e.hidden && !!e.offsetParent; };
      return {
        open: !lab.hidden, title: document.getElementById("skinlab-title").textContent,
        value: document.getElementById("skinlab-pick").value,
        fallback: document.getElementById("skinlab-default").textContent,
        extra: vis("skinlab-extra") ? document.getElementById("skinlab-extra").textContent : null,
        term: term.options.theme ? term.options.theme.background.toLowerCase() : null,
        old: !!document.getElementById("t-theme-wrap")
      };
    });
    const bgOf = n => p.evaluate(n => TERM_THEMES[n].background.toLowerCase(), n);

    await p.evaluate(() => pickTheme());
    let r = await state();
    if (!r.open || r.title !== "how this terminal looks") fail("try a theme opened '" + r.title + "' (open " + r.open + ").");
    if (r.value !== "nord") fail("the panel opened on '" + r.value + "', not the card's nord.");
    if (r.extra) fail("the theme panel grew an extra button: " + r.extra);
    if (r.old) fail("the inline header picker is still in the page.");

    // The arrows walk the list and repaint the terminal as they go.
    await p.click(".lab-step >> nth=1");
    r = await state();
    if (r.value === "nord" || r.term !== await bgOf(r.value)) fail("stepping to '" + r.value + "' left the terminal on " + r.term + ".");
    await p.click(".lab-step >> nth=0");
    await p.click(".lab-step >> nth=0");
    r = await state();
    if (r.term !== await bgOf(r.value)) fail("stepping back, '" + r.value + "' left the terminal on " + r.term + ".");
    if (patches.length) fail("stepping saved something: " + patches.join(" | "));

    // Cancel and escape both put nord back, saving nothing.
    await p.click("#skinlab .lab-answers button >> text=cancel");
    r = await state();
    if (r.open || r.term !== await bgOf("nord")) fail("cancel left the panel " + r.open + " and the terminal on " + r.term + ".");
    await p.evaluate(() => pickTheme());
    await p.focus("#skinlab-pick");
    // A select's own change, which is what an arrow key fires (the key itself opens the list on some platforms).
    await p.selectOption("#skinlab-pick", "dracula");
    r = await state();
    if (r.term !== await bgOf("dracula")) fail("a change on the select did not preview: " + r.value + " / " + r.term);
    await p.keyboard.press("Escape");
    r = await state();
    if (r.open || r.term !== await bgOf("nord")) fail("escape left the panel " + r.open + " and the terminal on " + r.term + ".");
    if (patches.length) fail("cancelling saved something: " + patches.join(" | "));

    // The project answer saves an empty theme, once.
    await p.evaluate(() => pickTheme());
    await p.selectOption("#skinlab-pick", "");
    await p.click("#skinlab-mine button >> text=this card");
    await p.waitForFunction(() => !document.getElementById("skinlab") || document.getElementById("skinlab").hidden);
    await p.waitForFunction(() => termTask.theme === "", null, { timeout: slow(5000) });
    if (patches.length !== 1 || !/tl-a .*"theme":""/.test(patches[0])) fail("use the project did not save an empty theme once: " + patches.join(" | "));

    // Use it saves what is chosen, once.
    patches.length = 0;
    await p.evaluate(() => pickTheme());
    await p.selectOption("#skinlab-pick", "dracula");
    await p.click("#skinlab-mine button.go");
    await p.waitForFunction(() => termTask.theme === "dracula", null, { timeout: slow(5000) });
    r = await state();
    if (r.open) fail("use it left the panel open.");
    if (patches.length !== 1 || !/tl-a .*"theme":"dracula"/.test(patches[0])) fail("use it did not save dracula once: " + patches.join(" | "));

    // The same panel, for the board's skin.
    await p.evaluate(() => { pastePrefs = pastePrefs || {}; if (!(pastePrefs.board_skins || []).length) pastePrefs.board_skins = ["dark", "light"]; });
    await p.evaluate(() => openSkinLab());
    r = await state();
    if (!r.open || r.title !== "how the board looks") fail("the skin lab opened '" + r.title + "'.");
    if (r.fallback !== "use the default") fail("the skin lab's default answer reads '" + r.fallback + "'.");
    if (r.extra) fail("the skin lab grew an extra button: " + r.extra);
    const skin0 = await p.evaluate(() => currentSkin());
    await p.click(".lab-step >> nth=1");
    const skin1 = await p.evaluate(() => currentSkin());
    if (skin1 === skin0) fail("stepping did not preview a skin.");
    await p.keyboard.press("Escape");
    r = await state();
    if (r.open || await p.evaluate(() => currentSkin()) !== skin0) fail("escape did not put the skin back.");
    // Opening one while the other is up answers the first.
    await p.evaluate(() => { pickTheme(); previewTheme("dracula"); openSkinLab(); });
    r = await state();
    if (r.title !== "how the board looks" || r.term !== await bgOf("dracula")) fail("opening the skin lab over the theme panel left " + r.title + " / " + r.term + ".");
    if (errors.length) fail("the theme lab page threw: " + errors.join(" | "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

// A THEME PREVIEW RECOLOURS THE CARD, not only the terminal: the attached row,
// its bridge, the pane's frame and the stack card follow the picker, for that
// card alone. Cancelling, detaching and switching cards put the saved colours
// back, nothing is written while previewing, and "use it" survives a reload.
async function themePreviewSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
    localStorage.setItem("atrium.cardColors", "1");
    window.__writes = [];
    const set = Storage.prototype.setItem;
    Storage.prototype.setItem = function (k, v) { window.__writes.push(k); return set.call(this, k, v); };
  });
  const patches = [];
  p.on("request", r => {
    if (r.method() === "PATCH" && /\/v1\/tasks\//.test(r.url())) patches.push(r.url() + " " + r.postData());
  });
  const was = mock.tasksMode;
  const live = (id, theme) => Object.assign({}, T1, {
    id, display_title: "row " + id, theme, supervised: true, pinned: true, worktree: "/tmp/tp/" + id
  });
  try {
    mock.wornTasks = [live("tp-a", "nord"), live("tp-b", "atrium")];
    mock.tasksMode = "worn";
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="tp-a"]', { state: "attached", timeout: slow(15000) });
    // Let the view switch finish. `switchView` clears `on` from every `.tab`,
    // the rows included, and a late one would unselect the stand-in attach.
    await p.waitForTimeout(1000);
    const bg = await p.evaluate(() => {
      const hex = h => { const n = parseInt(h.slice(1), 16); return `rgb(${n >> 16}, ${(n >> 8) & 255}, ${n & 255})`; };
      const o = {};
      for (const n of ["nord", "atrium", "dracula", "gruvbox-dark"]) o[n] = hex(TERM_THEMES[n].background);
      return o;
    });
    // Attach a stand-in terminal on tp-a. The picker and the bridge only ask
    // that there is one with options to set.
    const attach = id => p.evaluate(async id => {
      termTask = lastTasks.find(t => t.id === id);
      term = { options: {}, dispose() {} };
      await loadCards().catch(() => {}).then(renderTermList);
      await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
      placeTabBridge();
    }, id);
    const read = () => p.evaluate(async () => {
      await new Promise(r => setTimeout(r, 400));
      await loadCards().catch(() => {}).then(renderStack);
      const row = id => document.querySelector('#term-list .card.tab[data-id="' + id + '"]');
      const a = row("tp-a"), b = row("tp-b");
      const bridge = [...document.querySelectorAll("#term-layout .tabbridge")].find(x => !x.hidden);
      const pane = document.getElementById("term-pane");
      const stack = document.querySelector('#stack-list .stackrow[data-id="tp-a"]');
      return {
        a: a && getComputedStyle(a).backgroundColor, b: b && getComputedStyle(b).backgroundColor,
        aEdge: a && getComputedStyle(a).borderTopColor,
        bridge: bridge ? getComputedStyle(bridge).backgroundColor : null,
        frame: pane.style.getPropertyValue("--framec"),
        stack: stack ? stack.style.getPropertyValue("--card-0") : null,
        term: term && term.options.theme ? term.options.theme.background : null,
        wrap: !labIs("theme")
      };
    });
    const themeHex = n => p.evaluate(n => TERM_THEMES[n].background.toLowerCase(), n);

    await attach("tp-a");
    let r = await read();
    if (r.a !== bg.nord) fail("before a preview the attached row is " + r.a + ", not nord's " + bg.nord + ".");
    const saved = r;

    // Preview dracula: everything that wears tp-a's theme follows it, tp-b does not.
    await p.evaluate(() => { window.__writes = []; pickTheme(); previewTheme("dracula"); });
    r = await read();
    if (r.term !== await themeHex("dracula")) fail("the preview did not reach the terminal: " + r.term);
    if (r.a !== bg.dracula) fail("previewing dracula, the attached row stayed " + r.a + ".");
    if (r.b !== saved.b) fail("previewing a theme on tp-a recoloured tp-b: " + r.b);
    if (!r.bridge || r.bridge !== r.a) fail("previewing, the bridge is " + r.bridge + " and the row " + r.a + ".");
    if (r.frame !== r.aEdge) fail("previewing, the pane frame is " + r.frame + " and the row's edge " + r.aEdge + ".");
    if (r.stack !== await themeHex("dracula")) fail("previewing, the stack card wears " + r.stack + ".");
    // Another pick moves it again.
    await p.evaluate(() => previewTheme("gruvbox-dark"));
    r = await read();
    if (r.a !== bg["gruvbox-dark"]) fail("a second pick did not move the row: " + r.a);

    // Cancel: the saved colours come back, and nothing was written.
    await p.evaluate(() => cancelLab());
    r = await read();
    for (const k of ["a", "b", "bridge", "frame", "stack"]) {
      if (r[k] !== saved[k]) fail("after cancel, " + k + " is " + r[k] + ", saved was " + saved[k] + ".");
    }
    const writes = await p.evaluate(() => window.__writes);
    if (writes.length) fail("a preview wrote localStorage: " + [...new Set(writes)].join(", "));
    if (patches.length) fail("a preview wrote to the daemon: " + patches.join(" | "));

    // Detaching ends a preview.
    await p.evaluate(() => { pickTheme(); previewTheme("dracula"); });
    await p.evaluate(() => clearTermPane(false));
    // The repaint rides a queued refresh (dropThemePreview), which under load outlasts read()'s 400ms.
    await p.waitForFunction(a => {
      const row = document.querySelector('#term-list .card.tab[data-id="tp-a"]');
      return row && getComputedStyle(row).backgroundColor === a;
    }, saved.a, { timeout: slow(5000) }).catch(() => {});
    r = await read();
    if (r.a !== saved.a || !r.wrap) fail("after detaching mid-preview the row is " + r.a + ", picker hidden " + r.wrap);
    if (await p.evaluate(() => themePreview)) fail("detaching left a preview held.");

    // So does switching cards.
    await attach("tp-a");
    await p.evaluate(() => { pickTheme(); previewTheme("dracula"); });
    await p.evaluate(async () => {
      clearTermPane(true);
      termTask = lastTasks.find(t => t.id === "tp-b");
      term = { options: {}, dispose() {} };
    });
    r = await read();
    if (r.a !== saved.a) fail("after switching to tp-b mid-preview, tp-a is " + r.a + ", not its saved " + saved.a);

    // Use it: saved once, and kept across a reload.
    await attach("tp-a");
    await p.evaluate(async () => {
      pickTheme();
      document.getElementById("skinlab-pick").value = "dracula";
      previewTheme("dracula");
      await answerLab(0);
    });
    r = await read();
    if (r.a !== bg.dracula) fail("after use it the row is " + r.a + ", not dracula.");
    if (patches.length !== 1 || !/tp-a .*"theme":"dracula"/.test(patches[0])) {
      fail("use it did not save the theme once: " + patches.join(" | "));
    }
    await p.evaluate(() => history.replaceState(history.state, "", location.pathname + location.search));
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="tp-a"]', { state: "attached", timeout: slow(15000) });
    // Let the view switch finish. `switchView` clears `on` from every `.tab`,
    // the rows included, and a late one would unselect the stand-in attach.
    await p.waitForTimeout(1000);
    await attach("tp-a");
    r = await read();
    if (r.a !== bg.dracula) fail("after use it and a reload the row is " + r.a + ", not dracula.");
    if (errors.length) fail("the theme preview page threw: " + errors.join(" | "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

// ── a card's alias ────────────────────────────────────────────────────────
// A card with an alias wears it as an `@name` chip, one without draws none, the
// card menu and the terminal list both offer "alias…", and setting one PATCHes
// the card with what was typed. See js/alias.js and docs/test-plan.md BS.
async function aliasSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const ap = await ctx.newPage();
  const errors = [];
  ap.on("pageerror", e => errors.push(String(e)));
  mock.aliasWrites = [];
  try {
    await ap.goto(base, { waitUntil: "domcontentloaded" });
    await ap.waitForFunction(() => typeof cardHTML === "function" && typeof aliasMenuItem === "function",
      null, { timeout: slow(15000) });
    const got = await ap.evaluate(t1 => {
      const draw = t => { const box = document.createElement("div"); box.innerHTML = cardHTML(t); return box; };
      const withAlias = draw(Object.assign({}, t1, { id: "al1", alias: "sa89" }));
      const without = draw(Object.assign({}, t1, { id: "al2" }));
      const chip = withAlias.querySelector(".chip.alias");
      const item = aliasMenuItem(Object.assign({}, t1, { id: "al1", alias: "sa89" }));
      return {
        chip: chip ? chip.textContent.trim() : "", tip: chip ? chip.getAttribute("data-tip") : "",
        title: chip ? chip.hasAttribute("title") : false,
        none: !!without.querySelector(".chip.alias"),
        label: item && item.label, note: item && item.note,
      };
    }, T1);
    if (got.chip !== "@sa89") fail("a card with an alias does not wear it: " + JSON.stringify(got));
    if (got.tip) fail("the alias chip still carries a data-tip: " + got.tip);
    if (got.title) fail("the alias chip carries a native title tooltip.");
    if (got.none) fail("a card with no alias drew an alias chip.");
    if (got.label !== "alias…" || got.note !== "@sa89") fail("the alias menu item reads wrong: " + JSON.stringify(got));

    // Setting it PATCHes the card with what was typed, `@` and all: the room
    // normalizes it.
    await ap.evaluate(() => { window.__aliasDone = setTaskAlias("t1", ""); });
    await ap.waitForSelector("#ask[open] #ask-input", { timeout: slow(5000) });
    await ap.fill("#ask-input", "@dotfiles");
    await ap.click("#ask-actions button.go, #ask-actions button:last-child");
    await ap.evaluate(() => window.__aliasDone);
    const w = mock.aliasWrites.find(x => x.id === "t1");
    if (!w || w.body.alias !== "@dotfiles") fail("setting an alias did not PATCH it: " + JSON.stringify(mock.aliasWrites));

    // A default another card holds: a `no alias` chip saying who, and the menu
    // says it too. Backlog-2 item 47.
    const clash = await ap.evaluate(t1 => {
      const box = document.createElement("div");
      const note = "no alias: its default is taken. @saorch is already the alias of sa84, card c9";
      box.innerHTML = cardHTML(Object.assign({}, t1, { id: "al3", alias_note: note }));
      const chip = [...box.querySelectorAll(".chip")].find(c => c.textContent.trim() === "no alias");
      const item = aliasMenuItem(Object.assign({}, t1, { id: "al3", alias_note: note }));
      return { tip: chip ? chip.getAttribute("data-tip") : "", help: item.help, note: item.note };
    }, T1);
    if (!/@saorch is already the alias of sa84/.test(clash.tip)) fail("a clashing default does not say why on the card: " + JSON.stringify(clash));
    if (!/@saorch/.test(clash.help) || clash.note !== "taken") fail("the alias menu does not say why there is none: " + JSON.stringify(clash));

    // THE TERMINAL BAR. An alias takes the far-left slot as `@saorch`, drawn as
    // a handle, with the repo and branch in the tooltip. None keeps the label.
    // It follows an alias changed under it, and a click on it sets one.
    const bar = await ap.evaluate(t1 => {
      const t = Object.assign({}, t1, { id: "t1", worktree: "D:/git/github/dovholuknf/atrium",
        repo: "atrium", branch: "claude/main", display_title: "saorch: merger" });
      const el = document.getElementById("t-title");
      termTask = Object.assign({}, t);
      paintTermTitle(termTask);
      const plain = { text: el.textContent, handle: el.classList.contains("as-alias"), tip: el.dataset.tip };
      followTermAlias([Object.assign({}, t, { alias: "saorch" })]);
      const aliased = { text: el.textContent, handle: el.classList.contains("as-alias"), tip: el.dataset.tip,
        at: !!el.querySelector(".at") };
      followTermAlias([Object.assign({}, t, { alias: "" })]);
      const cleared = { text: el.textContent, handle: el.classList.contains("as-alias") };
      followTermAlias([Object.assign({}, t, { alias: "saorch" })]);
      return { plain, aliased, cleared, label: terminalLabel(t), win: windowTitle(termTask) };
    }, T1);
    if (process.env.ALIAS_SHOTS) {
      await ap.evaluate(() => { const t = Object.assign({}, termTask, { alias: "saorch" }); termTask = t; paintTermTitle(t); paintTermChips(t);
        const host = document.createElement("section"); host.className = "term-pane"; host.id = "shot-host";
        host.style.cssText = "position:fixed;top:0;left:0;width:900px;z-index:99999;display:block;background:var(--bg,#111)";
        host.appendChild(document.querySelector("#term-pane .term-bar")); document.body.appendChild(host); });
      await ap.locator("#shot-host").screenshot({ path: require("path").join(process.env.ALIAS_SHOTS, process.env.ALIAS_SHOT_NAME || "alias-chip.png") });
      await ap.evaluate(() => { const h = document.getElementById("shot-host"); document.getElementById("term-pane").prepend(h.firstElementChild); h.remove(); });
    }
    if (bar.plain.text !== bar.label || bar.plain.handle) fail("a card with no alias does not keep today's bar label: " + JSON.stringify(bar));
    if (!/click to give it an alias/.test(bar.plain.tip || "")) fail("the bar label does not offer an alias: " + JSON.stringify(bar.plain));
    if (bar.aliased.text !== "@saorch" || !bar.aliased.handle || !bar.aliased.at) fail("the bar does not wear the alias as a handle: " + JSON.stringify(bar.aliased));
    if (bar.aliased.tip) fail("the bar's alias chip still carries a tooltip: " + bar.aliased.tip);
    if (bar.cleared.text !== bar.label || bar.cleared.handle) fail("clearing the alias did not put the label back: " + JSON.stringify(bar.cleared));
    if (!/^@saorch · /.test(bar.win)) fail("a popped-out window's title does not lead with the alias: " + bar.win);

    // A drag that selects the name must not open the alias dialog; a plain click still does.
    const dragged = await ap.evaluate(() => {
      const el = document.getElementById("t-title");
      const r = el.getBoundingClientRect(), y = r.top + r.height / 2;
      const ev = (type, x) => new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y });
      el.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, clientX: r.left + 2, clientY: y }));
      window.getSelection().selectAllChildren(el);
      el.dispatchEvent(ev("click", r.left + 22));
      return !!document.querySelector("#ask[open]");
    });
    if (dragged) fail("a drag that selected the bar's name still opened the alias dialog");
    await ap.evaluate(() => window.getSelection().removeAllRanges());

    // THE COPY ICON. It writes `@handle`, flashes a check on itself with no toast, and does not open the rename.
    // A drag selecting across it copies nothing. The card's chip has the same icon.
    await ap.evaluate(() => { window.__clip = []; navigator.clipboard.writeText = async t => { window.__clip.push(t); }; });
    const toastsBefore = await ap.evaluate(() => document.querySelectorAll(".toast").length);
    await ap.evaluate(() => document.getElementById("t-copy").click());
    await ap.waitForFunction(() => document.getElementById("t-copy").textContent.trim() === "✓", null, { timeout: slow(3000) });
    const cp = await ap.evaluate(() => ({ clip: window.__clip.slice(), ask: !!document.querySelector("#ask[open]"),
      toasts: document.querySelectorAll(".toast").length, tip: document.getElementById("t-title").dataset.tip || "",
      title: document.getElementById("t-title").getAttribute("title") || "" }));
    if (cp.clip.join("|") !== "@saorch") fail("the copy icon did not write @saorch: " + JSON.stringify(cp));
    if (cp.ask) fail("the copy icon opened the rename");
    if (cp.toasts !== toastsBefore) fail("the copy icon raised a toast");
    if (cp.tip || cp.title) fail("the alias chip has a title or data-tip: " + JSON.stringify(cp));
    await ap.waitForFunction(() => !!document.querySelector("#t-copy svg"), null, { timeout: slow(3000) });
    const cardCopy = await ap.evaluate(t1 => {
      window.__clip = [];
      const box = document.createElement("div"); box.innerHTML = cardHTML(Object.assign({}, t1, { id: "al9", alias: "sa89" }));
      document.body.appendChild(box);
      const chip = box.querySelector(".chip.alias"), b = box.querySelector(".alias-copy");
      const r = { chipTip: chip.dataset.tip || chip.getAttribute("title") || "", has: !!b };
      b.click();
      return new Promise(res => setTimeout(() => { r.clip = window.__clip.slice(); r.mark = b.textContent.trim(); box.remove(); res(r); }, 50));
    }, T1);
    if (!cardCopy.has || cardCopy.chipTip || cardCopy.clip.join("|") !== "@sa89" || cardCopy.mark !== "✓")
      fail("the card's alias copy icon is wrong: " + JSON.stringify(cardCopy));

    mock.aliasWrites = [];
    await ap.evaluate(() => document.getElementById("t-title").click());
    await ap.waitForSelector("#ask[open] #ask-input", { timeout: slow(5000) });
    const pre = await ap.inputValue("#ask-input");
    if (pre !== "saorch") fail("the bar's alias dialog did not start from the alias: " + pre);
    await ap.fill("#ask-input", "@orch");
    await ap.click("#ask-actions button.go, #ask-actions button:last-child");
    await ap.waitForFunction(() => !document.querySelector("#ask[open]"), null, { timeout: slow(5000) });
    for (let i = 0; i < 50 && !mock.aliasWrites.length; i++) await ap.waitForTimeout(50);
    const bw = mock.aliasWrites.find(x => x.id === "t1");
    if (!bw || bw.body.alias !== "@orch") fail("setting the alias from the bar did not PATCH it: " + JSON.stringify(mock.aliasWrites));
  } finally {
    await ctx.close();
  }
  if (errors.length) fail("the alias page threw: " + errors.join(" | "));
}

// ── the read tab's own text size and scrollbar (u-read-tab-text-size) ────
// A long document must scroll in the read tab, and A-/A+ must size the document text only, under a key of
// its own, surviving a reload and never touching the board's origin zoom. RTS_SHOTS=<dir> takes pictures;
// RTS_BEFORE=1 only takes them (the old page has no controls to check).
async function readTabSizeSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  landCard("land-live", { supervised: true, room: "sg4", created_at: "2026-09-19T12:00:00Z" });
  mock.landList = [mock.LAND["land-live"]];
  mock.landPerms = [];
  const errors = [];
  const shots = process.env.RTS_SHOTS || "";
  const before = !!process.env.RTS_BEFORE;
  const ctx = await landContext(browser);
  try {
    const doc = ["# Long document", ""].concat(Array.from({ length: 120 }, (_, i) =>
      "Paragraph " + (i + 1) + ": the quick brown fox jumps over the lazy dog, again and again.\n")).join("\n");
    await ctx.route("**/files/view?*", route => route.fulfill({ status: 200, contentType: "text/plain; charset=utf-8", body: doc }));
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    const open = async () => {
      await p.goto(base + "/read.html#land-live/docs%2Fplan.md", { waitUntil: "domcontentloaded" });
      await p.waitForSelector("#md h1", { timeout: slow(8000) });
      await p.waitForTimeout(200);
    };
    await open();
    if (shots) await p.screenshot({ path: path.join(shots, before ? "before.png" : "after.png") });
    if (before) return;
    const m = () => p.evaluate(() => ({ h: document.scrollingElement.scrollHeight, v: innerHeight,
      y: scrollY, fs: parseFloat(getComputedStyle(document.getElementById("md")).fontSize),
      key: localStorage.getItem("atrium.readfont"), keys: Object.keys(localStorage) }));
    let r = await m();
    if (r.h <= r.v + 50) fail("the long document does not overflow the window: " + JSON.stringify(r));
    await p.mouse.move(300, 300);
    await p.mouse.wheel(0, 600);
    await p.waitForFunction(() => scrollY >= 100, null, { timeout: slow(5000) }).catch(() => {});
    if ((await m()).y < 100) fail("the wheel did not scroll the read tab");
    await p.keyboard.press("End");
    await p.waitForFunction(() => scrollY + innerHeight >= document.scrollingElement.scrollHeight - 5, null, { timeout: slow(5000) }).catch(() => {});
    r = await m();
    if (r.y + r.v < r.h - 5) fail("End did not reach the bottom: " + JSON.stringify(r));
    await p.evaluate(() => scrollTo(0, 0));
    // Headless Chromium draws overlay bars with no gutter, so the rule is checked rather than the pixels.
    const ov = await p.evaluate(() => getComputedStyle(document.documentElement).overflowY + "/" + getComputedStyle(document.body).overflowY);
    if (ov !== "scroll/visible") fail("the read page's overflow is " + ov);
    r = await m();
    if (r.fs !== 15) fail("default text size is " + r.fs);
    await p.click("#zoom-up");
    await p.keyboard.press("Control+=");
    r = await m();
    if (r.fs !== 17 || r.key !== "17") fail("A+ and ctrl+= did not reach 17: " + JSON.stringify(r));
    await p.keyboard.press("Control+-");
    await p.click("#zoom-down");
    await p.keyboard.press("Control+-");
    r = await m();
    if (r.fs !== 14) fail("shrinking did not reach 14: " + JSON.stringify(r));
    await p.keyboard.press("Control+=");
    await p.keyboard.press("Control+=");
    if (shots) await p.screenshot({ path: path.join(shots, "after-bigger.png") });
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#md h1");
    r = await m();
    if (r.fs !== 16) fail("the size did not survive a reload: " + JSON.stringify(r));
    await p.keyboard.press("Control+0");
    r = await m();
    if (r.fs !== 15) fail("ctrl+0 did not reset: " + JSON.stringify(r));
    if (r.keys.some(k => /read|zoom|font/i.test(k) && !["atrium.readfont", "atrium.readwidth", "atrium.readskin"].includes(k))) fail("the read page wrote another storage key: " + JSON.stringify(r.keys));
    if (errors.length) fail("the read tab threw: " + errors.join(" | "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

// ── the read tab's width and skin (u-reader-width-and-skins) ─────────────
// The rendered page was a 52rem column on any window, so a four-column table wrapped every cell. Width is now
// narrow / wide / full, a table may run past the column, and the page wears the board's skin with a picker of
// its own. RW_SHOTS=<dir> takes pictures, named by RW_PREFIX (before|after). RW_BEFORE=1 only takes them, since the
// old page has none of the controls.
async function readWidthSkinSection(browser, base) {
  const was = mock.tasksMode;
  mock.tasksMode = "land";
  landCard("land-live", { supervised: true, room: "sg4", created_at: "2026-09-19T12:00:00Z" });
  mock.landList = [mock.LAND["land-live"]];
  mock.landPerms = [];
  resetSkins();
  const errors = [];
  const shots = process.env.RW_SHOTS || "";
  const prefix = process.env.RW_PREFIX || (process.env.RW_BEFORE ? "before" : "after");
  const before = !!process.env.RW_BEFORE;
  const cell = "the quick brown fox jumps over the lazy dog and then does it all again";
  const doc = ["# Wide table", "",
    "A paragraph of prose that stays a readable column wherever the window is, so the eye does not have to travel.", "",
    "| name | owner | status | notes |", "| - | - | - | - |"]
    .concat(Array.from({ length: 6 }, (_, i) => `| item ${i + 1} | owner ${i + 1} | ${cell} | ${cell} again ${i + 1} |`))
    .concat(["", "```", "const x = 1;", "```", "", "Another paragraph after the table."]).join("\n");
  const ctx = await landContext(browser);
  try {
    await ctx.route("**/files/view?*", route => route.fulfill({ status: 200, contentType: "text/plain; charset=utf-8", body: doc }));
    const p = await ctx.newPage();
    p.on("pageerror", e => errors.push(String(e)));
    const open = async (w, h) => {
      await p.setViewportSize({ width: w, height: h });
      await p.goto(base + "/read.html#land-live/docs%2Fplan.md", { waitUntil: "domcontentloaded" });
      await p.waitForSelector("#md table", { timeout: slow(8000) }).catch(async e => {
        throw new Error(e.message + " body: " + (await p.evaluate(() => document.body.innerText.slice(0, 300))));
      });
      await p.waitForTimeout(400);
    };
    const m = () => p.evaluate(() => {
      const w = e => Math.round(e.getBoundingClientRect().width);
      const para = document.querySelector("#md p");
      const t = document.querySelector("#md table");
      const row = t.querySelector("tbody tr");
      return { para: w(para), table: w(t), cellH: Math.round(row.cells[2].getBoundingClientRect().height),
        line: parseFloat(getComputedStyle(row.cells[2]).lineHeight),
        skin: document.documentElement.getAttribute("data-skin"), width: document.documentElement.getAttribute("data-width"),
        bg: getComputedStyle(document.body).backgroundColor, sel: document.getElementById("skin") && document.getElementById("skin").value,
        opts: document.getElementById("skin") ? [...document.getElementById("skin").options].map(o => o.value) : [],
        pressed: [...document.querySelectorAll("#width button[aria-pressed=true]")].map(b => b.dataset.width),
        page: document.documentElement.scrollWidth, win: innerWidth,
        keys: Object.keys(localStorage).filter(k => /^atrium\.(read|skin)/.test(k)) };
    });
    if (before) {
      for (const [name, w, h] of [["wide", 2558, 1000], ["phone", 390, 844]]) {
        await open(w, h);
        if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-${name}.png`) });
      }
      return;
    }
    const sets0 = mock.skinFor[""];

    // 2558 wide. Narrow is the default and keeps the prose measure, and the table is let out of it.
    await open(2558, 1000);
    await p.waitForFunction(() => document.getElementById("skin").options.length > 1, null, { timeout: slow(5000) });
    let r = await m();
    if (r.width !== "narrow" || r.pressed.join() !== "narrow") fail("the default width is " + JSON.stringify(r));
    if (r.para > 52 * 16 + 4) fail("narrow is wider than 52rem: " + r.para);
    if (r.table < r.para + 200) fail("the table is held in the narrow column: " + JSON.stringify(r));
    if (r.cellH > r.line * 1.5) fail("a table cell still wraps on a 2558px window: " + JSON.stringify(r));
    if (r.page > r.win) fail("the page scrolls sideways: " + JSON.stringify(r));
    const lefts = await p.evaluate(() => ["h1", "p", "pre"].map(s => Math.round(document.querySelector("#md " + s).getBoundingClientRect().left)));
    if (new Set(lefts).size !== 1) fail("the heading, prose and code are not in one column: " + lefts.join());
    if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-wide.png`) });

    await p.click('#width button[data-width="wide"]');
    let r2 = await m();
    if (r2.width !== "wide" || r2.pressed.join() !== "wide" || r2.para <= r.para + 200) fail("wide did not widen the column: " + JSON.stringify(r2));
    await p.click('#width button[data-width="full"]');
    r2 = await m();
    if (r2.width !== "full" || r2.para < 2558 - 100) fail("full is not the whole window: " + JSON.stringify(r2));
    if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-full.png`) });
    if (r2.keys.join() !== "atrium.readwidth") fail("the width wrote these keys: " + r2.keys.join());
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#md table");
    r2 = await m();
    if (r2.width !== "full") fail("the width did not survive a reload: " + JSON.stringify(r2));
    await p.click('#width button[data-width="narrow"]');

    // The skin: the board's (the hub's, noir here), then this page's own, then back.
    await p.waitForFunction(() => document.getElementById("skin").options.length > 1, null, { timeout: slow(5000) });
    r = await m();
    if (r.skin !== mock.skinFor[""] || r.sel !== "") fail("the page does not wear the board's skin: " + JSON.stringify({ skin: r.skin, board: mock.skinFor[""], sel: r.sel }));
    if (JSON.stringify(r.opts) !== JSON.stringify([""].concat(SKINS))) fail("the picker is not the daemon's skin list: " + JSON.stringify(r.opts));
    const bg0 = r.bg;
    await p.selectOption("#skin", "ember");
    r = await m();
    if (r.skin !== "ember" || r.bg === bg0) fail("picking ember did not repaint: " + JSON.stringify(r));
    if (!r.keys.includes("atrium.readskin") || r.keys.includes("atrium.skin")) fail("the skin keys are " + r.keys.join());
    if (mock.skinFor[""] !== sets0) fail("the page changed the board's skin to " + mock.skinFor[""]);
    await p.reload({ waitUntil: "domcontentloaded" });
    await p.waitForSelector("#md table");
    await p.waitForFunction(() => document.getElementById("skin").options.length > 1, null, { timeout: slow(5000) });
    r = await m();
    if (r.skin !== "ember" || r.sel !== "ember") fail("the page's skin did not survive a reload: " + JSON.stringify(r));
    // The default skin has no rule of its own, so picking it clears the attribute.
    await p.selectOption("#skin", SKINS[0]);
    r = await m();
    if (r.skin !== null) fail("the default skin left an attribute: " + JSON.stringify(r));
    await p.selectOption("#skin", "");
    r = await m();
    if (r.skin !== mock.skinFor[""] || r.keys.includes("atrium.readskin")) fail("the board's skin did not come back: " + JSON.stringify(r));
    await p.selectOption("#skin", "sandstone");
    if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-wide-sandstone.png`) });
    await p.selectOption("#skin", "");

    // A phone: the column is the window, and a table scrolls inside itself instead of widening the page.
    await open(390, 844);
    r = await m();
    if (r.page > r.win) fail("the page scrolls sideways at 390px: " + JSON.stringify(r));
    if (r.table > 390) fail("the table is wider than the phone: " + JSON.stringify(r));
    if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-phone.png`) });
    await p.click('#width button[data-width="full"]');
    await p.selectOption("#skin", "sandstone");
    if (shots) await p.screenshot({ path: path.join(shots, `${prefix}-phone-sandstone.png`) });
    if (errors.length) fail("the read tab threw: " + errors.join(" | "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

// ── the board skin follows the room-picker scope ────────────────────────
// THREE SCOPES, THREE SKINS, HELD AT ONCE. The ALL view wears the hub's skin,
// and each of two rooms wears its own, so scoping ALL -> alpha -> sgg reads
// three different skins back. Then: a save in one scope reaches only that
// scope and leaks into no other, switching scope re-applies each
// independently, and a room attaching does not clobber the ALL skin. A fresh
// context keeps this test's per-scope localStorage out of the others'.
// Both rooms live, so all three scopes are pickable.
async function skinScopeSection(browser, base) {
  mock.hubMode = true;
  mock.sggAttached = true;
  resetSkins();
  const skinCtx = await browser.newContext();
  const skin = await skinCtx.newPage();
  const skinErrors = [];
  skin.on("pageerror", e => skinErrors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) {
    skin.on("console", m => console.error("[skin] " + m.type() + ": " + m.text()));
  }
  const dataSkin = () =>
    skin.evaluate(() => document.documentElement.getAttribute("data-skin"));
  // wears waits for the board to wear a skin (null is the default) and names
  // the step that did not, with what it wore instead. Returns whether it did.
  const wears = (want, what) => skin.waitForFunction(w =>
    document.documentElement.getAttribute("data-skin") === w, want, { timeout: slow(15000) })
    .then(() => true, async () => {
      fail(what + ": the board wore " + JSON.stringify(await dataSkin()) + ", wanted " +
        JSON.stringify(want) + ". The mock holds " + JSON.stringify(mock.skinFor));
      return false;
    });
  // THE LOAD'S OWN SETTINGS READS HAVE TO LAND BEFORE A SAVE. The remembered
  // skin paints at once, so the right skin on screen does not mean the board has
  // finished reading. A save made then was painted over by a read answered
  // before it, and the wait for the new skin ran out: the flaky main-flow
  // timeout. Counted per request, so this waits on the reads, not a clock.
  // A reload abandons the old page's reads without always saying so, so the set
  // starts over when the page does.
  const settingsOut = new Set();
  const isSettings = r => new URL(r.url()).pathname === "/v1/settings";
  skin.on("request", r => { if (isSettings(r)) settingsOut.add(r); });
  skin.on("requestfinished", r => settingsOut.delete(r));
  skin.on("requestfailed", r => settingsOut.delete(r));
  skin.on("framenavigated", f => { if (f === skin.mainFrame()) settingsOut.clear(); });
  const settled = async what => {
    await skin.waitForFunction(() => document.getElementById("conn").classList.contains("live"),
      null, { timeout: slow(15000) }).catch(() => fail(what + ": the event stream never opened."));
    await skin.evaluate(() => new Promise(r => setTimeout(r, 0)));
    for (const end = Date.now() + 15000; settingsOut.size && Date.now() < end;) {
      await skin.waitForTimeout(50);
    }
    if (settingsOut.size) fail(what + ": the settings reads never finished.");
    // One more turn of the page, so the last answer has been painted.
    await skin.evaluate(() => new Promise(r => setTimeout(r, 0)));
  };
  // scopeTo reloads the board into a scope (null for ALL) and waits for the
  // skin that scope wears to land and the reads behind it to finish.
  const picker = () => skin.waitForFunction(() => typeof pickRoom === "function", null, { timeout: slow(15000) });
  const scopeTo = async (room, want) => {
    await picker();
    await Promise.all([
      skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
      skin.evaluate(r => pickRoom(r), room)
    ]);
    const ok = await wears(want, "scoped to " + (room || "ALL"));
    await settled("scoped to " + (room || "ALL"));
    // And the board says so itself. Until it has, a room attaching re-reads the
    // skin, which is the heal, and would read whatever the mock holds by then.
    await skin.waitForFunction(() => skinHasSettled(), null, { timeout: slow(15000) })
      .catch(() => fail("scoped to " + (room || "ALL") + ": the skin never settled."));
    return ok;
  };
  try {
    // ALL scope: the hub's own skin, not the alphabetically-first room's.
    await skin.goto(base, { waitUntil: "domcontentloaded" });
    await wears("noir", "the ALL view on load");

    // Scope to each room in turn: three scopes, three different skins, at once.
    // The hub holds noir, alpha holds moss, sgg holds ember, and no two agree.
    await scopeTo("alpha", "moss");
    await scopeTo("sgg", "ember");
    const held = { "": mock.skinFor[""], alpha: mock.skinFor.alpha, sgg: mock.skinFor.sgg };
    const distinct = new Set(Object.values(held));
    if (distinct.size !== 3) {
      fail("the three scopes did not hold three different skins at once: " +
        JSON.stringify(held));
    }
    if (held[""] !== "noir" || held.alpha !== "moss" || held.sgg !== "ember") {
      fail("the three scopes wore the wrong skins: " + JSON.stringify(held));
    }

    // A skin saved from the ALL view lands on the hub (skinFor[""]) and leaves
    // both rooms alone. A 409 would have made saveSkin revert the paint.
    await scopeTo(null, "noir");
    await skin.evaluate(() => saveSkin("vapor"));
    await wears("vapor", "a skin saved from the ALL view");
    if (mock.skinFor[""] !== "vapor") {
      fail("a skin saved from the ALL view did not reach the hub: skinFor[''] is " +
        JSON.stringify(mock.skinFor[""]) + ", wanted vapor.");
    }
    if (mock.skinFor.alpha !== "moss" || mock.skinFor.sgg !== "ember") {
      fail("saving the ALL skin leaked into a room: " + JSON.stringify(mock.skinFor));
    }

    // A skin saved while scoped to alpha lands on alpha alone, and leaves the
    // hub's ALL skin and sgg's untouched.
    await scopeTo("alpha", "moss");
    await skin.evaluate(() => saveSkin("sandstone"));
    await wears("sandstone", "a skin saved while scoped to alpha");
    if (mock.skinFor.alpha !== "sandstone") {
      fail("a skin saved while scoped to alpha did not reach the room: skinFor.alpha is " +
        JSON.stringify(mock.skinFor.alpha) + ", wanted sandstone.");
    }
    if (mock.skinFor[""] !== "vapor" || mock.skinFor.sgg !== "ember") {
      fail("saving alpha's skin leaked into another scope: " + JSON.stringify(mock.skinFor));
    }

    // A skin saved while scoped to sgg lands on sgg alone.
    await scopeTo("sgg", "ember");
    await skin.evaluate(() => saveSkin("harbour"));
    await wears(null, "a skin saved while scoped to sgg");
    if (mock.skinFor.sgg !== "harbour") {
      fail("a skin saved while scoped to sgg did not reach the room: skinFor.sgg is " +
        JSON.stringify(mock.skinFor.sgg) + ", wanted harbour.");
    }
    if (mock.skinFor[""] !== "vapor" || mock.skinFor.alpha !== "sandstone") {
      fail("saving sgg's skin leaked into another scope: " + JSON.stringify(mock.skinFor));
    }

    // Switching scope re-applies each saved skin independently: ALL is vapor,
    // alpha is sandstone, sgg is the default harbour (drawn by removing the
    // attribute), and each is read fresh on its own reload.
    await scopeTo(null, "vapor");
    await scopeTo("alpha", "sandstone");
    await picker();
    await Promise.all([
      skin.waitForNavigation({ waitUntil: "domcontentloaded" }),
      skin.evaluate(() => pickRoom("sgg"))
    ]);
    await wears(null, "scoped back to sgg");

    // Back to ALL: the hub skin is what it was, and a room attaching or
    // leaving does not change it. This is the bug clint hit on a deploy: a
    // room connecting swapped his theme to its own skin and its leaving
    // reverted it, though he never changed scope off ALL.
    //
    // To catch it, the answer a re-read WOULD give is moved out from under the
    // settled skin: skinFor[""] is changed to a different skin, so any re-fetch
    // of the ALL scope now returns `ember`. A room attaching or detaching must
    // still leave the applied `vapor` alone, because the skin has settled and
    // ALL is not the scope of the room that changed. The old code re-read the
    // skin on every attached-set flip and would repaint to `ember` here.
    await scopeTo(null, "vapor");
    const allWas = mock.skinFor[""];
    mock.skinFor[""] = "ember";
    // Past loadHubRooms' 2s throttle, so the `rooms` event below actually runs
    // its body rather than being coalesced away. Then a room leaves and one
    // attaches: two changes to the attached set, neither of which is the ALL
    // scope the operator is on, so the settled skin must not move.
    await skin.waitForTimeout(2200);
    // The re-read a room flip would do is `bootSkin`, counted. A stream that reopens meanwhile reads settings too,
    // and that read is right to win (the daemon's answer wins), so with one the skin itself is not judged.
    let reopened = 0;
    skin.on("request", r => { if (/\/v1\/events/.test(r.url())) reopened++; });
    await skin.evaluate(() => {
      window.__bootSkins = 0;
      const f = bootSkin;
      window.bootSkin = (...a) => { window.__bootSkins++; return f(...a); };
    });
    mock.sggAttached = false;
    hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
    await skin.waitForTimeout(2200);
    mock.sggAttached = true;
    hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
    await skin.waitForTimeout(500);
    const reread = await skin.evaluate(() => window.__bootSkins);
    if (reread) fail("a room attaching or leaving re-read the settled ALL skin " + reread + " times.");
    if (!reopened && (await dataSkin()) !== "vapor") {
      fail("a room attaching or leaving clobbered the settled ALL skin: it " +
        "became " + JSON.stringify(await dataSkin()) + ", wanted the applied vapor.");
    }
    if (reopened) console.log("skinScope: the event stream reopened " + reopened + " times during the room flips, so the skin itself was not judged");
    mock.skinFor[""] = allWas;
    if (skinErrors.length) {
      fail("the skin page threw uncaught errors: " + skinErrors.join(" | "));
    }
  } finally {
    await skin.close();
    await skinCtx.close();
    mock.hubMode = false;
    mock.sggAttached = false;
    resetSkins();
  }
}

// ── a persisted skin heals when a room attaches, with no reload ──────────
// The board loads against a hub that has no room to borrow settings from
// yet, the window right after a hub restart. The ALL-view `/v1/settings`
// read is a 409, so the load-time skin read fails and the board is on the
// default dark. This is exactly what clint saw: a deploy restarts the hub,
// he reloads before a room is back, and the paper skin never paints. When a
// room attaches the read succeeds, and the skin must heal there rather than
// waiting for another manual reload.
async function skinHealSection(browser, base) {
  mock.hubMode = true;
  mock.hubHasRoom = false;
  mock.sggAttached = false;
  // The hub wears paper, a light skin, which is the one clint set and did not
  // see paint. resetSkins in the finally puts the default back.
  mock.skinFor = { "": "paper", alpha: "moss", sgg: "ember" };
  const healCtx = await browser.newContext();
  const heal = await healCtx.newPage();
  const healErrors = [];
  heal.on("pageerror", e => healErrors.push(String(e)));
  if (process.env.DEBUG_HEADLESS) {
    heal.on("console", m => console.error("[heal] " + m.type() + ": " + m.text()));
  }
  try {
    await heal.goto(base, { waitUntil: "domcontentloaded" });
    // No room to borrow from: the load-time read 409s and the board is dark.
    await heal.waitForTimeout(1500);
    const dark = await heal.evaluate(() =>
      document.documentElement.getAttribute("data-skin"));
    if (dark !== null) {
      fail("with the hub unable to answer settings, the board should be on the " +
        "default, but data-skin was " + JSON.stringify(dark));
    }
    // Past loadHubRooms' 2s throttle, then a room attaches: the read now
    // succeeds and the skin heals to the hub's paper without a reload.
    await heal.waitForTimeout(2200);
    mock.hubHasRoom = true;
    hubStreams.forEach(r => { try { r.write("event: rooms\ndata: {}\n\n"); } catch (e) {} });
    await heal.waitForFunction(() =>
      document.documentElement.getAttribute("data-skin") === "paper", null, { timeout: slow(15000) });
    if (healErrors.length) {
      fail("the skin-heal page threw uncaught errors: " + healErrors.join(" | "));
    }
  } finally {
    await heal.close();
    await healCtx.close();
    mock.hubMode = false;
    mock.hubHasRoom = true;
    resetSkins();
  }
}

async function dropdownsInstantSection(browser, base) {
  const wasHub = mock.hubMode, wasSgg = mock.sggAttached;
  mock.hubMode = true;
  mock.sggAttached = true;
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  try {
    const p = await ctx.newPage();
    const rows = [Object.assign({}, mock.HON, { room: "alpha" }), Object.assign({}, mock.HON, { id: "hon2", label: "gemini", room: "alpha" }),
      Object.assign({}, mock.HON, { room: "sgg" })];
    await p.route(/\/v1\//, async r => {
      await new Promise(x => setTimeout(x, 500));
      if (new URL(r.request().url()).pathname === "/v1/harnesses") {
        await r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ harnesses: rows }) });
      } else await r.continue().catch(() => {});
    });
    await p.goto(base + "/", { waitUntil: "domcontentloaded" });
    await p.waitForFunction(() => typeof hubRooms !== "undefined" && hubRooms.length >= 2, null, { timeout: slow(15000) });
    // Long enough for the 500ms answers to land if the board asks for them at start.
    await p.waitForTimeout(1500);
    const got = await p.evaluate(() => new Promise(done => {
      const t0 = performance.now();
      openLaunch(null, "", "", null, null);
      requestAnimationFrame(() => {
        const opts = id => [...document.querySelectorAll("#" + id + " option")].map(o => o.textContent);
        done({ open: document.getElementById("launch").open, ms: performance.now() - t0,
          room: opts("l-room"), runner: opts("l-harness"), roomShown: !document.getElementById("l-room-field").hidden });
      });
    }));
    if (!got.open) fail("dropdownsInstant: the launch dialog was not open the frame after the click (" + Math.round(got.ms) + "ms)");
    if (!got.roomShown || got.room.length < 2) fail("dropdownsInstant: the room select had " + JSON.stringify(got.room) + " when the dialog showed");
    if (!got.runner.length) fail("dropdownsInstant: the runner select was empty when the dialog showed");
    // Nothing is rebuilt while it is open: a node that was in the DOM at the first frame is still there afterwards.
    const mark = await p.evaluate(() => { const o = document.querySelector("#l-room option"); if (o) o.dataset.mark = "1"; return document.getElementById("l-room").innerHTML; });
    await p.waitForTimeout(1200);
    const after = await p.evaluate(() => ({ marked: !!document.querySelector("#l-room option[data-mark]"), html: document.getElementById("l-room").innerHTML,
      runner: document.getElementById("l-harness").innerHTML }));
    if (!after.marked || after.html !== mark) fail("dropdownsInstant: the room options were rebuilt after the dialog opened");
    if (process.env.DROPDOWNS_SHOTS) {
      await p.waitForFunction(() => document.getElementById("launch").open, null, { timeout: slow(5000) });
      await p.focus("#l-room");
      await p.screenshot({ path: path.join(process.env.DROPDOWNS_SHOTS, "launch-room.png") });
    }
  } finally {
    mock.hubMode = wasHub;
    mock.sggAttached = wasSgg;
    await ctx.close();
  }
  if (!bad) console.log("dropdownsInstant ok");
}

// THE THEME PICKER AND ITS PREVIEW, four things: the picker keeps a theme for one card or for the whole repo; attaching
// never recolours a card; the panel holds only what is acted on; and one card-and-terminal preview serves the themes
// editor, the card colours pane and the panel.
async function themePickerSection(browser, base) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  const errors = [];
  p.on("pageerror", e => errors.push(String(e)));
  await p.addInitScript(() => {
    localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true }));
  });
  const tag = "themePicker: ";
  const was = mock.tasksMode;
  const live = (id, extra) => Object.assign({}, T1, {
    id, display_title: "row " + id, theme: "", supervised: true, pinned: true, worktree: "/tmp/tpk/" + id
  }, extra);
  try {
    mock.wornTasks = [live("pk-a", { worktree: "/Users/me/git/github/acme/widgets/main" }), live("pk-b"), live("pk-c", { theme: "nord" })];
    mock.tasksMode = "worn";
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="pk-a"]', { state: "attached", timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(() => repoColorsHave({ card_colors: { default: "", repos: {} } }));
    const attach = id => p.evaluate(async id => {
      termTask = lastTasks.find(t => t.id === id);
      term = { options: {}, dispose() {} };
      await loadCards().catch(() => {}).then(renderTermList);
      await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
      placeTabBridge();
    }, id);
    const panel = () => p.evaluate(() => {
      const lab = document.getElementById("skinlab");
      const vis = e => !!e && !e.hidden && !!e.offsetParent;
      const first = document.querySelector("#skinlab-pick option");
      return {
        open: !lab.hidden,
        buttons: [...document.querySelectorAll("#skinlab-mine button")].map(b => b.textContent),
        tips: [...lab.querySelectorAll("#skinlab-mine [data-tip], #skinlab-mine [title]")].length,
        first: first && first.textContent,
        hint: vis(document.getElementById("skinlab-hint")), std: vis(document.getElementById("skinlab-std")),
        extra: vis(document.getElementById("skinlab-extra")),
        card: !!document.querySelector("#skinlab-show .tshow-card .card"),
        stamped: !!document.querySelector("#skinlab-show [data-id], #skinlab-show [onclick]"),
        termH: (document.querySelector("#skinlab-show .tshow-term") || { offsetHeight: 0 }).offsetHeight
      };
    });

    // 1 and 3: the panel for a card in a repo.
    await attach("pk-a");
    await p.evaluate(() => pickTheme());
    let r = await panel();
    if (JSON.stringify(r.buttons) !== JSON.stringify(["this card", "every card in github/acme/widgets", "cancel"])) fail(tag + "the panel's buttons are " + JSON.stringify(r.buttons));
    if (r.first !== "plain") fail(tag + "the first option reads '" + r.first + "', not plain.");
    if (r.hint || r.std || r.extra || r.tips) fail(tag + "the panel still has sub-text, tooltips or the old buttons: " + JSON.stringify(r));
    if (!r.card || r.stamped) fail(tag + "the panel has no card, or a card that answers clicks: " + JSON.stringify(r));
    if (r.termH < 200) fail(tag + "the terminal sample is only " + r.termH + "px high.");
    // The card in the panel follows the selection.
    await p.selectOption("#skinlab-pick", "dracula");
    const wore = await p.evaluate(() => document.querySelector("#skinlab-show .tshow-card .card").getAttribute("style") || "");
    if (!/#282a36/i.test(wore) && !wore.includes("--card-0")) fail(tag + "the panel's card did not take dracula: " + wore);
    const termBg = await p.evaluate(() => getComputedStyle(document.querySelector("#skinlab-show .tshow-term")).backgroundColor);
    if (termBg !== "rgb(40, 42, 54)") fail(tag + "the panel's terminal sample is " + termBg + ", not dracula's.");
    // Every card in the repo: written to the card colours, this card left to follow it.
    await p.click("#skinlab-mine button >> text=every card in");
    await p.waitForFunction(() => repoColors.get().repos["github/acme/widgets"] === "dracula", null, { timeout: slow(5000) })
      .catch(() => fail(tag + "every card in the repo did not write card_colors.repos"));
    if (await p.evaluate(() => !!document.querySelector("#skinlab") && !document.getElementById("skinlab").hidden)) fail(tag + "the panel stayed open.");
    const stored = await p.evaluate(async () => (await api("/v1/settings")).card_colors.repos["github/acme/widgets"]);
    if (stored !== "dracula") fail(tag + "the daemon holds '" + stored + "' for the repo.");
    // A card without a repo is offered only itself.
    await attach("pk-b");
    await p.evaluate(() => pickTheme());
    r = await panel();
    if (JSON.stringify(r.buttons) !== JSON.stringify(["this card", "cancel"])) fail(tag + "a card with no repo is offered " + JSON.stringify(r.buttons));
    await p.evaluate(() => cancelLab());
    // And the first option names what it falls back to.
    await attach("pk-a");
    await p.evaluate(() => pickTheme());
    r = await panel();
    if (r.first !== "repo colour: dracula") fail(tag + "with a repo colour the first option reads '" + r.first + "'.");
    await p.evaluate(() => cancelLab());

    // 2: attaching never changes a card. Uncoloured and coloured, in the default settings.
    await p.evaluate(() => repoColorsHave({ card_colors: { default: "", repos: {} } }));
    const look = id => p.evaluate(id => {
      const el = document.querySelector('#term-list .card.tab[data-id="' + id + '"]');
      const s = getComputedStyle(el);
      return { bg: s.backgroundColor, img: s.backgroundImage, edge: s.borderTopColor, title: getComputedStyle(el.querySelector(".title")).color,
        tabc: el.style.getPropertyValue("--tabc") };
    }, id);
    await p.evaluate(() => { termTask = null; term = null; });
    await p.evaluate(async () => { await loadCards().catch(() => {}).then(renderTermList); });
    for (const id of ["pk-b", "pk-c"]) {
      const off = await look(id);
      await attach(id);
      const on = await look(id);
      // A worn row's frame says which one is attached, so its edge may thicken; what the card is painted in may not move.
      for (const k of id === "pk-b" ? ["bg", "img", "edge", "title"] : ["bg", "img", "title"]) {
        if (off[k] !== on[k]) fail(tag + id + " changed " + k + " on attach: " + off[k] + " -> " + on[k]);
      }
      await p.evaluate(() => { termTask = null; term = null; });
      await p.evaluate(async () => { await loadCards().catch(() => {}).then(renderTermList); });
    }
    await attach("pk-b");
    const plainRow = await look("pk-b");
    if (plainRow.tabc) fail(tag + "an uncoloured row carries a tab colour: " + plainRow.tabc);
    const palette = await p.evaluate(() => themeFor(lastTasks.find(t => t.id === "pk-b")));
    if (/^#040b14$/i.test(palette.background) || /^#00e3b0$/i.test(palette.cursor)) fail(tag + "an uncoloured card's terminal is still atrium green: " + JSON.stringify(palette));
    const setDefault = await p.evaluate(() => { repoColorsHave({ card_colors: { default: "nord", repos: {} } }); return themeFor(lastTasks.find(t => t.id === "pk-b")).background; });
    if (setDefault.toLowerCase() !== "#2e3440") fail(tag + "a stored default is not used for an uncoloured card: " + setDefault);
    await p.evaluate(() => repoColorsHave({ card_colors: { default: "", repos: {} } }));

    // 4: one preview, three places.
    await p.evaluate(() => paintSettings());
    await p.evaluate(() => { rcDraft = null; paintRepoColors(true); });
    const rc = await p.evaluate(() => ({ card: !!document.querySelector("#rc-preview .tshow-card .card"), term: !!document.querySelector("#rc-preview .tshow-term"),
      stamped: !!document.querySelector("#rc-preview [data-id], #rc-preview [onclick]") }));
    if (!rc.card || !rc.term || rc.stamped) fail(tag + "the card colours pane's preview is " + JSON.stringify(rc));
    await p.evaluate(() => { rcFocus = "default"; repoColorsPick("default", "dracula"); });
    const rcBg = await p.evaluate(() => getComputedStyle(document.querySelector("#rc-preview .tshow-term")).backgroundColor);
    if (rcBg !== "rgb(40, 42, 54)") fail(tag + "the card colours preview terminal is " + rcBg + ".");
    await p.evaluate(() => { openThemeEditor("nord"); });
    const ed = await p.evaluate(() => ({ card: !!document.querySelector("#th-sample .tshow-card .card"), h: document.querySelector("#th-sample .tshow-term").offsetHeight,
      sub: /attached terminal follows/.test(document.getElementById("theme").textContent) }));
    if (!ed.card || ed.h < 200) fail(tag + "the editor preview is " + JSON.stringify(ed));
    if (ed.sub) fail(tag + "the editor still explains the preview under it.");
    await p.evaluate(() => { const b = document.getElementById("th-c-background"); b.value = "#102030"; b.dispatchEvent(new Event("input")); });
    const live1 = await p.evaluate(() => ({ term: getComputedStyle(document.querySelector("#th-sample .tshow-term")).backgroundColor,
      card: document.querySelector("#th-sample .tshow-card .card").getAttribute("style") }));
    if (live1.term !== "rgb(16, 32, 48)" || !/16,\s*32,\s*48|#102030|rgb\(16, 32, 48\)/i.test(live1.card || "")) fail(tag + "the editor preview did not follow the colours: " + JSON.stringify(live1));
    await p.evaluate(() => document.getElementById("theme").close());
    if (errors.length) fail(tag + "the page threw: " + errors.join(" | "));
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
  if (!bad) console.log("themePicker ok");
}

// NOT IN THE DEFAULT RUN: `THEMEPICKER_SHOTS=/dir HEADLESS_ONLY=themePickerShots`. Writes the three pictures the change is
// judged by, and uses only what both sides of the change have, so it runs before and after.
async function themePickerShotsSection(browser, base) {
  const dir = process.env.THEMEPICKER_SHOTS;
  if (!dir) return;
  const prefix = process.env.THEMEPICKER_PREFIX || "after";
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const p = await ctx.newPage();
  await p.addInitScript(() => localStorage.setItem("atrium.skipconfirm", JSON.stringify({ "width-floor": true })));
  const was = mock.tasksMode;
  const live = (id, extra) => Object.assign({}, T1, { id, display_title: "row " + id, theme: "", supervised: true, pinned: true, worktree: "/tmp/shot/" + id }, extra);
  try {
    mock.wornTasks = [live("sh-a", { display_title: "fix the flaky retry test", worktree: "/Users/me/git/github/acme/widgets/main" }),
      live("sh-b", { display_title: "second session" })];
    mock.tasksMode = "worn";
    await p.goto(base, { waitUntil: "domcontentloaded" });
    await p.waitForSelector("#stack-list .stackrow", { timeout: slow(15000) });
    await p.click('.tab[data-view="terms"]');
    await p.waitForSelector('#term-list .card.tab[data-id="sh-a"]', { state: "attached", timeout: slow(15000) });
    await p.waitForTimeout(1000);
    await p.evaluate(async () => {
      termTask = lastTasks.find(t => t.id === "sh-a");
      term = { options: {}, dispose() {} };
      await loadCards().catch(() => {}).then(renderTermList);
      paintPaneBg(themeFor(termTask));
      await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
      placeTabBridge();
    });
    await p.screenshot({ path: require("path").join(dir, prefix + "-attached-uncoloured.png") });
    await p.evaluate(() => { pickTheme(); const s = document.getElementById("skinlab-pick"); s.value = "nord"; previewLab(s.value); });
    await p.waitForTimeout(300);
    await p.screenshot({ path: require("path").join(dir, prefix + "-picker.png") });
    await p.evaluate(() => { cancelLab(); openThemeEditor("nord"); document.getElementById("th-sample").scrollIntoView({ block: "end" }); });
    await p.waitForTimeout(300);
    await p.screenshot({ path: require("path").join(dir, prefix + "-themes-editor.png") });
  } finally {
    mock.tasksMode = was;
    await ctx.close();
  }
}

async function prefsEverywhereSection(browser, base) {
  mock.soloMode = "ok";
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const errors = [];
  try {
    const a = await ctx.newPage();
    const b = await ctx.newPage();
    for (const pg of [a, b]) pg.on("pageerror", e => errors.push(String(e)));
    await a.goto(base, { waitUntil: "domcontentloaded" });
    await b.goto(base + "/#term=s1", { waitUntil: "domcontentloaded" });
    await b.waitForFunction(() => typeof soloID !== "undefined" && soloID === "s1", null, { timeout: slow(10000) });
    await a.waitForTimeout(800);
    // Counts a navigation, which is what "without a reload" must not do.
    await b.evaluate(() => { window.__noReload = true; });
    const SPAN = slow(4000);
    // Each: [name, run in A, wait in B (returns true once B shows the change)]
    const cases = [
      // Per card: switched in A for the card B shows.
      ["input lag log", () => toggleInputLag(true, "s1"), () => lagOn === true],
      ["input lag log off", () => toggleInputLag(false, "s1"), () => lagOn === false],
      ["typing gate readout", () => toggleTypingReadout(true, "s1"), () => typingOn === true && document.getElementById("s-typing").checked],
      ["typing gate readout off", () => toggleTypingReadout(false, "s1"), () => typingOn === false],
      ["copy on select", () => toggleCopyOnSelect(), () => copyOnSelect === true],
      ["focus on hover", () => toggleHoverFocus(true), () => hoverFocus === true && document.getElementById("s-hoverfocus").checked],
      ["card colours", () => toggleCardColors(true), () => cardColors === true && document.getElementById("s-cardcolors").checked],
      ["terminal row wear: exited", () => toggleTermWear("exited", true), () => termWearOn.exited === true && document.getElementById("s-termwear-exited").checked],
      ["terminal row wear: selected", () => toggleTermWear("selected", false), () => termWearOn.selected === false],
      ["text size", () => { const e = document.getElementById("s-cardsize"); e.value = "1.45"; e.dispatchEvent(new Event("change")); },
        () => getComputedStyle(document.documentElement).getPropertyValue("--uiscale").trim() === "1.45" && document.getElementById("s-cardsize").value === "1.45"],
      ["whitespace", () => { const e = document.getElementById("s-density"); e.value = "0.72"; e.dispatchEvent(new Event("change")); },
        () => getComputedStyle(document.documentElement).getPropertyValue("--density").trim() === "0.72" && document.getElementById("s-density").value === "0.72"],
      ["terminal list sort", () => setTermSortMode("started"), () => termSortStarted === true && sortByActivity === false],
      ["board sort", () => setBoardSort("name"), () => boardSortMode() === "name"],
      ["stack filter", () => setStackShow(new Set(["done", "needs-input"])), () => stackShow().has("done") && stackShow().size === 2],
      ["terminal list mode", () => setTermListMode("mini"), () => termListMode === "mini" && document.getElementById("term-layout").classList.contains("tl-mini")],
      ["terminals subagents chip", () => toggleTermShow("subagents"),
        () => termSubsOn() === true && document.querySelector('#term-show button[data-v="subagents"]').classList.contains("on")],
      ["terminals paused chip", () => toggleTermShow("paused"), () => termShow().has("paused") && termSubsOn()],
      ["terminals kept hidden", () => setTermKeep(KEEP_N_KEY, 3), () => document.getElementById("s-termkeep").value === "3"],
      ["sound mute", () => alerting.set({ muted: true }), () => alerting.get().muted === true],
      ["sound volume", () => alerting.set({ volume: 0.9 }), () => alerting.get().volume === 0.9 && document.getElementById("s-vol").value === "90"],
      ["growler", () => setGrowler(true), () => growlOn() === true && document.getElementById("s-growler").checked],
      ["switcher key", () => localStorage.setItem(SWITCH_KEY_STORE, "alt+KeyJ"), () => /j$/.test(document.getElementById("s-switchkey").textContent)],
    ];
    for (const [name, run, seen] of cases) {
      await a.evaluate(run);
      try {
        await b.waitForFunction(seen, null, { timeout: SPAN });
      } catch (e) {
        fail("prefsEverywhere: " + name + " changed in the board and the popped-out window did not follow");
      }
      // And the other way, board following the popout, for the ones the pop-out itself can flip.
    }
    // The other direction: the popped-out window changes one, the board follows.
    await b.evaluate(() => toggleCopyOnSelect());
    try { await a.waitForFunction(() => copyOnSelect === false, null, { timeout: SPAN }); }
    catch (e) { fail("prefsEverywhere: copy on select changed in the popped-out window and the board did not follow"); }
    // A cleared store puts every setting back to its default in the other window.
    await a.evaluate(() => localStorage.clear());
    try { await b.waitForFunction(() => cardColors === false && hoverFocus === false && boardSortMode() === "activity", null, { timeout: SPAN }); }
    catch (e) { fail("prefsEverywhere: clearing the browser's storage did not put the other window's settings back"); }
    if (!(await b.evaluate(() => window.__noReload === true))) fail("prefsEverywhere: the popped-out window reloaded");
    if (errors.length) fail("prefsEverywhere: page errors: " + errors.join(" | "));
  } finally {
    await ctx.close();
  }
  if (!bad) console.log("prefsEverywhere ok");
}

const units = [
  { name: "skinScope", run: ({ browser, base }) => skinScopeSection(browser, base) },
  { name: "skinHeal", run: ({ browser, base }) => skinHealSection(browser, base) },
  { name: "settingsOnce", run: ({ browser, base }) => settingsOnceSection(browser, base) },
  { name: "websiteSkin", run: ({ browser, base }) => websiteSkinSection(browser, base) },
  { name: "prefsEverywhere", run: ({ browser, base }) => prefsEverywhereSection(browser, base) },
  { name: "themePreview", run: ({ browser, base }) => themePreviewSection(browser, base) },
  { name: "themeLab", run: ({ browser, base }) => themeLabSection(browser, base) },
  { name: "alias", run: ({ browser, base }) => aliasSection(browser, base) },
  { name: "readTabSize", run: ({ browser, base }) => readTabSizeSection(browser, base) },
  { name: "readWidthSkin", run: ({ browser, base }) => readWidthSkinSection(browser, base) },
  { name: "themePicker", run: ({ browser, base }) => themePickerSection(browser, base) },
  { name: "themePickerShots", run: ({ browser, base }) => themePickerShotsSection(browser, base) },
  { name: "dropdownsInstant", run: ({ browser, base }) => dropdownsInstantSection(browser, base) },
];
const sections = { skinScope: skinScopeSection, skinHeal: skinHealSection, settingsOnce: settingsOnceSection, prefsEverywhere: prefsEverywhereSection, themePreview: themePreviewSection, themeLab: themeLabSection, alias: aliasSection, readTabSize: readTabSizeSection, readWidthSkin: readWidthSkinSection, themePicker: themePickerSection, themePickerShots: themePickerShotsSection, dropdownsInstant: dropdownsInstantSection };

module.exports = { units, sections };
if (require.main === module) require("../lib/alone.js")(units);
