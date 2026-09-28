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
// as it did until somebody sets one.
function aliasChip(t) {
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
    label: "alias…", note: t.alias ? "@" + t.alias : "",
    help: "A short name to mention this card by, like @dotfiles. Other sessions can " +
      "say it to atrium_say and atrium tell in place of the handle.",
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
}
