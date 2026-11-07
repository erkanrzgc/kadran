/**
 * Uçtan uca duman testi (K-165): duman ikilisini (`-tags smoke`) derler,
 * geçici bir kullanıcı verisiyle açar ve test/smoke/journey.ts'i yürütür.
 * Çıkış kodu 0 = geçti, 1 = kaldı.
 *
 * Sayfaya ulaşma yolu: go-webview2 hata ayıklama portunun dışarıdan
 * açılmasını engelliyor. İkili stdin'den {"id","code"} alır, kodu sayfada
 * çalıştırır ve sonucu stdout'a "SMOKE {…}" satırı olarak yazar
 * (desktop/smoke_windows.go). Konsol ve CSP hataları sayfadaki toplayıcıda
 * (window.__smokeProblems) birikir.
 *
 * Kullanım (desktop/ içinde, önce `npm run build`; PATH'te go ve node):
 *   node scripts/smoke.ts            # yolculukla
 *   node scripts/smoke.ts --quick    # yalnız açılış, köprü, Node sızıntısı
 *
 * Pencere görünür açılır: gizli pencerede WebView2 kare çizmez.
 */

import { spawn, execFileSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

import { checkWordmark, evaluate, journey, waitForRoot, type Page } from "../test/smoke/journey.ts";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = resolve(here, "..");
const exe = join(desktop, "dist", "Kadran-smoke.exe");
const quick = process.argv.includes("--quick");
const PREFIX = "SMOKE ";

/** Go köprüsünün dışa açık yöntemleri (desktop/internal/bridge/bridge_test.go Whitelist). */
const EXPECTED_BRIDGE = [
  "AddServer", "AlarmsList", "AppCreate", "AppDelete", "AppDeploy", "AppGet", "AppLogs", "AppRollback", "AppUpdate", "AppsList",
  "AuditList", "AuditVerify", "BackupCreate", "BackupsList", "CopyText", "ListServers", "LoadAppearance", "RemoveServer",
  "SaveAppearance", "Status", "UpdateServer", "Version", "WindowControl",
];

/** Çok uzun ad ve hedef: kartta taşma olmamalı. */
const LONG_NAME = "uzun-" + "ad".repeat(60);
const LONG_TARGET = "fake@" + "uzun-hedef.".repeat(11) + "example";

function prepareUserData(): string {
  const dir = mkdtempSync(join(tmpdir(), "kadran-smoke-"));
  // Ortak sunucu listesi (K-162): sahte sunucu bu dosyayı okur ve yazar;
  // kullanıcının gerçek servers.json'una dokunulmaz.
  const servers = [
    { name: "fake", target: "fake@test", provider: "hetzner" },
    { name: "down", target: "fake@down", provider: "hetzner" },
    { name: "badseq", target: "fake@badseq", provider: "google-cloud" },
    { name: "totp", target: "fake@totp", provider: "hostinger" },
    { name: "noalarms", target: "fake@noalarms" },
    { name: "root", target: "fake@root" },
    { name: "flaky", target: "fake@flaky" },
    { name: "flaky2", target: "fake@flaky2" },
    { name: "verifyfail", target: "fake@verifyfail" },
    { name: "slow", target: "fake@slow" },
    { name: LONG_NAME, target: LONG_TARGET, provider: "kendi-sunucum" },
  ];
  writeFileSync(join(dir, "fake-servers.json"), JSON.stringify({ version: 1, servers }));
  // Kaldırılan cam teması kayıtlı: açılışta açığa dönmeli (K-163).
  writeFileSync(join(dir, "appearance.json"), JSON.stringify({ version: 2, language: "en", theme: "glass" }));
  return dir;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

interface Reply { id: number; ok: boolean; value?: unknown; error?: string }

/** İkiliyle satır-bazlı konuşma: her komut bir id taşır, yanıt aynı id ile döner. */
class Session {
  private next = 0;
  private readonly pending = new Map<number, (r: Reply) => void>();

  constructor(child: ChildProcessWithoutNullStreams) {
    this.child = child;
    createInterface({ input: child.stdout }).on("line", (line) => {
      if (!line.startsWith(PREFIX)) {
        console.log(line);
        return;
      }
      const reply = JSON.parse(line.slice(PREFIX.length)) as Reply;
      const done = this.pending.get(reply.id);
      this.pending.delete(reply.id);
      done?.(reply);
    });
  }

  private readonly child: ChildProcessWithoutNullStreams;

  /** Yanıt gelmezse null: sayfa (ya da Wails çalışma zamanı) henüz hazır değil. */
  request(command: Record<string, unknown>, timeoutMs: number): Promise<Reply | null> {
    const id = ++this.next;
    return new Promise((done) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        done(null);
      }, timeoutMs);
      this.pending.set(id, (r) => {
        clearTimeout(timer);
        done(r);
      });
      this.child.stdin.write(JSON.stringify({ id, ...command }) + "\n");
    });
  }
}

function page(session: Session, evidence: string): Page {
  return {
    async evaluate(code) {
      const r = await session.request({ code }, 35_000);
      if (r === null) throw new Error(`renderer did not answer: ${code.slice(0, 120)}`);
      if (!r.ok) throw new Error(r.error ?? "evaluation failed");
      return r.value;
    },
    async screenshot(name) {
      const path = join(evidence, `${name}.png`);
      const r = await session.request({ capture: path }, 15_000);
      if (r === null || !r.ok) throw new Error(`screenshot ${name}: ${r?.error ?? "no answer"}`);
      console.log(`UI evidence: ${path}`);
    },
    async resize(width, height) {
      await this.evaluate(`window.runtime.WindowSetSize(${width}, ${height})`);
    },
  };
}

/** Sayfa ve Wails çalışma zamanı hazır olana dek kısa aralıklarla sorar. */
async function waitForPage(session: Session): Promise<boolean> {
  const until = Date.now() + 30_000;
  while (Date.now() < until) {
    const r = await session.request({ code: `document.readyState === "complete" && Boolean(window.runtime)` }, 1_000);
    if (r?.ok && r.value === true) return true;
    await sleep(200);
  }
  return false;
}

async function checks(win: Page, problems: string[]): Promise<void> {
  if (!(await waitForRoot(win))) problems.push("React root did not render");
  // Arayüzün gördüğü yüzeyi arayüze sorarak doğruluyoruz.
  const bound = await evaluate<string[]>(win, "Object.keys(window.go ?? {}).sort()");
  if (JSON.stringify(bound) !== JSON.stringify(["bridge"])) problems.push(`bound packages differ: ${JSON.stringify(bound)}`);
  const surface = await evaluate<string[]>(win, "Object.keys(window.go?.bridge?.Bridge ?? {}).sort()");
  if (JSON.stringify(surface) !== JSON.stringify(EXPECTED_BRIDGE)) problems.push(`bridge surface differs: ${JSON.stringify(surface)}`);
  // Arayüz Node'a ERİŞEMEMELİ.
  const leaked = await evaluate<string>(win, "[typeof require, typeof process, typeof module].join(',')");
  if (leaked !== "undefined,undefined,undefined") problems.push(`Node leaked into the renderer: ${leaked}`);
  // Permissions-Policy WebView2'ye ulaştı: konum istenemez (internal/headers).
  const geo = await evaluate<number>(win, `new Promise((done) => { const t = setTimeout(() => done(-1), 5000); navigator.geolocation.getCurrentPosition(() => { clearTimeout(t); done(0); }, (e) => { clearTimeout(t); done(e.code); }); })`);
  if (geo !== 1) problems.push(`geolocation was not denied by the permissions policy (${geo})`);
  await checkWordmark(win);
  if (!quick) await journey(win);
}

async function main(): Promise<number> {
  execFileSync("go", ["build", "-tags", "desktop,production,smoke", "-trimpath", "-o", exe, "."], { cwd: desktop, stdio: "inherit" });
  const userData = prepareUserData();
  process.env.KADRAN_FAKE_SERVERS_FILE = join(userData, "fake-servers.json");
  const child = spawn(exe, [], {
    cwd: desktop,
    stdio: ["pipe", "pipe", "inherit"],
    env: { ...process.env, KADRAN_SMOKE_USERDATA: userData, KADRAN_SMOKE_SIDECAR: join(desktop, "test", "fixtures", "fake-sidecar.mjs") },
  });
  let exited = false;
  child.on("exit", () => (exited = true));
  const session = new Session(child);
  const win = page(session, join(userData, "evidence"));

  const problems: string[] = [];
  try {
    if (!(await waitForPage(session))) throw new Error("the page did not answer in 30 s");
    await checks(win, problems);
  } catch (err) {
    problems.push(`smoke: ${(err as Error).message}`);
  }
  try {
    problems.push(...(await evaluate<string[]>(win, "window.__smokeProblems ?? ['the problem collector is missing']")));
  } catch (err) {
    problems.push(`could not read the page problems: ${(err as Error).message}`);
  }

  await session.request({ code: "window.runtime.Quit()" }, 2_000);
  child.stdin.end();
  for (let i = 0; i < 50 && !exited; i++) await sleep(100);
  if (!exited) child.kill();

  if (problems.length === 0) {
    console.log(quick ? "duman testi GEÇTİ: pencere açıldı, köprü yerinde, Node sızmıyor" : "duman testi GEÇTİ: yolculuk tamam (sahte sunucu)");
    return 0;
  }
  for (const problem of problems) console.error("duman testi:", problem);
  return 1;
}

main().then((code) => process.exit(code), (err: unknown) => {
  console.error("duman testi:", err);
  process.exit(1);
});
