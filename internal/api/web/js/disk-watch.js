const DISK_WATCH_POLL_MS = 30000;
let diskWatch = { at: 0, banners: [] };

// A critical notice says so in its words, which is how the hub's list tells the two apart without a second field.
function diskCritical(text) { return text.indexOf(": disk nearly full.") >= 0; }

function paintDiskWatch() {
  const host = document.getElementById("term-disk");
  if (!host) return;
  host.replaceChildren();
  host.hidden = diskWatch.banners.length === 0;
  host.classList.remove("critical");
  for (const b of diskWatch.banners) {
    const row = document.createElement("div");
    row.className = "td-row" + (diskCritical(b.text) ? " critical" : "");
    const text = document.createElement("div");
    text.className = "td-text";
    text.textContent = b.text;
    const btn = document.createElement("button");
    btn.className = "td-free";
    if (b.room) {
      btn.textContent = "open " + b.room;
      btn.setAttribute("data-tip", "open this room, where the free space button is");
      btn.addEventListener("click", () => pickRoom(b.room));
    } else {
      btn.textContent = "free space";
      btn.setAttribute("data-tip", "remove the folders of ended cards, their build caches and old temp files atrium made");
      btn.addEventListener("click", freeDiskSpace);
    }
    row.append(text, btn);
    host.append(row);
  }
}

async function freeDiskSpace() {
  const btn = document.querySelector("#term-disk .td-free");
  if (btn) btn.disabled = true;
  try {
    const r = await api("/v1/room/disk/free", { method: "POST", body: "{}" });
    const st = r.status || {};
    diskWatch = { at: Date.now(), banners: st.notice ? [{ text: st.notice }] : [] };
    toast("free space", r.freed_bytes > 0 ? "freed " + bytes(r.freed_bytes) : "nothing atrium owns was left to remove");
  } catch (e) {
    toast("could not free space", (e && e.message) || String(e));
  }
  paintDiskWatch();
}

async function renderDiskWatch() {
  if (typeof roomUnnamed === "function" && roomUnnamed()) {
    diskWatch = { at: 0, banners: (hubRooms || []).filter(r => r && r.attached && r.disk_low).map(r => ({ room: r.name, text: r.disk_low })) };
    paintDiskWatch();
    return;
  }
  if (Date.now() - diskWatch.at < DISK_WATCH_POLL_MS) return;
  diskWatch.at = Date.now();
  try {
    const r = await api("/v1/room/disk");
    diskWatch = { at: diskWatch.at, banners: r.notice ? [{ text: r.notice }] : [] };
  } catch (e) {
    // An old room has no such route, and a room that does not answer is not a finding.
    diskWatch = { at: diskWatch.at, banners: [] };
  }
  paintDiskWatch();
}
