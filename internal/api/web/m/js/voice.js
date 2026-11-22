// Push-to-talk: record while held, then the card's room turns the clip into text with its own whisper-server.
//
// LOCAL ONLY. The clip goes to `POST /v1/tasks/{id}/dictate` on the card's room and nowhere else: no browser speech
// engine, no hosted API. The room answers 503 when it has no whisper-server set.
//
// WAV, not webm. MediaRecorder gives webm or mp4, which whisper-server cannot read without ffmpeg, so the browser
// decodes the recording and sends 16 kHz mono 16-bit PCM, which is what whisper wants anyway.
//
// Shared by the board (js/dictate.js, the desk key and mic) and the composer (m/js/compose.js, the phone mic).
(function () {
  "use strict";

  const RATE = 16000;
  const MIN_MS = 300;
  let rec = null;

  // Why the mic cannot work here, or "" when it can. A plain http page that is not loopback is not a secure context,
  // and the browser has no getUserMedia there at all.
  function blocked() {
    if (!window.isSecureContext) {
      return "The microphone is blocked on " + location.origin + ", which the browser does not treat as secure. " +
        "Open the board at http://127.0.0.1 on its own machine, or over https, to dictate. Per-browser setting: docs/ui/voice-dictation.md, Microphone on a plain http board.";
    }
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia || !window.MediaRecorder) {
      return "This browser cannot record audio.";
    }
    return "";
  }

  const boopKind = () => { try { const v = localStorage.getItem("atrium.dictate.boop"); return v === "off" || v === "blip" ? v : "voice"; } catch (e) { return "voice"; } };

  const boopVol = () => { try { const v = parseInt(localStorage.getItem("atrium.dictate.boopvol"), 10); return v >= 0 && v <= 200 ? v / 100 : 1; } catch (e) { return 1; } };

  let clip = null, clipBytes = null;

  const fetchBoop = () => { clipBytes = clipBytes || fetch("/m/boop.mp3").then(r => r.arrayBuffer()); return clipBytes; };

  // The recorded boop is decoded once and played through the same context path as the blip.
  function voiceBoop(ctx) {
    clip = clip || fetchBoop().then(b => ctx.decodeAudioData(b.slice(0)));
    return clip.then(buf => {
      const src = ctx.createBufferSource();
      src.buffer = buf;
      const g = ctx.createGain();
      g.gain.value = boopVol();
      src.connect(g);
      g.connect(ctx.destination);
      src.onended = () => ctx.close();
      src.start();
    });
  }

  // Once the stream is live: off, a soft 80 ms sine blip, or the recorded voice boop.
  function boop() {
    const kind = boopKind();
    if (kind === "off") return;
    try {
      const Ctx = window.AudioContext || window.webkitAudioContext;
      const ctx = new Ctx();
      if (kind === "voice") { voiceBoop(ctx).catch(() => ctx.close()); return; }
      const osc = ctx.createOscillator(), g = ctx.createGain(), t = ctx.currentTime;
      osc.frequency.value = 660;
      g.gain.setValueAtTime(0, t);
      g.gain.linearRampToValueAtTime(0.08 * boopVol(), t + 0.02);
      g.gain.linearRampToValueAtTime(0, t + 0.08);
      osc.connect(g);
      g.connect(ctx.destination);
      osc.onended = () => ctx.close();
      osc.start(t);
      osc.stop(t + 0.08);
    } catch (e) {}
  }

  async function start() {
    if (rec) return;
    const why = blocked();
    if (why) throw new Error(why);
    const r = { chunks: [], at: 0, stream: null, mr: null, gone: false };
    rec = r;
    if (boopKind() === "voice") fetchBoop().catch(() => { clipBytes = null; });
    let stream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
    } catch (e) {
      if (rec === r) rec = null;
      throw new Error(e && e.name === "NotAllowedError"
        ? "The microphone was refused. Allow it for this page in the browser's site settings."
        : "No microphone: " + (e && e.message || e));
    }
    if (r.gone) { stream.getTracks().forEach(t => t.stop()); return; }
    r.stream = stream;
    r.mr = new MediaRecorder(stream);
    r.mr.ondataavailable = e => { if (e.data && e.data.size) r.chunks.push(e.data); };
    r.mr.start();
    r.at = Date.now();
    boop();
  }

  function release(r) {
    if (r.stream) r.stream.getTracks().forEach(t => t.stop());
  }

  function cancel() {
    const r = rec;
    rec = null;
    if (!r) return;
    r.gone = true;
    if (r.mr && r.mr.state !== "inactive") { r.mr.onstop = () => release(r); r.mr.stop(); } else release(r);
  }

  // Stop and transcribe. Answers "" for a clip too short to be speech, which is a tap.
  async function stop(cardId) {
    const r = rec;
    rec = null;
    if (!r) return "";
    if (!r.mr) { r.gone = true; return ""; }
    const long = Date.now() - r.at >= MIN_MS;
    await new Promise(res => { r.mr.onstop = res; r.mr.stop(); });
    release(r);
    if (!long || !r.chunks.length) return "";
    return send(cardId, await toWav(new Blob(r.chunks, { type: r.mr.mimeType || "audio/webm" })));
  }

  // A page that is not a secure context has no getUserMedia but still opens the phone's recorder through a file input.
  const capturable = () => !window.isSecureContext;

  // Transcribe a clip the phone's recorder made, which comes as m4a, 3gp or webm and is decoded like a recording.
  async function fromFile(cardId, file) {
    const t = performance.now();
    const wav = await toWav(file);
    return send(cardId, wav, performance.now() - t);
  }

  // How long the last clip took, for the status line: converting here, the whole round trip, and what the hub says it
  // spent reading the upload and in whisper.
  let last = null;
  const timing = () => last;

  async function send(cardId, wav, convert) {
    const t = performance.now();
    const res = await fetch("/v1/tasks/" + encodeURIComponent(cardId) + "/dictate", {
      method: "POST", headers: { "Content-Type": "audio/wav", "X-Atrium-Words": words() }, body: wav
    });
    let j = null;
    try { j = await res.json(); } catch (e) {}
    const st = {};
    for (const m of (res.headers.get("Server-Timing") || "").matchAll(/(\w+);dur=(\d+)/g)) st[m[1]] = +m[2];
    last = { convert, trip: performance.now() - t, kb: Math.round(wav.size / 1024), ...st };
    if (!res.ok) throw new Error((j && j.error) || ("the room answered " + res.status));
    return (j && j.text || "").trim();
  }

  // Why this card's room (or the hub) cannot transcribe, or "" when it can or cannot be asked. Asked on press, so a
  // room with no speech to text says so before anyone talks.
  async function ready(cardId) {
    try {
      const res = await fetch("/v1/tasks/" + encodeURIComponent(cardId) + "/dictate");
      if (!res.ok) return "";
      const j = await res.json();
      return j && j.ready === false ? (j.error || "dictation is not set up") : "";
    } catch (e) { return ""; }
  }

  // The hub holds no card list, so the board sends the names it shows, path-escaped, for whisper's prompt.
  function words() {
    const out = [];
    for (const t of (typeof lastTasks !== "undefined" && lastTasks) || []) {
      for (const w of [t.wire_name, t.alias]) if (w) out.push(encodeURIComponent(w));
      if (out.length >= 300) break;
    }
    return out.join(",");
  }

  async function toWav(blob) {
    const Ctx = window.AudioContext || window.webkitAudioContext;
    const ctx = new Ctx();
    let buf;
    try { buf = await ctx.decodeAudioData(await blob.arrayBuffer()); } finally { ctx.close(); }
    const off = new OfflineAudioContext(1, Math.max(1, Math.ceil(buf.duration * RATE)), RATE);
    const src = off.createBufferSource();
    src.buffer = buf;
    src.connect(off.destination);
    src.start();
    const pcm = (await off.startRendering()).getChannelData(0);
    const out = new DataView(new ArrayBuffer(44 + pcm.length * 2));
    const str = (o, s) => { for (let i = 0; i < s.length; i++) out.setUint8(o + i, s.charCodeAt(i)); };
    str(0, "RIFF"); out.setUint32(4, 36 + pcm.length * 2, true); str(8, "WAVE");
    str(12, "fmt "); out.setUint32(16, 16, true); out.setUint16(20, 1, true); out.setUint16(22, 1, true);
    out.setUint32(24, RATE, true); out.setUint32(28, RATE * 2, true); out.setUint16(32, 2, true); out.setUint16(34, 16, true);
    str(36, "data"); out.setUint32(40, pcm.length * 2, true);
    for (let i = 0; i < pcm.length; i++) {
      const v = Math.max(-1, Math.min(1, pcm[i]));
      out.setInt16(44 + i * 2, v < 0 ? v * 0x8000 : v * 0x7fff, true);
    }
    return new Blob([out.buffer], { type: "audio/wav" });
  }

  window.atriumVoice = { boop, blocked, capturable, fromFile, timing, ready, start, stop, cancel, active: () => !!rec };
})();
