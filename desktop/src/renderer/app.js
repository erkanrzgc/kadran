/**
 * Panely arayüz mantığı.
 *
 * Düz JavaScript, derleme adımı yok. Erişebildiği tek şey `window.panely`
 * — preload'daki beyaz liste (bkz. src/preload/index.ts). Node API'leri,
 * `require` ve `ipcRenderer` bu bağlamda YOK.
 *
 * DOM'a metin her zaman textContent ile yazılıyor, innerHTML ile değil:
 * sunucudan gelen değerler (hata mesajları, denetim alanları) güvenilmez
 * girdi sayılmalı.
 */

"use strict";

const api = window.panely;

const el = (id) => document.getElementById(id);

/** Şu an bağlı hedef. Boş dize yerel soketi ifade eder. */
let currentTarget = null;
let profiles = [];

// ── Yardımcılar ──────────────────────────────────────────────────────

function showToast(message) {
  const toast = el("toast");
  toast.textContent = message;
  toast.hidden = false;
  clearTimeout(showToast._timer);
  showToast._timer = setTimeout(() => {
    toast.hidden = true;
  }, 9000);
}

function errorMessage(err) {
  if (!err) return "bilinmeyen hata";
  // Electron IPC hataları "Error: ..." öneki taşır; kullanıcıya
  // göstermeden önce temizliyoruz.
  const raw = err.message ?? String(err);
  return raw.replace(/^Error:\s*/, "").replace(/^Error invoking remote method '[^']*':\s*/, "");
}

function humanBytes(n) {
  const value = Number(n);
  if (!Number.isFinite(value) || value <= 0) return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = value;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

/** humanDuration, saniyeyi en fazla iki birimle yazar. */
function humanDuration(seconds) {
  const total = Number(seconds);
  if (!Number.isFinite(total) || total < 1) return "0sn";

  const units = [
    [86400, "g"],
    [3600, "sa"],
    [60, "dk"],
    [1, "sn"],
  ];
  const parts = [];
  let rest = Math.floor(total);
  for (const [size, label] of units) {
    if (rest < size) continue;
    parts.push(`${Math.floor(rest / size)}${label}`);
    rest %= size;
    if (parts.length === 2) break;
  }
  return parts.join(" ");
}

function shortFingerprint(fp) {
  const body = String(fp).replace(/^SHA256:/, "");
  if (body.length <= 16) return fp;
  return `SHA256:${body.slice(0, 8)}…${body.slice(-6)}`;
}

function describeActor(actor) {
  if (!actor) return "bilinmiyor";
  if (actor.ssh_key_fingerprint) return shortFingerprint(actor.ssh_key_fingerprint);
  if (actor.label) return actor.label;
  if (actor.origin) return actor.origin;
  return "bilinmiyor";
}

function setRow(id, text, className) {
  const node = el(id);
  node.textContent = text;
  node.className = className ?? "";
}

// ── Zincir durumu ────────────────────────────────────────────────────

/**
 * Zincir durumunu görsel sınıfa çevirir.
 *
 * DOĞRULANAMADI ile GEÇERSİZ'in ayrı renkler taşıması kritik: birincisi
 * bir işletim sorunu (executor kapalı), ikincisi kurcalama şüphesi.
 * Aynı görünürlerse operatör her yeniden başlatmada sahte alarma koşar.
 */
const CHAIN_STATES = {
  CHAIN_STATUS_VALID: { label: "GEÇERLİ", tone: "is-ok" },
  CHAIN_STATUS_INVALID: { label: "GEÇERSİZ", tone: "is-bad" },
  CHAIN_STATUS_UNREACHABLE: { label: "DOĞRULANAMADI", tone: "is-warn" },
  CHAIN_STATUS_UNSPECIFIED: { label: "BİLİNMİYOR", tone: "" },
};

function renderChain(prefix, cardId, status, checked, detail) {
  const state = CHAIN_STATES[status] ?? CHAIN_STATES.CHAIN_STATUS_UNSPECIFIED;

  const badge = el(`${prefix}-chain-badge`);
  badge.textContent = state.label;
  badge.className = `badge ${state.tone}`;

  el(`${prefix}-chain-count`).textContent =
    Number(checked) === 1 ? "1 kayıt doğrulandı" : `${Number(checked) || 0} kayıt doğrulandı`;

  el(`${prefix}-chain-detail`).textContent = detail ?? "";
  el(cardId).className = `chain ${state.tone}`;
}

// ── Görünüm güncellemeleri ───────────────────────────────────────────

function renderStatus(target, payload) {
  el("stage-empty").hidden = true;
  el("stage-content").hidden = false;

  el("stage-target").textContent = target === "" ? "yerel soket" : target;

  const daemonVersion = payload.daemon_version || "—";
  setRow("daemon-version", daemonVersion);
  setRow("daemon-uptime", humanDuration(payload.daemon_uptime_seconds));

  // panelyd root çalışıyorsa ürünün merkezî iddiası çökmüş demektir; bu
  // sessizce geçilmez. panelyd zaten root ile başlamayı reddediyor —
  // burası o kontrolün görünür yedeği.
  const user = payload.running_as_user || "bilinmiyor";
  setRow("daemon-user", user === "root" ? "root — KURULUM BOZUK" : user, user === "root" ? "is-bad" : "");

  if (payload.executor_reachable) {
    setRow("executor-state", "erişilebilir", "is-ok");
    setRow("executor-version", payload.executor_version || "—");
  } else {
    setRow("executor-state", "erişilemiyor", "is-warn");
    setRow("executor-version", "—", "is-faint");
  }

  const host = payload.host ?? {};
  setRow("docker-version", host.docker_version || "yok", host.docker_version ? "" : "is-faint");
  setRow("host-name", payload.hostname || "—");
  setRow("host-kernel", host.kernel_version || "—");

  const total = Number(host.memory_total_bytes) || 0;
  setRow(
    "host-memory",
    total > 0 ? `${humanBytes(host.memory_available_bytes)} / ${humanBytes(total)}` : "—",
  );

  el("stage-meta").textContent = payload.executor_reachable
    ? "Ayrıcalıklı işlemler kullanılabilir."
    : "Executor erişilemiyor — ayrıcalıklı işlemler çalışmaz.";
}

function renderAudit(payload) {
  const body = el("audit-body");
  body.textContent = "";

  const records = payload.records ?? [];
  el("audit-empty").hidden = records.length > 0;
  if (records.length === 0) {
    el("audit-empty").textContent = "Zincirde kayıt yok.";
    return;
  }

  // En yeni üstte: operatörün aradığı şey genellikle en son olan.
  for (const record of [...records].reverse()) {
    const tr = document.createElement("tr");

    const cells = [
      String(record.seq ?? "—"),
      record.ts ? new Date(record.ts).toLocaleString() : "—",
      describeActor(record.actor),
      record.action || "—",
      record.target || "—",
    ];

    for (const text of cells) {
      const td = document.createElement("td");
      td.textContent = text;
      tr.appendChild(td);
    }

    const outcome = document.createElement("td");
    const info = {
      AUDIT_OUTCOME_SUCCESS: ["BAŞARILI", "outcome-ok"],
      AUDIT_OUTCOME_FAILURE: ["BAŞARISIZ", "outcome-bad"],
      AUDIT_OUTCOME_DENIED: ["REDDEDİLDİ", "outcome-denied"],
    }[record.outcome] ?? ["belirsiz", ""];
    outcome.textContent = info[0];
    outcome.className = info[1];
    tr.appendChild(outcome);

    body.appendChild(tr);
  }
}

function renderProfiles() {
  const list = el("profile-list");
  list.textContent = "";
  el("profiles-empty").hidden = profiles.length > 0;

  for (const profile of profiles) {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "profile-btn";
    button.textContent = profile.name;
    button.title = profile.target;
    if (profile.target === currentTarget) button.setAttribute("aria-current", "true");
    button.addEventListener("click", () => {
      el("target").value = profile.target;
      void connect(profile.target);
    });
    li.appendChild(button);
    list.appendChild(li);
  }
}

// ── Eylemler ─────────────────────────────────────────────────────────

async function withBusy(button, fn) {
  const previous = button.textContent;
  button.disabled = true;
  try {
    return await fn();
  } finally {
    button.disabled = false;
    button.textContent = previous;
  }
}

async function connect(target) {
  const button = el("connect-btn");
  await withBusy(button, async () => {
    button.textContent = "Bağlanıyor…";
    try {
      const status = await api.status(target);
      currentTarget = target;
      renderStatus(target, status);
      renderProfiles();
      await rememberProfile(target);
      await refreshAudit();
      await verifyChains();
    } catch (err) {
      showToast(`Bağlanılamadı: ${errorMessage(err)}`);
    }
  });
}

async function rememberProfile(target) {
  if (target === "") return;
  if (profiles.some((p) => p.target === target)) return;

  profiles = [...profiles, { name: target, target }];
  try {
    profiles = await api.saveProfiles(profiles);
  } catch (err) {
    showToast(`Profil kaydedilemedi: ${errorMessage(err)}`);
  }
  renderProfiles();
}

async function refreshAudit() {
  if (currentTarget === null) return;
  try {
    renderAudit(await api.auditList(currentTarget, 0, 50));
  } catch (err) {
    el("audit-empty").hidden = false;
    el("audit-empty").textContent = `Okunamadı: ${errorMessage(err)}`;
  }
}

async function verifyChains() {
  if (currentTarget === null) return;
  try {
    const result = await api.auditVerify(currentTarget);
    renderChain(
      "daemon",
      "chain-daemon",
      result.daemon_status,
      result.records_checked,
      result.detail,
    );
    renderChain(
      "executor",
      "chain-executor",
      result.executor_status,
      result.executor_records_checked,
      result.executor_detail,
    );
  } catch (err) {
    showToast(`Zincir doğrulanamadı: ${errorMessage(err)}`);
  }
}

// ── Bağlama ──────────────────────────────────────────────────────────

el("connect-form").addEventListener("submit", (event) => {
  event.preventDefault();
  void connect(el("target").value.trim());
});

el("verify-btn").addEventListener("click", (event) => {
  void withBusy(event.currentTarget, async () => {
    event.currentTarget.textContent = "Doğrulanıyor…";
    await verifyChains();
  });
});

el("audit-btn").addEventListener("click", (event) => {
  void withBusy(event.currentTarget, async () => {
    event.currentTarget.textContent = "Okunuyor…";
    await refreshAudit();
  });
});

// ── Açılış ───────────────────────────────────────────────────────────

void (async () => {
  try {
    const version = await api.version();
    el("sidecar-version").textContent = `sidecar ${version.version} · protokol ${version.protocol}`;
  } catch (err) {
    el("sidecar-version").textContent = "sidecar başlatılamadı";
    showToast(`Sidecar başlatılamadı: ${errorMessage(err)}`);
  }

  try {
    profiles = await api.loadProfiles();
    renderProfiles();
  } catch (err) {
    showToast(`Profiller okunamadı: ${errorMessage(err)}`);
  }
})();
