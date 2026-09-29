// ── a card's alias: the name you mention it by ──────────
//
// Handles are made up by the board, `dotfiles-41800`, and nobody types those.
// An alias is what somebody would type: `@dotfiles`, `@sa89`. `atrium_say`,
// `atrium_peers` and `atrium tell` take it wherever they take a handle. A
// launched worker starts with its title's prefix, and anything else has none
// until one is set here. Unique among live cards: taking one in use is refused
// by the room, and the refusal names the card holding it. See
// internal/store/alias.go.

// The chip on the card. Nothing for a card with no alias, so the board looks
// as it did until somebody sets one, unless its default was held by another
// card: then a `no alias` chip says who has it (`alias_note`, from the room).
function aliasChip(t) {
  if (t && !t.alias && t.alias_note) {
    return `<span class="chip warn"
      data-tip="${esc(t.alias_note + ". click to give it another")}"
      onclick="event.stopPropagation();setTaskAlias('${t.id}', '')">no alias</span>`;
  }
  if (!t || !t.alias) return "";
  return `<span class="chip alias"
    data-tip="${esc("mention this card as @" + t.alias + ". atrium_say, atrium_peers and atrium tell take it " +
      "wherever they take a handle. click to change it")}"
    onclick="event.stopPropagation();setTaskAlias('${t.id}', '${esc(t.alias)}')">@${esc(t.alias)}</span>`;
}

// The menu entry, beside rename on both surfaces.
function aliasMenuItem(t) {
  if (!t) return null;
  return {
    label: "alias…", note: t.alias ? "@" + t.alias : (t.alias_note ? "taken" : ""),
    help: "A short name to mention this card by, like @dotfiles. Other sessions can " +
      "say it to atrium_say and atrium tell in place of the handle." +
      (t.alias_note ? " " + t.alias_note + "." : ""),
    act: () => setTaskAlias(t.id, t.alias || "")
  };
}

// Empty clears it. A clash comes back from the room naming the holder, and
// `patchTask` puts that in a toast.
async function setTaskAlias(id, current) {
  const raw = await askText("alias for this card",
    "The name to mention it by, like @dotfiles. Lowercase letters, digits, '.', '_' or '-'. " +
    "It has to be free among the live cards. Leave it empty to clear it.",
    current || "", "sa89");
  if (raw === null) return;
  await patchTask(id, { alias: raw.trim() });
  refresh();
  // The terminal bar follows at once rather than at the next poll, and in a
  // popped-out window, whose poll is the card alone. What the room kept is
  // read back, so a refused one leaves the bar as it was.
  if (typeof termTask !== "undefined" && termTask && sameCard(termTask.id, id)) {
    try { followTermAlias([await api("/v1/tasks/" + encodeURIComponent(id))]); } catch (e) {}
  }
}

// ── the terminal bar ──
//
// clint: "it needs to show up on the terminal title bar in place of the
// current `github/dovholuknf/atrium:claude/main` stuff (far left) and it needs
// to be clear that it's an alias / handle". So a card with an alias wears
// `@saorch` in the name's slot, drawn as a handle (see `.as-alias` in
// css/terminal.css), and the address it replaced moves to the tooltip. A card
// with none keeps the label `paintTermTitle` drew. Either way a click on the
// label reads and sets the alias.
function paintTermAlias(task, el, label) {
  if (!task || !el) return;
  el.classList.toggle("as-alias", !!task.alias);
  el.classList.add("can-alias");
  el.onclick = () => { if (termTask) setTaskAlias(termTask.id, termTask.alias || ""); };
  if (!task.alias) {
    el.dataset.tip = [el.dataset.tip, task.alias_note, "click to give it an alias"]
      .filter(Boolean).join(" · ");
    return;
  }
  el.innerHTML = `<span class="at">@</span>${esc(task.alias)}`;
  const named = String(task.display_title || "").trim();
  el.dataset.tip = ["alias @" + task.alias + ", the handle to mention this card by",
    named && named !== label ? named : "", label, task.worktree || "", "click to change it"]
    .filter(Boolean).join(" · ");
}

// Repaints the bar when the attached card's alias changed under it: set from
// the card menu, or by an agent with `atrium_alias`. Handed whatever list a
// poll just read. Not on an exited pane, whose label says so instead.
function followTermAlias(list) {
  if (typeof termTask === "undefined" || !termTask || !Array.isArray(list)) return;
  const t = list.find(x => x && sameCard(x.id, termTask.id));
  if (!t) return;
  if ((t.alias || "") === (termTask.alias || "") && (t.alias_note || "") === (termTask.alias_note || "")) return;
  termTask.alias = t.alias || "";
  termTask.alias_note = t.alias_note || "";
  const pane = document.getElementById("term-pane");
  if (pane && pane.classList.contains("dead")) return;
  paintTermTitle(termTask);
}
