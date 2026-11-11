const LOGIN_HOST_KEY = "atrium.loginhost";

function loginLineDefault(room) { return `ssh -t ${room} claude auth login`; }

function loginRoomHdr(room) { return { "X-Atrium-Room": room }; }

function loginHostRoom() {
  const names = hubRooms.map(r => r.name);
  const kept = localStorage.getItem(LOGIN_HOST_KEY);
  if (names.includes(kept)) return kept;
  const local = hubRooms.find(r => r.transport === "local");
  return (local || hubRooms[0]).name;
}

function loginChip(state) {
  if (state === "in") return `<span class="chip live idle">logged in</span>`;
  if (state === "out") return `<span class="chip no">logged out</span>`;
  if (state === "unknown") return `<span class="chip warn" data-tip="claude auth status could not be run there">unknown</span>`;
  return `<span class="chip">checking</span>`;
}

// Parallel, so a room that does not answer cannot hold the dialog.
async function loginRead(room) {
  const [st, set] = await Promise.all([
    api("/v1/login-status", { headers: loginRoomHdr(room) }).catch(() => ({ state: "unknown" })),
    api("/v1/settings", { headers: loginRoomHdr(room) }).catch(() => ({}))
  ]);
  return { state: st.state || "unknown", stored: set.login_command || "" };
}

async function openLoginRooms() {
  if (!hubIsHub || !hubRooms.length) {
    tellUser("no rooms", "Logging in needs a hub with at least one attached room.");
    return;
  }
  const host = document.getElementById("ll-host");
  host.innerHTML = hubRooms.map(r => `<option>${esc(r.name)}</option>`).join("");
  host.value = loginHostRoom();
  const rows = document.getElementById("ll-rows");
  rows.innerHTML = hubRooms.map(r => `<div class="loginrow" data-room="${esc(r.name)}">
    <input type="checkbox" class="ll-tick" aria-label="log in ${esc(r.name)}">
    <b>${esc(r.name)}</b><span class="ll-chip">${loginChip("")}</span>
    <input type="text" class="ll-line" spellcheck="false" value="${esc(loginLineDefault(r.name))}"></div>`).join("");
  document.getElementById("ll-go").disabled = false;
  document.getElementById("loginrooms").showModal();
  await Promise.all(hubRooms.map(async r => {
    const got = await loginRead(r.name);
    const row = rows.querySelector(`.loginrow[data-room="${CSS.escape(r.name)}"]`);
    if (!row) return;
    row.dataset.stored = got.stored;
    row.querySelector(".ll-chip").innerHTML = loginChip(got.state);
    row.querySelector(".ll-line").value = got.stored || loginLineDefault(r.name);
    row.querySelector(".ll-tick").checked = got.state !== "in";
  }));
}

// The popped window is another page, so the line goes in through an attach of our own.
function loginType(card, room, line) {
  return new Promise(done => {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${proto}//${location.host}/v1/tasks/${card}/attach?atrium_room=${encodeURIComponent(room)}`);
    ws.onopen = () => {
      ws.send(JSON.stringify({ t: "in", d: line + "\r" }));
      setTimeout(() => { ws.close(); done(); }, 400);
    };
    ws.onerror = () => done();
  });
}

// Nothing is left to read the link once its window is gone.
function loginKillOnClose(card, room) {
  const win = popOuts.get(card);
  if (!win) return;
  const timer = setInterval(() => {
    if (!win.closed) return;
    clearInterval(timer);
    api(`/v1/tasks/${encodeURIComponent(card)}/kill`, { method: "POST", headers: loginRoomHdr(room) }).catch(() => {});
  }, 1000);
}

// There is no shell runner row by design, so a pop-out of the host's shell needs one that runs the same command.
async function loginShellRunner(host) {
  const set = await api("/v1/settings", { headers: loginRoomHdr(host) }).catch(() => ({}));
  const line = String(set.shell_command || set.shell_command_now || "").trim();
  if (!line || line === "off") { toast("no shell", "the shell setting on " + host + " is off."); return ""; }
  const parts = line.split(/\s+/);
  try {
    await api("/v1/harnesses/login-shell", {
      method: "PUT", headers: Object.assign({ "Content-Type": "application/json" }, loginRoomHdr(host)),
      body: JSON.stringify({
        label: "login shell", enabled: true, cmd: parts[0], args: parts.slice(1),
        launch_mode: "pty", exit_keys: ["exit"]
      })
    });
  } catch (e) { toast("no shell runner", e.message); return ""; }
  return "login-shell";
}

async function openLoginShells() {
  const host = document.getElementById("ll-host").value;
  localStorage.setItem(LOGIN_HOST_KEY, host);
  const picked = [...document.querySelectorAll("#ll-rows .loginrow")]
    .filter(r => r.querySelector(".ll-tick").checked)
    .map(r => ({
      room: r.dataset.room, stored: r.dataset.stored || "",
      line: r.querySelector(".ll-line").value.trim()
    }));
  if (!picked.length) { toast("nothing ticked", "tick the rooms to log in."); return; }
  const go = document.getElementById("ll-go");
  go.disabled = true;
  const cards = [];
  for (const p of picked) {
    const want = p.line === loginLineDefault(p.room) ? "" : p.line;
    if (want !== p.stored) {
      await api("/v1/settings", {
        method: "POST", headers: Object.assign({ "Content-Type": "application/json" }, loginRoomHdr(p.room)),
        body: JSON.stringify({ login_command: want })
      }).catch(e => toast("not saved for " + p.room, e.message));
    }
  }
  const runner = await loginShellRunner(host);
  if (!runner) { go.disabled = false; return; }
  await Promise.all(picked.map(async p => {
    try {
      const t = await api("/v1/launch", {
        method: "POST",
        headers: Object.assign({ "Content-Type": "application/json" }, loginRoomHdr(host)),
        body: JSON.stringify({ harness: runner, throwaway: true, title: "log in " + p.room })
      });
      cards.push({ card: t.id, line: p.line || loginLineDefault(p.room) });
    } catch (e) { tellText("could not open a shell for " + p.room, e.message); }
  }));
  document.getElementById("loginrooms").close();
  for (const c of cards) {
    await popOutTask(c.card);
    loginKillOnClose(c.card, host);
    await loginType(c.card, host, c.line);
  }
}
