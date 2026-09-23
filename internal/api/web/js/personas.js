// ── personas ────────────────────────────────────────────
//
// The persona pack, drawn: a pane on the runners page listing each persona,
// the "review with" entry on a card, and the lessons view. See
// docs/personas-design.md, "What atrium does, and what it does not".
//
// THE BUTTONS HERE EDIT FILES AND NOTHING ELSE. Promote, keep and delete change
// the dotagents working tree. The commit is clint's, so the view ends with the
// message and the command, for him to run.

let allPersonas = [];
let personaPack = { pack: "", setting: "persona_pack_path", error: "" };

// When the catalog was last read. The card menu asks on every open, and a pack
// that is not configured is an empty list that would otherwise be re-read on
// every right click.
let personasReadAt = 0;

async function loadPersonas(fresh) {
  if (!fresh && Date.now() - personasReadAt < 30000) return allPersonas;
  personasReadAt = Date.now();
  try {
    const r = await api("/v1/personas");
    allPersonas = r.personas || [];
    personaPack = { pack: r.pack || "", setting: r.setting || "persona_pack_path", error: r.error || "" };
  } catch (e) {
    allPersonas = [];
  }
  return allPersonas;
}

async function renderPersonas() {
  const host = document.getElementById("persona-list");
  if (!host) return;
  await loadPersonas(true);
  if (!allPersonas.length) {
    const why = personaPack.error
      ? esc(personaPack.error)
      : personaPack.pack
        ? `no persona.yaml under <code>${esc(personaPack.pack)}</code>.`
        : `no persona pack is configured. set <code>${esc(personaPack.setting)}</code> in settings to the ` +
          `personas folder, for example <code>D:/git/github/dovholuknf/dotagents/personas</code>.`;
    setHTML(host, `<div class="panel"><div class="empty">${why}</div></div>`);
    return;
  }
  setHTML(host, roomGroups(allPersonas, p => {
    const rv = p.reviews || {};
    const paths = (rv.paths || []).join(" ");
    const surfaces = (rv.surfaces || []).join(", ");
    const runners = (p.runners || []).map(r => {
      const ok = (p.renders || []).indexOf(r) >= 0;
      return `<span class="by ${ok ? "found" : "missing"}" title="${ok
        ? "renders for " + esc(r) : "declares " + esc(r) + " but has no render for it"}">${esc(r)}</span>`;
    }).join(" ");
    return `<div class="row line persona-row" data-id="${esc(p.id)}">
      <span class="tool" title="${esc(p.id)}">${esc(p.name || p.id)}</span>
      <span class="grow ell" title="${esc(p.description || "")}">${esc(p.description || "")}</span>
      ${paths ? `<code class="ell" title="reviews paths">${esc(paths)}</code>` : ""}
      ${surfaces ? `<span class="by ell" title="surfaces: ${esc(surfaces)}">${esc(surfaces)}</span>` : ""}
      ${runners}
      <span class="by" title="${esc(p.last_run || "never run from atrium")}">${
        p.last_run ? "ran " + esc(shortTime(p.last_run)) : "never run"}</span>
      ${p.problem ? `<span class="by missing" title="${esc(p.problem)}">problem</span>` : ""}
      <button data-lessons="${esc(p.id)}" data-room="${esc(p.room || "")}">lessons</button>
    </div>`;
  }));
  host.querySelectorAll("button[data-lessons]").forEach(b => {
    b.onclick = () => openLessons(b.dataset.lessons, b.dataset.room);
  });
}

// The flyout under "review with…" on a card: every persona on the card's
// machine, once per runner it renders for.
function personaReviewSub(id, t) {
  const mine = (allPersonas || []).filter(p => (p.room || "") === (t.room || "") && !p.problem);
  const sub = [];
  mine.forEach(p => (p.renders || []).forEach(r => sub.push({
    label: (p.name || p.id) + ((p.renders || []).length > 1 ? " on " + r : ""),
    note: r,
    act: () => reviewWithPersona(id, t, p, r)
  })));
  if (!sub.length) {
    sub.push({ quiet: personaPack.pack ? "no persona renders for a runner here" : "no persona pack is set" });
  }
  return sub;
}

async function reviewWithPersona(id, t, p, runner) {
  if (!await confirmUser("review with " + (p.name || p.id) + "?",
    `<p>Starts a fresh ${esc(runner)} session as <b>${esc(p.name || p.id)}</b>, in a run directory of its own,
      to review this card's branch against its merge-base with the default branch.</p>
    <p class="by">It reads only this repo's knowledge and memory, changes no files, and reports its findings
      back to this card.</p>`, "start the review")) return;
  try {
    await api(`/v1/tasks/${id}/persona-review`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ persona: p.id, runner: runner })
    });
  } catch (e) { tellUser("the review did not start", esc(e.message)); return; }
  refresh();
}

// ── the lessons view ────────────────────────────────────

let lessonsOpen = { id: "", room: "" };

function lessonsHeaders(extra) {
  const h = Object.assign({}, extra || {});
  if (lessonsOpen.room) h["X-Atrium-Room"] = lessonsOpen.room;
  return h;
}

async function openLessons(id, room) {
  lessonsOpen = { id: id, room: room || "" };
  const dlg = document.getElementById("lessons");
  document.getElementById("lessons-title").textContent = "lessons: " + id;
  setHTML(document.getElementById("lessons-body"), `<div class="empty">reading…</div>`);
  if (!dlg.open) dlg.showModal();
  let r;
  try {
    r = await api(`/v1/personas/${encodeURIComponent(id)}/lessons`, { headers: lessonsHeaders() });
  } catch (e) {
    setHTML(document.getElementById("lessons-body"), `<div class="empty">${esc(e.message)}</div>`);
    return;
  }
  renderLessons(r);
}

function renderLessons(r) {
  const since = r.baseline
    ? `since <code title="${esc(r.baseline_subject || "")}">${esc(r.baseline.slice(0, 10))}</code>,
       the last commit with <code>Lessons-reviewed: ${esc(r.persona)}</code>, plus anything uncommitted.`
    : `never reviewed, so its whole history is in scope.`;
  const lessons = (r.lessons || []).map(l => `
    <div class="lesson" data-file="${esc(l.file)}">
      <div class="row line">
        <span class="chip">${esc(l.status)}</span>
        <code class="grow ell" title="${esc(l.file)}">${esc(l.file)}</code>
        <span class="by">${l.repo ? "repo: " + esc(l.repo) : "no repo line"}</span>
        ${l.status === "deleted" ? `<span class="by">deleted, the commit records it</span>` : `
        <button class="go" data-act="promote">promote</button>
        <button data-act="keep">keep</button>
        <button class="no" data-act="delete">delete</button>`}
      </div>
      <p class="hintline">${esc(l.description || l.name || "")}${l.why
        ? ` <span class="by">Why: ${esc(l.why)}</span>` : ` <span class="by">no Why line</span>`}</p>
      <details><summary>diff</summary><pre class="code lesson-diff">${esc(l.diff || "")}</pre></details>
    </div>`).join("");
  const rej = (r.rejections || []).length
    ? `<ul class="lesson-rejections">${r.rejections.map(x => `<li><code>${esc(x)}</code></li>`).join("")}</ul>
       <p class="by">Folding repeated rejections into persona.md is a change to the persona itself, so it is done
         with /lessons-review in a session, not from here.</p>`
    : `<p class="by">no new rejected.md lines.</p>`;
  setHTML(document.getElementById("lessons-body"), `
    <p class="hintline">${since}</p>
    <h3>memory</h3>
    ${lessons || `<div class="empty">nothing new in memory.</div>`}
    <h3>rejections</h3>
    ${rej}
    <h3>commit</h3>
    <p class="by">Atrium never commits. Run this in the dotagents checkout when you are done, then
      <code>/safe-to-push</code> before any push.</p>
    <pre class="code lesson-commit">${esc(r.command || "")}</pre>`);
  document.querySelectorAll("#lessons-body .lesson button[data-act]").forEach(b => {
    b.onclick = () => decideLesson(b.closest(".lesson").dataset.file, b.dataset.act, r);
  });
}

async function decideLesson(file, act, r) {
  const l = (r.lessons || []).find(x => x.file === file) || {};
  const body = { file: file, action: act };
  if (act === "promote" && !l.repo) {
    const repo = await askUser({
      title: "promote to which repo?",
      body: `<p>This lesson has no <code>repo:</code> line. Name the repo it is about, like
        <code>github/openziti/ziti</code>, or <code>general</code>.</p>`,
      input: true, value: "general", list: ["general"],
      buttons: [{ label: "cancel", value: null }, { label: "promote", value: true, style: "go" }]
    });
    if (repo === null) return;
    body.repo = repo.trim();
  }
  if (act === "keep") {
    if (!l.repo) {
      const repo = await askUser({
        title: "which repo is it about?",
        body: `<p>Keeping a lesson adds the <code>repo:</code> line it is missing.</p>`,
        input: true, value: "general", list: ["general"],
        buttons: [{ label: "cancel", value: null }, { label: "next", value: true, style: "go" }]
      });
      if (repo === null) return;
      body.repo = repo.trim();
    }
    if (!l.why) {
      const why = await askUser({
        title: "why is it true?",
        body: `<p>Keeping a lesson adds the one-line <code>Why:</code> it is missing: what taught it.</p>`,
        input: true, value: "",
        buttons: [{ label: "cancel", value: null }, { label: "keep it", value: true, style: "go" }]
      });
      if (why === null) return;
      body.why = why.trim();
    }
  }
  if (act === "delete" && !await confirmUser("delete this lesson?",
    `<p><code>${esc(file)}</code> and its line in MEMORY.md go from the working tree. Git still has it
      until you commit.</p>`, "delete it")) return;
  let next;
  try {
    next = await api(`/v1/personas/${encodeURIComponent(r.persona)}/lessons`, {
      method: "POST", headers: lessonsHeaders({ "Content-Type": "application/json" }),
      body: JSON.stringify(body)
    });
  } catch (e) { tellUser("that did not change anything", esc(e.message)); return; }
  renderLessons(next);
}
