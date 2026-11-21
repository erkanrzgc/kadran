/**
 * Bellek ölçümü (K-169): node scripts/measure-memory.mjs <idle|console-load|navigate-load>
 *
 * Duman ikilisini (`-tags desktop,production,smoke`) derler, 5 sahte sunucuyla açar,
 * arayüzü duman stdin ExecJS protokolüyle sürer ve süreç ağacının (Kadran + WebView2
 * alt süreçleri) PrivateMemorySize64 toplamını yazar. Her okuma GPU süreci
 * (--type=gpu-process), sayfa/renderer (--type=renderer), Kadran ana süreci ve diğerleri
 * olarak ayrılır.
 *
 * idle:          bütçe ölçümü; konsol açık, 5.000 satır, 60/75/90. sn.
 * console-load:  konsol açık; 200 `app list` + 5 dk `logs -f` (sahte akış); sonra 60 sn boşta.
 * navigate-load: konsol kapalı; 200 gezinme + 5 dk gezinme; sonra 60 sn boşta.
 * console-load ve navigate-load: başlangıç okuması açılıştan 60 sn sonra, bitiş okuması
 * yük bittikten 60 sn sonra. Aynı anda yalnız BİR koşu çalıştırın (makine kilitlenir).
 *
 * Çıktının son satırı "RESULT {json}" (makine okuyabilir).
 */
import { spawn, execFileSync, execSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const MODES = ["idle", "console-load", "navigate-load"];
const mode = process.argv[2];
if (!MODES.includes(mode)) {
  console.error(`usage: node scripts/measure-memory.mjs <${MODES.join("|")}>`);
  process.exit(2);
}

const SETTLE_S = 60; // açılıştan başlangıç okumasına
const IDLE_AFTER_LOAD_S = 60; // yükten bitiş okumasına
const LOAD_REPEATS = 200; // app list sayısı / gezinme sayısı
const LOAD_SECONDS = 300; // logs -f süresi / ek gezinme süresi
const PROGRESS_EVERY_S = 60; // yük sırasında ara okuma
const HISTORY_LINES = 6000; // idle: tampon tavanı 5.000'in üstünde

const desktop = fileURLToPath(new URL("..", import.meta.url)).replace(/[\\/]$/, "");
const exe = join(desktop, "out", "smoke", "Kadran-smoke.exe");
execSync("npm run build", { cwd: desktop, stdio: "inherit" });
execFileSync("go", ["build", "-tags", "desktop,production,smoke", "-trimpath", "-o", exe, "."], { cwd: desktop, stdio: "inherit" });

const dir = mkdtempSync(join(tmpdir(), "kadran-measure-"));
const servers = ["fake", "badseq", "totp", "root", "slow"].map((n) => ({ name: n, target: n === "fake" ? "fake@test" : `fake@${n}`, provider: "hetzner" }));
writeFileSync(join(dir, "fake-servers.json"), JSON.stringify({ version: 1, servers }));
mkdirSync(join(dir, "roaming", "kadran"), { recursive: true });
writeFileSync(join(dir, "roaming", "kadran", "servers.json"), JSON.stringify({ version: 1, servers }));
writeFileSync(join(dir, "appearance.json"), JSON.stringify({ version: 2, language: "en", theme: "light" }));

const child = spawn(exe, [], {
  cwd: desktop,
  stdio: ["pipe", "pipe", "inherit"],
  env: { ...process.env, APPDATA: join(dir, "roaming"), KADRAN_FAKE_SERVERS_FILE: join(dir, "fake-servers.json"), KADRAN_SMOKE_USERDATA: dir, KADRAN_SMOKE_SIDECAR: join(desktop, "test", "fixtures", "fake-sidecar.mjs") },
});
const startedAt = Date.now();
const pending = new Map();
let next = 0;
createInterface({ input: child.stdout }).on("line", (line) => {
  if (!line.startsWith("SMOKE ")) return;
  const r = JSON.parse(line.slice(6));
  pending.get(r.id)?.(r);
  pending.delete(r.id);
});
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
function run(code, timeout = 30000) {
  const id = ++next;
  return new Promise((done) => {
    const t = setTimeout(() => { pending.delete(id); done(null); }, timeout);
    pending.set(id, (r) => { clearTimeout(t); done(r); });
    child.stdin.write(JSON.stringify({ id, code }) + "\n");
  });
}
const until = async (expr, label, ms = 30000, poll = 150) => {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    const r = await run(`Boolean(${expr})`, 5000);
    if (r?.ok && r.value) return;
    await sleep(poll);
  }
  throw new Error("timeout: " + label);
};
const q = (s) => `document.querySelector(${JSON.stringify(s)})`;
const click = (s) => run(`${q(s)}.click()`);
const INPUT = '[data-testid="console-input"]';
const OUTPUT = '[data-testid="console-output"]';

// Süreç ağacı (node.exe hariç) ve türe göre özel bellek (MB); ConvertTo-Json ayardan bağımsız.
const sampler = `
param($root)
$all = Get-CimInstance Win32_Process
$ids = New-Object System.Collections.Generic.HashSet[int]
[void]$ids.Add([int]$root)
$grew = $true
$born = @{}; foreach ($p in $all) { $born[[int]$p.ProcessId] = $p.CreationDate }
# PID yeniden kullanimi: yetim surec, adi yeniden verilmis bir koke bagli gorunebilir; cocuk ebeveyninden SONRA dogmus olmali.
while ($grew) { $grew = $false; foreach ($p in $all) { if ($ids.Contains([int]$p.ParentProcessId) -and -not $ids.Contains([int]$p.ProcessId) -and $p.Name -ne 'node.exe' -and $p.CreationDate -ge $born[[int]$p.ParentProcessId]) { [void]$ids.Add([int]$p.ProcessId); $grew = $true } } }
$cmd = @{}; foreach ($p in $all) { $cmd[[int]$p.ProcessId] = [string]$p.CommandLine }
$tot = 0; $gpu = 0; $ren = 0; $main = 0; $oth = 0; $n = 0
foreach ($id in $ids) {
  $pr = Get-Process -Id $id -ErrorAction SilentlyContinue
  if (-not $pr) { continue }
  $b = $pr.PrivateMemorySize64; $tot += $b; $n++
  if ($id -eq [int]$root) { $main += $b }
  elseif ($cmd[$id] -match '--type=gpu-process') { $gpu += $b }
  elseif ($cmd[$id] -match '--type=renderer') { $ren += $b }
  else { $oth += $b }
}
[pscustomobject]@{ total = [math]::Round($tot/1MB,1); gpu = [math]::Round($gpu/1MB,1); renderer = [math]::Round($ren/1MB,1); main = [math]::Round($main/1MB,1); other = [math]::Round($oth/1MB,1); processes = $n } | ConvertTo-Json -Compress
`;
writeFileSync(join(dir, "sample.ps1"), sampler);
function sample() {
  const out = execFileSync("powershell", ["-NoProfile", "-File", join(dir, "sample.ps1"), "-root", String(child.pid)], { encoding: "utf8" }).trim();
  return JSON.parse(out);
}
const readings = [];
function take(label) {
  const s = sample();
  const at = Math.round((Date.now() - startedAt) / 1000);
  readings.push({ label, at, ...s });
  console.log(`${mode} ${label} t=${at}s  totalMB=${s.total} gpuMB=${s.gpu} rendererMB=${s.renderer} mainMB=${s.main} otherMB=${s.other} processes=${s.processes}`);
  return s;
}
const waitUntilAge = (s) => sleep(Math.max(0, s * 1000 - (Date.now() - startedAt)));

async function openServerPage() {
  await until(`document.readyState === "complete" && window.runtime`, "page");
  await until(q('[data-testid="screen-servers"]'), "servers screen");
  await until(q('[data-testid="server-fake@test"]'), "card");
  await click('[data-testid="server-fake@test"]');
  await until(q('[data-testid="screen-status"]'), "status screen");
}

const setInput = (line) => run(`(() => { const el = ${q(INPUT)}; Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, ${JSON.stringify(line)}); el.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`);
const press = (init) => run(`(() => { const el = ${q(INPUT)}; if (!el) return false; el.focus(); el.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...${JSON.stringify(init)} })); return true; })()`);
async function typeLine(line) {
  await setInput(line);
  await press({ key: "Enter" });
}
const echoCount = async () => (await run(`document.querySelectorAll(".console-line.is-echo").length`))?.value ?? 0;
// n. yankı satırı çıkış işareti taşıyor ve girdi satırı açık (ok: yalnız is-exit-ok; değilse ok ya da bad).
const finishedExpr = (n, okOnly) => `(() => { const l = document.querySelectorAll(".console-line.is-echo"); const last = l[l.length - 1]; const i = ${q(INPUT)}; return l.length === ${n} && Boolean(last && ${okOnly ? 'last.classList.contains("is-exit-ok")' : '/is-exit-/.test(last.className)'} && i && !i.readOnly); })()`;

async function openConsole() {
  await click('[data-testid="console-open"]');
  await until(q(INPUT), "console input");
}

// 200 `app list` (okuma komutu, kutu yok); her biri bitince bir sonrası.
async function appListLoop() {
  for (let i = 0; i < LOAD_REPEATS; i++) {
    const n = await echoCount();
    await typeLine("app list");
    await until(finishedExpr(n + 1, true), `app list #${i + 1}`, 30000, 50);
  }
}

// logs -f LOAD_SECONDS sürer (sahte akış 120 ms'de bir satır), Ctrl+C ile biter.
async function followLoop() {
  const n = await echoCount();
  await typeLine("logs -f web");
  const t0 = Date.now();
  let nextProgress = PROGRESS_EVERY_S;
  while ((Date.now() - t0) / 1000 < LOAD_SECONDS) {
    await sleep(1000);
    if ((Date.now() - t0) / 1000 >= nextProgress) {
      take(`progress-${nextProgress}s`);
      nextProgress += PROGRESS_EVERY_S;
    }
  }
  await press({ key: "c", ctrlKey: true });
  await until(finishedExpr(n + 1, false), "logs -f stopped by Ctrl+C", 30000);
}

// Gezinme: sunucu alt menüsü + sol menü, konsol kapalı.
const SUBNAV = ["nav-apps", "nav-alarms", "nav-backups", "nav-audit", "nav-github", "nav-status"];
const RAIL = ["rail-apps", "rail-alarms", "rail-audit", "nav-settings", "rail-servers"];
let navCursor = 0;
async function navigateOnce() {
  const step = navCursor++ % (SUBNAV.length + RAIL.length);
  const id = step < SUBNAV.length ? SUBNAV[step] : RAIL[step - SUBNAV.length];
  await click(`[data-testid="${id}"]`);
  await sleep(60);
  if (id === "rail-servers") {
    // sunucu listesinden kart açıp durum ekranına dönülür; alt menü yine görünür olur
    await until(q('[data-testid="server-fake@test"]'), "card (return)", 10000);
    await click('[data-testid="server-fake@test"]');
    await until(q('[data-testid="screen-status"]'), "status screen (return)", 10000);
  }
}
async function navigateLoop() {
  for (let i = 0; i < LOAD_REPEATS; i++) await navigateOnce();
  const t0 = Date.now();
  let nextProgress = PROGRESS_EVERY_S;
  while ((Date.now() - t0) / 1000 < LOAD_SECONDS) {
    await navigateOnce();
    await sleep(100);
    if ((Date.now() - t0) / 1000 >= nextProgress) {
      take(`progress-${nextProgress}s`);
      nextProgress += PROGRESS_EVERY_S;
    }
  }
}

try {
  await openServerPage();
  if (mode !== "navigate-load") await openConsole();
  if (mode === "idle") {
    await typeLine(`logs -tail ${HISTORY_LINES} web`);
    await until(`${q(OUTPUT)}.textContent.includes("history line ${HISTORY_LINES}")`, `${HISTORY_LINES} lines`, 60000);
    console.log("panel lines:", (await run(`${q(OUTPUT)}.children.length`))?.value);
    for (const at of [60, 75, 90]) {
      await waitUntilAge(at);
      take(`t=${at}s`);
    }
  } else {
    await waitUntilAge(SETTLE_S);
    const start = take("start");
    if (mode === "console-load") {
      await appListLoop();
      take("after-app-list");
      await followLoop();
    } else {
      await navigateLoop();
    }
    take("after-load");
    await sleep(IDLE_AFTER_LOAD_S * 1000);
    const end = take("end");
    console.log(`${mode} growthMB total=${(end.total - start.total).toFixed(1)} gpu=${(end.gpu - start.gpu).toFixed(1)} renderer=${(end.renderer - start.renderer).toFixed(1)} main=${(end.main - start.main).toFixed(1)}`);
  }
  console.log("RESULT " + JSON.stringify({ mode, readings }));
} finally {
  await run("window.runtime.Quit()", 2000);
  child.stdin.end();
  await sleep(1500);
  child.kill();
}
