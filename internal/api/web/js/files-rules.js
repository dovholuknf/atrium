// The file drawer's rules, as pure functions so scripts/test-files-rules.js can run them without a browser.

// Whether `files/open` would open a window in front of the person at this browser. It runs the editor where the
// card's room is, so only a browser on that same machine sees it: the board reached on loopback, and a card on the
// board's own daemon or on the hub's own room. The same test `homeCanOpen` uses for a card's folder.
function fileOpensHere(id, host, rooms) {
  if (!/^(127\.0\.0\.1|localhost|\[::1\])$/.test(String(host || ""))) return false;
  const s = String(id || "");
  const cut = s.indexOf("~");
  const room = cut > 0 ? s.slice(0, cut) : "";
  return !room || (rooms || []).some(r => r && r.name === room && r.transport === "local");
}

// The rows as shown: filtered by name, then sorted by `sort` with folders always first.
function filesShownBy(rows, q, sort) {
  q = String(q || "").trim().toLowerCase();
  const out = q ? rows.filter(e => String(e.name).toLowerCase().includes(q)) : rows.slice();
  const k = sort && sort.key, sign = sort && sort.desc ? -1 : 1;
  const byName = (a, b) => String(a.name).localeCompare(String(b.name), undefined, { numeric: true, sensitivity: "base" });
  const cmp = (a, b) => k === "size" ? ((a.size || 0) - (b.size || 0)) * sign
    : k === "mtime" ? (Date.parse(a.mtime || 0) - Date.parse(b.mtime || 0)) * sign : byName(a, b) * sign;
  return out.sort((a, b) => (!!b.dir - !!a.dir) || cmp(a, b) || byName(a, b));
}

// The breadcrumb's steps for `at` under the card's `root`. A Windows path compares without case, since the room echoes
// the case it was asked in. A folder outside the card is one step named whole, after the card's own.
function crumbSteps(root, at) {
  root = String(root || "").replace(/\/+$/, "");
  at = String(at || "").replace(/\/+$/, "");
  const fold = s => /^[A-Za-z]:\//.test(root) ? s.toLowerCase() : s;
  const inside = fold(at) === fold(root) || fold(at).startsWith(fold(root) + "/");
  const parts = [{ label: root.split("/").filter(Boolean).pop() || root, path: root }];
  if (!inside) {
    if (at) parts.push({ label: at, path: at, whole: true });
    return parts;
  }
  let walk = root;
  for (const s of at.slice(root.length).split("/").filter(Boolean)) {
    walk += "/" + s;
    parts.push({ label: s, path: walk });
  }
  return parts;
}

// `at` as the card names it, from the card's own folder down, as in `/work/img`. "" for a folder outside the card.
function cardRelPath(root, at) {
  root = String(root || "").replace(/\/+$/, "");
  at = String(at || "").replace(/\/+$/, "");
  const fold = s => /^[A-Za-z]:\//.test(root) ? s.toLowerCase() : s;
  if (!root || !(fold(at) === fold(root) || fold(at).startsWith(fold(root) + "/"))) return "";
  return "/" + (root.split("/").filter(Boolean).pop() || "") + at.slice(root.length);
}
