"use strict";

// Pawon UI — vanilla JS, satu router per halaman via body[data-page].

const $ = (s) => document.querySelector(s);
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

const BTN = "rounded-lg px-3 py-1.5 text-xs font-medium transition";
const BTN_START = `${BTN} bg-emerald-600 text-white hover:bg-emerald-500`;
const BTN_STOP = `${BTN} bg-red-600 text-white hover:bg-red-500`;
const BTN_RESTART = `${BTN} bg-slate-700 text-slate-100 hover:bg-slate-600`;
const BTN_GHOST = `${BTN} border border-slate-600 text-slate-200 hover:bg-slate-800`;
const BTN_DANGER = `${BTN} border border-red-800/60 text-red-300 hover:bg-red-950`;

const badge = (ok, on, off) =>
  `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${ok ? "bg-emerald-500/15 text-emerald-400" : "bg-red-500/15 text-red-400"}">${ok ? esc(on) : esc(off)}</span>`;

// api: fetch + JSON + toast error. raw=true → respons dibaca sebagai teks
// (GET .env); quiet=true → tanpa toast (untuk polling yang gagal itu normal).
//
// Content-Type: application/json SELALU dikirim untuk POST/PUT, termasuk saat
// tanpa body. Endpoint mutasi mewajibkannya sebagai pertahanan CSRF: request
// lintas-origin dengan Content-Type JSON memicu preflight, sedangkan
// text/plain & form-urlencoded (CORS-safelisted) tidak — jadi tanpa aturan ini
// halaman web mana pun bisa memanggil stop/start layanan diam-diam.
async function api(path, { method = "GET", body, raw = false, quiet = false } = {}) {
  const init = { method, headers: {} };
  if (method === "POST" || method === "PUT") {
    init.headers["Content-Type"] = "application/json";
  }
  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }
  let res, data;
  try {
    res = await fetch(path, init);
    data = raw ? await res.text() : await res.json();
  } catch (e) {
    if (!quiet) toast(`Gagal menghubungi panel: ${e.message}`, "err");
    throw e;
  }
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try { msg = (typeof data === "string" ? JSON.parse(data) : data).error || msg; } catch { if (typeof data === "string" && data) msg = data; }
    if (!quiet) toast(msg, "err");
    throw new Error(msg);
  }
  return data;
}

function toast(msg, kind = "ok") {
  const t = document.createElement("div");
  t.className = `rounded-lg border px-4 py-2 text-sm shadow-lg ${
    kind === "err" ? "border-red-500/50 bg-red-950 text-red-200" : "border-emerald-500/50 bg-emerald-950 text-emerald-200"}`;
  t.textContent = msg;
  $("#toasts").appendChild(t);
  setTimeout(() => t.remove(), 4000);
}

// Versi PHP terinstal = nama service php-* di /api/status (bukan field terpisah).
async function phpVersions() {
  try {
    const s = await api("/api/status", { quiet: true });
    return [...new Set((s.services || []).filter((x) => /^php-[\d.]+-\d+$/.test(x.Name)).map((x) => x.Name.replace(/^php-/, "").replace(/-\d+$/, "")))];
  } catch { return []; }
}

// ---------- Dashboard ----------

let lastStatus = "";

// groupServices: satu kartu per layanan, tapi worker PHP (php-8.4-1..4)
// dikelompokkan jadi satu kartu per versi. Tiap versi menjalankan 4 worker,
// jadi tanpa pengelompokan dashboard menampilkan 16 kartu PHP yang isinya
// nyaris sama — dan satu worker mati dari empat justru tenggelam di antara
// tiga kartu hijau lainnya.
function groupServices(services) {
  const groups = new Map();
  for (const s of services || []) {
    const m = /^php-([\d.]+)-(\d+)$/.exec(s.Name);
    const key = m ? `php-${m[1]}` : s.Name;
    let g = groups.get(key);
    if (!g) {
      g = { key, name: key, isPHP: !!m, version: m ? m[1] : "", workers: [], running: 0, restarts: 0, lastErr: "" };
      groups.set(key, g);
    }
    if (m) {
      g.workers.push(s);
      if (s.Running) g.running++;
      g.restarts += s.Restarts || 0;
      // Error worker yang mati lebih berguna daripada worker yang sehat.
      if (!s.Running && s.LastErr) g.lastErr = s.LastErr;
    } else {
      g.svc = s;
      g.running = s.Running ? 1 : 0;
      g.restarts = s.Restarts || 0;
      g.lastErr = s.LastErr || "";
    }
  }
  return [...groups.values()];
}

function serviceCard(g) {
  const total = g.isPHP ? g.workers.length : 1;
  const ok = g.isPHP ? g.running > 0 : g.running === 1;
  const label = g.isPHP ? `${g.running}/${total} worker` : g.running ? "jalan" : "mati";
  // Worker PHP punya port masing-masing (pool 9100+, 9200+, …).
  const pids = g.isPHP
    ? g.workers.filter((w) => w.Running).map((w) => w.PID).filter(Boolean)
    : [g.svc.PID].filter(Boolean);
  const pid = pids.length ? (pids.length > 4 ? `${pids.slice(0, 3).join(", ")} +${pids.length - 3}` : pids.join(", ")) : "\u2013";
  const restartNote = g.restarts ? ` \u00b7 restart ${g.restarts}` : "";
  // Aksi dikirim per-worker (Supervisor tidak mengenal grup).
  const targets = g.isPHP ? g.workers.map((w) => w.Name) : [g.svc.Name];
  const btn = (op, cls, text) =>
    targets.map((n) => `<button data-op="${op}" data-svc="${esc(n)}" class="${cls}">${text}</button>`).join("");
  const acts = g.isPHP
    ? `<div class="mt-3 flex gap-2">
         <button data-op="start" data-group="${esc(g.key)}" class="${BTN_START}">Start semua</button>
         <button data-op="stop" data-group="${esc(g.key)}" class="${BTN_STOP}">Stop semua</button>
       </div>
       <div class="mt-2 flex flex-wrap gap-2">
         ${g.workers.map((w) => `<button data-op="restart" data-svc="${esc(w.Name)}" class="${BTN_RESTART}">${esc(w.Name.replace("php-" + g.version + "-", "#"))}</button>`).join("")}
       </div>`
    : `<div class="mt-3 flex gap-2">${btn("start", BTN_START, "Start")}${btn("stop", BTN_STOP, "Stop")}${btn("restart", BTN_RESTART, "Restart")}</div>`;
  return `
    <div class="rounded-xl border border-slate-800 bg-slate-900 p-4">
      <div class="flex items-center justify-between gap-2">
        <h3 class="font-semibold text-white">${esc(g.name)}</h3>
        ${badge(ok, label, label)}
      </div>
      <p class="mt-1 truncate text-xs text-slate-500">PID ${esc(pid)}${restartNote}${g.lastErr ? " \u00b7 " + esc(g.lastErr) : ""}</p>
      ${acts}
    </div>`;
}

async function refreshStatus() {
  const s = await api("/api/status");
  const sig = JSON.stringify(s);
  if (sig === lastStatus) return;
  lastStatus = sig;
  $("#services").innerHTML = groupServices(s.services).map(serviceCard).join("");
  $("#sum-sites").textContent = s.sites ?? 0;
  const t = s.tunnel;
  $("#sum-tunnel").innerHTML = t
    ? badge(t.Status === "healthy", `${esc(t.Status)} · ${t.Connections?.length ?? 0} koneksi`, `${esc(t.Status)} · ${t.Connections?.length ?? 0} koneksi`)
    : s.err
      ? `<span class="text-xs text-red-400">${esc(s.err)}</span>`
      : badge(false, "", "belum di-setup");
}

async function serviceAction(e) {
  const b = e.target.closest("button[data-op]");
  if (!b) return;
  b.disabled = true;
  const op = b.dataset.op;
  try {
    if (b.dataset.group) {
      // Start/Stop "semua": kirim ke tiap worker grup, tunggu semuanya.
      const s = await api("/api/status");
      const names = (s.services || []).filter((x) => x.Name.startsWith(b.dataset.group + "-")).map((x) => x.Name);
      const results = await Promise.allSettled(names.map((n) => api(`/api/services/${encodeURIComponent(n)}/${op}`, { method: "POST", quiet: true })));
      const failed = results.filter((r) => r.status === "rejected").length;
      if (failed) toast(`${b.dataset.group}: ${failed}/${names.length} worker gagal di-${op}`, "err");
      else toast(`${b.dataset.group}: ${op} ${names.length} worker OK`);
    } else {
      await api(`/api/services/${encodeURIComponent(b.dataset.svc)}/${op}`, { method: "POST" });
      toast(`${b.dataset.svc}: ${op} OK`);
    }
  } catch {}
  b.disabled = false;
  refreshStatus();
}

let logTimer = null;

async function initLogViewer() {
  let sites = [];
  try { sites = (await api("/api/sites", { quiet: true })) || []; } catch {}
  const opts = [
    ["pawon", "Panel (pawon.log)"],
    ["nginx-error", "nginx error (global)"],
    ["mariadb", "MariaDB"],
    ["cloudflared", "cloudflared"],
  ];
  for (const v of await phpVersions()) opts.push([`php-${v}`, `PHP ${v}`]);
  for (const s of sites) {
    opts.push([`nginx/${s.hostname}-access`, `nginx · ${s.subdomain} access`]);
    opts.push([`nginx/${s.hostname}-error`, `nginx · ${s.subdomain} error`]);
    if (s.type === "laravel") opts.push([`laravel:${s.id}`, `Laravel · ${s.subdomain}`]);
  }
  const sel = $("#log-source");
  sel.innerHTML = opts.map(([v, l]) => `<option value="${esc(v)}">${esc(l)}</option>`).join("");
  sel.addEventListener("change", tailLog);
  $("#log-tail").addEventListener("change", tailLog);
  $("#log-refresh").addEventListener("click", tailLog);
  $("#log-pause").addEventListener("click", () => setAuto(!logTimer));
  setAuto(true);
}

function setAuto(on) {
  clearInterval(logTimer);
  logTimer = on ? setInterval(tailLog, 2000) : null;
  const b = $("#log-pause");
  b.textContent = on ? "Berhenti" : "Auto-refresh";
  b.className = on ? BTN_GHOST : BTN_START;
}

async function tailLog() {
  const name = $("#log-source").value;
  if (!name) return;
  const out = $("#log-output");
  try {
    // {name...} wildcard handler → slash dipertahankan per-segment.
    const d = await api(`/api/logs/${name.split("/").map(encodeURIComponent).join("/")}?tail=${$("#log-tail").value}`, { quiet: true });
    out.textContent = d.lines.length ? d.lines.join("\n") : "(log kosong)";
    out.scrollTop = out.scrollHeight;
    $("#log-meta").textContent = `${d.name} · ${d.lines.length} baris`;
  } catch (e) {
    $("#log-meta").textContent = e.message; // 404 = file belum ada, jangan spam toast
  }
}

async function initDashboard() {
  $("#services").addEventListener("click", serviceAction);
  await Promise.all([refreshStatus(), initLogViewer()]);
  setInterval(refreshStatus, 3000);
}

// ---------- Sites ----------

async function loadSites() {
  const sites = (await api("/api/sites")) || [];
  $("#sites-body").innerHTML = sites.map((s) => `
    <tr class="border-t border-slate-800">
      <td class="px-3 py-2">
        <a class="text-emerald-400 hover:underline" href="https://${esc(s.hostname)}" target="_blank" rel="noopener">${esc(s.hostname)}</a>
        <div class="text-xs text-slate-500">http://${esc(s.subdomain)}.test</div>
      </td>
      <td class="px-3 py-2">${esc(s.type)}</td>
      <td class="px-3 py-2">${esc(s.php)}</td>
      <td class="px-3 py-2">${s.db ? `<span class="text-emerald-400">${esc(s.db.name)}</span> <span class="text-xs text-slate-500">${esc(s.db.user)}</span>` : `<span class="text-xs text-slate-500">tanpa DB</span>`}</td>
      <td class="px-3 py-2 space-x-1">${badge(s.ingress_ok, "ingress", "ingress?")} ${badge(s.dns_ok, "dns", "dns?")}</td>
      <td class="whitespace-nowrap px-3 py-2 text-right">
        <button data-act="db" data-id="${esc(s.id)}" data-host="${esc(s.hostname)}" data-has="${s.db ? "1" : ""}" class="${BTN_GHOST}">${s.db ? "Reset DB" : "Buat DB"}</button>
        <button data-act="env" data-id="${esc(s.id)}" data-host="${esc(s.hostname)}" class="${BTN_GHOST}">.env</button>
        <button data-act="del" data-id="${esc(s.id)}" data-host="${esc(s.hostname)}" class="${BTN_DANGER}">Hapus</button>
      </td>
    </tr>`).join("") || `<tr><td colspan="6" class="px-3 py-8 text-center text-slate-500">Belum ada site.</td></tr>`;
}

async function loadZones() {
  const zones = (await api("/api/zones")) || [];
  $("#f-zone").innerHTML = zones.length
    ? zones.map((z) => `<option value="${esc(z.id)}">${esc(z.name)}</option>`).join("")
    : `<option value="">— setup tunnel dulu —</option>`;
}

async function rowAction(e) {
  const b = e.target.closest("button[data-act]");
  if (!b) return;
  if (b.dataset.act === "del") {
    if (!confirm(`Hapus site ${b.dataset.host}? Vhost, hosts, DNS & ingress ikut dihapus; folder tidak.`)) return;
    await api(`/api/sites/${encodeURIComponent(b.dataset.id)}`, { method: "DELETE" });
    toast("Site dihapus");
    await loadSites();
  } else if (b.dataset.act === "db") {
    const has = b.dataset.has === "1";
    const msg = has
      ? `Reset password database ${b.dataset.host}? Password lama tidak berlaku lagi; perbarui .env site ini.`
      : `Buat database untuk ${b.dataset.host}? Kredensial akan ditampilkan di tabel.`;
    if (!confirm(msg)) return;
    const s = await api("/api/dbs", { method: "POST", body: { site_id: b.dataset.id } });
    toast(`DB ${s.db.name} (${s.db.user}) siap — kredensial ada di tabel`);
    await loadSites();
  } else if (b.dataset.act === "env") {
    await openEnv(b.dataset.id, b.dataset.host);
  }
}

let envSiteId = null;

async function openEnv(id, host) {
  envSiteId = id;
  $("#env-host").textContent = `— ${host}`;
  $("#env-text").value = "(memuat…)";
  $("#modal").classList.replace("hidden", "flex");
  try {
    $("#env-text").value = await api(`/api/sites/${encodeURIComponent(id)}/env`, { raw: true, quiet: true });
  } catch {
    $("#env-text").value = ""; // .env belum ada → simpan untuk membuat
  }
}

function closeEnv() {
  envSiteId = null;
  $("#modal").classList.replace("flex", "hidden");
}

async function saveEnv() {
  if (!envSiteId) return;
  // Body dikirim sebagai string JSON (guard mewajibkan Content-Type JSON).
  await api(`/api/sites/${encodeURIComponent(envSiteId)}/env`, { method: "PUT", body: $("#env-text").value });
  toast(".env disimpan");
  closeEnv();
}

async function addSite(e) {
  e.preventDefault();
  const body = {
    subdomain: $("#f-sub").value.trim(),
    zone_id: $("#f-zone").value,
    root: $("#f-root").value.trim(),
    type: $("#f-type").value,
    php: $("#f-php").value,
  };
  const s = await api("/api/sites", { method: "POST", body });
  toast(`Site ${s.hostname} dibuat`);
  if ($("#f-db").checked) {
    const s2 = await api("/api/dbs", { method: "POST", body: { site_id: s.id } });
    if (s2.db) toast(`DB ${s2.db.name} (${s2.db.user}) dibuat — kredensial ada di tabel`);
  }
  e.target.reset();
  delete $("#f-root").dataset.touched;
  await loadSites();
}

async function initSites() {
  $("#site-form").addEventListener("submit", addSite);
  $("#sites-body").addEventListener("click", rowAction);
  $("#env-close").addEventListener("click", closeEnv);
  $("#env-save").addEventListener("click", saveEnv);
  $("#modal").addEventListener("click", (e) => { if (e.target === $("#modal")) closeEnv(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") closeEnv(); });
  $("#f-sub").addEventListener("input", () => {
    const r = $("#f-root");
    if (!r.dataset.touched) r.value = "sites/" + $("#f-sub").value.trim().toLowerCase();
  });
  $("#f-root").addEventListener("input", () => { $("#f-root").dataset.touched = "1"; });
  await Promise.all([loadSites(), loadZones(), fillPHP()]);
}

async function fillPHP() {
  const v = await phpVersions();
  $("#f-php").innerHTML = v.length
    ? v.map((x) => `<option>${esc(x)}</option>`).join("")
    : `<option value="">— pool PHP belum jalan —</option>`;
}

// ---------- Tunnel ----------

async function refreshTunnel() {
  const box = $("#tunnel-status");
  try {
    const t = await api("/api/tunnel/status", { quiet: true });
    box.innerHTML = `
      <div class="flex flex-wrap items-center gap-3">
        ${badge(t.Status === "healthy", `${esc(t.Status)} · ${t.Connections?.length ?? 0} koneksi`, `${esc(t.Status)} · ${t.Connections?.length ?? 0} koneksi`)}
        <span class="text-slate-300">Tunnel <b class="text-white">${esc(t.Name)}</b></span>
        <span class="font-mono text-xs text-slate-500">${esc(t.ID)}</span>
      </div>`;
  } catch (e) {
    box.innerHTML = `${badge(false, "", "belum di-setup")}<p class="mt-2">${esc(e.message)} — paste API token di bawah untuk setup.</p>`;
  }
}

async function setupTunnel(e) {
  e.preventDefault();
  await api("/api/tunnel/setup", { method: "POST", body: { api_token: $("#t-token").value.trim() } });
  toast("Tunnel di-setup — site berikutnya otomatis ter-wire ke ingress");
  $("#t-token").value = "";
  await refreshTunnel();
}

async function initTunnel() {
  $("#tunnel-form").addEventListener("submit", setupTunnel);
  $("#t-refresh").addEventListener("click", refreshTunnel);
  await refreshTunnel();
}

// ---------- Settings ----------

async function initSettings() {
  const v = await phpVersions();
  $("#php-live").textContent = v.length ? v.join(", ") : "\u2013";
  $("#copy-log-path").addEventListener("click", async () => {
    await navigator.clipboard.writeText($("#log-path").textContent);
    toast("Path log disalin");
  });
}

// ---------- init ----------


const pages = { dashboard: initDashboard, sites: initSites, tunnel: initTunnel, settings: initSettings };
function boot() {
  const page = document.body.dataset.page;
  document.querySelectorAll("[data-nav]").forEach((a) => {
    a.className = `text-sm ${a.dataset.nav === page ? "font-semibold text-emerald-400" : "text-slate-300 hover:text-white"}`;
  });
  pages[page]?.();
}
if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
else boot();
