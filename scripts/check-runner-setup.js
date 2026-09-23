// Check the runner setup chip and dialog, against the real page, without a browser.
//
// The setup report rides on each runner row and the board draws it. None of the
// ways this breaks is a syntax error: a dialog id that moved, a chip that stops
// being drawn, a fix button that posts somewhere else, or an explained fix that
// loses its command. So the functions are pulled out of the page and RUN
// against a fake row, and what they draw is checked.
const fs = require("fs");
const vm = require("vm");

const html = fs.readFileSync(process.argv[2], "utf8");
let fail = false;
const bad = msg => { console.error("FAIL: " + msg); fail = true; };

for (const id of ["runner-setup", "runner-setup-status", "runner-setup-title"]) {
  if (!html.includes(`id="${id}"`)) bad(`the page has no #${id}`);
}
if (!/hookcell[^]*?setupChip\(h\)/.test(html)) bad("the runner row no longer draws setupChip");

// Pull one top-level function's source out of the page by name.
function fnSource(name) {
  const re = new RegExp(`(?:async )?function ${name}\\(`);
  const m = re.exec(html);
  if (!m) { bad(`no function ${name}`); return ""; }
  let i = html.indexOf("{", m.index), depth = 0;
  for (; i < html.length; i++) {
    if (html[i] === "{") depth++;
    else if (html[i] === "}" && --depth === 0) break;
  }
  return html.slice(m.index, i + 1);
}
const constMatch = /const SETUP_STATE = [^;]*;/.exec(html);
if (!constMatch) bad("no SETUP_STATE");

const host = { html: "" };
const els = {
  "runner-setup": { dataset: {}, open: false, showModal() { this.open = true; } },
  "runner-setup-status": host,
  "runner-setup-title": { textContent: "" },
};
const posted = [];
const ctx = {
  esc: s => String(s == null ? "" : s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])),
  rowOf: (list, id, room) => list.find(x => x.id === id && (!room || (x.room || "") === room)),
  setHTML: (el, s) => { el.html = s; },
  document: { getElementById: id => els[id] },
  confirmUser: async () => true,
  chooseWriteRoom: async () => true,
  api: async (url, opts) => { posted.push({ url, body: JSON.parse(opts.body) }); return { report: allHarnesses[0].setup, changed: true, backup: "x.bak" }; },
  toast: () => {}, tellUser: () => {}, refresh: () => {},
  JSON,
};
const allHarnesses = [{
  id: "gem", label: "gemini", room: "",
  setup: {
    adapter: "gemini", installed: true, exe: "C:/npm/gemini.cmd", version: "0.60.0", failing: 2,
    checks: [
      { id: "trust", label: "trusts the workspace", state: "fail", detail: "untrusted", fix: "apply",
        fix_label: "trust it", targets: ["D:/worktrees", "D:/git"], path: "C:/h/.gemini/trustedFolders.json" },
      { id: "auth", label: "signed in", state: "fail", detail: "no sign-in", fix: "explain", command: "gemini" },
      { id: "hooks", label: "atrium hooks", state: "n/a", detail: "none" },
    ],
  },
}];
ctx.allHarnesses = allHarnesses;
vm.createContext(ctx);
vm.runInContext([constMatch && constMatch[0], ...["setupChip", "openRunnerSetup", "setupCheckRow",
  "renderRunnerSetup", "fixRunnerSetup"].map(fnSource)].join("\n") + "\nthis.__=1;", ctx);

(async () => {
  const chip = ctx.setupChip(allHarnesses[0]);
  if (!/setup: 2 to fix/.test(chip)) bad("the chip does not count failing checks: " + chip);
  if (ctx.setupChip({ id: "x" }) !== "") bad("a row with no setup draws a chip");
  const okChip = ctx.setupChip({ id: "y", setup: { adapter: "claude", failing: 0 } });
  if (!/setup ok/.test(okChip)) bad("a passing row does not read setup ok");

  ctx.openRunnerSetup("gem", "");
  const out = host.html;
  if ((out.match(/data-setup-fix="trust"/g) || []).length !== 2) bad("an apply fix should draw one button per target");
  if (!/<code class="grow">gemini<\/code>/.test(out)) bad("an explained fix lost its command");
  if (/data-setup-fix="auth"/.test(out)) bad("sign-in drew a fix button. it must only be explained");
  if (/data-setup-fix="hooks"/.test(out)) bad("an n/a check drew a fix button");
  if (!/installed 0\.60\.0/.test(out)) bad("the dialog does not show the version");

  await ctx.fixRunnerSetup("trust", "D:/git");
  const p = posted[0];
  if (!p || p.url !== "/v1/harnesses/gem/setup/fix" || p.body.check !== "trust" || p.body.target !== "D:/git") {
    bad("the fix posted the wrong thing: " + JSON.stringify(p));
  }

  if (fail) process.exit(1);
  console.log("runner setup: chip, dialog, apply and explain fixes all draw and post correctly.");
})();
