// Every card colour on every skin, the chat view drawn from the real stylesheets, every text against what it sits on, AA or fail.
const fs = require("fs");
const path = require("path");
const { SHEETS, BODY } = require("./chat-fixture.js");
const pairs = require("./chat-contrast.js");

async function chatColours(browser, base, webRoot) {
  const skins = [...fs.readFileSync(path.join(webRoot, "css/themes.css"), "utf8").matchAll(/:root\[data-skin="([a-z]+)"\]\s*\{/g)].map(m => m[1]);
  const page = await browser.newPage();
  try {
    await page.goto(base + "/css/tokens.css");
    await page.setContent('<!doctype html><html><head>' + SHEETS.map(h => '<link rel="stylesheet" href="' + base + "/" + h + '">').join("") +
      '<style>*,*::before,*::after{transition:none!important;animation:none!important}</style><script>var prefLive = function () { return {}; };</script><script src="' + base + '/js/themes.js"></script></head><body class="m chat-embed">' + BODY + "</body></html>");
    await page.waitForFunction(() => document.styleSheets.length >= 10 && typeof wearOf === "function", null, { timeout: 15000 });
    return await page.evaluate(([skins, fn]) => {
      const run = (new Function("return " + fn))();
      const names = Object.keys(allThemes());
      const bad = [];
      const probe = document.body.appendChild(document.createElement("div"));
      let total = 0;
      const min = n => /svg|mic|send$|attach/.test(n) && !/disabled/.test(n) ? 3 : 4.5;
      for (const skin of skins.concat([""])) {
        if (skin) document.documentElement.setAttribute("data-skin", skin); else document.documentElement.removeAttribute("data-skin");
        for (const name of [""].concat(names)) {
          const body = document.body;
          body.removeAttribute("style");
          body.classList.remove("worn");
          const w = name && wearOf(allThemes()[name]);
          if (w) { body.setAttribute("style", w.style); body.classList.add("worn"); }
          probe.style.background = w ? "var(--card-0)" : "var(--bg-1)";
          body.dataset.pane = getComputedStyle(probe).backgroundColor;
          for (const p of run()) {
            total++;
            if (p.ratio < min(p.name)) bad.push({ skin: skin || "(none)", card: name || "(no colour)", name: p.name, ratio: Math.round(p.ratio * 100) / 100 });
          }
        }
      }
      return { bad, total };
    }, [skins, pairs.toString()]);
  } finally {
    await page.close();
  }
}
module.exports = { chatColours };
