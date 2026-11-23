// attachFolderComplete offers the folders under what has been typed in a text box, from /v1/browse, as a datalist.
// A folder outside the browse roots lists nothing, which leaves the box a plain text box.
function attachFolderComplete(input) {
  if (!input || input.dataset.fc) return;
  input.dataset.fc = "1";
  const list = document.createElement("datalist");
  list.id = input.id + "-fc";
  input.after(list);
  input.setAttribute("list", list.id);
  let seq = 0, shown = "", entries = [];
  const suggest = async () => {
    const v = input.value.replace(/\\/g, "/");
    const cut = v.lastIndexOf("/");
    if (cut < 0) return;
    let dir = v.slice(0, cut) || "/";
    if (/^[a-zA-Z]:$/.test(dir)) dir += "/";
    if (dir !== shown) {
      const my = ++seq;
      let got = [];
      try { got = (await api("/v1/browse?quiet=1&path=" + encodeURIComponent(dir))).entries || []; } catch (e) {}
      if (my !== seq) return;
      shown = dir;
      entries = got;
    }
    // The cap applies after the typed name, so a folder past the first 60 (repos sort first) is still offered.
    const typed = v.slice(cut + 1).toLowerCase();
    list.replaceChildren(...entries.filter(e => String(e.name).toLowerCase().startsWith(typed)).slice(0, 60).map(e => {
      const o = document.createElement("option");
      o.value = e.path + "/";
      if (e.repo) o.label = "git repo";
      return o;
    }));
  };
  input.addEventListener("input", suggest);
  input.addEventListener("focus", suggest);
}
