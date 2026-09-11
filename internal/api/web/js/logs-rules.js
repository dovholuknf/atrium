// ── sorting the decision log ────────────────────────────
// Newest first is the right default and was the only option, with nothing on
// screen saying so. Four columns are worth sorting by, and which one is active
// is shown in the header rather than left to be guessed.
const HIST_SORTS = {
  when: { label: "when", cmp: (a, b) => (b.decided_at || "").localeCompare(a.decided_at || "") },
  agent: { label: "agent", cmp: (a, b) => (a.agent || "").localeCompare(b.agent || "") },
  tool: { label: "tool", cmp: (a, b) => (a.tool || "").localeCompare(b.tool || "") },
  command: { label: "command", cmp: (a, b) => (a.command || "").localeCompare(b.command || "") },
  answer: { label: "answer", cmp: (a, b) => (a.decided_by || "").localeCompare(b.decided_by || "") }
};
let histSort = "when";
let histDesc = false;

function sortHistory(list) {
  const s = HIST_SORTS[histSort] || HIST_SORTS.when;
  list.sort(s.cmp);
  // `when` is already newest first, so reversing it means oldest first.
  if (histDesc) list.reverse();
}

function setHistSort(key) {
  if (histSort === key) histDesc = !histDesc;
  else { histSort = key; histDesc = false; }
  paintHistSort();
  paintHistory();
}

function paintHistSort() {
  document.getElementById("hist-sort").innerHTML =
    Object.entries(HIST_SORTS).map(([k, s]) =>
      `<button class="${k === histSort ? "on" : ""}" onclick="setHistSort('${k}')"
        title="sort by ${s.label}${k === histSort ? ", again to reverse" : ""}"
        >${s.label}${k === histSort ? (histDesc ? " ↑" : " ↓") : ""}</button>`
    ).join("");
}

// hintsFor offers a few ready-made patterns per request, so widening a rule to
// a whole repo is one click instead of typing a path by hand.
// Colors the change block. The hook marks removals and additions with the
// same --- and +++ a diff uses, so the lines under each marker are tinted
// accordingly and a change is readable at a glance.
// The hook sends the whole before and after text. Tinting both wholesale makes
// a one word change look like a hundred line rewrite, so the two are diffed
// here and only the lines that actually differ are marked. Unchanged context
// stays dim, and within a replaced line the changed words are picked out.
function splitChange(text) {
  // Scanned line by line rather than matched with a regex: an end anchor under
  // the multiline flag matches at every line break, which silently truncated
  // the removed block to nothing and made every line look added.
  const hunks = [];
  let cur = null, into = null;
  for (const line of (text || "").split("\n")) {
    if (line === "=== next edit ===") { cur = null; into = null; continue; }
    if (line === "--- removing") {
      cur = { old: [], now: [] };
      hunks.push(cur);
      into = "old";
      continue;
    }
    if (line === "+++ adding" || line === "+++ writing") {
      if (!cur) { cur = { old: [], now: [] }; hunks.push(cur); }
      into = "now";
      continue;
    }
    if (cur && into) cur[into].push(line);
  }
  return hunks.map(h => ({ old: h.old.join("\n"), now: h.now.join("\n") }));
}

// Longest common subsequence over lines, walked back into a unified list.
function lineDiff(a, b) {
  const n = a.length, m = b.length;
  // Guard against a pathological diff on a huge write.
  if (n * m > 4000000) {
    return [...a.map(t => ({ t: "del", s: t })), ...b.map(t => ({ t: "add", s: t }))];
  }
  const dp = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push({ t: "same", s: a[i] }); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) { out.push({ t: "del", s: a[i++] }); }
    else { out.push({ t: "add", s: b[j++] }); }
  }
  while (i < n) out.push({ t: "del", s: a[i++] });
  while (j < m) out.push({ t: "add", s: b[j++] });
  return out;
}

// Marks the words that differ between a removed line and the line that
// replaced it, so the eye lands on the edit rather than the line.
function wordEmphasis(oldLine, newLine) {
  const split = s => s.split(/(\s+)/);
  const a = split(oldLine), b = split(newLine);
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head &&
         a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const wrap = (parts) => esc(parts.slice(0, head).join("")) +
    `<b>${esc(parts.slice(head, parts.length - tail).join(""))}</b>` +
    esc(parts.slice(parts.length - tail).join(""));
  // No shared edges means the whole line changed, so emphasis adds nothing.
  if (head === 0 && tail === 0) return [esc(oldLine), esc(newLine)];
  return [wrap(a), wrap(b)];
}

function diffHTML(text) {
  const hunks = splitChange(text);
  // Anything that is not a before and after pair is shown as it arrived.
  if (!hunks.length) {
    return (text || "").split("\n").map(l => `<i>${esc(l) || " "}</i>`).join("");
  }

  return hunks.map((h, idx) => {
    const ops = lineDiff(h.old.split("\n"), h.now.split("\n"));
    let html = idx ? `<i class="hdr">=== next edit ===</i>` : "";
    const line = (cls, s) => `<i class="${cls}">${s || " "}</i>`;

    for (let k = 0; k < ops.length; k++) {
      if (ops[k].t === "same") {
        html += line("ctx", esc(ops[k].s));
        continue;
      }
      // Gather the whole run of removals, then the whole run of additions that
      // follows. Pairing only a single removal with a single addition missed
      // every multi line replacement, which is most real edits, and left them
      // as two undifferentiated blocks of red and green.
      const dels = [];
      while (k < ops.length && ops[k].t === "del") dels.push(ops[k++].s);
      const adds = [];
      while (k < ops.length && ops[k].t === "add") adds.push(ops[k++].s);
      k--;

      // Interleave them so a replaced line sits directly above its
      // replacement, and mark the words that differ in each pair.
      const paired = Math.min(dels.length, adds.length);
      for (let p = 0; p < paired; p++) {
        const [l, r] = wordEmphasis(dels[p], adds[p]);
        html += line("del", l) + line("add", r);
      }
      for (let p = paired; p < dels.length; p++) html += line("del", esc(dels[p]));
      for (let p = paired; p < adds.length; p++) html += line("add", esc(adds[p]));
    }
    return html;
  }).join("");
}

// hintsFor offers ready-made patterns, ordered the way the path reads: broad
// on the left, narrow on the right. Every one carries an explicit trailing *
// so what you see is what it matches.
// Directories mentioned anywhere in a command, shallowest first, so "let it
// work in here" is one click.
//
// As a command pattern the same thing means writing a glob that accounts for
// the quoting around the path, and `rm -f "C:/x/*"` silently fails to match
// `rm -f "C:/x/y.db"` over the closing quote alone.
function dirsIn(command) {
  const out = [];
  const seen = new Set();
  // Quoted runs first, since a path with spaces only survives in quotes.
  const tokens = (command.match(/"[^"]+"|'[^']+'|\S+/g) || [])
    .map(t => t.replace(/^["']|["']$/g, "").replace(/\\/g, "/"));
  for (const t of tokens) {
    if (!t.includes("/")) continue;
    // A url is not a directory on this machine.
    if (/^[a-z]+:\/\//i.test(t)) continue;
    const parts = t.split("/");
    // The last segment is a file or the directory itself, so its parent is the
    // shallowest useful rule and the walk covers the rest.
    for (let i = 2; i < parts.length; i++) {
      const dir = parts.slice(0, i).join("/");
      // Too short to be anything but a drive or a root.
      if (dir.replace(/[/:]/g, "").length < 2) continue;
      if (seen.has(dir)) continue;
      seen.add(dir);
      out.push(dir);
    }
  }
  // Deepest last, so the list reads outward. Capped, so a command naming ten
  // paths does not bury the buttons that decide it.
  return out.slice(0, 4);
}

function hintsFor(p) {
  const cmd = (p.command || "").replace(/\\/g, "/");
  const out = [];
  // Offered for every tool: "anywhere under this folder" reads the same for a
  // shell command and a file edit.
  for (const dir of dirsIn(cmd)) {
    const leaf = dir.split("/").filter(Boolean).pop() || dir;
    out.push({
      value: dir, kind: "path", label: leaf, icon: "\u{1F4C1}",
      why: "anything anywhere under " + dir + ", whatever the command"
    });
  }
  if (FILE_TOOLS.includes(p.tool)) {
    const parts = cmd.split(/\s+/)[0].split("/");
    // Shallowest first, so it reads left to right like the path does.
    for (let i = 2; i <= parts.length - 1; i++) {
      const dir = parts.slice(0, i).join("/") + "/";
      out.push({ value: dir + "*", label: parts[i - 1] + "/*", why: "anything under " + dir });
    }
    const ext = (cmd.split(/\s+/)[0].split(".").pop() || "").replace(/[^a-z0-9]/gi, "");
    if (ext) out.push({ value: "*." + ext + " <- *", label: "*." + ext, why: "any ." + ext + " file, anywhere" });
  } else {
    const words = cmd.trim().split(/\s+/);
    if (words[0]) out.push({ value: words[0] + " *", label: words[0] + " *", why: "any " + words[0] + " command" });
    if (words.length > 1 && !words[1].startsWith("-")) {
      out.push({ value: words[0] + " " + words[1] + " *", label: words[0] + " " + words[1] + " *",
        why: "any " + words[0] + " " + words[1] + " command" });
    }
  }
  const seen = new Set();
  return out.filter(h => h.value && !seen.has(h.kind + h.value) && seen.add(h.kind + h.value));
}


// prefixOf mirrors the server's default so the tooltip tells you exactly what
// "always" will cover. File tools get their directory, commands get two words.
const FILE_TOOLS = ["Edit", "Write", "Read", "MultiEdit", "NotebookEdit"];
function prefixOf(tool, command) {
  const f = (command || "").trim().split(/\s+/);
  if (!f[0]) return "";
  if (FILE_TOOLS.includes(tool)) {
    const path = f[0].replace(/\\/g, "/");
    const i = path.lastIndexOf("/");
    return i > 0 ? path.slice(0, i + 1) : path;
  }
  if (f.length < 2) return f[0];
  return f[1].startsWith("-") ? f[0] : f[0] + " " + f[1];
}

const RULE_TOOLBAR = `
  <div class="toolbar">
    <button class="go big" onclick="allowFolder()"
      title="stop asking about anything under a folder, whatever the command">
      &#128193;&nbsp; allow a folder</button>
    <button class="go big" onclick="importClaude()"
      title="read your Claude Code allow and deny lists and turn them into standing rules">
      &#8681;&nbsp; import rules from claude</button>
    <button onclick="exportRules()" title="download every rule as json">export</button>
    <button onclick="document.getElementById('rules-file').click()" title="load rules from a json file">import file</button>
    <input type="file" id="rules-file" accept="application/json" hidden onchange="importFile(this)">
  </div>`;

// Dates on a rule and a decision answer "when did I agree to this". Absolute
// for anything older than a day, relative while it is still fresh.
// Always both: the timestamp you can read or copy, and how long ago that was.
// Showing only one of the two makes you do arithmetic.
function when(iso) {
  if (!iso) return "";
  const t = new Date(iso.endsWith("Z") ? iso : iso + "Z");
  if (isNaN(t)) return esc(iso);
  const pad = n => String(n).padStart(2, "0");
  const sameDay = t.toDateString() === new Date().toDateString();
  const stamp = (sameDay ? "" : `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} `)
    + `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`;
  const secs = (Date.now() - t) / 1000;
  const rel = secs < 60 ? "just now"
    : secs < 3600 ? Math.floor(secs / 60) + "m ago"
    : secs < 86400 ? Math.floor(secs / 3600) + "h ago"
    : Math.floor(secs / 86400) + "d ago";
  return `<time datetime="${esc(iso)}">${stamp}</time> <i>${rel}</i>`;
}

let allRules = [];

async function renderRules() {
  try { allRules = (await api("/v1/rules")).rules || []; } catch (e) { return; }
  document.getElementById("rules-toolbar").innerHTML = RULE_TOOLBAR;
  document.getElementById("rules-count").textContent = allRules.length;
  paintRuleSort();
  paintRules();
}

// ── sorting the rules ───────────────────────────────────
// Most used first was the only order and nothing said so. With a hundred and
// thirty rules, "which of these has never fired" and "what did I add last" are
// both questions the list could not answer.
const RULE_SORTS = {
  hits: { label: "most used", cmp: (a, b) => (b.hits || 0) - (a.hits || 0) },
  newest: { label: "newest", cmp: (a, b) => (b.created_at || "").localeCompare(a.created_at || "") },
  tool: { label: "tool", cmp: (a, b) => (a.tool || "").localeCompare(b.tool || "") },
  pattern: { label: "pattern", cmp: (a, b) => (a.prefix || "").localeCompare(b.prefix || "") },
  kind: { label: "kind", cmp: (a, b) => (a.kind || "").localeCompare(b.kind || "") }
};
let ruleSort = "hits";
let ruleDesc = false;

function sortRules(list) {
  const s = RULE_SORTS[ruleSort] || RULE_SORTS.hits;
  list.sort(s.cmp);
  if (ruleDesc) list.reverse();
}

function setRuleSort(key) {
  if (ruleSort === key) ruleDesc = !ruleDesc;
  else { ruleSort = key; ruleDesc = false; }
  paintRuleSort();
  paintRules();
}

function paintRuleSort() {
  const host = document.getElementById("rules-sort");
  if (!host) return;
  host.innerHTML = Object.entries(RULE_SORTS).map(([k, s]) =>
    `<button class="${k === ruleSort ? "on" : ""}" onclick="setRuleSort('${k}')"
      title="sort by ${s.label}${k === ruleSort ? ", again to reverse" : ""}"
      >${s.label}${k === ruleSort ? (ruleDesc ? " ↑" : " ↓") : ""}</button>`).join("");
}

function paintRules() {
  const q = (document.getElementById("rules-q").value || "").toLowerCase();
  const only = document.querySelector("#rules-seg .on").dataset.v;
  const list = allRules.filter(r =>
    (!only || r.decision === only) &&
    (!q || (r.tool + " " + r.prefix + " " + (r.reason || "")).toLowerCase().includes(q)));

  document.getElementById("rules-match").textContent =
    list.length === allRules.length ? "" : `${list.length} of ${allRules.length}`;

  sortRules(list);

  setHTML(document.getElementById("rules-list"), list.length
    ? `<div class="panel">` + list.map(r => `
        <div class="row line">
          <span class="chip ${r.decision === "approve" ? "accent" : "warn"}">${r.decision}</span>
          <span class="stamp">${when(r.created_at)}</span>
          <span class="tool">${esc(r.tool)}</span>
          <code class="grow ell ${r.kind === "path" ? "path" : ""}"
            title="${esc(r.prefix)}">${r.kind === "path" ? "\u{1F4C1} " : ""}${esc(r.prefix)}</code>
          <span class="by">${r.kind === "path" ? "folder"
            : /[*?]/.test(r.prefix) ? "wildcard" : "prefix"}</span>
          <span class="by">${r.hits} use${r.hits === 1 ? "" : "s"}</span>
          <button class="no" onclick="dropRule('${r.id}')">forget</button>
        </div>`).join("") + `</div>`
    : `<div class="panel"><div class="empty">${allRules.length
        ? "nothing matches that filter"
        : "no rules yet. import from claude to start with the ones you already trust."}</div></div>`);
}

// Keeps a collapsible's open state across reloads.
function rememberOpen(id, key) {
  const d = document.getElementById(id);
  if (!d) return;
  if (localStorage.getItem(key) === "1") d.open = true;
  d.addEventListener("toggle", () => localStorage.setItem(key, d.open ? "1" : "0"));
}

// Wire the filter controls once. They live in the static shell, so they keep
// focus and content across every refresh.
function wireFilters(seg, input, repaint) {
  wireSeg(seg, repaint);
  if (input) document.getElementById(input).addEventListener("input", repaint);
}

// A segment with no search box beside it.
function wireSeg(seg, repaint) {
  document.querySelectorAll(`#${seg} button`).forEach(b => b.addEventListener("click", () => {
    document.querySelectorAll(`#${seg} button`).forEach(x => x.classList.toggle("on", x === b));
    repaint();
  }));
}

// "Let it work in here, stop asking."
//
// A rule could otherwise only be born from a request just read, through always
// or never, which misses the case where the answer is already known.
//
// A folder rule rather than a command glob: the glob has to account for the
// quoting around the path, and `rm -f "C:/x/*"` silently fails against
// `rm -f "C:/x/y.db"` over the closing quote alone.
async function allowFolder() {
  const dir = await askText("allow a folder",
    "Every request that touches anything under this folder stops asking, " +
    "whatever the command and however the path is quoted." +
    "<br><br>Name the narrowest folder that covers the work.",
    "", "D:/git/github/dovholuknf/atrium");
  if (dir === null || !dir.trim()) return;

  const tools = await askUser({
    title: "which tools",
    body: `Under <code>${esc(dir.trim())}</code>.`,
    buttons: [
      { label: "everything", value: "all", style: "go" },
      { label: "just file edits", value: "files" },
      { label: "just shell commands", value: "bash" }
    ]
  });
  if (tools === null) return;

  const which = tools === "bash" ? ["Bash"]
    : tools === "files" ? FILE_TOOLS
    : ["Bash", ...FILE_TOOLS];

  let made = 0;
  for (const tool of which) {
    try {
      await api("/v1/rules", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          tool, kind: "path", prefix: dir.trim(), decision: "approve",
          reason: "allowed this folder"
        })
      });
      made++;
    } catch (e) {
      toast("could not write the rule", e.message);
      return;
    }
  }
  toast(`${dir.trim()} is allowed`,
    `${made} rule${made === 1 ? "" : "s"} written, nothing under it will ask again`);
  refresh();
}

async function dropRule(id) {
  try { await api(`/v1/rules/${id}`, { method: "DELETE" }); } catch (e) { tellUser("atrium", e.message); }
  refresh();
}

// Import shows what it would do before it does it, because a rule set is not
// something to take on trust.
async function importClaude() {
  let preview;
  try { preview = await api("/v1/rules/preview-claude"); } catch (e) { tellUser("atrium", e.message); return; }
  const rules = preview.rules || [], skipped = preview.skipped || [];
  const broad = rules.filter(r => r.broad);
  const normal = rules.length - broad.length;
  if (!rules.length) {
    tellUser("nothing to import", "looked in:\n" + (preview.sources || []).join("\n"));
    return;
  }
  let body = `Read from:<code>${esc((preview.sources || []).join("\n"))}</code>`
    + `<code>` + esc(rules.filter(r => !r.broad).slice(0, 12)
        .map(r => `${r.decision === "block" ? "block" : "allow"}  ${r.tool} ${r.pattern}`).join("\n"))
    + (normal > 12 ? esc(`\n...and ${normal - 12} more`) : "") + `</code>`;
  if (skipped.length) {
    body += `${skipped.length} entries cannot be translated and will be left alone.`;
  }
  if (!await confirmUser(`import ${normal} rule${normal === 1 ? "" : "s"} from claude?`,
    body, "import them")) return;

  let includeBroad = false;
  if (broad.length) {
    includeBroad = await confirmUser("some of these match everything",
      `${broad.length} of them match <b>every</b> request for their tool:` +
      `<code>${esc(broad.map(r => `${r.decision}  ${r.tool}`).join("\n"))}</code>` +
      "Including them stops atrium asking about those tools at all.",
      "include them too");
  }
  await runImport({ source: "claude", include_broad: includeBroad });
}

async function importFile(input) {
  const file = input.files && input.files[0];
  input.value = "";
  if (!file) return;
  let parsed;
  try { parsed = JSON.parse(await file.text()); } catch (e) { tellUser("not valid json", e.message); return; }
  const rules = parsed.rules || parsed;
  if (!Array.isArray(rules)) { tellUser("atrium", "expected a rules array"); return; }
  if (!await confirmUser("import from a file",
    `${rules.length} rule${rules.length === 1 ? "" : "s"} from <b>${esc(file.name)}</b>.`,
    "import them")) return;
  await runImport({ source: "json", rules, include_broad: true });
}

async function runImport(body) {
  try {
    const res = await api("/v1/rules/import", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body)
    });
    const failed = res.failed || [];
    // A clean import is a toast: you answered a dialog to get here and the
    // rules list is about to show the result. A partial one is a dialog,
    // because a list of patterns that did not make it has to be read.
    if (!failed.length) {
      toast("imported", `added ${res.added}, updated ${res.updated}`);
    } else {
      tellUser(`imported ${res.added + res.updated}, ${failed.length} failed`,
        `<code>${esc(failed.slice(0, 8)
          .map(f => `${f.tool} ${f.pattern}: ${f.error}`).join("\n"))}</code>`);
    }
  } catch (e) { toast("import failed", e.message); }
  refresh();
}

function exportRules() {
  window.location = "/v1/rules/export";
}

// Exports what is currently on screen, filters included, so a search then an
// export gives you exactly the slice you were looking at.
function download(name, text, mime) {
  const url = URL.createObjectURL(new Blob([text], { type: mime }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 2000);
}

function toCSV(rows, cols) {
  const cell = v => {
    const s = v == null ? "" : String(v);
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  return [cols.join(","), ...rows.map(r => cols.map(c => cell(r[c])).join(","))].join("\n");
}

function stamp() {
  const d = new Date(), p = n => String(n).padStart(2, "0");
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}`;
}

function filteredHistory() {
  const q = (document.getElementById("hist-q").value || "").toLowerCase();
  const only = document.querySelector("#hist-seg .on").dataset.v;
  const byRule = p => p.decided_by && p.decided_by !== "you";
  return allHistory.filter(p => {
    if (only === "rule" && !byRule(p)) return false;
    if (only === "you" && byRule(p)) return false;
    if ((only === "approve" || only === "block") && p.decision !== only) return false;
    // The agent is searchable too, so "show me everything the deploy session
    // did" is one filter rather than a scan.
    return !q || (p.tool + " " + p.command + " " + (p.decided_by || "") + " " +
      (p.agent || "")).toLowerCase().includes(q);
  });
}

function exportHistory(fmt) {
  const rows = filteredHistory();
  if (!rows.length) { tellUser("atrium", "nothing to export"); return; }
  if (fmt === "csv") {
    download(`atrium-decisions-${stamp()}.csv`,
      toCSV(rows, ["decided_at", "agent", "decision", "tool", "command", "decided_by", "reason"]),
      "text/csv");
    return;
  }
  download(`atrium-decisions-${stamp()}.json`, JSON.stringify({ permissions: rows }, null, 2),
    "application/json");
}

function exportRulesView(fmt) {
  const q = (document.getElementById("rules-q").value || "").toLowerCase();
  const only = document.querySelector("#rules-seg .on").dataset.v;
  const rows = allRules.filter(r =>
    (!only || r.decision === only) &&
    (!q || (r.tool + " " + r.prefix + " " + (r.reason || "")).toLowerCase().includes(q)));
  if (!rows.length) { tellUser("atrium", "nothing to export"); return; }
  if (fmt === "csv") {
    download(`atrium-rules-${stamp()}.csv`,
      toCSV(rows, ["created_at", "decision", "tool", "prefix", "hits", "reason"]), "text/csv");
    return;
  }
  // Shaped so it can be handed straight back to import file.
  download(`atrium-rules-${stamp()}.json`, JSON.stringify({
    rules: rows.map(r => ({ tool: r.tool, pattern: r.prefix, decision: r.decision, source: "atrium" }))
  }, null, 2), "application/json");
}

/* ── alerting ───────────────────────────────────────────
   Two tones, so you can tell from the next room which one it is: a permission
   is a rising two-note that needs you now, a task waiting for input is one
   softer note. Tones are synthesised rather than shipped as audio files so the
   binary stays self-contained. */

// Each sound is a list of notes: [frequency, startSeconds, durationSeconds,
// gain, waveform]. Synthesised rather than shipped as files so the binary
// stays self-contained.
const SOUNDS = {
  chime:  { label: "chime",       notes: [[880,0,.18,.9,"sine"], [1318,.14,.3,.8,"sine"]] },
  rise:   { label: "rise",        notes: [[523,0,.12,.8,"sine"], [784,.1,.12,.8,"sine"], [1046,.2,.3,.7,"sine"]] },
  ping:   { label: "ping",        notes: [[1046,0,.35,.7,"sine"]] },
  blip:   { label: "blip",        notes: [[660,0,.09,.8,"square"]] },
  knock:  { label: "knock",       notes: [[196,0,.09,1,"triangle"], [196,.15,.09,.9,"triangle"]] },
  bell:   { label: "bell",        notes: [[1318,0,.9,.55,"sine"], [2637,0,.5,.18,"sine"]] },
  sub:    { label: "low thud",    notes: [[110,0,.4,1,"sine"]] },
  alarm:  { label: "alarm",       notes: [[880,0,.1,.9,"square"], [880,.16,.1,.9,"square"], [880,.32,.1,.9,"square"]] },
  drop:   { label: "drop",        notes: [[880,0,.1,.8,"sine"], [440,.09,.28,.8,"sine"]] },
  chirp:  { label: "chirp",       notes: [[1400,0,.06,.6,"sine"], [1800,.05,.08,.5,"sine"]] },
  none:   { label: "silent",      notes: [] }
};

// expiry is how many seconds a desktop notification stays on screen. 0 keeps it
// until it is answered or clicked, which on Windows is what made them pile up.
// debounce holds an alert for a moment so several agents finishing together
// are one alert rather than a burst. Off by default: a delay between something
// needing you and being told is a real cost, and it is only worth paying once
// the pile-up is worse than the wait.
const DEFAULT_PREFS = {
  muted: false, volume: 0.35, input: "chime", perm: "alarm",
  desktop: true, expiry: 30, debounce: 0
};

function loadPrefs() {
  try {
    return Object.assign({}, DEFAULT_PREFS, JSON.parse(localStorage.getItem("atrium.sound") || "{}"));
  } catch (e) { return Object.assign({}, DEFAULT_PREFS); }
}

// Whether the board is the thing you are looking at right now.
//
// `visibilityState` alone is not that, and believing it was is why a
// notification never arrived while the browser sat behind an editor. It only
// says the tab is the active tab of a window that is not minimised, which
// stays true when the whole browser is buried three windows deep. Focus is the
// missing half: `hasFocus()` is false as soon as any other application has the
// keyboard.
//
// Both are needed. Focus alone would call a background tab of a focused
// browser the foreground.
function inForeground() {
  return document.visibilityState === "visible" && document.hasFocus();
}

// Whether this document is on a screen at all, without asking whether it has
// the keyboard.
//
// The two questions are different and the difference is the whole of a bug
// somebody hit: a popped-out terminal on a second monitor is VISIBLE and not
// FOCUSED, so `inForeground` said no and the window announced, out loud, a
// thing the operator was watching happen.
//
// Which test to use depends on what the document is FOR. The board is a tab
// among many and being visible does not mean you are reading it, so it keeps
// `inForeground`. A popped-out window holds one session and exists to be
// looked at, so for that one, visible is enough.
function onScreen() {
  return document.visibilityState === "visible";
}

