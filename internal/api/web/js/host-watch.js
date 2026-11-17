const HOST_WATCH_POLL_MS = 30000;
let hostWatch = { at: 0, lines: [] };

// A line ends " stop it: <command>", and an elevated one adds "  (from an administrator prompt)".
function hostWatchSplit(line) {
  const i = line.lastIndexOf(" stop it: ");
  if (i < 0) return { text: line, cmd: "", note: "" };
  const rest = line.slice(i + 10), j = rest.indexOf("  (");
  return { text: line.slice(0, i), cmd: j < 0 ? rest : rest.slice(0, j), note: j < 0 ? "" : rest.slice(j + 2) };
}

function paintHostWatch() {
  const host = document.getElementById("term-host");
  if (!host) return;
  host.replaceChildren();
  host.hidden = hostWatch.lines.length === 0;
  for (const line of hostWatch.lines) {
    const p = hostWatchSplit(line);
    const row = document.createElement("div");
    row.className = "th-row";
    const text = document.createElement("div");
    text.className = "th-text";
    text.append(p.text);
    if (p.cmd) {
      const code = document.createElement("code");
      code.className = "th-cmd";
      code.textContent = p.cmd;
      text.append(" stop it: ", code);
      if (p.note) text.append(" " + p.note);
    }
    row.append(text);
    if (p.cmd) {
      const btn = document.createElement("button");
      btn.className = "th-copy";
      btn.textContent = "copy";
      btn.setAttribute("data-tip", "copy the stop command");
      btn.addEventListener("click", () => copyText(btn, p.cmd));
      row.append(btn);
    }
    host.append(row);
  }
}

function hostWatchLines(notice) {
  return String(notice || "").split("\n").filter(Boolean);
}

async function renderHostWatch() {
  if (typeof roomUnnamed === "function" && roomUnnamed()) {
    hostWatch = { at: 0, lines: (hubRooms || []).filter(r => r && r.attached && r.host_runaway).flatMap(r => hostWatchLines(r.host_runaway)) };
    paintHostWatch();
    return;
  }
  if (Date.now() - hostWatch.at < HOST_WATCH_POLL_MS) return;
  hostWatch.at = Date.now();
  try {
    const r = await api("/v1/room/host");
    hostWatch = { at: hostWatch.at, lines: hostWatchLines(r.notice) };
  } catch (e) {
    // An old room has no such route, and a room that does not answer is not a finding.
    hostWatch = { at: hostWatch.at, lines: [] };
  }
  paintHostWatch();
}
