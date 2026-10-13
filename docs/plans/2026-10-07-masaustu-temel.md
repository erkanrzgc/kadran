# Masaüstü 1. parça — Temel ve cam tasarım: Uygulama Planı

> **Ajanlar için:** GEREKLİ ALT BECERİ: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) ya da superpowers:executing-plans kullan. Adımlar `- [ ]` onay kutusuyla izlenir.

**Amaç:** Kadran masaüstü uygulamasını React + Vite + Motion ile yeniden kurmak. Cam pencere modları, kullanıcının ayarlayabildiği Görünüm, sistem yazı tipleri, TR/EN dil desteği, Coolify düzeninden alınan kabuk ve Status / Audit / Settings ekranları gelir. Güvenlik duruşu korunur, izinler sıkılaşır.

**Mimari:**
- Ana süreç (Electron, ESM, `tsc` ile derlenir) korunur.
- Saf mantık ayrı dosyalara alınır, böylece `node --test` ile ölçülebilir: `window-options.ts`, `ipc-validate.ts`, `permissions.ts`, `shared/appearance.ts`.
- Arayüz `src/renderer/` altında React ile yazılır ve Vite ile `out/renderer`'a derlenir. `file://` üzerinden `loadFile` ile yüklenir, CSP bugünkü kadar sıkı kalır.
- Saf arayüz mantığı `src/renderer/lib/*.ts` içindedir; içinde JSX yoktur, node ile test edilir.
- Bileşenler `.tsx`'tir. Onları gerçek Electron + sahte sidecar ile çalışan uçtan uca test ölçer.

**Teknoloji:** Electron 43.2, React 19.3, motion 13.5.1, Vite 8.3.3, @vitejs/plugin-react 6.1.2, @electron/packager 20.3, TypeScript 5.9, Node ≥ 22.6 (tip sıyırma ile `node --test`).

**Tasarım belgesi:** `docs/specs/2026-10-07-masaustu-temel-design.md`. Uygulayıcı önce onu okur; bu plan oradaki kararları uygular.

## Genel kısıtlar

Her görevin gereksinimlerine bu bölüm de dahildir.

- **Dal:** `masaustu-temel`. Bu dal, birleşmemiş PR #10'un (`cli-ingilizce-3`, commit `dc62cc1`) üstünde. PR açılmadan önce #10 birleşmeli ve dal `main` üstüne alınmalı.
- **Commit kuralı:** `Co-Authored-By` ya da Claude satırı YOK. Yazar `erkanrizgic2004@gmail.com` (depo yerel ayarı zaten böyle). Biçim: `<tür>: <açıklama>`, Türkçe.
- **Dil:**
  - Kullanıcıya görünen bütün metin `src/renderer/i18n/en.json` ve `tr.json`'dan gelir. Varsayılan `en`.
  - Ana süreçte ve paylaşılan kodda kullanıcıya ulaşan hata iletileri (`new Error(...)`, `new SidecarError(...)`) İngilizcedir.
  - Kod yorumları, test adları ve konsol tanılamaları Türkçe kalır (K-141).
- **Sürüm sabitleri:** `react` 19.3.0, `react-dom` 19.3.0, `motion` 13.5.1 (14 değil: kopyalanan ikonlar `motion/react` `useAnimation`'a ve `^13`'e göre yazılmış), `vite` 8.3.3, `@vitejs/plugin-react` 6.1.2, `@electron/packager` 20.3.0, `@types/react` ve `@types/react-dom` 19.3.0.
- **Electron paket adı `kadran-desktop` DEĞİŞMEZ.** Kullanıcı verisi dizini ondan türüyor (K-136).
- **CSP,** `src/renderer/index.html`'de birebir şu dize:
  `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; form-action 'none'; base-uri 'none'`
- **Yasaklar:**
  - `dangerouslySetInnerHTML`, `.innerHTML =`, `insertAdjacentHTML`, `outerHTML =`.
  - Satır içi `<script>` ve `<style>`.
  - Statik HTML'de `style=` niteliği. React'in `style={{}}` özelliği CSSOM ile yazdığı için CSP'yi bozmaz ve serbesttir.
  - Motion'ın `<style>` etiketi enjekte eden özellikleri, örneğin `AnimatePresence mode="popLayout"`.
- **İzinler:**
  - Yalnız kontrol işleyicisinde ve yalnız `local-fonts` (string karşılaştırması; bu ad Electron'un TS tiplerinde yok). İstek işleyicisi her şeyi reddeder.
  - Ölçüldü: reddedilince `queryLocalFonts()` hata değil BOŞ liste döner.
- **Sidecar JSON biçimi** (`cmd/kadran/json.go`, `protojson` ile `EmitUnpopulated: true` ve `UseProtoNames: true`):
  - Alanlar snake_case.
  - uint64 ve int64 METİN olarak gelir (`seq`, `latest_seq`, `records_checked`, `daemon_uptime_seconds`, bellek baytları).
  - enum'lar adla gelir (`CHAIN_STATUS_VALID`).
  - false ve sıfır alanlar da yazılır.
  - Boş mesaj alanı `null`.
- **Audit sayfalama:** `after_seq` HARİÇ tutulur ve sunucu artan sırada döner (`internal/store/store.go:345`). "En yeni N" için önce `latest_seq` okunur, sonra `after_seq = max(0, latest − N)` istenir.
- **Dosya boyu:** her dosya 400 satırın altında.

## İnceleme odağı

Spec'in ima ettiği ama kolayca gözden kaçacak beş girdi ya da hata kipi. Her birinin testi, sahibi olan görevde.

1. **Zincirde 50'den fazla kayıt var.** "Son kayıtlar" en yeniyi göstermeli: `latest_seq = 1284` → 1284…1275. Bugünkü uygulama EN ESKİ 50'yi gösteriyor. → Görev 3 (birim) ve Görev 17 (uçtan uca).
2. **Yazı tipi listesi boş:** izin reddedildi ya da API yok. Arayüz çökmemeli, "listelenemedi" notu ve varsayılan yazı tipleriyle devam etmeli. → Görev 10 (birim) ve Görev 16 (not metni).
3. **Clear ↔ Acrylic/Mica/Solid geçişi.** Pencere yeniden kurulur; konum, boyut ve büyütülmüş hâl korunmalı, kullanıcı Settings'te kalmalı. → Görev 9 (birim) ve elle kontrol listesi.
4. **Elle bozulmuş `appearance.json`:** yanlış tipler, sınır dışı sayılar, bu sistemde olmayan mod, 10.000 karakterlik yazı tipi adı. Varsayılana dönmeli, çökmemeli. → Görev 6 ve Görev 7.
5. **Türkçe arayüzde uzun, İngilizce bir sunucu hatası.** Bildirim tam metni göstermeli ve satır kırmalı; metin HTML olarak işlenmemeli. → Görev 12 (CSS ve bileşen) ve Görev 17 (uçtan uca).

## Dosya yapısı

```
desktop/
  package.json                  (değişir)  bağımlılıklar, betikler
  tsconfig.json                 (değişir)  ana süreç + paylaşılan
  tsconfig.renderer.json        (yeni)     DOM + JSX, noEmit
  vite.config.ts                (yeni)
  scripts/copy-preload.mjs      (yeni; copy-renderer.mjs silinir)
  scripts/vendor-icons.mjs      (yeni)     ikonları sabit commit'ten kopyalar
  scripts/e2e.mjs               (yeni)     sahte sidecar ile uçtan uca
  scripts/notices.mjs           (yeni)     üçüncü taraf bildirimleri
  scripts/dist.mjs              (yeni)     Windows paketi
  src/main/index.ts             (değişir)  yaşam döngüsü + IPC kaydı
  src/main/window-options.ts    (yeni, saf)  mod → pencere seçenekleri
  src/main/window.ts            (yeni)     pencere kurma / yeniden kurma
  src/main/ipc-validate.ts      (yeni, saf)  IPC girdi doğrulama
  src/main/permissions.ts       (yeni, saf + kurulum)
  src/main/appearance.ts        (yeni)     appearance.json oku/yaz (atomik)
  src/main/smoke.ts             (yeni)     duman + uçtan uca yolculuk
  src/main/profiles.ts          (değişir)  hata iletisi İngilizce
  src/main/spawn-sidecar.ts     (değişir)  hata iletileri İngilizce
  src/shared/appearance.ts      (yeni, saf)  şema, modlar, varsayılanlar, temizleme
  src/shared/channels.ts        (değişir)  yeni kanallar
  src/shared/sidecar-client.ts  (değişir)  hata iletileri İngilizce
  src/preload/index.cjs         (değişir)  yeni köprü üyeleri
  src/renderer/index.html, main.tsx, App.tsx, bridge.ts
  src/renderer/i18n/{en.json,tr.json,I18nProvider.tsx}
  src/renderer/appearance/AppearanceProvider.tsx
  src/renderer/toast/{ToastProvider.tsx,Toast.tsx}
  src/renderer/hooks/{useServer.ts,useAudit.ts}
  src/renderer/lib/{types,errors,format,chain,health,samples,audit-window,i18n,languages,fonts,font-loader,apply-appearance}.ts
  src/renderer/icons/*.tsx + index.ts + LICENSE   (kopyalanan)
  src/renderer/fonts/JetBrainsMono-{Regular,Medium,Bold}.woff2 + OFL.txt
  src/renderer/components/{Titlebar,TopBar,ServerSwitcher,Rail,NavButton,Sparkline,Tile,InfoCard,ChainCard,AuditTable,OutcomeBar,Segmented,SetRow,FontPicker,EmptyState}.tsx
  src/renderer/screens/{StatusScreen,AuditScreen,SettingsScreen}.tsx
  src/renderer/styles/{tokens,base,glass,layout,controls,screens,focus,fonts}.css
  test/*.test.ts                (node --test)
  test/built/html.test.ts       (derleme sonrası)
  test/fixtures/{status,verify,audit-record}.json + fake-sidecar.mjs
cmd/kadran/sidecar_fixtures_test.go  (yeni)  fikstürler gerçek protoToJSON çıktısı
scripts/mutate-desktop.sh            (yeni)
.github/workflows/ci.yml             (değişir)
docs/decisions.md, README.md, CHANGELOG.md, NOTICE   (değişir)
```

Bütün komutlar, aksi yazılmadıkça `desktop/` dizininden çalıştırılır. Go komutları depo kökünden çalışır ve önce `export PATH="/c/Program Files/Go/bin:$PATH"` gerektirir (Windows Git Bash).

---
### Görev 1: Vite + React'i `file://` üzerinden yükleme (spike, kapı)

Bu görev bir kapıdır. Vite'ın `type="module" crossorigin` çıktısı `loadFile` ile ve bugünkü CSP ile açılmazsa, sonraki hiçbir görev başlamaz. Uygulayıcı durur ve duman testinin konsol hatasını aynen raporlar.

**Dosyalar:**
- Değişir: `desktop/package.json`, `desktop/tsconfig.json` (yalnız yorum)
- Yeni: `desktop/tsconfig.renderer.json`, `desktop/vite.config.ts`, `desktop/scripts/copy-preload.mjs`, `desktop/src/renderer/main.tsx`, `desktop/src/renderer/App.tsx`, `desktop/src/renderer/styles/base.css`
- Yeniden yazılır: `desktop/src/renderer/index.html`
- Silinir: `desktop/scripts/copy-renderer.mjs`, `desktop/src/renderer/app.js`, `desktop/src/renderer/app.css`
- Değişir: `desktop/src/main/index.ts` (duman testine kök denetimi eklenir)
- Test: `desktop/test/csp.test.ts`, `desktop/test/built/html.test.ts`

**Arayüzler:**
- Üretir:
  - `npm run build`: `tsc && vite build && node scripts/copy-preload.mjs`
  - `npm run test:built`
  - `App` bileşeni, kökte `data-testid="app"` taşır.

- [ ] **Adım 1: Bağımlılıkları kur**

`desktop/package.json`'ı şu hâle getir:

```json
{
  "name": "kadran-desktop",
  "version": "0.1.0",
  "private": true,
  "license": "Apache-2.0",
  "description": "Kadran masaüstü kabuğu — kadran sidecar'ını çocuk süreç olarak çalıştırır",
  "type": "module",
  "main": "out/main/index.js",
  "scripts": {
    "test": "node --test \"test/*.test.ts\"",
    "test:built": "node --test \"test/built/*.test.ts\"",
    "typecheck": "tsc --noEmit && tsc --noEmit -p tsconfig.renderer.json",
    "build": "tsc && vite build && node scripts/copy-preload.mjs",
    "dev": "npm run build && electron ."
  },
  "dependencies": {
    "motion": "13.5.1",
    "react": "19.3.0",
    "react-dom": "19.3.0"
  },
  "devDependencies": {
    "@types/node": "^22.10.2",
    "@types/react": "19.3.0",
    "@types/react-dom": "19.3.0",
    "@vitejs/plugin-react": "6.1.2",
    "electron": "^43.2.0",
    "typescript": "^5.7.2",
    "vite": "8.3.3"
  },
  "engines": {
    "node": ">=22.6"
  }
}
```

Çalıştır: `npm install`
Beklenen: hata yok, `package-lock.json` güncellendi. Bir peer uyarısı kurulumu durdurursa dur ve uyarıyı raporla. Sürümleri kendiliğinden değiştirme.

- [ ] **Adım 2: Kaynak CSP'sini koruyan testi yaz**

`desktop/test/csp.test.ts`:

```ts
/**
 * Arayüzün kaynak HTML'indeki CSP'nin birebir korunduğunu doğrular.
 *
 * CSP'yi gevşetmek (örneğin 'unsafe-inline') bir XSS'i renderer'da kod
 * çalıştırmaya çevirir. Değişiklik bilinçli olacaksa bu test de bilinçli
 * değiştirilir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const EXPECTED_CSP =
  "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; form-action 'none'; base-uri 'none'";

export function cspOf(html: string): string | null {
  const match = html.match(/http-equiv="Content-Security-Policy"\s+content="([^"]*)"/);
  return match?.[1] ?? null;
}

test("kaynak index.html CSP'yi birebir taşıyor", async () => {
  const html = await readFile(join(here, "..", "src", "renderer", "index.html"), "utf8");
  assert.equal(cspOf(html), EXPECTED_CSP);
});

test("kaynak index.html'de satır içi betik ve stil yok", async () => {
  const html = await readFile(join(here, "..", "src", "renderer", "index.html"), "utf8");
  const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)];
  assert.equal(scripts.length, 1, "tek bir giriş betiği olmalı");
  assert.match(scripts[0]![1]!, /\bsrc="\.\/main\.tsx"/);
  assert.equal(scripts[0]![2]!.trim(), "", "betik gövdesi boş olmalı");
  assert.doesNotMatch(html, /<style\b/);
  assert.doesNotMatch(html, /\sstyle="/);
});
```

- [ ] **Adım 3: Testin düştüğünü gör**

Çalıştır: `node --test test/csp.test.ts`
Beklenen: FAIL. İkinci test düşer, çünkü bugünkü `index.html`'de `<script src="app.js">` var, `./main.tsx` yok.

- [ ] **Adım 4: Arayüz iskeletini yaz**

`desktop/src/renderer/index.html` (tamamen yeniden):

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <!--
      İçerik güvenliği politikası.

      default-src 'none' ile başlayıp yalnızca gerekeni açıyoruz. Satır içi
      script ve stil YASAK. connect-src kapalı: arayüzün ağa çıkması
      gerekmiyor, her şey sidecar üzerinden IPC ile geliyor. Metin
      test/csp.test.ts'de birebir sabit.
    -->
    <meta
      http-equiv="Content-Security-Policy"
      content="default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; form-action 'none'; base-uri 'none'"
    />
    <title>KΛDRΛN</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="./main.tsx"></script>
  </body>
</html>
```

`desktop/src/renderer/main.tsx`:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App.tsx";
import "./styles/base.css";

const root = document.getElementById("root");
if (!root) throw new Error("root element missing");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

`desktop/src/renderer/App.tsx` (Görev 12'de büyüyecek):

```tsx
export function App() {
  return (
    <div className="app" data-testid="app">
      <span className="wordmark">KΛDRΛN</span>
    </div>
  );
}
```

`desktop/src/renderer/styles/base.css`:

```css
/* Pencerenin kendisi saydam; renk tonu .app'te (Görev 11). */
* { box-sizing: border-box; }
html, body { margin: 0; height: 100%; background: transparent; overflow: hidden; }
#root { height: 100%; }
```

`desktop/tsconfig.renderer.json`:

```json
{
  // Arayüz Vite ile derleniyor; bu dosya yalnız tip denetimi için (noEmit).
  // Ana süreçten ayrı: DOM kütüphanesi ve JSX burada, Node tipleri yok.
  "compilerOptions": {
    "target": "ES2023",
    "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "verbatimModuleSyntax": true,
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noEmit": true,
    "skipLibCheck": true,
    "types": []
  },
  "include": ["src/renderer/**/*.ts", "src/renderer/**/*.tsx", "src/shared/appearance.ts"]
}
```

`desktop/tsconfig.json`'da yalnız sondaki yorumu değiştir. Önceki yorum "renderer/ derlenmiyor: düz JavaScript olarak kopyalanıyor…" diyordu, artık şunu desin:

```jsonc
  // renderer/ burada derlenmiyor: Vite derliyor, tip denetimi
  // tsconfig.renderer.json'da (DOM + JSX). İki dünya ayrı tutuluyor.
```

`desktop/vite.config.ts`:

```ts
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Arayüz paketlenmiş uygulamada file:// üzerinden loadFile ile açılıyor;
// bu yüzden yollar GÖRELİ (base: "./"). Geliştirmede de canlı sunucu yok:
// CSP'ye geliştirme istisnası açılmasın diye hep derlenmiş dosya yüklenir.
export default defineConfig({
  root: fileURLToPath(new URL("./src/renderer", import.meta.url)),
  base: "./",
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL("./out/renderer", import.meta.url)),
    emptyOutDir: true,
    sourcemap: true,
    target: "esnext",
  },
});
```

`desktop/scripts/copy-preload.mjs`:

```js
/**
 * Preload'u out/ altına kopyalar.
 *
 * preload/index.cjs CommonJS olmak ZORUNDA: `sandbox: true` iken Electron
 * ESM preload çalıştırmıyor ve package.json'daki "type": "module" her .js
 * dosyasını ESM yapardı. Arayüz artık Vite ile derleniyor; bu betik
 * out/renderer'a DOKUNMAZ (dokunursa Vite çıktısını ezer).
 */

import { cp, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const from = join(here, "..", "src", "preload", "index.cjs");
const to = join(here, "..", "out", "preload", "index.cjs");

await mkdir(dirname(to), { recursive: true });
await cp(from, to);
console.log(`kopyalandı: ${from} → ${to}`);
```

Eski dosyaları sil:

```bash
git rm desktop/scripts/copy-renderer.mjs desktop/src/renderer/app.js desktop/src/renderer/app.css
```

- [ ] **Adım 5: Derlenmiş HTML testini yaz**

`desktop/test/built/html.test.ts`:

```ts
/**
 * Vite'ın ürettiği out/renderer/index.html'i ölçer: CSP aynı mı, satır içi
 * betik ya da stil var mı, yollar göreli mi, başvurulan dosyalar var mı.
 * Vite'ın varsayılan çıktısına güvenilmiyor; ölçülüyor.
 *
 * Önce `npm run build` çalışmış olmalı.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { cspOf, EXPECTED_CSP } from "../csp.test.ts";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "..", "out", "renderer");

test("derlenmiş index.html CSP'yi koruyor ve satır içi kod taşımıyor", async () => {
  const html = await readFile(join(outDir, "index.html"), "utf8");
  assert.equal(cspOf(html), EXPECTED_CSP);

  for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
    assert.match(m[1]!, /\bsrc="/, "src'siz betik var");
    assert.equal(m[2]!.trim(), "", "betik gövdesi boş değil");
  }
  assert.doesNotMatch(html, /<style\b/);
  assert.doesNotMatch(html, /\sstyle="/);
});

test("derlenmiş index.html'deki yollar göreli ve dosyalar mevcut", async () => {
  const html = await readFile(join(outDir, "index.html"), "utf8");
  const refs = [...html.matchAll(/\b(?:src|href)="([^"]+)"/g)].map((m) => m[1]!);
  assert.ok(refs.length >= 2, `en az bir JS ve bir CSS bekleniyordu: ${refs.join(", ")}`);
  for (const ref of refs) {
    assert.match(ref, /^\.\//, `göreli olmayan yol: ${ref}`);
    assert.ok(existsSync(join(outDir, ref)), `dosya yok: ${ref}`);
  }
});
```

Not: Bu dosya `csp.test.ts`'i içe aktarıyor. Bu yüzden `csp.test.ts`'in testleri de bu koşuda bir kez daha çalışır; zararsız ve hızlı.

- [ ] **Adım 6: Duman testine kök denetimi ekle**

`desktop/src/main/index.ts` içindeki `runSmokeTest`'te, `const leaked = …` bloğundan hemen önce şunu ekle:

```ts
    // React gerçekten çizdi mi? Vite çıktısı file:// üzerinden yüklenemezse
    // (modül/CORS ya da CSP) kök boş kalır ve konsola hata düşer.
    const rendered = (await window.webContents.executeJavaScript(
      "Boolean(document.querySelector('[data-testid=\"app\"]'))",
    )) as boolean;
    if (!rendered) problems.push("React kökü çizilmedi");
```

`executeJavaScript` çağrısı `did-finish-load`'dan hemen sonra gelir. React'in ilk çizimi eşzamanlı olduğundan kök o anda hazırdır. Değilse test düşer; o durumda bekleme ekleme, raporla.

- [ ] **Adım 7: Derle, testleri ve duman testini koş**

```bash
npm run build
node --test test/csp.test.ts
npm run test:built
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen:
- İki test dosyası PASS.
- Duman testi `duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor` basar ve 0 ile çıkar.

Duman testi `konsol hatası: … CORS …` ya da `… Content Security Policy …` ile düşerse DUR. Hata metnini aynen raporla. CSP'yi gevşetme.

- [ ] **Adım 8: Bütün birim testleri ve tip denetimi**

```bash
npm run typecheck
npm test
```

Beklenen: tip hatası yok; mevcut 26 test ve yeni CSP testi PASS.

- [ ] **Adım 9: Commit**

```bash
git add desktop/package.json desktop/package-lock.json desktop/tsconfig.json desktop/tsconfig.renderer.json desktop/vite.config.ts desktop/scripts/copy-preload.mjs desktop/src/renderer desktop/src/main/index.ts desktop/test/csp.test.ts desktop/test/built/html.test.ts
git commit -m "feat(masaüstü): arayüz React + Vite, file:// ve CSP korunarak"
```

---

### Görev 2: Sidecar fikstürleri gerçek `protoToJSON` çıktısından

Sahte sidecar elle yazılmış JSON döndürürse gerçek biçimden sessizce ayrışır. Fikstürleri bir Go testi üretir ve korur.

**Dosyalar:**
- Yeni: `cmd/kadran/sidecar_fixtures_test.go`
- Yeni (üretilir): `desktop/test/fixtures/status.json`, `desktop/test/fixtures/verify.json`, `desktop/test/fixtures/audit-record.json`
- Yeni: `desktop/src/renderer/lib/types.ts`
- Test: `desktop/test/fixtures-shape.test.ts`

**Arayüzler:**
- Üretir (`lib/types.ts`): `ChainStatusName`, `AuditOutcomeName`, `U64`, `HostInfo`, `StatusPayload`, `Actor`, `AuditRecord`, `AuditListPayload`, `VerifyPayload`, `VersionPayload`. Alan adları aşağıdaki gibi, birebir.

- [ ] **Adım 1: Go fikstür testini yaz**

`cmd/kadran/sidecar_fixtures_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// sidecarFixtures, masaüstünün sahte sidecar'ının döndürdüğü yanıtlar.
//
// Biçim elle yazılmıyor: dosyalar protoToJSON'ın GERÇEK çıktısı. Proto
// değişip dosyalar yenilenmezse bu test düşer ve masaüstünün sahte
// sunucuya karşı geçen testleri gerçeğinden ayrışmaz. Yenilemek için:
//
//	KADRAN_UPDATE_FIXTURES=1 go test ./cmd/kadran -run TestSidecarFixtures
var sidecarFixtures = map[string]proto.Message{
	"status.json": &kadranv1.GetSystemInfoResponse{
		DaemonVersion:       "0.5.1",
		Hostname:            "kadran-nbg1",
		DaemonUptimeSeconds: 371_520,
		RunningAsUser:       "kadran",
		ExecutorReachable:   false,
		Host: &kadranv1.HostInfo{
			Os:                   "linux",
			KernelVersion:        "6.12.48",
			Architecture:         "amd64",
			CpuCount:             2,
			MemoryTotalBytes:     3_970_000_000,
			MemoryAvailableBytes: 1_720_000_000,
			DockerVersion:        "28.4.0",
			DiskTotalBytes:       40_000_000_000,
			DiskAvailableBytes:   22_000_000_000,
		},
	},
	"verify.json": &kadranv1.VerifyAuditChainResponse{
		DaemonStatus:   kadranv1.ChainStatus_CHAIN_STATUS_VALID,
		RecordsChecked: 1284,
		ExecutorStatus: kadranv1.ChainStatus_CHAIN_STATUS_UNREACHABLE,
		ExecutorDetail: "executor unreachable: dial unix /run/kadran/exec.sock: connect: no such file or directory",
	},
	"audit-record.json": &kadranv1.AuditRecord{
		Seq: 1,
		Ts:  timestamppb.New(time.Date(2026, 10, 6, 18, 42, 10, 0, time.UTC)),
		Actor: &kadranv1.Actor{
			SshKeyFingerprint: "SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2",
			Origin:            "ssh",
		},
		Action:   "deploy",
		Target:   "hello",
		Outcome:  kadranv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS,
		PrevHash: []byte{1, 2, 3},
		Hash:     []byte{4, 5, 6},
		Source:   kadranv1.AuditSource_AUDIT_SOURCE_DAEMON,
	},
}

func TestSidecarFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "desktop", "test", "fixtures")
	update := os.Getenv("KADRAN_UPDATE_FIXTURES") == "1"

	for name, msg := range sidecarFixtures {
		raw, err := protoToJSON(msg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// protojson boşlukları bilerek rastgeleleştiriyor; bayt bayt
		// karşılaştırma kararsız olurdu. Anlam karşılaştırılıyor.
		var got any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		path := filepath.Join(dir, name)
		if update {
			body, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s okunamadı (KADRAN_UPDATE_FIXTURES=1 ile üret): %v", name, err)
		}
		var want any
		if err := json.Unmarshal(body, &want); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s protoToJSON çıktısıyla uyuşmuyor; proto değişmiş olabilir, KADRAN_UPDATE_FIXTURES=1 ile yenile", name)
		}
	}
}
```

- [ ] **Adım 2: Testin düştüğünü gör**

Depo kökünden:

```bash
go test ./cmd/kadran -run TestSidecarFixtures -count=1
```

Beklenen: FAIL, `status.json okunamadı (KADRAN_UPDATE_FIXTURES=1 ile üret)`.

- [ ] **Adım 3: Fikstürleri üret ve yeniden koş**

```bash
mkdir -p desktop/test/fixtures
KADRAN_UPDATE_FIXTURES=1 go test ./cmd/kadran -run TestSidecarFixtures -count=1
go test ./cmd/kadran -run TestSidecarFixtures -count=1
```

Beklenen: ikinci koşu PASS. `desktop/test/fixtures/status.json`'u aç ve şunları gör:
- `"daemon_uptime_seconds": "371520"` metin;
- `"executor_reachable": false` var;
- `"memory_total_bytes": "3970000000"` metin.

Biri yoksa dur ve raporla. O zaman plandaki biçim varsayımı yanlıştır.

- [ ] **Adım 4: Biçim testini yaz (TS tarafı)**

`desktop/test/fixtures-shape.test.ts`:

```ts
/**
 * Fikstürlerin, arayüzün beklediği tel biçimini taşıdığını doğrular.
 * Fikstürleri Go testi gerçek protoToJSON çıktısından üretiyor
 * (cmd/kadran/sidecar_fixtures_test.go); bu test TS tiplerinin o biçimle
 * aynı varsayımı yaptığını ölçer.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
async function fixture(name: string): Promise<Record<string, unknown>> {
  return JSON.parse(await readFile(join(here, "fixtures", name), "utf8")) as Record<string, unknown>;
}

test("status: 64 bit sayılar metin, false alan yazılmış", async () => {
  const s = await fixture("status.json");
  assert.equal(typeof s.daemon_uptime_seconds, "string");
  assert.equal(s.executor_reachable, false);
  const host = s.host as Record<string, unknown>;
  assert.equal(typeof host.memory_total_bytes, "string");
  assert.equal(typeof host.memory_available_bytes, "string");
  assert.equal(typeof host.cpu_count, "number");
});

test("verify: enum adla, sayaç metin", async () => {
  const v = await fixture("verify.json");
  assert.equal(v.daemon_status, "CHAIN_STATUS_VALID");
  assert.equal(v.executor_status, "CHAIN_STATUS_UNREACHABLE");
  assert.equal(v.records_checked, "1284");
  assert.equal(v.first_invalid_seq, "0");
});

test("audit kaydı: seq metin, zaman RFC 3339, sonuç adla", async () => {
  const r = await fixture("audit-record.json");
  assert.equal(r.seq, "1");
  assert.match(String(r.ts), /^2026-10-06T18:42:10Z$/);
  assert.equal(r.outcome, "AUDIT_OUTCOME_SUCCESS");
  assert.equal((r.actor as Record<string, unknown>).source_ip, "");
});
```

- [ ] **Adım 5: Tipleri yaz**

`desktop/src/renderer/lib/types.ts`:

```ts
/**
 * Sidecar'ın döndürdüğü JSON'un tipleri.
 *
 * Biçim protojson'dan (cmd/kadran/json.go): alanlar snake_case, uint64 ve
 * int64 METİN, enum'lar ad olarak, false/sıfır alanlar da yazılır, boş
 * mesaj alanı null. test/fixtures-shape.test.ts bu varsayımları gerçek
 * çıktıdan üretilen fikstürlere karşı ölçüyor.
 */

export type ChainStatusName =
  | "CHAIN_STATUS_UNSPECIFIED"
  | "CHAIN_STATUS_VALID"
  | "CHAIN_STATUS_INVALID"
  | "CHAIN_STATUS_UNREACHABLE";

export type AuditOutcomeName =
  | "AUDIT_OUTCOME_UNSPECIFIED"
  | "AUDIT_OUTCOME_SUCCESS"
  | "AUDIT_OUTCOME_FAILURE"
  | "AUDIT_OUTCOME_DENIED";

/** protojson'da 64 bit tamsayılar metin olarak gelir. */
export type U64 = string;

export interface HostInfo {
  os: string;
  kernel_version: string;
  architecture: string;
  cpu_count: number;
  memory_total_bytes: U64;
  memory_available_bytes: U64;
  docker_version: string;
  disk_total_bytes: U64;
  disk_available_bytes: U64;
}

export interface StatusPayload {
  daemon_version: string;
  hostname: string;
  daemon_uptime_seconds: U64;
  running_as_user: string;
  executor_reachable: boolean;
  executor_version: string;
  host: HostInfo | null;
}

export interface Actor {
  ssh_key_fingerprint: string;
  source_ip: string;
  label: string;
  origin: string;
}

export interface AuditRecord {
  seq: U64;
  ts: string | null;
  actor: Actor | null;
  action: string;
  target: string;
  params_json: string;
  outcome: AuditOutcomeName;
  detail: string;
  source: string;
}

export interface AuditListPayload {
  records: AuditRecord[];
  latest_seq: U64;
}

export interface VerifyPayload {
  daemon_status: ChainStatusName;
  records_checked: U64;
  first_invalid_seq: U64;
  detail: string;
  executor_status: ChainStatusName;
  executor_records_checked: U64;
  executor_detail: string;
}

export interface VersionPayload {
  version: string;
  commit: string;
  protocol: number;
}
```

- [ ] **Adım 6: Testleri koş**

```bash
node --test test/fixtures-shape.test.ts
npm run typecheck
```

Beklenen: PASS, tip hatası yok.

- [ ] **Adım 7: Commit**

```bash
git add cmd/kadran/sidecar_fixtures_test.go desktop/test/fixtures desktop/test/fixtures-shape.test.ts desktop/src/renderer/lib/types.ts
git commit -m "test(masaüstü): sidecar fikstürleri gerçek protoToJSON çıktısından"
```

---
### Görev 3: Saf arayüz mantığı — biçim, zincir, sağlık, örnek, audit penceresi

**Dosyalar:**
- Yeni: `desktop/src/renderer/lib/format.ts`, `chain.ts`, `health.ts`, `samples.ts`, `audit-window.ts`, `errors.ts`
- Test: `desktop/test/format.test.ts`, `chain.test.ts`, `health.test.ts`, `samples.test.ts`, `audit-window.test.ts`, `errors.test.ts`

**Arayüzler:**
- Tüketir: `lib/types.ts` (Görev 2).
- Üretir:
  - `format.ts`:
    - `humanDuration(seconds: number | string): string`
    - `humanBytes(value: number | string): string`
    - `shortFingerprint(fp: string): string`
    - `describeActor(actor: Actor | null): string | null`
    - `memoryUsedGiB(host: HostInfo | null): number`
    - `memoryText(host: HostInfo | null): string`
    - `formatTime(ts: string | null, locale: string): string`
    - `formatCount(n: number, locale: string): string`
  - `chain.ts`:
    - `type Tone = "ok" | "warn" | "bad" | "muted"`
    - `chainView(status): { label: "VALID" | "BROKEN" | "UNVERIFIABLE" | "UNKNOWN"; tone: Tone }`
    - `type OutcomeKey = "success" | "failure" | "denied" | "unspecified"`, `OUTCOME_KEYS`
    - `outcomeKey(o): OutcomeKey`
    - `outcomeView(o): { label: "SUCCESS" | "FAILED" | "DENIED" | "unspecified"; tone: Tone }`
    - `countOutcomes(records): Record<OutcomeKey, number>`
    - `filterRecords(records, { action, outcome }): AuditRecord[]`
    - `worstTone(...tones: Tone[]): Tone`
  - `health.ts`:
    - `type Health = "unknown" | "healthy" | "attention" | "tampering" | "broken-install"`
    - `healthOf(status, verify): Health`
    - `healthTone(h): Tone`
    - `healthKey(h): string` (i18n anahtarı)
  - `samples.ts`: `MAX_SAMPLES = 60`, `pushSample(samples, value, max?): number[]`, `sparklinePath(samples, width, height): string`
  - `audit-window.ts`:
    - `AUDIT_PAGE = 50`
    - `clampLimit(n): number`
    - `newestWindow(latestSeq, n): AuditWindow`
    - `olderWindow(oldestShown, n): AuditWindow | null`
    - `mergeNewestFirst(current, incoming): AuditRecord[]`
    - `interface AuditWindow { afterSeq: number; limit: number }`
  - `errors.ts`: `errorText(err: unknown): string`

- [ ] **Adım 1: Testleri yaz**

`desktop/test/format.test.ts`:

```ts
/**
 * Biçimlendiriciler CLI ile AYNI çıktıyı vermeli (cmd/kadran/format.go):
 * kullanıcı aynı değeri iki yerde farklı okumamalı.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  describeActor,
  formatCount,
  formatTime,
  humanBytes,
  humanDuration,
  memoryText,
  memoryUsedGiB,
  shortFingerprint,
} from "../src/renderer/lib/format.ts";

test("humanDuration CLI ile aynı: en çok iki birim, d h m s", () => {
  assert.equal(humanDuration(0), "0s");
  assert.equal(humanDuration("0.5"), "0s");
  assert.equal(humanDuration(59), "59s");
  assert.equal(humanDuration(61), "1m 1s");
  assert.equal(humanDuration(3_600 * 3 + 5), "3h 5s");
  assert.equal(humanDuration("371520"), "4d 7h");
  assert.equal(humanDuration(86_400 * 3 + 300), "3d 5m");
  assert.equal(humanDuration("abc"), "0s");
});

test("humanBytes CLI ile aynı", () => {
  assert.equal(humanBytes(512), "512 B");
  assert.equal(humanBytes(1024), "1.0 KiB");
  assert.equal(humanBytes("3970000000"), "3.7 GiB");
  assert.equal(humanBytes(-1), "—");
  assert.equal(humanBytes("x"), "—");
});

test("shortFingerprint uzun izi kısaltır, kısa izi bırakır", () => {
  assert.equal(
    shortFingerprint("SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2"),
    "SHA256:4f1c2b7d…ffa9e2",
  );
  assert.equal(shortFingerprint("SHA256:abc"), "SHA256:abc");
});

test("describeActor: parmak izi > etiket > köken > null", () => {
  const base = { ssh_key_fingerprint: "", source_ip: "", label: "", origin: "" };
  assert.equal(describeActor(null), null);
  assert.equal(describeActor(base), null);
  assert.equal(describeActor({ ...base, origin: "ssh" }), "ssh");
  assert.equal(describeActor({ ...base, origin: "ssh", label: "ci" }), "ci");
  assert.equal(
    describeActor({ ...base, label: "ci", ssh_key_fingerprint: "SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2" }),
    "SHA256:4f1c2b7d…ffa9e2",
  );
});

test("bellek: kullanılan GiB ve metin; eksik host güvenli", () => {
  const host = {
    os: "linux", kernel_version: "", architecture: "", cpu_count: 2,
    memory_total_bytes: "4294967296", memory_available_bytes: "1073741824",
    docker_version: "", disk_total_bytes: "0", disk_available_bytes: "0",
  };
  assert.equal(memoryUsedGiB(host), 3);
  assert.equal(memoryText(host), "3.0 GiB / 4.0 GiB");
  assert.ok(Number.isNaN(memoryUsedGiB(null)));
  assert.equal(memoryText(null), "—");
  assert.equal(memoryText({ ...host, memory_total_bytes: "0" }), "—");
});

test("formatTime ve formatCount", () => {
  assert.equal(formatTime(null, "en"), "—");
  assert.notEqual(formatTime("2026-10-06T18:42:10Z", "en"), "—");
  assert.equal(formatCount(1284, "en"), "1,284");
});
```

`desktop/test/chain.test.ts`:

```ts
/**
 * Zincir ve sonuç etiketleri CLI ile aynı (cmd/kadran/format.go).
 * UNREACHABLE "BROKEN" değil "UNVERIFIABLE": biri kurcalama şüphesi,
 * öteki işletim sorunu; aynı görünürlerse operatör sahte alarma koşar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { chainView, countOutcomes, filterRecords, outcomeKey, outcomeView, worstTone } from "../src/renderer/lib/chain.ts";
import type { AuditRecord } from "../src/renderer/lib/types.ts";

function rec(seq: number, action: string, outcome: AuditRecord["outcome"]): AuditRecord {
  return { seq: String(seq), ts: null, actor: null, action, target: "", params_json: "", outcome, detail: "", source: "" };
}

test("chainView etiket ve tonları", () => {
  assert.deepEqual(chainView("CHAIN_STATUS_VALID"), { label: "VALID", tone: "ok" });
  assert.deepEqual(chainView("CHAIN_STATUS_INVALID"), { label: "BROKEN", tone: "bad" });
  assert.deepEqual(chainView("CHAIN_STATUS_UNREACHABLE"), { label: "UNVERIFIABLE", tone: "warn" });
  assert.deepEqual(chainView("CHAIN_STATUS_UNSPECIFIED"), { label: "UNKNOWN", tone: "muted" });
  assert.deepEqual(chainView(undefined), { label: "UNKNOWN", tone: "muted" });
});

test("BROKEN ile UNVERIFIABLE farklı ton", () => {
  assert.notEqual(chainView("CHAIN_STATUS_INVALID").tone, chainView("CHAIN_STATUS_UNREACHABLE").tone);
});

test("sonuç anahtarı ve görünümü", () => {
  assert.equal(outcomeKey("AUDIT_OUTCOME_SUCCESS"), "success");
  assert.equal(outcomeKey("AUDIT_OUTCOME_FAILURE"), "failure");
  assert.equal(outcomeKey("AUDIT_OUTCOME_DENIED"), "denied");
  assert.equal(outcomeKey("garip"), "unspecified");
  assert.deepEqual(outcomeView("AUDIT_OUTCOME_FAILURE"), { label: "FAILED", tone: "bad" });
  assert.deepEqual(outcomeView("AUDIT_OUTCOME_DENIED"), { label: "DENIED", tone: "warn" });
});

test("countOutcomes ve filterRecords", () => {
  const records = [
    rec(3, "deploy", "AUDIT_OUTCOME_SUCCESS"),
    rec(2, "app.delete", "AUDIT_OUTCOME_DENIED"),
    rec(1, "deploy", "AUDIT_OUTCOME_FAILURE"),
  ];
  assert.deepEqual(countOutcomes(records), { success: 1, failure: 1, denied: 1, unspecified: 0 });
  assert.deepEqual(filterRecords(records, { action: "deploy", outcome: "" }).map((r) => r.seq), ["3", "1"]);
  assert.deepEqual(filterRecords(records, { action: "", outcome: "denied" }).map((r) => r.seq), ["2"]);
  assert.equal(filterRecords(records, { action: "", outcome: "" }).length, 3);
});

test("worstTone: bad > warn > muted > ok", () => {
  assert.equal(worstTone("ok", "warn"), "warn");
  assert.equal(worstTone("warn", "bad", "ok"), "bad");
  assert.equal(worstTone("ok", "muted"), "muted");
  assert.equal(worstTone("ok"), "ok");
});
```

`desktop/test/health.test.ts`:

```ts
/** Sağlık hapı: root > BROKEN > ulaşılamayan executor / UNVERIFIABLE > sağlıklı. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { healthKey, healthOf, healthTone } from "../src/renderer/lib/health.ts";

const ok = { running_as_user: "kadran", executor_reachable: true };
const valid = { daemon_status: "CHAIN_STATUS_VALID", executor_status: "CHAIN_STATUS_VALID" };

test("durum yoksa bilinmiyor", () => {
  assert.equal(healthOf(null, null), "unknown");
});

test("root her şeyi ezer", () => {
  assert.equal(healthOf({ ...ok, running_as_user: "root" }, valid), "broken-install");
});

test("BROKEN her zaman Attention'ı ezer", () => {
  assert.equal(healthOf({ ...ok, executor_reachable: false }, { ...valid, executor_status: "CHAIN_STATUS_INVALID" }), "tampering");
  assert.equal(healthOf(ok, { ...valid, daemon_status: "CHAIN_STATUS_INVALID" }), "tampering");
});

test("executor kapalı ya da UNVERIFIABLE → attention", () => {
  assert.equal(healthOf({ ...ok, executor_reachable: false }, null), "attention");
  assert.equal(healthOf(ok, { ...valid, executor_status: "CHAIN_STATUS_UNREACHABLE" }), "attention");
});

test("doğrulama yoksa ve executor açıksa bilinmiyor; ikisi VALID ise sağlıklı", () => {
  assert.equal(healthOf(ok, null), "unknown");
  assert.equal(healthOf(ok, valid), "healthy");
});

test("ton ve i18n anahtarı", () => {
  assert.equal(healthTone("healthy"), "ok");
  assert.equal(healthTone("attention"), "warn");
  assert.equal(healthTone("tampering"), "bad");
  assert.equal(healthTone("broken-install"), "bad");
  assert.equal(healthTone("unknown"), "muted");
  assert.equal(healthKey("broken-install"), "health.broken_install");
  assert.equal(healthKey("healthy"), "health.healthy");
});
```

`desktop/test/samples.test.ts`:

```ts
/** Çizgi grafik örnekleri: yalnız bellekte, en çok 60, en eski düşer. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { MAX_SAMPLES, pushSample, sparklinePath } from "../src/renderer/lib/samples.ts";

test("pushSample yeni dizi döndürür, eskisine dokunmaz", () => {
  const a = [1, 2];
  const b = pushSample(a, 3);
  assert.deepEqual(a, [1, 2]);
  assert.deepEqual(b, [1, 2, 3]);
});

test("en fazla MAX_SAMPLES; en eski düşer", () => {
  let s: number[] = [];
  for (let i = 0; i < MAX_SAMPLES + 5; i++) s = pushSample(s, i);
  assert.equal(s.length, MAX_SAMPLES);
  assert.equal(s[0], 5);
  assert.equal(s[s.length - 1], MAX_SAMPLES + 4);
});

test("sayı olmayan örnek eklenmez", () => {
  assert.deepEqual(pushSample([1], Number.NaN), [1]);
});

test("sparklinePath: iki noktadan azında boş; düz seride orta çizgi", () => {
  assert.equal(sparklinePath([5], 100, 20), "");
  assert.equal(sparklinePath([], 100, 20), "");
  assert.equal(sparklinePath([4, 4], 100, 20), "M0.0 10.0 L100.0 10.0");
  assert.equal(sparklinePath([0, 10], 100, 20), "M0.0 20.0 L100.0 0.0");
});
```

`desktop/test/audit-window.test.ts`:

```ts
/**
 * Audit sayfalama. Sunucu `after_seq`'i HARİÇ tutuyor ve ARTAN sırada
 * dönüyor (internal/store/store.go). Eski arayüz (0, 50) istiyordu ve
 * "son kayıtlar" diye EN ESKİ 50'yi gösteriyordu.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { clampLimit, mergeNewestFirst, newestWindow, olderWindow } from "../src/renderer/lib/audit-window.ts";
import type { AuditRecord } from "../src/renderer/lib/types.ts";

function rec(seq: number): AuditRecord {
  return { seq: String(seq), ts: null, actor: null, action: "a", target: "", params_json: "", outcome: "AUDIT_OUTCOME_SUCCESS", detail: "", source: "" };
}

/** Sunucunun davranışı: after_seq'ten SONRAKİLER, artan, en çok limit. */
function serverPage(latest: number, afterSeq: number, limit: number): AuditRecord[] {
  const out: AuditRecord[] = [];
  for (let s = afterSeq + 1; s <= latest && out.length < limit; s++) out.push(rec(s));
  return out;
}

test("en yeni 10: latest 1284 → 1284…1275", () => {
  const w = newestWindow(1284, 10);
  assert.deepEqual(w, { afterSeq: 1274, limit: 10 });
  const shown = mergeNewestFirst([], serverPage(1284, w.afterSeq, w.limit));
  assert.deepEqual(shown.map((r) => r.seq), ["1284", "1283", "1282", "1281", "1280", "1279", "1278", "1277", "1276", "1275"]);
});

test("kayıt sayısı pencereden azsa baştan", () => {
  assert.deepEqual(newestWindow(5, 50), { afterSeq: 0, limit: 50 });
});

test("daha eskiler: ekrandaki en eskinin hemen öncesi", () => {
  assert.deepEqual(olderWindow(1235, 50), { afterSeq: 1184, limit: 50 });
  const page = serverPage(1284, 1184, 50);
  assert.equal(page[0]!.seq, "1185");
  assert.equal(page[page.length - 1]!.seq, "1234");
  assert.deepEqual(olderWindow(30, 50), { afterSeq: 0, limit: 29 });
  assert.equal(olderWindow(1, 50), null);
  assert.equal(olderWindow(0, 50), null);
});

test("birleştirme tekrar etmez ve en yeni üstte", () => {
  const merged = mergeNewestFirst([rec(5), rec(4)], [rec(3), rec(4)]);
  assert.deepEqual(merged.map((r) => r.seq), ["5", "4", "3"]);
});

test("limit 1…1000 aralığında", () => {
  assert.equal(clampLimit(0), 1);
  assert.equal(clampLimit(5000), 1000);
  assert.equal(clampLimit(Number.NaN), 1);
  assert.equal(clampLimit(50.7), 50);
});
```

`desktop/test/errors.test.ts`:

```ts
/** IPC hata önekleri temizlenir; sunucunun metni olduğu gibi kalır. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { errorText } from "../src/renderer/lib/errors.ts";

test("Electron IPC öneki ve Error: öneki gider", () => {
  assert.equal(
    errorText(new Error("Error invoking remote method 'kadran:status': SidecarError: could not connect: refused")),
    "could not connect: refused",
  );
  assert.equal(errorText(new Error("Error: boom")), "boom");
  assert.equal(errorText("düz metin"), "düz metin");
  assert.equal(errorText(undefined), "unknown error");
});
```

- [ ] **Adım 2: Testlerin düştüğünü gör**

Çalıştır: `npm test`
Beklenen: FAIL; altı yeni dosya da `Cannot find module …/lib/format.ts` (ve diğerleri) ile düşer.

- [ ] **Adım 3: Uygulamayı yaz**

`desktop/src/renderer/lib/format.ts`:

```ts
/**
 * Sayı, süre ve kimlik biçimleri. CLI ile birebir aynı (cmd/kadran/format.go):
 * kullanıcı aynı değeri iki araçta farklı okumamalı.
 */

import type { Actor, HostInfo } from "./types.ts";

const UNITS: ReadonlyArray<readonly [number, string]> = [
  [86_400, "d"],
  [3_600, "h"],
  [60, "m"],
  [1, "s"],
];

/** humanDuration, saniyeyi en çok iki birimle yazar: "4d 7h". */
export function humanDuration(seconds: number | string): string {
  const total = Math.floor(Number(seconds));
  if (!Number.isFinite(total) || total < 1) return "0s";
  const parts: string[] = [];
  let rest = total;
  for (const [size, label] of UNITS) {
    if (rest < size) continue;
    parts.push(`${Math.floor(rest / size)}${label}`);
    rest %= size;
    if (parts.length === 2) break;
  }
  return parts.join(" ");
}

/** humanBytes, CLI'daki gibi 1024 tabanlı yazar: "3.7 GiB". */
export function humanBytes(value: number | string): string {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${Math.floor(n)} B`;
  let div = 1024;
  let exp = 0;
  for (let v = Math.floor(n / 1024); v >= 1024; v = Math.floor(v / 1024)) {
    div *= 1024;
    exp++;
  }
  return `${(n / div).toFixed(1)} ${"KMGTPE"[exp]}iB`;
}

/** shortFingerprint, uzun SSH parmak izini listede okunur kılar. */
export function shortFingerprint(fp: string): string {
  const prefix = "SHA256:";
  const body = fp.startsWith(prefix) ? fp.slice(prefix.length) : fp;
  if (body.length <= 16) return fp;
  return `${prefix}${body.slice(0, 8)}…${body.slice(-6)}`;
}

/** describeActor, aktörü tek hücreye sığdırır; bilinmiyorsa null (arayüz çevirir). */
export function describeActor(actor: Actor | null): string | null {
  if (!actor) return null;
  if (actor.ssh_key_fingerprint) return shortFingerprint(actor.ssh_key_fingerprint);
  if (actor.label) return actor.label;
  if (actor.origin) return actor.origin;
  return null;
}

const GIB = 1024 ** 3;

/** memoryUsedGiB, çizgi grafik örneği için kullanılan belleği GiB olarak verir. */
export function memoryUsedGiB(host: HostInfo | null): number {
  if (!host) return Number.NaN;
  const total = Number(host.memory_total_bytes);
  const available = Number(host.memory_available_bytes);
  if (!Number.isFinite(total) || !Number.isFinite(available) || total <= 0) return Number.NaN;
  return (total - available) / GIB;
}

/** memoryText, "kullanılan / toplam" biçiminde yazar. */
export function memoryText(host: HostInfo | null): string {
  if (!host || Number(host.memory_total_bytes) <= 0) return "—";
  const total = Number(host.memory_total_bytes);
  const used = total - Number(host.memory_available_bytes);
  return `${humanBytes(used)} / ${humanBytes(total)}`;
}

/** formatTime, RFC 3339 zamanı arayüz dilinde yazar. */
export function formatTime(ts: string | null, locale: string): string {
  if (!ts) return "—";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString(locale);
}

export function formatCount(n: number, locale: string): string {
  return n.toLocaleString(locale);
}
```

`desktop/src/renderer/lib/chain.ts`:

```ts
/**
 * Zincir ve sonuç etiketleri. CLI ile aynı (cmd/kadran/format.go):
 * UNREACHABLE "UNVERIFIABLE" olarak görünür, "BROKEN" değil.
 */

import type { AuditRecord } from "./types.ts";

export type Tone = "ok" | "warn" | "bad" | "muted";

export interface ChainView {
  label: "VALID" | "BROKEN" | "UNVERIFIABLE" | "UNKNOWN";
  tone: Tone;
}

export function chainView(status: string | null | undefined): ChainView {
  switch (status) {
    case "CHAIN_STATUS_VALID":
      return { label: "VALID", tone: "ok" };
    case "CHAIN_STATUS_INVALID":
      return { label: "BROKEN", tone: "bad" };
    case "CHAIN_STATUS_UNREACHABLE":
      return { label: "UNVERIFIABLE", tone: "warn" };
    default:
      return { label: "UNKNOWN", tone: "muted" };
  }
}

export type OutcomeKey = "success" | "failure" | "denied" | "unspecified";
export const OUTCOME_KEYS: readonly OutcomeKey[] = ["success", "denied", "failure"];

export function outcomeKey(outcome: string | null | undefined): OutcomeKey {
  switch (outcome) {
    case "AUDIT_OUTCOME_SUCCESS":
      return "success";
    case "AUDIT_OUTCOME_FAILURE":
      return "failure";
    case "AUDIT_OUTCOME_DENIED":
      return "denied";
    default:
      return "unspecified";
  }
}

const OUTCOME_VIEW: Readonly<Record<OutcomeKey, { label: "SUCCESS" | "FAILED" | "DENIED" | "unspecified"; tone: Tone }>> = {
  success: { label: "SUCCESS", tone: "ok" },
  failure: { label: "FAILED", tone: "bad" },
  denied: { label: "DENIED", tone: "warn" },
  unspecified: { label: "unspecified", tone: "muted" },
};

export function outcomeView(outcome: string | null | undefined) {
  return OUTCOME_VIEW[outcomeKey(outcome)];
}

export const OUTCOME_TONE: Readonly<Record<OutcomeKey, Tone>> = {
  success: "ok",
  failure: "bad",
  denied: "warn",
  unspecified: "muted",
};

export function countOutcomes(records: readonly AuditRecord[]): Record<OutcomeKey, number> {
  const counts: Record<OutcomeKey, number> = { success: 0, failure: 0, denied: 0, unspecified: 0 };
  for (const r of records) counts[outcomeKey(r.outcome)] += 1;
  return counts;
}

export interface RecordFilter {
  action: string;
  outcome: OutcomeKey | "";
}

export function filterRecords(records: readonly AuditRecord[], filter: RecordFilter): AuditRecord[] {
  return records.filter(
    (r) =>
      (filter.action === "" || r.action === filter.action) &&
      (filter.outcome === "" || outcomeKey(r.outcome) === filter.outcome),
  );
}

const RANK: Readonly<Record<Tone, number>> = { ok: 0, muted: 1, warn: 2, bad: 3 };

/** worstTone, birden çok durumun en kötüsünü verir (bad > warn > muted > ok). */
export function worstTone(...tones: Tone[]): Tone {
  return tones.reduce<Tone>((worst, t) => (RANK[t] > RANK[worst] ? t : worst), "ok");
}
```

`desktop/src/renderer/lib/health.ts`:

```ts
/**
 * Üst çubuktaki sağlık hapı.
 *
 * Öncelik: daemon root çalışıyorsa kurulum bozuk; bir zincir BROKEN ise
 * kurcalama şüphesi (Attention'ı her zaman ezer); executor kapalıysa ya
 * da bir zincir doğrulanamıyorsa ilgi gerekiyor; ikisi de VALID ise sağlıklı.
 */

import type { Tone } from "./chain.ts";

export type Health = "unknown" | "healthy" | "attention" | "tampering" | "broken-install";

export interface HealthStatusInput {
  running_as_user?: string;
  executor_reachable?: boolean;
}

export interface HealthVerifyInput {
  daemon_status?: string;
  executor_status?: string;
}

export function healthOf(status: HealthStatusInput | null, verify: HealthVerifyInput | null): Health {
  if (!status) return "unknown";
  if (status.running_as_user === "root") return "broken-install";
  if (
    verify &&
    (verify.daemon_status === "CHAIN_STATUS_INVALID" || verify.executor_status === "CHAIN_STATUS_INVALID")
  ) {
    return "tampering";
  }
  if (!status.executor_reachable) return "attention";
  if (!verify) return "unknown";
  if (verify.daemon_status !== "CHAIN_STATUS_VALID" || verify.executor_status !== "CHAIN_STATUS_VALID") {
    return "attention";
  }
  return "healthy";
}

const TONE: Readonly<Record<Health, Tone>> = {
  unknown: "muted",
  healthy: "ok",
  attention: "warn",
  tampering: "bad",
  "broken-install": "bad",
};

export function healthTone(h: Health): Tone {
  return TONE[h];
}

export function healthKey(h: Health): string {
  return `health.${h.replace("-", "_")}`;
}
```

`desktop/src/renderer/lib/samples.ts`:

```ts
/**
 * Çizgi grafik örnekleri. Yalnız ekran açıkken toplanır ve bellekte durur;
 * diske ya da sunucuya yazılmaz.
 */

export const MAX_SAMPLES = 60;

export function pushSample(samples: readonly number[], value: number, max = MAX_SAMPLES): number[] {
  if (!Number.isFinite(value)) return [...samples];
  const next = [...samples, value];
  return next.length > max ? next.slice(next.length - max) : next;
}

/** sparklinePath, örnekleri width×height kutusuna SVG yolu olarak çizer. */
export function sparklinePath(samples: readonly number[], width: number, height: number): string {
  if (samples.length < 2) return "";
  const min = Math.min(...samples);
  const max = Math.max(...samples);
  const step = width / (samples.length - 1);
  return samples
    .map((v, i) => {
      const y = max === min ? height / 2 : height - ((v - min) / (max - min)) * height;
      return `${i === 0 ? "M" : "L"}${(i * step).toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(" ");
}
```

`desktop/src/renderer/lib/audit-window.ts`:

```ts
/**
 * Audit sayfalaması.
 *
 * Sunucu `after_seq`'i HARİÇ tutar ve kayıtları ARTAN sırada döner
 * (internal/store/store.go: WHERE seq > ? ORDER BY seq ASC LIMIT ?).
 * "En yeni N" için önce latest_seq okunur, sonra after_seq = latest − N
 * istenir. Eski arayüz (0, 50) istiyordu ve en ESKİ 50'yi gösteriyordu.
 */

import type { AuditRecord } from "./types.ts";

export const AUDIT_PAGE = 50;
const MAX_LIMIT = 1000;

export interface AuditWindow {
  afterSeq: number;
  limit: number;
}

export function clampLimit(n: number): number {
  if (!Number.isFinite(n) || n < 1) return 1;
  return Math.min(MAX_LIMIT, Math.floor(n));
}

export function newestWindow(latestSeq: number, n: number): AuditWindow {
  const limit = clampLimit(n);
  return { afterSeq: Math.max(0, latestSeq - limit), limit };
}

/** olderWindow, ekrandaki en eski kaydın hemen öncesindeki n kaydı ister. */
export function olderWindow(oldestShown: number, n: number): AuditWindow | null {
  if (!Number.isFinite(oldestShown) || oldestShown <= 1) return null;
  const limit = Math.min(clampLimit(n), oldestShown - 1);
  return { afterSeq: oldestShown - 1 - limit, limit };
}

/** mergeNewestFirst, iki listeyi birleştirir; aynı seq bir kez, en yeni üstte. */
export function mergeNewestFirst(current: readonly AuditRecord[], incoming: readonly AuditRecord[]): AuditRecord[] {
  const bySeq = new Map<string, AuditRecord>();
  for (const r of [...current, ...incoming]) bySeq.set(r.seq, r);
  return [...bySeq.values()].sort((a, b) => Number(b.seq) - Number(a.seq));
}
```

`desktop/src/renderer/lib/errors.ts`:

```ts
/**
 * errorText, IPC hatasının Electron önekini temizler. Kalan metin
 * sunucudan ya da sidecar'dan gelir ve olduğu gibi gösterilir; hep düz
 * metin olarak (React kaçırarak yazar).
 */
export function errorText(err: unknown): string {
  if (err === undefined || err === null) return "unknown error";
  const raw = err instanceof Error ? err.message : String(err);
  return raw
    .replace(/^Error invoking remote method '[^']*':\s*/, "")
    .replace(/^(?:Sidecar)?Error:\s*/, "");
}
```

- [ ] **Adım 4: Testlerin geçtiğini gör**

Çalıştır: `npm test` ve `npm run typecheck`
Beklenen: bütün testler PASS, tip hatası yok.

`formatCount(1284, "en")` Node'da "1,284" vermezse (ICU eksik bir Node derlemesi), testi değiştirme ve raporla.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/renderer/lib desktop/test/format.test.ts desktop/test/chain.test.ts desktop/test/health.test.ts desktop/test/samples.test.ts desktop/test/audit-window.test.ts desktop/test/errors.test.ts
git commit -m "feat(masaüstü): saf arayüz mantığı — biçim, zincir, sağlık, audit sayfalama"
```

---
### Görev 4: Dil katmanı — en/tr sözlükleri, `translate`, korumalar

**Dosyalar:**
- Yeni: `desktop/src/renderer/lib/i18n.ts`, `desktop/src/renderer/lib/languages.ts`, `desktop/src/renderer/i18n/en.json`, `desktop/src/renderer/i18n/tr.json`
- Test: `desktop/test/i18n.test.ts`, `desktop/test/no-raw-text.test.ts`

**Arayüzler:**
- Üretir:
  - `type Dict = Readonly<Record<string, string>>`
  - `type Params = Readonly<Record<string, string | number>>`
  - `translate(dict: Dict, fallback: Dict, key: string, params?: Params): string`
  - `placeholders(template: string): string[]`
  - `LANGUAGES: readonly { code: Language; name: string }[]`
- Çoğul kuralı: `params.count` bir sayıysa önce `<key>_one` (count === 1) ya da `<key>_other` aranır.
- Görev 12 ve sonrası yalnız bu dosyadaki anahtarları kullanır. Yeni bir anahtar gerekirse İKİ dosyaya da eklenir.

- [ ] **Adım 1: Testleri yaz**

`desktop/test/i18n.test.ts`:

```ts
/**
 * Dil dosyaları: iki dilin anahtarları birebir aynı, yer tutucuları aynı,
 * İngilizce dosyada Türkçe harf yok, koddaki her t("…") anahtarı var.
 * Yeni bir ekrana Türkçesi eklenmezse CI burada kırmızı yanar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { placeholders, translate } from "../src/renderer/lib/i18n.ts";

const here = dirname(fileURLToPath(import.meta.url));
const i18nDir = join(here, "..", "src", "renderer", "i18n");
const rendererDir = join(here, "..", "src", "renderer");

async function dict(lang: string): Promise<Record<string, string>> {
  return JSON.parse(await readFile(join(i18nDir, `${lang}.json`), "utf8")) as Record<string, string>;
}

async function sourceFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "icons") out.push(...(await sourceFiles(path)));
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      out.push(path);
    }
  }
  return out;
}

test("en ve tr anahtarları birebir aynı", async () => {
  const en = Object.keys(await dict("en")).sort();
  const tr = Object.keys(await dict("tr")).sort();
  assert.deepEqual(tr, en);
});

test("her anahtarın yer tutucuları iki dilde aynı", async () => {
  const en = await dict("en");
  const tr = await dict("tr");
  for (const [key, value] of Object.entries(en)) {
    assert.deepEqual(placeholders(tr[key] ?? ""), placeholders(value), `yer tutucu farkı: ${key}`);
  }
});

test("İngilizce dosyada Türkçe harf yok", async () => {
  for (const [key, value] of Object.entries(await dict("en"))) {
    assert.doesNotMatch(value, /[çğıöşüÇĞİÖŞÜ]/, `Türkçe metin: ${key} = ${value}`);
  }
});

test("koddaki her t(\"…\") anahtarı en.json'da var", async () => {
  const en = await dict("en");
  const missing: string[] = [];
  for (const file of await sourceFiles(rendererDir)) {
    const src = await readFile(file, "utf8");
    for (const m of src.matchAll(/\bt\(\s*"([a-z0-9_.]+)"/g)) {
      const key = m[1]!;
      if (!(key in en) && !(`${key}_one` in en && `${key}_other` in en)) missing.push(`${file}: ${key}`);
    }
  }
  assert.deepEqual(missing, []);
});

test("translate: yer tutucu, çoğul, yedek dil, bilinmeyen anahtar", () => {
  const en = { hello: "Hi {name}", "n_one": "1 thing", "n_other": "{count} things", only_en: "fallback" };
  const tr = { hello: "Selam {name}", "n_one": "1 şey", "n_other": "{count} şey" };
  assert.equal(translate(tr, en, "hello", { name: "Ali" }), "Selam Ali");
  assert.equal(translate(tr, en, "n", { count: 1 }), "1 şey");
  assert.equal(translate(tr, en, "n", { count: 3 }), "3 şey");
  assert.equal(translate(tr, en, "only_en"), "fallback");
  assert.equal(translate(tr, en, "yok.boyle"), "yok.boyle");
  assert.equal(translate(tr, en, "hello"), "Selam {name}");
});
```

`desktop/test/no-raw-text.test.ts`:

```ts
/**
 * Arayüz kodunda çeviri dışı görünür metin yok: JSX metni ve görünür
 * nitelikler (title, aria-label, placeholder, alt) t() ile gelmeli.
 * TypeScript AST'si üstünde çalışır; yorumlar ve kod içi dizeler sayılmaz.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");

/** Çevrilmeyen, adıyla izin verilen metinler: marka ve ayraçlar. */
const ALLOWED = new Set(["KΛDRΛN"]);
const VISIBLE_ATTRS = new Set(["title", "aria-label", "placeholder", "alt"]);
const LETTER = /\p{L}/u;

async function tsxFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "icons") out.push(...(await tsxFiles(path)));
    } else if (entry.name.endsWith(".tsx")) {
      out.push(path);
    }
  }
  return out;
}

export async function rawTexts(): Promise<{ files: number; problems: string[] }> {
  const problems: string[] = [];
  const files = await tsxFiles(rendererDir);
  for (const file of files) {
    const source = ts.createSourceFile(file, await readFile(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const visit = (node: ts.Node): void => {
      if (ts.isJsxText(node)) {
        const text = node.text.trim();
        if (text && LETTER.test(text) && !ALLOWED.has(text)) problems.push(`${file}: "${text}"`);
      }
      if (ts.isJsxAttribute(node) && VISIBLE_ATTRS.has(node.name.getText(source))) {
        const init = node.initializer;
        if (init && ts.isStringLiteral(init) && LETTER.test(init.text) && !ALLOWED.has(init.text)) {
          problems.push(`${file}: ${node.name.getText(source)}="${init.text}"`);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
  }
  return { files: files.length, problems };
}

test("JSX'te çeviri dışı görünür metin yok", async () => {
  const { files, problems } = await rawTexts();
  assert.ok(files >= 1, "hiç .tsx dosyası taranmadı — yol kaymış olabilir");
  assert.deepEqual(problems, []);
});
```

- [ ] **Adım 2: Testlerin düştüğünü gör**

Çalıştır: `npm test`
Beklenen: FAIL; `i18n.test.ts` `Cannot find module …/lib/i18n.ts` ile düşer.

- [ ] **Adım 3: `i18n.ts` ve `languages.ts`'i yaz**

`desktop/src/renderer/lib/i18n.ts`:

```ts
/**
 * Küçük çeviri katmanı. Kütüphane yok: yer tutucu ({ad}) ve bir/çok
 * biçimi (`_one` / `_other`) yeterli. Anahtar seçilen dilde yoksa
 * İngilizceye, orada da yoksa anahtarın kendisine düşer; böylece eksik
 * çeviri ekranı boşaltmaz, testte de yakalanır.
 */

export type Dict = Readonly<Record<string, string>>;
export type Params = Readonly<Record<string, string | number>>;

export function translate(dict: Dict, fallback: Dict, key: string, params: Params = {}): string {
  const count = params.count;
  const pluralKey = typeof count === "number" ? `${key}_${count === 1 ? "one" : "other"}` : null;
  const template =
    (pluralKey !== null ? dict[pluralKey] ?? fallback[pluralKey] : undefined) ?? dict[key] ?? fallback[key] ?? key;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : match,
  );
}

/** placeholders, bir şablondaki yer tutucu adlarını sıralı verir. */
export function placeholders(template: string): string[] {
  return [...template.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? "").sort();
}
```

`desktop/src/renderer/lib/languages.ts`:

```ts
import type { Language } from "../../shared/appearance.ts";

/** Dil adları kendi dillerinde yazılır ve çevrilmez. */
export const LANGUAGES: ReadonlyArray<{ code: Language; name: string }> = [
  { code: "en", name: "English" },
  { code: "tr", name: "Türkçe" },
];
```

Not: `languages.ts`, `Language` tipini Görev 6'da yazılacak `src/shared/appearance.ts`'ten alır. Tip denetimi o görevden önce düşer. Bu yüzden bu görevde `npm run typecheck` KOŞULMAZ, Görev 6'nın sonunda koşulur. `npm test` etkilenmez, çünkü node tip içe aktarımlarını siler.

- [ ] **Adım 4: Sözlükleri yaz**

`desktop/src/renderer/i18n/en.json`:

```json
{
  "brand.aria": "Kadran",
  "window.minimize": "Minimize",
  "window.maximize": "Maximize",
  "window.close": "Close",
  "nav.label": "Main menu",
  "nav.status": "Status",
  "nav.audit": "Audit",
  "nav.settings": "Settings",
  "rail.target_label": "Server",
  "rail.target_placeholder": "kadran-client@1.2.3.4",
  "rail.connect": "Connect",
  "rail.connecting": "Connecting…",
  "rail.hint": "ssh handles sign-in. Nothing secret is stored here.",
  "rail.saved": "Saved servers",
  "rail.no_saved": "No saved servers yet.",
  "rail.version": "KΛDRΛN {version}",
  "topbar.crumbs": "Location",
  "topbar.servers": "Servers",
  "topbar.not_connected": "Not connected",
  "topbar.verify": "Verify chains",
  "topbar.verifying": "Verifying…",
  "topbar.rtt": "{ms} ms",
  "topbar.rtt_title": "Round-trip time of the last status call",
  "health.unknown": "Checking…",
  "health.healthy": "Healthy",
  "health.attention": "Attention required",
  "health.tampering": "Tampering suspected",
  "health.broken_install": "Broken install",
  "status.empty_title": "Connect to a server",
  "status.empty_body": "Type a target on the left, or pick a saved server.",
  "status.eyebrow": "Connected",
  "status.meta_ok": "Executor reachable. Privileged operations work.",
  "status.meta_down": "Executor unreachable. Privileged operations will fail.",
  "status.overview": "Overview",
  "status.live": "Live refresh",
  "status.refresh": "Refresh",
  "tile.uptime": "Uptime",
  "tile.memory": "Memory",
  "tile.rtt": "Round-trip",
  "tile.executor": "Executor",
  "tile.chains": "Chains",
  "value.reachable": "reachable",
  "value.unreachable": "unreachable",
  "value.none": "none",
  "value.broken_install": "{user} — BROKEN INSTALL",
  "card.daemon": "Daemon",
  "card.executor": "Executor",
  "card.host": "Host",
  "row.version": "Version",
  "row.uptime": "Uptime",
  "row.user": "User",
  "row.state": "State",
  "row.docker": "Docker",
  "row.name": "Name",
  "row.kernel": "Kernel",
  "row.memory": "Memory",
  "chains.daemon": "daemon chain",
  "chains.executor": "executor chain",
  "chains.records_one": "1 record verified",
  "chains.records_other": "{n} records verified",
  "chains.not_verified": "not verified yet",
  "chains.note": "Kept apart on purpose: kadrand can't write the executor's log, so a compromised daemon can only clean up its own.",
  "audit.recent": "Recent audit records",
  "audit.see_all": "See all",
  "audit.eyebrow": "Audit",
  "audit.title": "Audit log",
  "audit.col_seq": "Seq",
  "audit.col_time": "Time",
  "audit.col_actor": "Actor",
  "audit.col_action": "Action",
  "audit.col_target": "Target",
  "audit.col_outcome": "Outcome",
  "audit.load_older": "Load older",
  "audit.loading": "Loading…",
  "audit.empty": "The chain has no records.",
  "audit.range": "Show",
  "audit.last": "last {n}",
  "audit.filter_action": "Action",
  "audit.filter_outcome": "Outcome",
  "audit.filter_all": "All",
  "audit.copied": "Copied",
  "audit.copy": "Copy fingerprint",
  "audit.outcomes": "Outcomes",
  "audit.unknown_actor": "unknown",
  "audit.no_target_title": "No server connected",
  "audit.no_target_body": "Connect to a server first to read its audit log.",
  "outcome.success": "ok",
  "outcome.failure": "failed",
  "outcome.denied": "denied",
  "outcome.unspecified": "unspecified",
  "settings.title": "Settings",
  "settings.appearance": "Appearance",
  "settings.appearance_meta": "Changes apply right away and stay on this computer.",
  "settings.window": "Window",
  "settings.window_hint": "How much of the desktop shows through.",
  "settings.tint": "Window tint",
  "settings.tint_hint": "Lower is more see-through.",
  "settings.panel": "Panel glass",
  "settings.panel_hint": "How milky the cards are.",
  "settings.edge": "Edge highlight",
  "settings.edge_hint": "The bright line around glass.",
  "settings.glow": "Hover / focus glow",
  "settings.glow_hint": "The keyboard focus ring never fully goes away.",
  "settings.accent": "Accent",
  "settings.accent_hint": "Focus rings, links and the main button.",
  "settings.theme": "Theme",
  "settings.theme_hint": "Dark or light glass.",
  "settings.ui_font": "Interface font",
  "settings.ui_font_hint": "Any font installed on this computer.",
  "settings.code_font": "Code font",
  "settings.code_font_hint": "Hashes, versions and sequence numbers.",
  "settings.language": "Language",
  "settings.language_hint": "Messages from the server stay in English.",
  "settings.servers": "Servers",
  "settings.servers_hint": "Saved targets. Removing one doesn't touch the server.",
  "settings.no_servers": "No saved servers.",
  "settings.remove": "Remove",
  "settings.about": "About",
  "settings.about_hint": "Versions on this computer.",
  "settings.reset": "Reset to defaults",
  "mode.clear": "Clear",
  "mode.acrylic": "Acrylic",
  "mode.mica": "Mica",
  "mode.vibrancy": "Vibrancy",
  "mode.solid": "Solid",
  "accent.ice": "Ice",
  "accent.amber": "Amber",
  "accent.phosphor": "Phosphor",
  "accent.paper": "Paper",
  "tone.dark": "Dark",
  "tone.light": "Light",
  "font.system": "System default",
  "font.bundled": "JetBrains Mono (bundled)",
  "font.families_one": "1 font family on this computer",
  "font.families_other": "{n} font families on this computer",
  "font.unavailable": "Couldn't list installed fonts. Using the defaults.",
  "about.line": "KΛDRΛN {version} · sidecar protocol {protocol}",
  "toast.connect_failed": "Couldn't connect: {error}",
  "toast.verify_failed": "Couldn't verify the chains: {error}",
  "toast.audit_failed": "Couldn't read the audit log: {error}",
  "toast.save_failed": "Couldn't save: {error}",
  "toast.sidecar_failed": "The sidecar didn't start: {error}",
  "toast.copy_failed": "Couldn't copy: {error}",
  "toast.profiles_failed": "Couldn't read saved servers: {error}",
  "toast.dismiss": "Dismiss"
}
```

`desktop/src/renderer/i18n/tr.json`:

```json
{
  "brand.aria": "Kadran",
  "window.minimize": "Küçült",
  "window.maximize": "Büyüt",
  "window.close": "Kapat",
  "nav.label": "Ana menü",
  "nav.status": "Durum",
  "nav.audit": "Denetim",
  "nav.settings": "Ayarlar",
  "rail.target_label": "Sunucu",
  "rail.target_placeholder": "kadran-client@1.2.3.4",
  "rail.connect": "Bağlan",
  "rail.connecting": "Bağlanıyor…",
  "rail.hint": "Girişi ssh yapar. Burada gizli bir şey saklanmaz.",
  "rail.saved": "Kayıtlı sunucular",
  "rail.no_saved": "Henüz kayıtlı sunucu yok.",
  "rail.version": "KΛDRΛN {version}",
  "topbar.crumbs": "Konum",
  "topbar.servers": "Sunucular",
  "topbar.not_connected": "Bağlı değil",
  "topbar.verify": "Zincirleri doğrula",
  "topbar.verifying": "Doğrulanıyor…",
  "topbar.rtt": "{ms} ms",
  "topbar.rtt_title": "Son durum çağrısının gidiş-dönüş süresi",
  "health.unknown": "Bakılıyor…",
  "health.healthy": "Sağlıklı",
  "health.attention": "İlgi gerekiyor",
  "health.tampering": "Kurcalama şüphesi",
  "health.broken_install": "Kurulum bozuk",
  "status.empty_title": "Bir sunucuya bağlan",
  "status.empty_body": "Soldaki kutuya bir hedef yaz ya da kayıtlı bir sunucu seç.",
  "status.eyebrow": "Bağlı",
  "status.meta_ok": "Executor erişilebilir. Ayrıcalıklı işlemler çalışır.",
  "status.meta_down": "Executor erişilemiyor. Ayrıcalıklı işlemler başarısız olur.",
  "status.overview": "Genel bakış",
  "status.live": "Canlı yenile",
  "status.refresh": "Yenile",
  "tile.uptime": "Çalışma süresi",
  "tile.memory": "Bellek",
  "tile.rtt": "Gidiş-dönüş",
  "tile.executor": "Executor",
  "tile.chains": "Zincirler",
  "value.reachable": "erişilebilir",
  "value.unreachable": "erişilemiyor",
  "value.none": "yok",
  "value.broken_install": "{user} — KURULUM BOZUK",
  "card.daemon": "Daemon",
  "card.executor": "Executor",
  "card.host": "Sunucu",
  "row.version": "Sürüm",
  "row.uptime": "Çalışma süresi",
  "row.user": "Kullanıcı",
  "row.state": "Durum",
  "row.docker": "Docker",
  "row.name": "Ad",
  "row.kernel": "Çekirdek",
  "row.memory": "Bellek",
  "chains.daemon": "daemon zinciri",
  "chains.executor": "executor zinciri",
  "chains.records_one": "1 kayıt doğrulandı",
  "chains.records_other": "{n} kayıt doğrulandı",
  "chains.not_verified": "henüz doğrulanmadı",
  "chains.note": "Bilerek ayrı tutuluyorlar: kadrand executor'ın günlüğüne yazamaz; ele geçirilmiş bir daemon yalnız kendi kaydını temizleyebilir.",
  "audit.recent": "Son denetim kayıtları",
  "audit.see_all": "Tümünü gör",
  "audit.eyebrow": "Denetim",
  "audit.title": "Denetim günlüğü",
  "audit.col_seq": "Sıra",
  "audit.col_time": "Zaman",
  "audit.col_actor": "Aktör",
  "audit.col_action": "Eylem",
  "audit.col_target": "Hedef",
  "audit.col_outcome": "Sonuç",
  "audit.load_older": "Daha eskiler",
  "audit.loading": "Yükleniyor…",
  "audit.empty": "Zincirde kayıt yok.",
  "audit.range": "Göster",
  "audit.last": "son {n}",
  "audit.filter_action": "Eylem",
  "audit.filter_outcome": "Sonuç",
  "audit.filter_all": "Tümü",
  "audit.copied": "Kopyalandı",
  "audit.copy": "Parmak izini kopyala",
  "audit.outcomes": "Sonuçlar",
  "audit.unknown_actor": "bilinmiyor",
  "audit.no_target_title": "Bağlı sunucu yok",
  "audit.no_target_body": "Denetim günlüğünü görmek için önce bir sunucuya bağlan.",
  "outcome.success": "başarılı",
  "outcome.failure": "başarısız",
  "outcome.denied": "reddedildi",
  "outcome.unspecified": "belirsiz",
  "settings.title": "Ayarlar",
  "settings.appearance": "Görünüm",
  "settings.appearance_meta": "Değişiklikler hemen uygulanır ve bu bilgisayarda kalır.",
  "settings.window": "Pencere",
  "settings.window_hint": "Masaüstünün ne kadarı görünsün.",
  "settings.tint": "Pencere tonu",
  "settings.tint_hint": "Düşük değer daha saydam.",
  "settings.panel": "Panel camı",
  "settings.panel_hint": "Kartlar ne kadar buzlu.",
  "settings.edge": "Kenar ışığı",
  "settings.edge_hint": "Camın çevresindeki parlak çizgi.",
  "settings.glow": "Üstüne gelme / odak parlaması",
  "settings.glow_hint": "Klavye odak halkası hiçbir zaman tamamen kaybolmaz.",
  "settings.accent": "Vurgu rengi",
  "settings.accent_hint": "Odak halkası, bağlantılar ve ana düğme.",
  "settings.theme": "Tema",
  "settings.theme_hint": "Koyu ya da açık cam.",
  "settings.ui_font": "Arayüz yazı tipi",
  "settings.ui_font_hint": "Bu bilgisayarda kurulu herhangi bir yazı tipi.",
  "settings.code_font": "Kod yazı tipi",
  "settings.code_font_hint": "Hash'ler, sürümler ve sıra numaraları.",
  "settings.language": "Dil",
  "settings.language_hint": "Sunucudan gelen iletiler İngilizce kalır.",
  "settings.servers": "Sunucular",
  "settings.servers_hint": "Kayıtlı hedefler. Silmek sunucuya dokunmaz.",
  "settings.no_servers": "Kayıtlı sunucu yok.",
  "settings.remove": "Kaldır",
  "settings.about": "Hakkında",
  "settings.about_hint": "Bu bilgisayardaki sürümler.",
  "settings.reset": "Varsayılanlara dön",
  "mode.clear": "Saydam",
  "mode.acrylic": "Akrilik",
  "mode.mica": "Mica",
  "mode.vibrancy": "Vibrancy",
  "mode.solid": "Opak",
  "accent.ice": "Buz",
  "accent.amber": "Kehribar",
  "accent.phosphor": "Fosfor",
  "accent.paper": "Kâğıt",
  "tone.dark": "Koyu",
  "tone.light": "Açık",
  "font.system": "Sistem varsayılanı",
  "font.bundled": "JetBrains Mono (gömülü)",
  "font.families_one": "Bu bilgisayarda 1 yazı tipi ailesi var",
  "font.families_other": "Bu bilgisayarda {n} yazı tipi ailesi var",
  "font.unavailable": "Kurulu yazı tipleri listelenemedi. Varsayılanlar kullanılıyor.",
  "about.line": "KΛDRΛN {version} · sidecar protokolü {protocol}",
  "toast.connect_failed": "Bağlanılamadı: {error}",
  "toast.verify_failed": "Zincirler doğrulanamadı: {error}",
  "toast.audit_failed": "Denetim günlüğü okunamadı: {error}",
  "toast.save_failed": "Kaydedilemedi: {error}",
  "toast.sidecar_failed": "Sidecar başlamadı: {error}",
  "toast.copy_failed": "Kopyalanamadı: {error}",
  "toast.profiles_failed": "Kayıtlı sunucular okunamadı: {error}",
  "toast.dismiss": "Kapat"
}
```

- [ ] **Adım 5: Testlerin geçtiğini gör**

Çalıştır: `npm test`
Beklenen: PASS. `no-raw-text` şu an yalnız `App.tsx`'i tarar; içinde yalnız izinli `KΛDRΛN` var.

- [ ] **Adım 6: Koruma gerçekten koruyor mu? (KIRMIZI ölç)**

Geçici olarak `App.tsx`'te `<span className="wordmark">KΛDRΛN</span>`'ın yanına `<b>Hello</b>` ekle ve `node --test test/no-raw-text.test.ts` koş. Test `"Hello"` göstererek düşmeli. Sonra ekleneni geri al ve testi yeniden koş: PASS. Aynısını `en.json`'da bir değeri geçici olarak `"Ayarlar"` yaparak `i18n.test.ts` için de yap.

- [ ] **Adım 7: Commit**

```bash
git add desktop/src/renderer/lib/i18n.ts desktop/src/renderer/lib/languages.ts desktop/src/renderer/i18n desktop/test/i18n.test.ts desktop/test/no-raw-text.test.ts
git commit -m "feat(masaüstü): en/tr dil katmanı ve çeviri korumaları"
```

---

### Görev 5: Ana süreçten kullanıcıya ulaşan hatalar İngilizce

Sidecar istemcisinin ve ana sürecin fırlattığı hatalar IPC üzerinden arayüze ve bildirime ulaşıyor. Bu yüzden İngilizce olmaları gerekiyor (K-141). Konsol tanılamaları (`onDiagnostic`, `onWarning`) Türkçe kalır.

**Dosyalar:**
- Değişir: `desktop/src/shared/sidecar-client.ts`, `desktop/src/main/profiles.ts`, `desktop/src/main/spawn-sidecar.ts`
- Değişir: `desktop/test/sidecar-client.test.ts:239` (`/yanıtlanmadı/` → `/got no answer/`)
- Test: `desktop/test/errors-english.test.ts`

- [ ] **Adım 1: Korumayı yaz**

`desktop/test/errors-english.test.ts`:

```ts
/**
 * Ana süreçte ve paylaşılan kodda `new …Error(…)` iletileri İngilizce:
 * IPC üzerinden arayüze, oradan bildirime ulaşıyorlar (K-141). Konsol
 * tanılamaları (onWarning, onDiagnostic) bu kurala tabi değil.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const here = dirname(fileURLToPath(import.meta.url));
const roots = [join(here, "..", "src", "main"), join(here, "..", "src", "shared")];
const TURKISH = /[çğıöşüÇĞİÖŞÜ]/;

async function tsFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    if (e.isDirectory()) out.push(...(await tsFiles(join(dir, e.name))));
    else if (e.name.endsWith(".ts")) out.push(join(dir, e.name));
  }
  return out;
}

test("Error iletileri İngilizce", async () => {
  const problems: string[] = [];
  let errors = 0;
  for (const root of roots) {
    for (const file of await tsFiles(root)) {
      const sf = ts.createSourceFile(file, await readFile(file, "utf8"), ts.ScriptTarget.Latest, true);
      const visit = (node: ts.Node): void => {
        if (ts.isNewExpression(node) && /Error$/.test(node.expression.getText(sf))) {
          errors++;
          const texts: string[] = [];
          const collect = (n: ts.Node): void => {
            if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) texts.push(n.text);
            if (ts.isTemplateExpression(n)) {
              texts.push(n.head.text, ...n.templateSpans.map((s) => s.literal.text));
            }
            ts.forEachChild(n, collect);
          };
          for (const arg of node.arguments ?? []) collect(arg);
          for (const text of texts) if (TURKISH.test(text)) problems.push(`${file}: ${text}`);
        }
        ts.forEachChild(node, visit);
      };
      visit(sf);
    }
  }
  assert.ok(errors >= 8, `yalnız ${errors} hata ifadesi bulundu — yollar kaymış olabilir`);
  assert.deepEqual(problems, []);
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/errors-english.test.ts`
Beklenen: FAIL. `sidecar kapatıldı`, `sidecar başlatılamadı`, `yanıtlanmadı`, `istek yazılamadı`, `sonlandı`, `hata verdi`, `tampon`, `profiller bir dizi olmalı`, `boş — komut belirtilmedi`, `resourcesPath gerekli`, `repoRoot gerekli` listelenir.

- [ ] **Adım 3: İletileri çevir**

`desktop/src/shared/sidecar-client.ts` içinde, her biri tam olarak bir kez geçer:

| Eski | Yeni |
|---|---|
| `new SidecarError("sidecar kapatıldı")` (iki yer: 124 ve 177. satırlar) | `new SidecarError("the sidecar is closed")` |
| `` `sidecar başlatılamadı: ${(err as Error).message}` `` | `` `the sidecar did not start: ${(err as Error).message}` `` |
| `` `"${method}" çağrısı ${this.#timeoutMs} ms içinde yanıtlanmadı` `` | `` `"${method}" got no answer within ${this.#timeoutMs} ms` `` |
| `` `istek yazılamadı: ${(err as Error).message}` `` | `` `could not send the request: ${(err as Error).message}` `` |
| `` `sidecar süreci sonlandı (çıkış kodu ${code ?? "bilinmiyor"})` `` | `` `the sidecar exited (code ${code ?? "unknown"})` `` |
| `` `sidecar süreci hata verdi: ${err.message}` `` | `` `the sidecar process failed: ${err.message}` `` |
| `"sidecar çıktısı tampon sınırını aştı — süreç kapatılıyor"` | `"the sidecar's output went over the buffer limit; stopping it"` |

`onDiagnostic` iletileri (`çözümlenemeyen sidecar satırı`, `id'siz sidecar yanıtı`) DEĞİŞMEZ. `sidecar-client.test.ts:209` onlara bakıyor.

`desktop/src/main/profiles.ts:75`: `throw new Error("profiller bir dizi olmalı")` → `throw new Error("profiles must be a list")`.

`desktop/src/main/spawn-sidecar.ts`:

| Eski | Yeni |
|---|---|
| `` `${COMMAND_OVERRIDE} boş — komut belirtilmedi` `` | `` `${COMMAND_OVERRIDE} is empty: no command given` `` |
| `"paketlenmiş uygulamada resourcesPath gerekli — " + "binary electron-builder'da extraResources olarak paketlenmeli"` | `"resourcesPath is required in a packaged app; the binary ships as an extra resource"` |
| `"geliştirme kipinde repoRoot gerekli"` | `"repoRoot is required in development"` |

`desktop/test/sidecar-client.test.ts:239`: `assert.match(err.message, /yanıtlanmadı/);` → `assert.match(err.message, /got no answer/);`.

- [ ] **Adım 4: Testleri koş**

Çalıştır: `npm test`
Beklenen: PASS. Sayaç en az 8 hata ifadesi bulur.

`main/index.ts`'teki `requireTarget` (`hedef boş olamaz`) bu turda hâlâ Türkçe ve testi düşürür. O fonksiyon Görev 9'da `ipc-validate.ts`'e taşınıyor. Şimdi `index.ts:192-195`'teki iki iletiyi `"target is empty"` ve `"target is too long"` yap, böylece test bu görevde yeşile döner.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/shared/sidecar-client.ts desktop/src/main/profiles.ts desktop/src/main/spawn-sidecar.ts desktop/src/main/index.ts desktop/test/sidecar-client.test.ts desktop/test/errors-english.test.ts
git commit -m "fix(masaüstü): kullanıcıya ulaşan hata iletileri İngilizce (K-141)"
```

---
### Görev 6: Görünüm şeması — modlar, varsayılanlar, temizleme (paylaşılan, saf)

**Dosyalar:**
- Yeni: `desktop/src/shared/appearance.ts`
- Test: `desktop/test/appearance-schema.test.ts`

**Arayüzler:**
- Üretir:
  - Tipler:
    - `type WindowMode = "clear" | "acrylic" | "mica" | "vibrancy" | "solid"`
    - `type Accent = "ice" | "amber" | "phosphor" | "paper"`
    - `type Tone = "dark" | "light"`
    - `type Language = "en" | "tr"`
    - `interface FontChoice { family: string; weight: number }`
    - `interface Appearance { version: 1; mode; tint; panel; edge; glow; accent; tone; language; uiFont: FontChoice; codeFont: FontChoice }`
    - `interface PlatformInfo { platform: string; release: string }`
  - Sabitler: `ALL_MODES`, `ACCENTS`, `TONES`, `LANGUAGE_CODES`, `LIMITS`, `MAX_FONT_FAMILY = 200`
  - Fonksiyonlar:
    - `isWindows11(info): boolean`
    - `modesFor(info): readonly WindowMode[]`
    - `defaultMode(info): WindowMode`
    - `defaultAppearance(info): Appearance`
    - `sanitizeAppearance(raw: unknown, info): Appearance`

- [ ] **Adım 1: Testi yaz**

`desktop/test/appearance-schema.test.ts`:

```ts
/**
 * Görünüm ayarlarının şeması. Dosya elle düzenlenebilir ya da bozulabilir:
 * her alan doğrulanır, geçersiz olan varsayılana döner, program çökmez.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  defaultAppearance,
  isWindows11,
  modesFor,
  sanitizeAppearance,
  type PlatformInfo,
} from "../src/shared/appearance.ts";

const WIN11: PlatformInfo = { platform: "win32", release: "10.0.26200" };
const WIN10: PlatformInfo = { platform: "win32", release: "10.0.19045" };
const MAC: PlatformInfo = { platform: "darwin", release: "24.6.0" };
const LINUX: PlatformInfo = { platform: "linux", release: "6.12.48" };

test("Windows 11 ayrımı yapı numarasından (22H2 = 22621)", () => {
  assert.equal(isWindows11(WIN11), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22621" }), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22000" }), false);
  assert.equal(isWindows11(WIN10), false);
  assert.equal(isWindows11(MAC), false);
});

test("her sistemde sunulan modlar ve varsayılan", () => {
  assert.deepEqual(modesFor(WIN11), ["clear", "acrylic", "mica", "solid"]);
  assert.deepEqual(modesFor(WIN10), ["clear", "solid"]);
  assert.deepEqual(modesFor(MAC), ["clear", "vibrancy", "solid"]);
  assert.deepEqual(modesFor(LINUX), ["clear", "solid"]);
  assert.equal(defaultAppearance(WIN11).mode, "acrylic");
  assert.equal(defaultAppearance(WIN10).mode, "clear");
  assert.equal(defaultAppearance(MAC).mode, "vibrancy");
  assert.equal(defaultAppearance(LINUX).mode, "solid");
});

test("varsayılan değerler spec ile aynı", () => {
  const d = defaultAppearance(WIN11);
  assert.deepEqual(d, {
    version: 1,
    mode: "acrylic",
    tint: 60,
    panel: 7,
    edge: 22,
    glow: 60,
    accent: "ice",
    tone: "dark",
    language: "en",
    uiFont: { family: "", weight: 400 },
    codeFont: { family: "", weight: 400 },
  });
});

test("nesne değilse ya da diziyse varsayılan", () => {
  for (const raw of [null, undefined, 42, "x", [], [1, 2]]) {
    assert.deepEqual(sanitizeAppearance(raw, WIN11), defaultAppearance(WIN11));
  }
});

test("sınır dışı sayılar kırpılır, sayı olmayanlar varsayılana döner", () => {
  const a = sanitizeAppearance({ tint: 150, panel: -4, edge: 61.6, glow: "90" }, WIN11);
  assert.equal(a.tint, 100);
  assert.equal(a.panel, 0);
  assert.equal(a.edge, 60);
  assert.equal(a.glow, 60);
  assert.equal(sanitizeAppearance({ tint: Number.NaN }, WIN11).tint, 60);
  assert.equal(sanitizeAppearance({ tint: 33.4 }, WIN11).tint, 33);
});

test("bu sistemde olmayan mod varsayılana döner", () => {
  assert.equal(sanitizeAppearance({ mode: "acrylic" }, WIN10).mode, "clear");
  assert.equal(sanitizeAppearance({ mode: "vibrancy" }, WIN11).mode, "acrylic");
  assert.equal(sanitizeAppearance({ mode: "mica" }, WIN11).mode, "mica");
  assert.equal(sanitizeAppearance({ mode: "holographic" }, WIN11).mode, "acrylic");
});

test("bilinmeyen seçimler varsayılana döner", () => {
  const a = sanitizeAppearance({ accent: "neon", tone: "sepia", language: "de" }, WIN11);
  assert.equal(a.accent, "ice");
  assert.equal(a.tone, "dark");
  assert.equal(a.language, "en");
  assert.equal(sanitizeAppearance({ language: "tr" }, WIN11).language, "tr");
});

test("yazı tipi: uzun ad reddedilir, kalınlık 100–900", () => {
  const long = "x".repeat(10_000);
  const a = sanitizeAppearance({ uiFont: { family: long, weight: 500 }, codeFont: { family: "JetBrainsMono Nerd Font", weight: 1200 } }, WIN11);
  assert.deepEqual(a.uiFont, { family: "", weight: 500 });
  assert.deepEqual(a.codeFont, { family: "JetBrainsMono Nerd Font", weight: 900 });
  assert.deepEqual(sanitizeAppearance({ uiFont: "Arial" }, WIN11).uiFont, { family: "", weight: 400 });
});

test("geçerli kısmi nesne kendi alanlarını korur", () => {
  const a = sanitizeAppearance({ tint: 48, panel: 6, edge: 8, mode: "clear" }, WIN11);
  assert.equal(a.tint, 48);
  assert.equal(a.panel, 6);
  assert.equal(a.edge, 8);
  assert.equal(a.mode, "clear");
  assert.equal(a.version, 1);
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/appearance-schema.test.ts`
Beklenen: FAIL, modül bulunamadı.

- [ ] **Adım 3: Şemayı yaz**

`desktop/src/shared/appearance.ts`:

```ts
/**
 * Görünüm ayarları: şema, işletim sistemine göre modlar, varsayılanlar ve
 * temizleme. Ana süreç (doğrulayıp diske yazmak için) ve arayüz (Reset ve
 * seçenekler için) aynı dosyayı kullanır; Node ya da DOM'a bağımlı değil.
 *
 * Dosya elle düzenlenebilir ya da bozulabilir. Her alan TEK TEK
 * doğrulanır; geçersiz alan varsayılana döner, geçerli alanlar korunur.
 */

export type WindowMode = "clear" | "acrylic" | "mica" | "vibrancy" | "solid";
export type Accent = "ice" | "amber" | "phosphor" | "paper";
export type Tone = "dark" | "light";
export type Language = "en" | "tr";

export interface FontChoice {
  /** Boş dize: arayüzde sistem yazı tipi, kodda gömülü JetBrains Mono. */
  family: string;
  weight: number;
}

export interface Appearance {
  version: 1;
  mode: WindowMode;
  tint: number;
  panel: number;
  edge: number;
  glow: number;
  accent: Accent;
  tone: Tone;
  language: Language;
  uiFont: FontChoice;
  codeFont: FontChoice;
}

export interface PlatformInfo {
  platform: string;
  /** os.release(): Windows'ta "10.0.<yapı>". */
  release: string;
}

export const ALL_MODES: readonly WindowMode[] = ["clear", "acrylic", "mica", "vibrancy", "solid"];
export const ACCENTS: readonly Accent[] = ["ice", "amber", "phosphor", "paper"];
export const TONES: readonly Tone[] = ["dark", "light"];
export const LANGUAGE_CODES: readonly Language[] = ["en", "tr"];

export const LIMITS = {
  tint: [0, 100],
  panel: [0, 30],
  edge: [0, 60],
  glow: [0, 100],
  weight: [100, 900],
} as const satisfies Record<string, readonly [number, number]>;

export const MAX_FONT_FAMILY = 200;

/** Acrylic ve Mica (backgroundMaterial) Windows 11 22H2 (yapı 22621) ister. */
const WIN11_BUILD = 22621;

export function isWindows11(info: PlatformInfo): boolean {
  if (info.platform !== "win32") return false;
  const build = Number(info.release.split(".")[2]);
  return Number.isFinite(build) && build >= WIN11_BUILD;
}

export function modesFor(info: PlatformInfo): readonly WindowMode[] {
  if (info.platform === "win32") return isWindows11(info) ? ["clear", "acrylic", "mica", "solid"] : ["clear", "solid"];
  if (info.platform === "darwin") return ["clear", "vibrancy", "solid"];
  return ["clear", "solid"];
}

export function defaultMode(info: PlatformInfo): WindowMode {
  if (isWindows11(info)) return "acrylic";
  if (info.platform === "win32") return "clear";
  if (info.platform === "darwin") return "vibrancy";
  return "solid";
}

export function defaultAppearance(info: PlatformInfo): Appearance {
  return {
    version: 1,
    mode: defaultMode(info),
    tint: 60,
    panel: 7,
    edge: 22,
    glow: 60,
    accent: "ice",
    tone: "dark",
    language: "en",
    uiFont: { family: "", weight: 400 },
    codeFont: { family: "", weight: 400 },
  };
}

function clampInt(value: unknown, [lo, hi]: readonly [number, number], fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(hi, Math.max(lo, Math.round(value)));
}

function pick<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  return typeof value === "string" && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function font(value: unknown, fallback: FontChoice): FontChoice {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return fallback;
  const o = value as Record<string, unknown>;
  const family =
    typeof o.family === "string" && o.family.length <= MAX_FONT_FAMILY ? o.family : fallback.family;
  return { family, weight: clampInt(o.weight, LIMITS.weight, fallback.weight) };
}

/** sanitizeAppearance, güvenilmez bir değerden geçerli bir Appearance üretir. */
export function sanitizeAppearance(raw: unknown, info: PlatformInfo): Appearance {
  const d = defaultAppearance(info);
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return d;
  const o = raw as Record<string, unknown>;
  return {
    version: 1,
    mode: pick(o.mode, modesFor(info), d.mode),
    tint: clampInt(o.tint, LIMITS.tint, d.tint),
    panel: clampInt(o.panel, LIMITS.panel, d.panel),
    edge: clampInt(o.edge, LIMITS.edge, d.edge),
    glow: clampInt(o.glow, LIMITS.glow, d.glow),
    accent: pick(o.accent, ACCENTS, d.accent),
    tone: pick(o.tone, TONES, d.tone),
    language: pick(o.language, LANGUAGE_CODES, d.language),
    uiFont: font(o.uiFont, d.uiFont),
    codeFont: font(o.codeFont, d.codeFont),
  };
}
```

Not: `ALL_MODES` mutasyon betiğinin (Görev 19) "mod doğrulaması gevşedi" mutantı için dışa aktarılır. Ayrıca `window-options.ts` testi her modu dolaşırken onu kullanır.

- [ ] **Adım 4: Testleri ve tip denetimini koş**

```bash
npm test
npm run typecheck
```

Beklenen: PASS. Görev 4'teki `languages.ts` artık tip denetiminden geçer.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/shared/appearance.ts desktop/test/appearance-schema.test.ts
git commit -m "feat(masaüstü): görünüm şeması, modlar ve doğrulama"
```

---

### Görev 7: `appearance.json` okuma ve atomik yazma (ana süreç)

**Dosyalar:**
- Yeni: `desktop/src/main/appearance.ts`
- Test: `desktop/test/appearance-store.test.ts`

**Arayüzler:**
- Tüketir: `sanitizeAppearance`, `Appearance`, `PlatformInfo` (Görev 6).
- Üretir:
  - `appearancePath(userDataDir: string): string`
  - `loadAppearance(userDataDir, info, onWarning?): Promise<Appearance>`
  - `saveAppearance(userDataDir, raw: unknown, info): Promise<Appearance>`

- [ ] **Adım 1: Testi yaz**

`desktop/test/appearance-store.test.ts`:

```ts
/**
 * appearance.json: yoksa varsayılan, bozuksa varsayılan + uyarı, yazım
 * atomik (geçici dosya + yeniden adlandırma); başarısız yazımda geçici
 * dosya kalmaz.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { appearancePath, loadAppearance, saveAppearance } from "../src/main/appearance.ts";
import { defaultAppearance } from "../src/shared/appearance.ts";

const WIN11 = { platform: "win32", release: "10.0.26200" };

async function tempDir(): Promise<string> {
  return mkdtemp(join(tmpdir(), "kadran-gorunum-"));
}

test("dosya yoksa varsayılan, uyarı yok", async () => {
  const dir = await tempDir();
  try {
    const warnings: string[] = [];
    assert.deepEqual(await loadAppearance(dir, WIN11, (m) => warnings.push(m)), defaultAppearance(WIN11));
    assert.deepEqual(warnings, []);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("bozuk JSON varsayılana döner ve uyarır", async () => {
  const dir = await tempDir();
  try {
    await writeFile(appearancePath(dir), "{ bu json değil", "utf8");
    const warnings: string[] = [];
    assert.deepEqual(await loadAppearance(dir, WIN11, (m) => warnings.push(m)), defaultAppearance(WIN11));
    assert.equal(warnings.length, 1);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("kaydet → oku aynı değeri verir; değer temizlenerek yazılır", async () => {
  const dir = await tempDir();
  try {
    const saved = await saveAppearance(dir, { tint: 48, panel: 99, mode: "mica", extra: "atılır" }, WIN11);
    assert.equal(saved.tint, 48);
    assert.equal(saved.panel, 30);
    assert.equal(saved.mode, "mica");
    assert.deepEqual(await loadAppearance(dir, WIN11), saved);
    const onDisk = JSON.parse(await readFile(appearancePath(dir), "utf8")) as Record<string, unknown>;
    assert.equal("extra" in onDisk, false);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("var olan dosyanın üstüne yazılır ve geçici dosya kalmaz", async () => {
  const dir = await tempDir();
  try {
    await saveAppearance(dir, { tint: 10 }, WIN11);
    await saveAppearance(dir, { tint: 20 }, WIN11);
    assert.equal((await loadAppearance(dir, WIN11)).tint, 20);
    assert.deepEqual((await readdir(dir)).sort(), ["appearance.json"]);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("yazım başarısızsa hata fırlar, geçici dosya silinir", async () => {
  const dir = await tempDir();
  try {
    // Hedef bir DİZİN: yeniden adlandırma başarısız olur.
    await mkdir(appearancePath(dir));
    await assert.rejects(saveAppearance(dir, { tint: 10 }, WIN11));
    assert.deepEqual(await readdir(dir), ["appearance.json"]);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/appearance-store.test.ts`
Beklenen: FAIL, modül bulunamadı.

- [ ] **Adım 3: Uygulamayı yaz**

`desktop/src/main/appearance.ts`:

```ts
/**
 * Görünüm ayarlarının kalıcı saklanması: profiles.json'ın yanında
 * appearance.json.
 *
 * Yazım ATOMİK: önce geçici dosyaya, sonra yeniden adlandırma. Yarıda
 * kesilen bir yazım (güç kesintisi, çökme) yarım bir JSON bırakmaz; en
 * kötü durumda eski dosya kalır. (profiles.ts bugün düz writeFile
 * kullanıyor; bu desen yeni ve ona dokunulmadı.)
 *
 * Okunan her değer sanitizeAppearance'tan geçer: dosya elle düzenlenmiş
 * ya da bozulmuş olabilir.
 */

import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";

import { sanitizeAppearance, type Appearance, type PlatformInfo } from "../shared/appearance.ts";

export function appearancePath(userDataDir: string): string {
  return join(userDataDir, "appearance.json");
}

export async function loadAppearance(
  userDataDir: string,
  info: PlatformInfo,
  onWarning?: (message: string) => void,
): Promise<Appearance> {
  let raw: string;
  try {
    raw = await readFile(appearancePath(userDataDir), "utf8");
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      onWarning?.(`görünüm ayarları okunamadı: ${(err as Error).message}`);
    }
    return sanitizeAppearance(null, info);
  }
  try {
    return sanitizeAppearance(JSON.parse(raw) as unknown, info);
  } catch (err) {
    onWarning?.(`görünüm dosyası çözümlenemedi, varsayılan kullanılıyor: ${(err as Error).message}`);
    return sanitizeAppearance(null, info);
  }
}

export async function saveAppearance(userDataDir: string, raw: unknown, info: PlatformInfo): Promise<Appearance> {
  const clean = sanitizeAppearance(raw, info);
  const path = appearancePath(userDataDir);
  await mkdir(dirname(path), { recursive: true });
  const tmp = `${path}.${process.pid}.${Date.now()}.tmp`;
  try {
    await writeFile(tmp, JSON.stringify(clean, null, 2) + "\n", "utf8");
    await rename(tmp, path);
  } catch (err) {
    await rm(tmp, { force: true });
    throw err;
  }
  return clean;
}
```

- [ ] **Adım 4: Testleri koş**

Çalıştır: `npm test`
Beklenen: PASS.

Windows'ta `rename` hedef bir dizinse `EPERM` verir, Linux'ta `EISDIR`. İkisinde de reddedilir; test yalnız reddedildiğine ve geçici dosyanın kalmadığına bakıyor.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/main/appearance.ts desktop/test/appearance-store.test.ts
git commit -m "feat(masaüstü): appearance.json — doğrulayarak okuma, atomik yazma"
```

---

### Görev 8: İzinler — yalnız yazı tipi listesi

**Dosyalar:**
- Yeni: `desktop/src/main/permissions.ts`
- Değişir: `desktop/src/main/index.ts` (`whenReady` içinde `lockDownPermissions(session.defaultSession)`)
- Test: `desktop/test/permissions.test.ts`

**Arayüzler:**
- Üretir:
  - `ALLOWED_CHECKS: ReadonlySet<string>`
  - `allowPermissionCheck(permission: string): boolean`
  - `allowPermissionRequest(permission: string): boolean`
  - `lockDownPermissions(ses: PermissionSession): void`
  - `interface PermissionSession { setPermissionCheckHandler; setPermissionRequestHandler }`

- [ ] **Adım 1: Testi yaz**

`desktop/test/permissions.test.ts`:

```ts
/**
 * İzinler. Electron işleyici kurulmazsa HER izni verir; bundan sonra
 * yalnız yazı tipi listesine izin var.
 *
 * Ölçüldü (Electron 43, Windows 11): Chromium yazı tipi listesini İSTEK
 * değil KONTROL işleyicisinden "local-fonts" adıyla soruyor; bu ad
 * Electron'un TypeScript tiplerinde yok. Reddedilince queryLocalFonts()
 * hata değil BOŞ liste döndürüyor.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  allowPermissionCheck,
  allowPermissionRequest,
  lockDownPermissions,
  type PermissionSession,
} from "../src/main/permissions.ts";

test("kontrol: yalnız local-fonts", () => {
  assert.equal(allowPermissionCheck("local-fonts"), true);
  for (const p of ["notifications", "media", "geolocation", "clipboard-read", "clipboard-sanitized-write", "fullscreen", "openExternal", "", "LOCAL-FONTS"]) {
    assert.equal(allowPermissionCheck(p), false, p);
  }
});

test("istek: hiçbiri", () => {
  for (const p of ["local-fonts", "notifications", "media", "unknown"]) {
    assert.equal(allowPermissionRequest(p), false, p);
  }
});

test("lockDownPermissions iki işleyiciyi de kurar ve onlar kurala uyar", () => {
  let check: ((wc: unknown, permission: string) => boolean) | null = null;
  let request: ((wc: unknown, permission: string, cb: (granted: boolean) => void) => void) | null = null;
  const fake = {
    setPermissionCheckHandler: (h: typeof check) => {
      check = h;
    },
    setPermissionRequestHandler: (h: typeof request) => {
      request = h;
    },
  } as unknown as PermissionSession;

  lockDownPermissions(fake);
  assert.ok(check && request, "işleyiciler kurulmadı");
  assert.equal(check!(null, "local-fonts"), true);
  assert.equal(check!(null, "notifications"), false);
  let granted: boolean | null = null;
  request!(null, "media", (g) => {
    granted = g;
  });
  assert.equal(granted, false);
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/permissions.test.ts`
Beklenen: FAIL, modül bulunamadı.

- [ ] **Adım 3: Uygulamayı yaz**

`desktop/src/main/permissions.ts`:

```ts
/**
 * Oturum izinleri.
 *
 * Electron, işleyici kurulmazsa her izin isteğini onaylar. Renderer'ın
 * kamera, mikrofon, bildirim, konum ya da pano erişimine ihtiyacı yok.
 * Tek gerekli şey kurulu yazı tiplerinin listesi (Görünüm ayarları).
 *
 * Ölçüldü (Electron 43, Windows 11): Chromium bu listeyi İSTEK değil
 * KONTROL işleyicisinden "local-fonts" adıyla soruyor; ad Electron'un
 * TypeScript birleşim tipinde yok, bu yüzden karşılaştırma string
 * üzerinden. Reddedilirse queryLocalFonts() boş liste döndürür; arayüz
 * bunu "listelenemedi" olarak gösterir.
 *
 * Pano izni AÇILMAZ: parmak izi kopyalama köprü üzerinden ana süreçte
 * yapılır (copyText).
 */

import type { Session } from "electron";

export const ALLOWED_CHECKS: ReadonlySet<string> = new Set(["local-fonts"]);

export function allowPermissionCheck(permission: string): boolean {
  return ALLOWED_CHECKS.has(permission);
}

export function allowPermissionRequest(_permission: string): boolean {
  return false;
}

export type PermissionSession = Pick<Session, "setPermissionCheckHandler" | "setPermissionRequestHandler">;

export function lockDownPermissions(ses: PermissionSession): void {
  ses.setPermissionCheckHandler((_wc, permission) => allowPermissionCheck(permission as string));
  ses.setPermissionRequestHandler((_wc, permission, callback) => callback(allowPermissionRequest(permission as string)));
}
```

`desktop/src/main/index.ts`'te `import { app, BrowserWindow, ipcMain, shell } from "electron";` satırına `session` ekle. Şunu da içe aktar: `import { lockDownPermissions } from "./permissions.ts";`. `app.whenReady().then(async () => {` bloğunun İLK satırı şu olsun:

```ts
    lockDownPermissions(session.defaultSession);
```

- [ ] **Adım 4: Testleri ve duman testini koş**

```bash
npm test
npm run typecheck
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen: PASS; duman testi GEÇTİ.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/main/permissions.ts desktop/src/main/index.ts desktop/test/permissions.test.ts
git commit -m "feat(masaüstü): izinler kapalı, yalnız yazı tipi listesi"
```

---
### Görev 9: Pencere modları, IPC doğrulama, köprü ve ana sürecin yeniden düzeni

**Dosyalar:**
- Yeni: `desktop/src/main/window-options.ts` (saf), `desktop/src/main/window.ts`, `desktop/src/main/ipc-validate.ts` (saf), `desktop/src/main/smoke.ts`
- Değişir: `desktop/src/shared/channels.ts`, `desktop/src/preload/index.cjs`, `desktop/src/main/index.ts` (tamamen yeniden)
- Yeni: `desktop/src/renderer/bridge.ts`
- Test: `desktop/test/window-options.test.ts`, `desktop/test/ipc-validate.test.ts`; mevcut `desktop/test/channels.test.ts` değişmeden geçmeli.

**Arayüzler:**
- Tüketir:
  - `WindowMode`, `ALL_MODES`, `modesFor`, `defaultAppearance`, `PlatformInfo` (Görev 6)
  - `loadAppearance`, `saveAppearance` (Görev 7)
  - `lockDownPermissions` (Görev 8)
- Üretir:
  - `window-options.ts`:
    - `windowOptions(mode, platform, preloadPath, state?): BrowserWindowConstructorOptions`
    - `needsRecreate(from, to): boolean`
    - `interface WindowState { bounds?: Rectangle; maximized: boolean }`
    - `SOLID_BACKGROUND`
  - `window.ts`: `createWindowHost(opts): WindowHost`. `WindowHost` şunlara sahip:
    - `current(): BrowserWindow | null`
    - `open(mode, view?): BrowserWindow`
    - `applyMode(mode): void`
  - `ipc-validate.ts`:
    - `requireTarget(v): string`
    - `toUint(v, fallback): number`
    - `requireWindowAction(v): WindowAction`
    - `requireCopyText(v): string`
    - `WINDOW_ACTIONS`, `MAX_COPY = 1024`, `MAX_TARGET = 500`
  - Yeni kanallar:
    - `CHANNELS.loadAppearance = "kadran:appearance-load"`
    - `CHANNELS.saveAppearance = "kadran:appearance-save"`
    - `CHANNELS.windowControl = "kadran:window"`
    - `CHANNELS.copyText = "kadran:copy"`
  - Köprü (`window.kadran`), anahtar listesi sıralı: `auditList, auditVerify, copyText, loadAppearance, loadProfiles, platform, saveAppearance, saveProfiles, status, version, windowControl`.
  - `bridge.ts`: `bridge: KadranBridge`, `Profile`, `AppearanceBundle { appearance, defaults, modes }`, `WindowAction`.
  - `smoke.ts`:
    - `SMOKE_TEST`
    - `EXPECTED_BRIDGE`
    - `prepareSmokeUserData(): string`
    - `runSmokeTest(win): Promise<void>`

- [ ] **Adım 1: Saf testleri yaz**

`desktop/test/window-options.test.ts`:

```ts
/**
 * Pencere seçenekleri: güvenlik ayarları HER modda aynı; mod yalnız
 * görünüşü değiştirir. Saydamlık pencere kurulduktan sonra açılıp
 * kapanamaz, bu yüzden Clear ile diğerleri arasında pencere yeniden kurulur.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { ALL_MODES } from "../src/shared/appearance.ts";
import { needsRecreate, SOLID_BACKGROUND, windowOptions } from "../src/main/window-options.ts";

const PRELOAD = "/out/preload/index.cjs";

test("her modda güvenlik ayarları aynı ve sıkı", () => {
  for (const mode of ALL_MODES) {
    for (const platform of ["win32", "darwin", "linux"]) {
      const o = windowOptions(mode, platform, PRELOAD);
      assert.deepEqual(
        o.webPreferences,
        { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true, webviewTag: false },
        `${mode}/${platform}`,
      );
      assert.equal(o.show, false, "hazır olmadan gösterilmemeli");
    }
  }
});

test("mod → pencere malzemesi", () => {
  const clear = windowOptions("clear", "win32", PRELOAD);
  assert.equal(clear.transparent, true);
  assert.equal(clear.backgroundColor, "#00000000");
  assert.equal(clear.frame, false);

  assert.equal(windowOptions("acrylic", "win32", PRELOAD).backgroundMaterial, "acrylic");
  assert.equal(windowOptions("mica", "win32", PRELOAD).backgroundMaterial, "mica");
  assert.equal(windowOptions("acrylic", "win32", PRELOAD).transparent, undefined);

  const vib = windowOptions("vibrancy", "darwin", PRELOAD);
  assert.equal(vib.vibrancy, "under-window");
  assert.equal(vib.titleBarStyle, "hiddenInset");
  assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");

  const solid = windowOptions("solid", "linux", PRELOAD);
  assert.equal(solid.backgroundColor, SOLID_BACKGROUND);
  assert.equal(solid.transparent, undefined);
});

test("önceki durum taşınır", () => {
  const o = windowOptions("clear", "win32", PRELOAD, { bounds: { x: 10, y: 20, width: 900, height: 600 }, maximized: true });
  assert.equal(o.x, 10);
  assert.equal(o.y, 20);
  assert.equal(o.width, 900);
  assert.equal(o.height, 600);
});

test("yalnız Acrylic ↔ Mica yerinde değişir", () => {
  assert.equal(needsRecreate("acrylic", "mica"), false);
  assert.equal(needsRecreate("mica", "acrylic"), false);
  assert.equal(needsRecreate("mica", "mica"), false);
  assert.equal(needsRecreate("clear", "acrylic"), true);
  assert.equal(needsRecreate("acrylic", "clear"), true);
  assert.equal(needsRecreate("solid", "mica"), true);
  assert.equal(needsRecreate("vibrancy", "solid"), true);
});
```

`desktop/test/ipc-validate.test.ts`:

```ts
/** Renderer güvenilmez kaynaktır: her IPC girdisi ana süreçte doğrulanır. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { MAX_COPY, requireCopyText, requireTarget, requireWindowAction, toUint } from "../src/main/ipc-validate.ts";

test("hedef: boş, boşluk ve 500 karakterden uzun reddedilir; kırpılır", () => {
  assert.throws(() => requireTarget(""), /target is empty/);
  assert.throws(() => requireTarget("   "), /target is empty/);
  assert.throws(() => requireTarget(42), /target is empty/);
  assert.throws(() => requireTarget("a".repeat(501)), /target is too long/);
  assert.equal(requireTarget("  kadran-client@host  "), "kadran-client@host");
});

test("toUint: negatif, sonsuz ve sayı olmayan → varsayılan; kesir aşağı", () => {
  assert.equal(toUint(-1, 7), 7);
  assert.equal(toUint(Number.POSITIVE_INFINITY, 7), 7);
  assert.equal(toUint("x", 7), 7);
  assert.equal(toUint("12", 7), 12);
  assert.equal(toUint(3.9, 7), 3);
});

test("pencere eylemi beyaz listede", () => {
  assert.equal(requireWindowAction("minimize"), "minimize");
  assert.equal(requireWindowAction("maximize"), "maximize");
  assert.equal(requireWindowAction("close"), "close");
  assert.throws(() => requireWindowAction("devtools"), /unknown window action/);
  assert.throws(() => requireWindowAction(null), /unknown window action/);
});

test("kopyalama metni 1…1024 karakter", () => {
  assert.equal(requireCopyText("SHA256:abc"), "SHA256:abc");
  assert.throws(() => requireCopyText(""), /1-1024/);
  assert.throws(() => requireCopyText("x".repeat(MAX_COPY + 1)), /1-1024/);
  assert.throws(() => requireCopyText({}), /1-1024/);
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `npm test`
Beklenen: FAIL; iki yeni dosyanın modülleri yok.

- [ ] **Adım 3: Saf modülleri yaz**

`desktop/src/main/window-options.ts`:

```ts
/**
 * Mod → BrowserWindow seçenekleri. SAF: Electron'u yalnız tip olarak
 * içe aktarır, böylece node --test ile ölçülür.
 *
 * Güvenlik ayarları (webPreferences) HER modda aynıdır. Üçü birlikte
 * renderer'ı Node'dan tamamen ayırır; Electron'un varsayılanları olsalar
 * da AÇIKÇA yazılıyorlar: varsayılana güvenmek bir sürüm yükseltmesinde
 * sessizce değişebilir.
 */

import type { BrowserWindowConstructorOptions, Rectangle } from "electron";

import type { WindowMode } from "../shared/appearance.ts";

export const SOLID_BACKGROUND = "#0e1016";
const CLEAR = "#00000000";

export interface WindowState {
  bounds?: Rectangle;
  maximized: boolean;
}

export function windowOptions(
  mode: WindowMode,
  platform: string,
  preloadPath: string,
  state?: WindowState,
): BrowserWindowConstructorOptions {
  const base: BrowserWindowConstructorOptions = {
    width: 1180,
    height: 780,
    minWidth: 760,
    minHeight: 520,
    title: "KΛDRΛN",
    show: false,
    ...(state?.bounds ?? {}),
    webPreferences: {
      preload: preloadPath,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webviewTag: false,
    },
  };
  // Başlık çubuğunu biz çiziyoruz; macOS'ta sistem trafik ışıkları kalır.
  const chrome: BrowserWindowConstructorOptions =
    platform === "darwin" ? { titleBarStyle: "hiddenInset" } : { frame: false };

  switch (mode) {
    case "clear":
      return { ...base, ...chrome, transparent: true, backgroundColor: CLEAR };
    case "acrylic":
    case "mica":
      return { ...base, ...chrome, backgroundColor: CLEAR, backgroundMaterial: mode };
    case "vibrancy":
      return { ...base, ...chrome, backgroundColor: CLEAR, vibrancy: "under-window", visualEffectState: "active" };
    case "solid":
      return { ...base, ...chrome, backgroundColor: SOLID_BACKGROUND };
  }
}

/** needsRecreate: yalnız Acrylic ↔ Mica (backgroundMaterial) yerinde değişebilir. */
export function needsRecreate(from: WindowMode, to: WindowMode): boolean {
  if (from === to) return false;
  const live = (m: WindowMode) => m === "acrylic" || m === "mica";
  return !(live(from) && live(to));
}
```

`desktop/src/main/ipc-validate.ts`:

```ts
/**
 * IPC girdilerinin doğrulanması. Renderer güvenilmez bir kaynak:
 * contextIsolation onu Node'dan ayırıyor ama gönderdiği veriyi
 * doğrulamıyor. Hata iletileri İngilizce: arayüze ulaşıyorlar (K-141).
 */

export const MAX_TARGET = 500;
export const MAX_COPY = 1024;
export const WINDOW_ACTIONS = ["minimize", "maximize", "close"] as const;
export type WindowAction = (typeof WINDOW_ACTIONS)[number];

export function requireTarget(value: unknown): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error("target is empty");
  if (value.length > MAX_TARGET) throw new Error("target is too long");
  return value.trim();
}

export function toUint(value: unknown, fallback: number): number {
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n) || n < 0) return fallback;
  return Math.floor(n);
}

export function requireWindowAction(value: unknown): WindowAction {
  if (typeof value === "string" && (WINDOW_ACTIONS as readonly string[]).includes(value)) {
    return value as WindowAction;
  }
  throw new Error("unknown window action");
}

export function requireCopyText(value: unknown): string {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_COPY) {
    throw new Error("copy text must be 1-1024 characters");
  }
  return value;
}
```

- [ ] **Adım 4: Saf testlerin geçtiğini gör**

Çalıştır: `node --test test/window-options.test.ts test/ipc-validate.test.ts`
Beklenen: PASS.

- [ ] **Adım 5: Kanallar, preload ve köprü tipleri**

`desktop/src/shared/channels.ts` içindeki `CHANNELS` nesnesi:

```ts
export const CHANNELS = {
  version: "kadran:version",
  status: "kadran:status",
  auditList: "kadran:audit-list",
  auditVerify: "kadran:audit-verify",
  loadProfiles: "kadran:profiles-load",
  saveProfiles: "kadran:profiles-save",
  loadAppearance: "kadran:appearance-load",
  saveAppearance: "kadran:appearance-save",
  windowControl: "kadran:window",
  copyText: "kadran:copy",
} as const;
```

`desktop/src/preload/index.cjs`:
- `CHANNELS` nesnesini aynı on girdiyle güncelle. `test/channels.test.ts` iki listeyi karşılaştırıyor.
- Dosya başındaki yorumda "Burada yalnızca altı işlem var ve hiçbiri sunucunun durumunu DEĞİŞTİRMİYOR." cümlesini şununla değiştir: "Burada sunucunun durumunu DEĞİŞTİREN hiçbir işlem yok; eklenenler (görünüm, pencere düğmeleri, panoya kopyalama) yalnız bu bilgisayarı etkiliyor."
- `exposeInMainWorld` nesnesine, `saveProfiles`'tan sonra şunları ekle:

```js
  /** Çalışılan işletim sistemi; başlık çubuğu macOS'ta trafik ışıklarına yer bırakır. */
  platform: process.platform,

  /** Görünüm ayarları, varsayılanlar ve bu sistemde sunulan modlar. */
  loadAppearance: () => ipcRenderer.invoke(CHANNELS.loadAppearance),

  /**
   * Görünümü kaydeder. Ana süreç doğrular; mod değiştiyse pencereyi
   * yeniden kurabilir (Clear ↔ diğerleri) ya da yerinde değiştirir.
   */
  saveAppearance: (appearance) => ipcRenderer.invoke(CHANNELS.saveAppearance, appearance),

  /** Özel başlık çubuğunun düğmeleri: minimize | maximize | close. */
  windowControl: (action) => ipcRenderer.invoke(CHANNELS.windowControl, action),

  /** Parmak izi gibi kısa bir metni panoya yazar (en fazla 1024 karakter). */
  copyText: (text) => ipcRenderer.invoke(CHANNELS.copyText, text),
```

`desktop/src/renderer/bridge.ts`:

```ts
/**
 * Preload'un açtığı köprünün tipleri. Köprü bir beyaz liste: arayüz
 * yalnız bunları çağırabilir (src/preload/index.cjs).
 */

import type { Appearance, WindowMode } from "../shared/appearance.ts";
import type { AuditListPayload, StatusPayload, VerifyPayload, VersionPayload } from "./lib/types.ts";

export interface Profile {
  name: string;
  target: string;
}

export interface AppearanceBundle {
  appearance: Appearance;
  defaults: Appearance;
  modes: WindowMode[];
}

export type WindowAction = "minimize" | "maximize" | "close";

export interface KadranBridge {
  readonly platform: string;
  version(): Promise<VersionPayload>;
  status(target: string): Promise<StatusPayload>;
  auditList(target: string, afterSeq: number, limit: number): Promise<AuditListPayload>;
  auditVerify(target: string): Promise<VerifyPayload>;
  loadProfiles(): Promise<Profile[]>;
  saveProfiles(profiles: Profile[]): Promise<Profile[]>;
  loadAppearance(): Promise<AppearanceBundle>;
  saveAppearance(appearance: Appearance): Promise<Appearance>;
  windowControl(action: WindowAction): Promise<void>;
  copyText(text: string): Promise<void>;
}

declare global {
  interface Window {
    readonly kadran: KadranBridge;
  }
}

export const bridge: KadranBridge = window.kadran;
```

- [ ] **Adım 6: Pencere sahibi ve duman testi modülleri**

`desktop/src/main/window.ts`:

```ts
/**
 * Pencereyi kurar ve görünüm modu değişince uygular.
 *
 * Saydamlık (Clear) pencere KURULURKEN verilir, sonradan açılıp
 * kapanamaz. Bu yüzden Clear ile diğerleri arasında yeni bir pencere
 * kurulur: konum, boyut ve büyütülmüş hâl taşınır, arayüz Settings
 * ekranında açılır (mod yalnız oradan değiştiriliyor). Acrylic ↔ Mica
 * yerinde değişir.
 *
 * Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa kullanıcının
 * tarayıcısında açılır; pencere hiçbir zaman uzak bir içeriğe dönüşemez.
 */

import { BrowserWindow, shell } from "electron";

import type { WindowMode } from "../shared/appearance.ts";
import { needsRecreate, windowOptions, type WindowState } from "./window-options.ts";

export interface WindowHostOptions {
  preloadPath: string;
  rendererIndex: string;
  platform: string;
  showOnReady: boolean;
}

export interface WindowHost {
  current(): BrowserWindow | null;
  open(mode: WindowMode, view?: string): BrowserWindow;
  applyMode(mode: WindowMode): void;
}

function lockNavigation(win: BrowserWindow): void {
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("https://")) void shell.openExternal(url);
    return { action: "deny" };
  });
  win.webContents.on("will-navigate", (event) => event.preventDefault());
}

export function createWindowHost(opts: WindowHostOptions): WindowHost {
  let win: BrowserWindow | null = null;
  let mode: WindowMode | null = null;

  function open(next: WindowMode, view?: string, state?: WindowState): BrowserWindow {
    const created = new BrowserWindow(windowOptions(next, opts.platform, opts.preloadPath, state));
    lockNavigation(created);
    created.once("ready-to-show", () => {
      if (state?.maximized) created.maximize();
      if (opts.showOnReady) created.show();
    });
    void created.loadFile(opts.rendererIndex, view ? { query: { view } } : undefined);
    created.on("closed", () => {
      if (win === created) {
        win = null;
        mode = null;
      }
    });
    win = created;
    mode = next;
    return created;
  }

  function applyMode(next: WindowMode): void {
    if (!win || !mode || next === mode) return;
    if (!needsRecreate(mode, next)) {
      win.setBackgroundMaterial(next === "mica" ? "mica" : "acrylic");
      mode = next;
      return;
    }
    const old = win;
    open(next, "settings", { bounds: old.getNormalBounds(), maximized: old.isMaximized() });
    old.destroy();
  }

  return { current: () => win, open: (m, view) => open(m, view), applyMode };
}
```

`desktop/src/main/smoke.ts`:

```ts
/**
 * Duman testi kipi (KADRAN_SMOKE_TEST=1).
 *
 * Pencereyi gösterme; yüklendiğini, köprünün yerinde olduğunu, Node'un
 * sızmadığını ve konsola hata düşmediğini doğrula; çık. Bir GUI'nin
 * "açılıyor mu" sorusu ancak çalıştırılarak yanıtlanır. Görev 17 bu
 * dosyaya sahte sidecar'a karşı bir kullanıcı yolculuğu ekler.
 *
 * Çıkış kodu 0 = geçti, 1 = kaldı.
 */

import { app, type BrowserWindow } from "electron";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const SMOKE_TEST = process.env.KADRAN_SMOKE_TEST === "1";

export const EXPECTED_BRIDGE = [
  "auditList",
  "auditVerify",
  "copyText",
  "loadAppearance",
  "loadProfiles",
  "platform",
  "saveAppearance",
  "saveProfiles",
  "status",
  "version",
  "windowControl",
];

/**
 * prepareSmokeUserData, testi kullanıcının gerçek verisinden ayırır:
 * geçici bir kullanıcı verisi dizini, iki sahte profil ve Solid mod
 * (CI'daki sanal ekranda saydamlık yok). `whenReady`'den ÖNCE çağrılmalı.
 */
export function prepareSmokeUserData(): string {
  const dir = mkdtempSync(join(tmpdir(), "kadran-smoke-"));
  writeFileSync(
    join(dir, "profiles.json"),
    JSON.stringify([
      { name: "fake", target: "fake@test" },
      { name: "down", target: "fake@down" },
    ]),
  );
  writeFileSync(join(dir, "appearance.json"), JSON.stringify({ mode: "solid" }));
  app.setPath("userData", dir);
  return dir;
}

export async function evaluate<T>(win: BrowserWindow, code: string): Promise<T> {
  return (await win.webContents.executeJavaScript(code, true)) as T;
}

export async function runSmokeTest(win: BrowserWindow): Promise<void> {
  const problems: string[] = [];

  win.webContents.on("console-message", (_event, level, message) => {
    // level 3 = error. CSP ihlalleri de buraya düşer.
    if (level >= 3) problems.push(`console error: ${message}`);
  });
  win.webContents.on("did-fail-load", (_event, code, description) => {
    problems.push(`page failed to load (${code}): ${description}`);
  });
  win.webContents.on("preload-error", (_event, path, error) => {
    problems.push(`preload error (${path}): ${error.message}`);
  });

  await new Promise<void>((done) => {
    win.webContents.once("did-finish-load", () => done());
    setTimeout(() => {
      problems.push("page did not load in 20 s");
      done();
    }, 20_000);
  });

  try {
    // Renderer'ın gördüğü yüzeyi renderer'a sorarak doğruluyoruz: ana
    // süreçten bakmak preload'un gerçekten çalıştığını kanıtlamazdı.
    const surface = await evaluate<string[]>(win, "Object.keys(window.kadran ?? {}).sort()");
    if (JSON.stringify(surface) !== JSON.stringify(EXPECTED_BRIDGE)) {
      problems.push(`bridge surface differs: ${JSON.stringify(surface)} != ${JSON.stringify(EXPECTED_BRIDGE)}`);
    }
    const rendered = await evaluate<boolean>(win, "Boolean(document.querySelector('[data-testid=\"app\"]'))");
    if (!rendered) problems.push("React root did not render");

    // Renderer Node'a ERİŞEMEMELİ; erişebiliyorsa ayrıcalık ayrımı
    // istemci tarafından delinmiş olur.
    const leaked = await evaluate<string>(win, "[typeof require, typeof process, typeof module].join(',')");
    if (leaked !== "undefined,undefined,undefined") problems.push(`Node leaked into the renderer: ${leaked}`);
  } catch (err) {
    problems.push(`could not query the bridge: ${(err as Error).message}`);
  }

  if (problems.length === 0) {
    console.log("duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor");
    app.exit(0);
    return;
  }
  for (const problem of problems) console.error("duman testi:", problem);
  app.exit(1);
}
```

Not: Bu dosyada `problems.push(…)` metinleri İngilizce. Bir `Error` değiller; Görev 5'in koruması onlara bakmıyor. CI günlüğünde okunuyorlar.

- [ ] **Adım 7: `index.ts`'i yeniden yaz**

`desktop/src/main/index.ts` (tamamı):

```ts
/**
 * Electron ana süreci.
 *
 * `kadran sidecar`'ı çocuk süreç olarak başlatır ve renderer'a yalnızca
 * dar bir yüzey açar (bkz. src/preload/index.cjs).
 *
 * # Güvenlik duruşu
 *
 * Sunucu tarafındaki ayrıcalık ayrımını istemci tarafından delmemek için:
 * contextIsolation açık, nodeIntegration kapalı, sandbox açık
 * (window-options.ts), uzak içerik yüklenmiyor, CSP ile satır içi script
 * yasak, gezinme ve yeni pencere engelli (window.ts) ve izinler kapalı;
 * yalnız yazı tipi listesine izin var (permissions.ts).
 */

import { app, BrowserWindow, clipboard, ipcMain, session } from "electron";
import { spawn } from "node:child_process";
import { release } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { defaultAppearance, modesFor, type PlatformInfo } from "../shared/appearance.ts";
import { CHANNELS } from "../shared/channels.ts";
import { SidecarClient } from "../shared/sidecar-client.ts";
import type { SidecarProcess } from "../shared/sidecar-client.ts";
import { loadAppearance, saveAppearance } from "./appearance.ts";
import { requireCopyText, requireTarget, requireWindowAction, toUint } from "./ipc-validate.ts";
import { lockDownPermissions } from "./permissions.ts";
import { loadProfiles, migrateLegacyProfiles, saveProfiles } from "./profiles.ts";
import { prepareSmokeUserData, runSmokeTest, SMOKE_TEST } from "./smoke.ts";
import { resolveSidecarCommand } from "./spawn-sidecar.ts";
import { createWindowHost, type WindowHost } from "./window.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const preloadPath = join(here, "..", "preload", "index.cjs");
const rendererIndex = join(here, "..", "renderer", "index.html");
const platformInfo: PlatformInfo = { platform: process.platform, release: release() };
const warn = (message: string): void => console.warn("[kadran]", message);

// Kullanıcı verisi dizini whenReady'den ÖNCE belirlenmeli.
if (SMOKE_TEST) prepareSmokeUserData();

let client: SidecarClient | null = null;
let host: WindowHost | null = null;

function sidecar(): SidecarClient {
  if (client) return client;
  const command = resolveSidecarCommand({ isPackaged: app.isPackaged, resourcesPath: process.resourcesPath, repoRoot });
  client = new SidecarClient(
    () =>
      spawn(command.command, command.args, {
        stdio: ["pipe", "pipe", "pipe"],
        // Kabuk YOK: argümanlar doğrudan geçiyor. shell:true olsaydı
        // yoldaki bir boşluk ya da özel karakter komut enjeksiyonuna
        // dönüşebilirdi.
        shell: false,
      }) as unknown as SidecarProcess,
    { onDiagnostic: (line) => console.warn("[sidecar]", line) },
  );
  return client;
}

function userData(): string {
  return app.getPath("userData");
}

// ── IPC işleyicileri ─────────────────────────────────────────────────
//
// Her biri renderer'dan gelen girdiyi doğrular (ipc-validate.ts).

function registerHandlers(): void {
  ipcMain.handle(CHANNELS.version, () => sidecar().call("version"));
  ipcMain.handle(CHANNELS.status, (_event, target: unknown) =>
    sidecar().call("status", { target: requireTarget(target) }),
  );
  ipcMain.handle(CHANNELS.auditList, (_event, target: unknown, afterSeq: unknown, limit: unknown) =>
    sidecar().call("audit.list", { target: requireTarget(target), after_seq: toUint(afterSeq, 0), limit: toUint(limit, 50) }),
  );
  ipcMain.handle(CHANNELS.auditVerify, (_event, target: unknown) =>
    sidecar().call("audit.verify", { target: requireTarget(target) }),
  );
  ipcMain.handle(CHANNELS.loadProfiles, () => loadProfiles(userData(), (msg) => console.warn("[profiller]", msg)));
  ipcMain.handle(CHANNELS.saveProfiles, (_event, profiles: unknown) => saveProfiles(userData(), profiles));
  ipcMain.handle(CHANNELS.loadAppearance, async () => ({
    appearance: await loadAppearance(userData(), platformInfo, warn),
    defaults: defaultAppearance(platformInfo),
    modes: [...modesFor(platformInfo)],
  }));
  ipcMain.handle(CHANNELS.saveAppearance, async (_event, raw: unknown) => {
    const saved = await saveAppearance(userData(), raw, platformInfo);
    host?.applyMode(saved.mode);
    return saved;
  });
  ipcMain.handle(CHANNELS.windowControl, (event, action: unknown) => {
    const what = requireWindowAction(action);
    const win = BrowserWindow.fromWebContents(event.sender);
    if (!win) return;
    if (what === "minimize") win.minimize();
    else if (what === "maximize") {
      if (win.isMaximized()) win.unmaximize();
      else win.maximize();
    } else win.close();
  });
  ipcMain.handle(CHANNELS.copyText, (_event, text: unknown) => {
    clipboard.writeText(requireCopyText(text));
  });
}

// ── Yaşam döngüsü ────────────────────────────────────────────────────

// Tek örnek: ikinci bir kopya aynı profil dosyasına yazmasın.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => {
    const win = host?.current();
    if (win) {
      if (win.isMinimized()) win.restore();
      win.focus();
    }
  });

  void app.whenReady().then(async () => {
    lockDownPermissions(session.defaultSession);

    // Eski adlı uygulamanın (panely-desktop) profilleri, pencere onları
    // okumadan ÖNCE taşınır (K-136). Hata uygulamayı durdurmaz.
    await migrateLegacyProfiles(userData(), join(app.getPath("appData"), "panely-desktop"), (msg) =>
      console.warn("[profiller]", msg),
    ).catch((err: unknown) => console.warn("[profiller]", (err as Error).message));

    registerHandlers();
    const appearance = await loadAppearance(userData(), platformInfo, warn);
    host = createWindowHost({ preloadPath, rendererIndex, platform: process.platform, showOnReady: !SMOKE_TEST });
    const win = host.open(appearance.mode);
    if (SMOKE_TEST) void runSmokeTest(win);

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) host?.open(appearance.mode);
    });
  });

  app.on("window-all-closed", () => {
    if (process.platform !== "darwin") app.quit();
  });

  // Sidecar alt süreci uygulamayla birlikte ölmeli; yoksa arkada açık
  // SSH bağlantıları taşıyan yetim süreçler birikir.
  app.on("before-quit", () => {
    client?.close();
    client = null;
  });
}
```

- [ ] **Adım 8: Hepsini koş**

```bash
npm run typecheck
npm test
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen:
- Tip hatası yok.
- Bütün testler PASS; `channels.test.ts` dahil.
- Duman testi `duman testi GEÇTİ` basar. Köprü yüzeyi on bir anahtarla eşleşir.

Dikkat: Duman testi `prepareSmokeUserData` ile Solid modda açılır, gerçek profiller kullanılmaz.

- [ ] **Adım 9: Elle bir bakış (kullanıcının makinesinde)**

`npm run dev`. Pencere çerçevesiz, varsayılan Acrylic açılmalı. Arayüz henüz yalnız `KΛDRΛN` yazısı. Başlık çubuğu Görev 13'te gelecek; o zamana kadar pencere `Alt+F4` ile kapanır.

- [ ] **Adım 10: Commit**

```bash
git add desktop/src/main desktop/src/shared/channels.ts desktop/src/preload/index.cjs desktop/src/renderer/bridge.ts desktop/test/window-options.test.ts desktop/test/ipc-validate.test.ts
git commit -m "feat(masaüstü): pencere modları, IPC doğrulama, genişleyen köprü"
```

---
### Görev 10: Yazı tipleri — sistem listesi, kalınlık eşlemesi, gömülü JetBrains Mono

**Dosyalar:**
- Yeni: `desktop/src/renderer/lib/fonts.ts` (saf), `desktop/src/renderer/lib/font-loader.ts` (DOM)
- Yeni (indirilir): `desktop/src/renderer/fonts/JetBrainsMono-Regular.woff2`, `JetBrainsMono-Medium.woff2`, `JetBrainsMono-Bold.woff2`, `OFL.txt`
- Yeni: `desktop/src/renderer/styles/fonts.css`
- Test: `desktop/test/fonts.test.ts`

**Arayüzler:**
- Üretir (`fonts.ts`):
  - `interface LocalFontLike { family: string; style: string; postscriptName: string }`
  - `interface Face { family: string; style: string; postscriptName: string; weight: number; italic: boolean }`
  - `parseStyle(style): { weight: number; italic: boolean }`
  - `groupFaces(list): Map<string, Face[]>`
  - `weightsOf(faces): number[]`
  - `nearestWeight(faces, wanted): number`
  - `strongerWeight(faces, base): number`
  - `cssLocalSource(postscriptName): string`
- Üretir (`font-loader.ts`):
  - `listLocalFonts(): Promise<LocalFontLike[]>`: hata ya da izin yoksa `[]`
  - `registerFamily(alias, faces): Promise<number>`: yüklenen yüz sayısı
- Üretir (`fonts.css`): `"JetBrains Mono Bundled"` ailesi; 400, 500 ve 700.

- [ ] **Adım 1: Testi yaz**

`desktop/test/fonts.test.ts`:

```ts
/**
 * Yazı tipi listesi. Seçilen ailenin her yüzü kendi kalınlığıyla
 * kaydedilir; böylece "Medium" gerçekten Medium çizilir. Liste boşsa
 * (izin yok, API yok) her şey varsayılanla çalışmaya devam eder.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { cssLocalSource, groupFaces, nearestWeight, parseStyle, strongerWeight, weightsOf } from "../src/renderer/lib/fonts.ts";

test("stil adı → kalınlık ve italik", () => {
  assert.deepEqual(parseStyle("Medium"), { weight: 500, italic: false });
  assert.deepEqual(parseStyle("Thin Italic"), { weight: 100, italic: true });
  assert.deepEqual(parseStyle("SemiBold"), { weight: 600, italic: false });
  assert.deepEqual(parseStyle("Extra-Light"), { weight: 200, italic: false });
  assert.deepEqual(parseStyle("Italic"), { weight: 400, italic: true });
  assert.deepEqual(parseStyle("Regular"), { weight: 400, italic: false });
  assert.deepEqual(parseStyle("Garip Stil"), { weight: 400, italic: false });
});

const LIST = [
  { family: "JetBrainsMono Nerd Font", style: "Medium", postscriptName: "JetBrainsMonoNF-Medium" },
  { family: "Segoe UI", style: "Regular", postscriptName: "SegoeUI" },
  { family: "Segoe UI", style: "Bold", postscriptName: "SegoeUI-Bold" },
  { family: "Segoe UI", style: "Light Italic", postscriptName: "SegoeUI-LightItalic" },
  { family: "Segoe UI", style: "Semibold", postscriptName: "SegoeUI-Semibold" },
];

test("aileye göre gruplar", () => {
  const map = groupFaces(LIST);
  assert.deepEqual([...map.keys()].sort(), ["JetBrainsMono Nerd Font", "Segoe UI"]);
  assert.equal(map.get("Segoe UI")!.length, 4);
  assert.equal(map.get("JetBrainsMono Nerd Font")![0]!.weight, 500);
});

test("kalınlıklar: italik hariç, tekil, artan; boşsa 400", () => {
  const map = groupFaces(LIST);
  assert.deepEqual(weightsOf(map.get("Segoe UI")!), [400, 600, 700]);
  assert.deepEqual(weightsOf(map.get("JetBrainsMono Nerd Font")!), [500]);
  assert.deepEqual(weightsOf([]), [400]);
});

test("en yakın kalınlık ve bir üstü", () => {
  const segoe = groupFaces(LIST).get("Segoe UI")!;
  const jb = groupFaces(LIST).get("JetBrainsMono Nerd Font")!;
  assert.equal(nearestWeight(jb, 400), 500);
  assert.equal(nearestWeight(segoe, 650), 600);
  assert.equal(nearestWeight([], 450), 450);
  assert.equal(strongerWeight(segoe, 400), 600);
  assert.equal(strongerWeight(segoe, 700), 700);
  assert.equal(strongerWeight(jb, 500), 500);
});

test("boş liste güvenli", () => {
  assert.equal(groupFaces([]).size, 0);
});

test("local() kaynağı tırnak ve ters bölüyü kaçırır", () => {
  assert.equal(cssLocalSource("JetBrainsMonoNF-Medium"), 'local("JetBrainsMonoNF-Medium")');
  assert.equal(cssLocalSource('Kötü"Ad\\'), 'local("Kötü\\"Ad\\\\")');
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/fonts.test.ts`
Beklenen: FAIL, modül yok.

- [ ] **Adım 3: Uygulamayı yaz**

`desktop/src/renderer/lib/fonts.ts`:

```ts
/**
 * Kurulu yazı tipleri. queryLocalFonts() her YÜZÜ ayrı verir (aile, stil,
 * PostScript adı). Seçilen ailenin her yüzü kendi kalınlığıyla
 * kaydedilir; sahte kalınlaştırma kapalı (font-synthesis: none). Böylece
 * JetBrainsMono Nerd Font yalnız Medium olarak kuruluysa Medium çizilir.
 */

export interface LocalFontLike {
  family: string;
  style: string;
  postscriptName: string;
}

export interface Face extends LocalFontLike {
  weight: number;
  italic: boolean;
}

const WEIGHTS: Readonly<Record<string, number>> = {
  thin: 100,
  hairline: 100,
  extralight: 200,
  ultralight: 200,
  light: 300,
  semilight: 350,
  regular: 400,
  normal: 400,
  book: 400,
  retina: 450,
  medium: 500,
  semibold: 600,
  demibold: 600,
  bold: 700,
  extrabold: 800,
  ultrabold: 800,
  black: 900,
  heavy: 900,
};

export function parseStyle(style: string): { weight: number; italic: boolean } {
  const s = style.toLowerCase().replace(/[\s_-]/g, "");
  const italic = /italic|oblique/.test(s);
  const core = s.replace(/italic|oblique/g, "") || "regular";
  return { weight: WEIGHTS[core] ?? 400, italic };
}

export function groupFaces(list: readonly LocalFontLike[]): Map<string, Face[]> {
  const map = new Map<string, Face[]>();
  for (const f of list) {
    const face: Face = { ...f, ...parseStyle(f.style) };
    map.set(f.family, [...(map.get(f.family) ?? []), face]);
  }
  return map;
}

/** weightsOf, ailedeki italik olmayan kalınlıklar; aile boşsa [400]. */
export function weightsOf(faces: readonly Face[]): number[] {
  const set = new Set(faces.filter((f) => !f.italic).map((f) => f.weight));
  return set.size === 0 ? [400] : [...set].sort((a, b) => a - b);
}

export function nearestWeight(faces: readonly Face[], wanted: number): number {
  if (faces.length === 0) return wanted;
  return weightsOf(faces).reduce((best, w) => (Math.abs(w - wanted) < Math.abs(best - wanted) ? w : best));
}

/** strongerWeight, başlıklar için bir üst kalınlık; yoksa aynısı. */
export function strongerWeight(faces: readonly Face[], base: number): number {
  return weightsOf(faces).find((w) => w > base) ?? base;
}

export function cssLocalSource(postscriptName: string): string {
  return `local("${postscriptName.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}")`;
}
```

`desktop/src/renderer/lib/font-loader.ts`:

```ts
/**
 * Tarayıcı tarafı: kurulu yazı tiplerini listeler ve bir aileyi bir takma
 * adla (kadran-ui / kadran-code) kaydeder.
 *
 * Ölçüldü (Electron 43): izin reddedilirse queryLocalFonts() hata değil
 * BOŞ liste döner. İkisi de "listelenemedi" demek; arayüz bunu gösterir.
 */

import { cssLocalSource, type Face, type LocalFontLike } from "./fonts.ts";

type QueryLocalFonts = () => Promise<LocalFontLike[]>;

export async function listLocalFonts(): Promise<LocalFontLike[]> {
  const query = (window as unknown as { queryLocalFonts?: QueryLocalFonts }).queryLocalFonts;
  if (typeof query !== "function") return [];
  try {
    return await query();
  } catch {
    return [];
  }
}

const registered = new Map<string, FontFace[]>();

/** registerFamily, eski kaydı siler, yüzleri yükler; yüklenen yüz sayısını verir. */
export async function registerFamily(alias: string, faces: readonly Face[]): Promise<number> {
  for (const old of registered.get(alias) ?? []) document.fonts.delete(old);
  const added: FontFace[] = [];
  for (const face of faces) {
    const ff = new FontFace(alias, cssLocalSource(face.postscriptName), {
      weight: String(face.weight),
      style: face.italic ? "italic" : "normal",
    });
    try {
      await ff.load();
      document.fonts.add(ff);
      added.push(ff);
    } catch {
      // Yüklenemeyen yüz atlanır; ailenin diğer yüzleri yeterli olabilir.
    }
  }
  registered.set(alias, added);
  return added.length;
}
```

- [ ] **Adım 4: JetBrains Mono'yu indir ve bağla**

Depo kökünden:

```bash
mkdir -p desktop/src/renderer/fonts
for w in Regular Medium Bold; do
  curl -fsSL -o "desktop/src/renderer/fonts/JetBrainsMono-$w.woff2" \
    "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/master/fonts/webfonts/JetBrainsMono-$w.woff2"
done
curl -fsSL -o desktop/src/renderer/fonts/OFL.txt https://raw.githubusercontent.com/JetBrains/JetBrainsMono/master/OFL.txt
ls -la desktop/src/renderer/fonts
head -3 desktop/src/renderer/fonts/OFL.txt
```

Beklenen: üç woff2 dosyası (her biri 80–120 KB arası) ve OFL.txt. OFL.txt'nin ilk satırları "Copyright 2020 The JetBrains Mono Project Authors" ve "SIL OPEN FONT LICENSE Version 1.1" içerir. Değilse dur.

`desktop/src/renderer/styles/fonts.css`:

```css
/*
 * Gömülü JetBrains Mono (SIL OFL 1.1, fonts/OFL.txt). Kod yazı tipinin
 * varsayılanı; kimsenin bilgisayarında bozuk görünmesin diye uygulamayla
 * gelir. CSP font-src 'self' bu dosyalara izin verir.
 */
@font-face {
  font-family: "JetBrains Mono Bundled";
  src: url("../fonts/JetBrainsMono-Regular.woff2") format("woff2");
  font-weight: 400;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: "JetBrains Mono Bundled";
  src: url("../fonts/JetBrainsMono-Medium.woff2") format("woff2");
  font-weight: 500;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: "JetBrains Mono Bundled";
  src: url("../fonts/JetBrainsMono-Bold.woff2") format("woff2");
  font-weight: 700;
  font-style: normal;
  font-display: swap;
}
```

`desktop/src/renderer/main.tsx`'e, `import "./styles/base.css";`'in altına `import "./styles/fonts.css";` ekle.

- [ ] **Adım 5: Testleri ve derlemeyi koş**

```bash
npm test
npm run typecheck
npm run build
npm run test:built
```

Beklenen: PASS. `out/renderer/assets/` altında üç `.woff2` görünür; derlenmiş HTML testi CSS bağlantısının göreli olduğunu doğrular.

- [ ] **Adım 6: Commit**

```bash
git add desktop/src/renderer/lib/fonts.ts desktop/src/renderer/lib/font-loader.ts desktop/src/renderer/fonts desktop/src/renderer/styles/fonts.css desktop/src/renderer/main.tsx desktop/test/fonts.test.ts
git commit -m "feat(masaüstü): sistem yazı tipleri ve gömülü JetBrains Mono (OFL)"
```

---

### Görev 11: İkonlar — lucide-animated'den sabit commit'le kopyalama

**Dosyalar:**
- Yeni: `desktop/scripts/vendor-icons.mjs`
- Yeni (üretilir): `desktop/src/renderer/icons/{activity,shield-check,settings,refresh-cw,server,copy,check,chevron-down,history,badge-alert}.tsx`, `index.ts`, `LICENSE`
- Test: `desktop/test/icons.test.ts`

**Arayüzler:**
- Üretir:
  - `src/renderer/icons/index.ts` şu bileşenleri dışa aktarır: `ActivityIcon`, `ShieldCheckIcon`, `SettingsIcon`, `RefreshCwIcon`, `ServerIcon`, `CopyIcon`, `CheckIcon`, `ChevronDownIcon`, `HistoryIcon`, `BadgeAlertIcon`.
  - Her ikon `forwardRef<Handle, { size?: number; className?: string; … }>`. `Handle` tipi `{ startAnimation(): void; stopAnimation(): void }`.

Kaynak sabit: `pqoqubbw/icons` commit `072c38b1b04ea738d90a084485ccaad4b890ddca` (MIT). Çizimler Lucide'den (ISC). Bu sette `info`, `trash-2`, `database`, `circle-alert` ve `hard-drive` YOK; hata göstergesi için `badge-alert` kullanılır.

- [ ] **Adım 1: Testi yaz**

`desktop/test/icons.test.ts`:

```ts
/**
 * Kopyalanan ikonlar: lisans başlığı var, shadcn'in "@/lib/utils" içe
 * aktarması kalmamış, "use client" kalmamış, motion/react kullanıyor;
 * LICENSE iki lisansı da (MIT + ISC) taşıyor.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const iconsDir = join(here, "..", "src", "renderer", "icons");
const EXPECTED = ["activity", "badge-alert", "check", "chevron-down", "copy", "history", "refresh-cw", "server", "settings", "shield-check"];

test("beklenen ikonlar kopyalanmış", async () => {
  const files = (await readdir(iconsDir)).filter((f) => f.endsWith(".tsx")).map((f) => f.slice(0, -4)).sort();
  assert.deepEqual(files, EXPECTED);
});

test("her ikon temizlenmiş ve lisans başlığı taşıyor", async () => {
  for (const name of EXPECTED) {
    const src = await readFile(join(iconsDir, `${name}.tsx`), "utf8");
    assert.match(src, /pqoqubbw\/icons@072c38b1b04ea738d90a084485ccaad4b890ddca/, name);
    assert.match(src, /MIT/, name);
    assert.doesNotMatch(src, /@\/lib\/utils/, name);
    assert.doesNotMatch(src, /"use client"/, name);
    assert.doesNotMatch(src, /\bReact\.MouseEvent/, name);
    assert.match(src, /from "motion\/react"/, name);
  }
});

test("LICENSE MIT ve ISC metinlerini taşıyor", async () => {
  const lic = await readFile(join(iconsDir, "LICENSE"), "utf8");
  assert.match(lic, /MIT License/);
  assert.match(lic, /pqoqubbw/);
  assert.match(lic, /ISC License/);
  assert.match(lic, /Lucide/);
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/icons.test.ts`
Beklenen: FAIL, `icons` dizini yok.

- [ ] **Adım 3: Kopyalama betiğini yaz**

`desktop/scripts/vendor-icons.mjs`:

```js
/**
 * lucide-animated ikonlarını (pqoqubbw/icons, MIT; çizimler Lucide, ISC)
 * SABİT bir commit'ten kopyalar ve projeye uyarlar:
 *
 *   - "use client" ve shadcn'in `cn` içe aktarması kalkar (`cn` yerine
 *     düz className);
 *   - `React.MouseEvent` → içe aktarılan `MouseEvent` tipi;
 *   - başa kaynak ve lisans başlığı eklenir.
 *
 * Yeniden çalıştırmak aynı çıktıyı verir. İkon eklemek için NAMES'e ekle,
 * betiği çalıştır, test/icons.test.ts'teki EXPECTED'i güncelle.
 */

import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const COMMIT = "072c38b1b04ea738d90a084485ccaad4b890ddca";
const NAMES = ["activity", "shield-check", "settings", "refresh-cw", "server", "copy", "check", "chevron-down", "history", "badge-alert"];
const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "src", "renderer", "icons");

async function fetchText(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.text();
}

function adapt(name, src) {
  let s = src;
  s = s.replace(/^"use client";\s*\n/m, "");
  s = s.replace(/^import \{ cn \} from "@\/lib\/utils";\s*\n/m, "");
  s = s.replace(/className=\{cn\(className\)\}/g, "className={className}");
  if (/\bReact\.MouseEvent\b/.test(s)) {
    s = s.replace(/\bReact\.MouseEvent\b/g, "MouseEvent");
    s = s.replace(/^(import type \{ HTMLAttributes) \} from "react";/m, '$1, MouseEvent } from "react";');
  }
  if (/@\/lib\/utils|\bcn\(/.test(s)) throw new Error(`${name}: cn kaldırılamadı`);
  if (/\bReact\./.test(s)) throw new Error(`${name}: React. başvurusu kaldı`);
  const header =
    `// Kaynak: pqoqubbw/icons@${COMMIT} icons/${name}.tsx (MIT).\n` +
    `// Çizim Lucide'den (ISC). Lisanslar: ./LICENSE. Uyarlama: scripts/vendor-icons.mjs\n\n`;
  return header + s;
}

function exportName(src) {
  const m = src.match(/export \{ (\w+Icon) \};/);
  if (!m) throw new Error("dışa aktarım bulunamadı");
  return m[1];
}

await mkdir(outDir, { recursive: true });
const exports = [];
for (const name of NAMES) {
  const src = await fetchText(`https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/icons/${name}.tsx`);
  const adapted = adapt(name, src);
  await writeFile(join(outDir, `${name}.tsx`), adapted, "utf8");
  exports.push(`export { ${exportName(adapted)} } from "./${name}.tsx";`);
}
await writeFile(join(outDir, "index.ts"), exports.join("\n") + "\n", "utf8");

const mit = await fetchText(`https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/LICENSE`);
const isc = await fetchText("https://raw.githubusercontent.com/lucide-icons/lucide/main/LICENSE");
await writeFile(
  join(outDir, "LICENSE"),
  `Animasyonlu ikonlar: pqoqubbw/icons (${COMMIT})\n\n${mit.trim()}\n\n---\n\nÇizimler: Lucide\n\n${isc.trim()}\n`,
  "utf8",
);
console.log(`${NAMES.length} ikon kopyalandı → ${outDir}`);
```

- [ ] **Adım 4: Çalıştır ve gözden geçir**

```bash
node scripts/vendor-icons.mjs
git diff --stat
```

`src/renderer/icons/activity.tsx`'i aç ve kontrol et:
- Başta iki satırlık kaynak başlığı var.
- `"use client"` yok.
- `cn` yok.
- `<div className={className} …>`.

Bir ikon `HTMLAttributes` dışında başka bir `react` içe aktarması kullanıyor ve `React.` başvurusu kalıyorsa betik hata verip durur. Durursa uyarlamayı o ikona göre genişlet ve raporla.

- [ ] **Adım 5: Testleri ve tip denetimini koş**

```bash
npm test
npm run typecheck
```

Beklenen: PASS. `motion` 13.5.1 `useAnimation`'ı dışa aktarıyor; tip hatası çıkarsa sürümü değiştirme, raporla.

- [ ] **Adım 6: Commit**

```bash
git add desktop/scripts/vendor-icons.mjs desktop/src/renderer/icons desktop/test/icons.test.ts
git commit -m "feat(masaüstü): lucide-animated ikonları sabit commit'ten (MIT/ISC)"
```

---
### Görev 12: Görünümün arayüze uygulanması — stiller, sağlayıcılar, bildirim

**Dosyalar:**
- Yeni: `desktop/src/renderer/lib/apply-appearance.ts` (saf)
- Yeni: `desktop/src/renderer/appearance/AppearanceProvider.tsx`, `desktop/src/renderer/i18n/I18nProvider.tsx`, `desktop/src/renderer/toast/ToastProvider.tsx`, `desktop/src/renderer/toast/Toast.tsx`
- Yeni: `desktop/src/renderer/styles/tokens.css`, `glass.css`, `controls.css`, `focus.css`
- Değişir: `desktop/src/renderer/styles/base.css`, `desktop/src/renderer/main.tsx`
- Test: `desktop/test/apply-appearance.test.ts`

**Arayüzler:**
- Tüketir:
  - `Appearance` (Görev 6)
  - `bridge`, `AppearanceBundle` (Görev 9)
  - `groupFaces`, `strongerWeight`, `Face` (Görev 10)
  - `listLocalFonts`, `registerFamily` (Görev 10)
  - `translate`, `Params` (Görev 4)
  - `errorText` (Görev 3)
- Üretir:
  - `apply-appearance.ts`:
    - `ACCENT_RGB`, `UI_FALLBACK`, `CODE_FALLBACK`
    - `fontStack(alias: string | null, fallback: string): string`
    - `interface FontStacks { ui: string; code: string; strong: number }`
    - `appearanceVars(a, fonts): Record<string, string>`
    - `interface StyleTarget { style: { setProperty(name, value): void }; dataset: DOMStringMap | Record<string, string | undefined> }`
    - `applyAppearance(target: StyleTarget, a, fonts): void`
  - `AppearanceProvider({ initial: AppearanceBundle, children })`. `useAppearance()` şunu döner: `{ appearance, defaults, modes, families, fontsListed, update(patch), reset() }`.
  - `I18nProvider({ language, children })`, `useT(): Translate`, `useLanguage(): Language`. `type Translate = (key: string, params?: Params) => string`.
  - `ToastProvider({ children })`, `useToast(): (key: string, params?: Params) => void`, `useToastState()`, `Toast` bileşeni (`data-testid="toast"`).
  - CSS değişkenleri: `--tint --panel --edge --glow --acc --font-ui --font-mono --w-ui --w-strong --w-mono`. Kökte `data-mode` ve `data-tone` öznitelikleri.

- [ ] **Adım 1: Saf testi yaz**

`desktop/test/apply-appearance.test.ts`:

```ts
/** Görünüm → CSS değişkenleri ve kök öznitelikleri. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { ACCENT_RGB, applyAppearance, appearanceVars, CODE_FALLBACK, fontStack, UI_FALLBACK } from "../src/renderer/lib/apply-appearance.ts";
import { defaultAppearance } from "../src/shared/appearance.ts";

const A = { ...defaultAppearance({ platform: "win32", release: "10.0.26200" }), tint: 48, panel: 6, edge: 8, glow: 75, accent: "amber" as const, tone: "light" as const };
const STACKS = { ui: fontStack("kadran-ui", UI_FALLBACK), code: CODE_FALLBACK, strong: 700 };

test("kaydırıcılar 0–1 aralığına, vurgu RGB üçlüsüne", () => {
  const v = appearanceVars(A, STACKS);
  assert.equal(v["--tint"], "0.48");
  assert.equal(v["--panel"], "0.06");
  assert.equal(v["--edge"], "0.08");
  assert.equal(v["--glow"], "0.75");
  assert.equal(v["--acc"], ACCENT_RGB.amber);
  assert.equal(v["--w-strong"], "700");
  assert.equal(v["--font-ui"], `"kadran-ui", ${UI_FALLBACK}`);
  assert.equal(v["--font-mono"], CODE_FALLBACK);
});

test("fontStack: takma ad yoksa yedek yığın", () => {
  assert.equal(fontStack(null, UI_FALLBACK), UI_FALLBACK);
  assert.match(CODE_FALLBACK, /^"JetBrains Mono Bundled"/);
});

test("applyAppearance değişkenleri ve öznitelikleri yazar", () => {
  const props = new Map<string, string>();
  const target = { style: { setProperty: (k: string, v: string) => props.set(k, v) }, dataset: {} as Record<string, string | undefined> };
  applyAppearance(target, A, STACKS);
  assert.equal(props.get("--tint"), "0.48");
  assert.equal(target.dataset.mode, "acrylic");
  assert.equal(target.dataset.tone, "light");
});
```

- [ ] **Adım 2: Düştüğünü gör**

Çalıştır: `node --test test/apply-appearance.test.ts`
Beklenen: FAIL, modül yok.

- [ ] **Adım 3: Saf modülü yaz**

`desktop/src/renderer/lib/apply-appearance.ts`:

```ts
/**
 * Görünüm ayarlarını CSS değişkenlerine ve kök özniteliklerine çevirir.
 * DOM'a doğrudan bağlı değil: hedef yalnız style.setProperty ve dataset
 * taşıyan bir nesne, böylece node --test ile ölçülür.
 */

import type { Accent, Appearance } from "../../shared/appearance.ts";

export const ACCENT_RGB: Readonly<Record<Accent, string>> = {
  ice: "125, 211, 252",
  amber: "255, 176, 32",
  phosphor: "126, 224, 138",
  paper: "236, 235, 230",
};

export const UI_FALLBACK = '"Segoe UI Variable Text", "Segoe UI", system-ui, sans-serif';
export const CODE_FALLBACK = '"JetBrains Mono Bundled", ui-monospace, Consolas, monospace';

export function fontStack(alias: string | null, fallback: string): string {
  return alias ? `"${alias}", ${fallback}` : fallback;
}

export interface FontStacks {
  ui: string;
  code: string;
  strong: number;
}

export function appearanceVars(a: Appearance, fonts: FontStacks): Record<string, string> {
  return {
    "--tint": String(a.tint / 100),
    "--panel": String(a.panel / 100),
    "--edge": String(a.edge / 100),
    "--glow": String(a.glow / 100),
    "--acc": ACCENT_RGB[a.accent],
    "--font-ui": fonts.ui,
    "--font-mono": fonts.code,
    "--w-ui": String(a.uiFont.weight),
    "--w-strong": String(fonts.strong),
    "--w-mono": String(a.codeFont.weight),
  };
}

export interface StyleTarget {
  style: { setProperty(name: string, value: string): void };
  dataset: Record<string, string | undefined>;
}

export function applyAppearance(target: StyleTarget, a: Appearance, fonts: FontStacks): void {
  for (const [name, value] of Object.entries(appearanceVars(a, fonts))) target.style.setProperty(name, value);
  target.dataset.mode = a.mode;
  target.dataset.tone = a.tone;
}
```

Not: `--w-ui` kullanıcının seçtiği kalınlıktır. Sistem yazı tipinde (`family: ""`) de 400'dür.

- [ ] **Adım 4: Testi koş**

Çalıştır: `node --test test/apply-appearance.test.ts`
Beklenen: PASS.

- [ ] **Adım 5: Sağlayıcıları yaz**

`desktop/src/renderer/toast/ToastProvider.tsx`:

```tsx
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

import type { Params } from "../lib/i18n.ts";

/**
 * Bildirimler çeviri ANAHTARIYLA tutulur, metinle değil: bildirimi
 * gönderen kod (ör. görünüm kaydı) dil sağlayıcısının dışında olabilir.
 * Metne Toast bileşeni çevirir.
 */
export interface ToastMessage {
  id: number;
  key: string;
  params: Params;
}

type Show = (key: string, params?: Params) => void;

interface ToastContext {
  message: ToastMessage | null;
  show: Show;
  dismiss: () => void;
}

const Ctx = createContext<ToastContext | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState<ToastMessage | null>(null);
  const show = useCallback<Show>((key, params = {}) => setMessage({ id: Date.now(), key, params }), []);
  const dismiss = useCallback(() => setMessage(null), []);
  const value = useMemo(() => ({ message, show, dismiss }), [message, show, dismiss]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

function useCtx(): ToastContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("ToastProvider missing");
  return ctx;
}

export function useToast(): Show {
  return useCtx().show;
}

export function useToastState(): Pick<ToastContext, "message" | "dismiss"> {
  const { message, dismiss } = useCtx();
  return { message, dismiss };
}
```

`desktop/src/renderer/toast/Toast.tsx`:

```tsx
import { useEffect } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { useToastState } from "./ToastProvider.tsx";

const TOAST_MS = 9_000;

/**
 * Bildirim. Sunucudan gelen hata metni olduğu gibi gösterilir; her zaman
 * düz metin (React kaçırarak yazar). Uzun metin satır kırar (controls.css).
 */
export function Toast() {
  const t = useT();
  const { message, dismiss } = useToastState();

  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(dismiss, TOAST_MS);
    return () => clearTimeout(timer);
  }, [message, dismiss]);

  if (!message) return null;
  return (
    <div className="toast glass" role="status" aria-live="polite" data-testid="toast">
      <p className="toast-text">{t(message.key, message.params)}</p>
      <button type="button" className="toast-close" aria-label={t("toast.dismiss")} onClick={dismiss}>
        ×
      </button>
    </div>
  );
}
```

`desktop/src/renderer/i18n/I18nProvider.tsx`:

```tsx
import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";

import type { Language } from "../../shared/appearance.ts";
import { translate, type Dict, type Params } from "../lib/i18n.ts";
import en from "./en.json";
import tr from "./tr.json";

const DICTS: Readonly<Record<Language, Dict>> = { en, tr };

export type Translate = (key: string, params?: Params) => string;

interface I18nValue {
  t: Translate;
  language: Language;
}

const Ctx = createContext<I18nValue>({ t: (key) => key, language: "en" });

export function I18nProvider({ language, children }: { language: Language; children: ReactNode }) {
  const t = useCallback<Translate>((key, params) => translate(DICTS[language], en, key, params), [language]);
  const value = useMemo(() => ({ t, language }), [t, language]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useT(): Translate {
  return useContext(Ctx).t;
}

export function useLanguage(): Language {
  return useContext(Ctx).language;
}
```

`desktop/src/renderer/appearance/AppearanceProvider.tsx`:

```tsx
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { Appearance, WindowMode } from "../../shared/appearance.ts";
import { bridge, type AppearanceBundle } from "../bridge.ts";
import { applyAppearance, CODE_FALLBACK, fontStack, UI_FALLBACK, type FontStacks } from "../lib/apply-appearance.ts";
import { errorText } from "../lib/errors.ts";
import { listLocalFonts, registerFamily } from "../lib/font-loader.ts";
import { groupFaces, strongerWeight, type Face } from "../lib/fonts.ts";
import { useToast } from "../toast/ToastProvider.tsx";

/** Kaydırıcı sürüklenirken her adımda diske yazılmasın. */
const SAVE_DELAY_MS = 250;

export interface AppearanceApi {
  appearance: Appearance;
  defaults: Appearance;
  modes: readonly WindowMode[];
  families: ReadonlyMap<string, Face[]>;
  fontsListed: boolean;
  update: (patch: Partial<Appearance>) => void;
  reset: () => void;
}

const Ctx = createContext<AppearanceApi | null>(null);

export function AppearanceProvider({ initial, children }: { initial: AppearanceBundle; children: ReactNode }) {
  const toast = useToast();
  const [appearance, setAppearance] = useState(initial.appearance);
  const [families, setFamilies] = useState<ReadonlyMap<string, Face[]>>(new Map());
  const [fontsListed, setFontsListed] = useState(false);
  const [stacks, setStacks] = useState<FontStacks>({ ui: UI_FALLBACK, code: CODE_FALLBACK, strong: 600 });
  const latest = useRef(initial.appearance);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    void listLocalFonts().then((list) => {
      setFamilies(groupFaces(list));
      setFontsListed(list.length > 0);
    });
  }, []);

  const uiFamily = appearance.uiFont.family;
  const uiWeight = appearance.uiFont.weight;
  const codeFamily = appearance.codeFont.family;
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const uiFaces = families.get(uiFamily) ?? [];
      const codeFaces = families.get(codeFamily) ?? [];
      const uiOk = uiFaces.length > 0 && (await registerFamily("kadran-ui", uiFaces)) > 0;
      const codeOk = codeFaces.length > 0 && (await registerFamily("kadran-code", codeFaces)) > 0;
      if (cancelled) return;
      setStacks({
        ui: fontStack(uiOk ? "kadran-ui" : null, UI_FALLBACK),
        code: fontStack(codeOk ? "kadran-code" : null, CODE_FALLBACK),
        strong: uiOk ? strongerWeight(uiFaces, uiWeight) : 600,
      });
    })();
    return () => {
      cancelled = true;
    };
  }, [families, uiFamily, uiWeight, codeFamily]);

  useEffect(() => {
    applyAppearance(document.documentElement, appearance, stacks);
    document.documentElement.lang = appearance.language;
  }, [appearance, stacks]);

  const save = useCallback(
    (next: Appearance) => {
      void bridge.saveAppearance(next).catch((err: unknown) => toast("toast.save_failed", { error: errorText(err) }));
    },
    [toast],
  );

  const update = useCallback(
    (patch: Partial<Appearance>) => {
      const prev = latest.current;
      const next: Appearance = { ...prev, ...patch };
      latest.current = next;
      setAppearance(next);
      if (timer.current) clearTimeout(timer.current);
      // Mod değişimi pencereyi yeniden kurabilir: bekletmeden kaydet.
      if (patch.mode !== undefined && patch.mode !== prev.mode) save(next);
      else timer.current = setTimeout(() => save(next), SAVE_DELAY_MS);
    },
    [save],
  );

  const reset = useCallback(() => update({ ...initial.defaults }), [update, initial.defaults]);

  const value = useMemo<AppearanceApi>(
    () => ({ appearance, defaults: initial.defaults, modes: initial.modes, families, fontsListed, update, reset }),
    [appearance, initial.defaults, initial.modes, families, fontsListed, update, reset],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppearance(): AppearanceApi {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("AppearanceProvider missing");
  return ctx;
}
```

`desktop/src/renderer/main.tsx` (tamamen yeniden):

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App.tsx";
import { AppearanceProvider, useAppearance } from "./appearance/AppearanceProvider.tsx";
import { bridge } from "./bridge.ts";
import { I18nProvider } from "./i18n/I18nProvider.tsx";
import { Toast } from "./toast/Toast.tsx";
import { ToastProvider } from "./toast/ToastProvider.tsx";
import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/fonts.css";
import "./styles/glass.css";
import "./styles/controls.css";
import "./styles/focus.css";

/** Dil görünüm ayarlarından geliyor; sağlayıcı onun içinde. */
function Localized() {
  const { appearance } = useAppearance();
  return (
    <I18nProvider language={appearance.language}>
      <App />
      <Toast />
    </I18nProvider>
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("root element missing");

// Görünüm ilk çizimden ÖNCE okunur: yanlış renkte bir kare yanıp sönmesin.
const bundle = await bridge.loadAppearance();

createRoot(root).render(
  <StrictMode>
    <ToastProvider>
      <AppearanceProvider initial={bundle}>
        <Localized />
      </AppearanceProvider>
    </ToastProvider>
  </StrictMode>,
);
```

- [ ] **Adım 6: Stilleri yaz**

`desktop/src/renderer/styles/tokens.css`:

```css
/* Tasarım belirteçleri. Kaydırıcıların değerlerini AppearanceProvider yazar. */
:root {
  --tint: 0.6;
  --panel: 0.07;
  --edge: 0.22;
  --glow: 0.6;
  --acc: 125, 211, 252;
  --ring: var(--acc);
  --accent: rgb(var(--acc));
  --tone: 14, 16, 22;
  --glass: 255, 255, 255;
  --fg: #f2f4f8;
  --fg-dim: rgba(242, 244, 248, 0.62);
  --fg-faint: rgba(242, 244, 248, 0.4);
  --line: rgba(255, 255, 255, 0.08);
  --ok: #4ade80;
  --warn: #fbbf24;
  --bad: #f87171;
  --muted: rgba(242, 244, 248, 0.45);
  --radius: 18px;
  --radius-sm: 10px;
  --font-ui: "Segoe UI Variable Text", "Segoe UI", system-ui, sans-serif;
  --font-mono: "JetBrains Mono Bundled", ui-monospace, Consolas, monospace;
  --w-ui: 400;
  --w-strong: 600;
  --w-mono: 400;
}

:root[data-tone="light"] {
  --tone: 246, 247, 250;
  --fg: #11141a;
  --fg-dim: rgba(17, 20, 26, 0.62);
  --fg-faint: rgba(17, 20, 26, 0.42);
  --line: rgba(17, 20, 26, 0.1);
  --ok: #15803d;
  --warn: #b45309;
  --bad: #b91c1c;
  --muted: rgba(17, 20, 26, 0.45);
}
```

`desktop/src/renderer/styles/base.css` (tamamen yeniden):

```css
* { box-sizing: border-box; }

html,
body {
  margin: 0;
  height: 100%;
  background: transparent;
  color: var(--fg);
  overflow: hidden;
  font-family: var(--font-ui);
  font-weight: var(--w-ui);
  font-synthesis: none;
  -webkit-font-smoothing: antialiased;
}

#root { height: 100%; }

.mono {
  font-family: var(--font-mono);
  font-weight: var(--w-mono);
  font-variant-numeric: tabular-nums;
}

/* Pencerenin kendisi: köşeler yuvarlak, renk tonu kaydırıcıdan. */
.app {
  position: relative;
  height: 100%;
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  overflow: hidden;
  background: rgba(var(--tone), var(--tint));
  border: 1px solid rgba(255, 255, 255, calc(var(--edge) * 0.6));
}
:root[data-mode="acrylic"] .app,
:root[data-mode="mica"] .app,
:root[data-mode="vibrancy"] .app { border-radius: 0; border: 0; }
:root[data-mode="solid"] .app { border-radius: 0; border: 0; background: rgb(var(--tone)); }

.is-ok { color: var(--ok); }
.is-warn { color: var(--warn); }
.is-bad { color: var(--bad); }
.is-muted { color: var(--muted); }
```

`desktop/src/renderer/styles/glass.css`:

```css
/* Cam kart: gradyanlı dolgu, ince parlak kenar, üst kenar ışığı. */
.glass {
  background: linear-gradient(
    160deg,
    rgba(var(--glass), calc(var(--panel) * 1.6)) 0%,
    rgba(var(--glass), var(--panel)) 45%,
    rgba(var(--glass), calc(var(--panel) * 0.5)) 100%
  );
  border: 1px solid rgba(255, 255, 255, var(--edge));
  border-radius: var(--radius);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, calc(var(--edge) * 1.4)),
    inset 0 -1px 0 rgba(255, 255, 255, calc(var(--edge) * 0.25)),
    0 10px 30px rgba(0, 0, 0, 0.18);
}
:root[data-tone="light"] .glass {
  background: rgba(255, 255, 255, calc(var(--panel) * 4));
  border-color: rgba(0, 0, 0, calc(var(--edge) * 0.4));
}
```

`desktop/src/renderer/styles/controls.css`:

```css
.label { margin: 6px 0 2px; font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-faint); }
.hint, .empty { margin: 6px 0; font-size: 12px; color: var(--fg-faint); line-height: 1.45; }

.field {
  flex: 1; min-width: 0; padding: 8px 10px; border-radius: var(--radius-sm); font: inherit; font-size: 13px; color: var(--fg);
  background: rgba(0, 0, 0, 0.22); border: 1px solid rgba(255, 255, 255, 0.12); outline: none;
}
:root[data-tone="light"] .field { background: rgba(255, 255, 255, 0.55); border-color: rgba(0, 0, 0, 0.12); }

.btn { font: inherit; font-size: 13px; border-radius: var(--radius-sm); padding: 7px 12px; cursor: pointer; color: var(--fg); }
.btn:disabled { opacity: 0.5; cursor: default; }
.btn-primary {
  border: 1px solid rgba(255, 255, 255, 0.3);
  background: linear-gradient(180deg, rgba(var(--acc), 0.55), rgba(var(--acc), 0.3));
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.45), 0 4px 14px rgba(var(--acc), 0.25);
}
.btn-ghost { border: 1px solid rgba(255, 255, 255, var(--edge)); background: rgba(255, 255, 255, 0.06); }

.sel {
  min-width: 0; font: inherit; font-size: 12.5px; color: var(--fg); padding: 6px 8px; border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.14); background: rgba(0, 0, 0, 0.3);
}
:root[data-tone="light"] .sel { background: rgba(255, 255, 255, 0.7); border-color: rgba(0, 0, 0, 0.12); }
.sel option { background: #1b1e25; color: #f2f4f8; }

.seg { display: flex; gap: 4px; }
.seg button {
  flex: 1; font: inherit; font-size: 12px; padding: 6px 10px; border-radius: 8px; cursor: pointer; color: var(--fg-dim);
  border: 1px solid rgba(255, 255, 255, 0.12); background: transparent;
}
.seg button.is-on { color: var(--fg); background: rgba(var(--acc), 0.25); border-color: rgba(var(--acc), 0.6); }

.pill {
  display: inline-flex; align-items: center; gap: 7px; padding: 3px 10px; border-radius: 99px; font-size: 12px;
  border: 1px solid currentColor; background: rgba(255, 255, 255, 0.04);
}
.pill-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; box-shadow: 0 0 8px currentColor; }

.badge {
  font-size: 11px; font-weight: var(--w-strong); letter-spacing: 0.08em; padding: 3px 9px; border-radius: 99px;
  border: 1px solid currentColor; background: rgba(255, 255, 255, 0.05);
}

/* Uzun sunucu hatası da tam görünmeli ve satır kırmalı. */
.toast {
  position: absolute; right: 18px; bottom: 18px; max-width: min(560px, calc(100% - 36px)); padding: 12px 14px;
  display: flex; align-items: flex-start; gap: 12px; z-index: 10; background: rgba(var(--tone), 0.92);
}
.toast-text { margin: 0; font-size: 13px; line-height: 1.45; overflow-wrap: anywhere; white-space: pre-wrap; }
.toast-close { border: 0; background: transparent; color: var(--fg-dim); font-size: 18px; line-height: 1; cursor: pointer; }
```

`desktop/src/renderer/styles/focus.css`:

```css
/* Üstüne gelme ve odak: belirgin; şiddet --glow'dan. Klavye halkası
   --glow 0'da bile görünür (erişilebilirlik). */
.btn, .server, .nav-item, .seg button, .sel, .field, .crumb {
  transition: transform 0.16s ease, box-shadow 0.22s ease, background-color 0.22s ease, border-color 0.22s ease,
    color 0.22s ease, filter 0.22s ease;
}

.btn:not(:disabled):hover {
  transform: translateY(-1px);
  filter: brightness(calc(1 + var(--glow) * 0.3));
  border-color: rgba(255, 255, 255, calc(0.25 + var(--glow) * 0.5));
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.5),
    0 0 0 1px rgba(var(--ring), calc(var(--glow) * 0.6)),
    0 6px calc(10px + var(--glow) * 22px) rgba(var(--ring), calc(var(--glow) * 0.55));
}
.btn:not(:disabled):active { transform: translateY(0) scale(0.97); filter: brightness(1); }

.server:hover, .nav-item:hover, .seg button:hover, .crumb:hover {
  color: var(--fg);
  background: rgba(255, 255, 255, calc(0.06 + var(--glow) * 0.1));
  border-color: rgba(255, 255, 255, calc(0.1 + var(--glow) * 0.3));
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, calc(var(--glow) * 0.3)),
    0 0 calc(var(--glow) * 18px) rgba(var(--ring), calc(var(--glow) * 0.3));
}
.server:hover, .nav-item:hover { transform: translateX(calc(var(--glow) * 3px)); }

.btn:focus-visible, .server:focus-visible, .nav-item:focus-visible, .seg button:focus-visible, .sel:focus-visible,
.crumb:focus-visible, .tb-btn:focus-visible, .toast-close:focus-visible, .fp:focus-visible {
  outline: none;
  box-shadow:
    0 0 0 2px rgba(var(--ring), 0.95),
    0 0 0 5px rgba(var(--ring), calc(0.15 + var(--glow) * 0.25)),
    0 0 calc(8px + var(--glow) * 22px) rgba(var(--ring), calc(var(--glow) * 0.6));
}
.field:focus {
  border-color: rgba(var(--ring), 0.95);
  box-shadow: 0 0 0 3px rgba(var(--ring), calc(0.15 + var(--glow) * 0.25)), 0 0 calc(var(--glow) * 20px) rgba(var(--ring), calc(var(--glow) * 0.45));
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { transition: none !important; animation: none !important; }
}
```

- [ ] **Adım 7: Derle ve duman testi**

```bash
npm run typecheck
npm test
npm run build
npm run test:built
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen: hepsi PASS; duman testi GEÇTİ.

`main.tsx` artık köprüyü bekliyor (`await bridge.loadAppearance()`). Duman testi bunun çalıştığını, yani kökün çizildiğini doğrular. Bir CSP ya da konsol hatası çıkarsa raporla.

- [ ] **Adım 8: Commit**

```bash
git add desktop/src/renderer desktop/test/apply-appearance.test.ts
git commit -m "feat(masaüstü): görünüm sağlayıcısı, dil ve bildirim, cam stilleri"
```

---
### Görev 13: Kabuk — başlık çubuğu, üst çubuk, sol panel, bağlantı durumu

**Dosyalar:**
- Yeni: `desktop/src/renderer/hooks/useServer.ts`
- Yeni: `desktop/src/renderer/components/{Titlebar,TopBar,ServerSwitcher,Rail,NavButton,EmptyState}.tsx`
- Yeni: `desktop/src/renderer/styles/layout.css`
- Değişir: `desktop/src/renderer/App.tsx` (tamamen yeniden), `desktop/src/renderer/main.tsx` (`import "./styles/layout.css";`)

**Arayüzler:**
- Tüketir:
  - `bridge`, `Profile` (Görev 9)
  - `useT`, `useLanguage` (Görev 12)
  - `useToast` (Görev 12)
  - `healthOf`, `healthTone`, `healthKey` (Görev 3)
  - `pushSample`, `memoryUsedGiB`, `errorText` (Görev 3)
  - ikonlar (Görev 11)
- Üretir:
  - `useServer()` şunu döner: `{ state: ServerState, connect(target): Promise<boolean>, refresh(): Promise<void>, verifyChains(): Promise<void> }`. `ServerState` alanları: `target`, `status`, `verify`, `rtt`, `memorySamples`, `rttSamples`, `connecting`, `verifying`.
  - `type View = "status" | "audit" | "settings"`. `App`'te durur; `?view=` sorgusuyla başlar.
  - `NavButton({ icon, label, active, onClick, testId })`, `interface AnimatedIconHandle`, `type AnimatedIcon`
  - `EmptyState({ title, body })`
  - `data-testid` değerleri:
    - `app`
    - `server-<target>`
    - `nav-status`, `nav-audit`, `nav-settings`
    - `target-input`, `connect`
    - `switcher`
    - `health`, `rtt`, `verify`
    - `rail-version`

- [ ] **Adım 1: Bağlantı kancasını yaz**

`desktop/src/renderer/hooks/useServer.ts`:

```ts
/**
 * Bağlı sunucunun durumu. Hedef değişince eski istekler yok sayılır:
 * yavaş bir sunucunun geç gelen cevabı yeni seçilen sunucunun üstüne
 * yazmamalı.
 */

import { useCallback, useMemo, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { memoryUsedGiB } from "../lib/format.ts";
import { pushSample } from "../lib/samples.ts";
import type { StatusPayload, VerifyPayload } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export interface ServerState {
  target: string | null;
  status: StatusPayload | null;
  verify: VerifyPayload | null;
  /** Son status çağrısının gidiş-dönüş süresi (ms); ssh süresi değil. */
  rtt: number | null;
  memorySamples: readonly number[];
  rttSamples: readonly number[];
  connecting: boolean;
  verifying: boolean;
}

const EMPTY: ServerState = {
  target: null,
  status: null,
  verify: null,
  rtt: null,
  memorySamples: [],
  rttSamples: [],
  connecting: false,
  verifying: false,
};

export function useServer() {
  const toast = useToast();
  const [state, setState] = useState<ServerState>(EMPTY);
  const current = useRef<string | null>(null);

  const loadStatus = useCallback(
    async (target: string): Promise<boolean> => {
      const started = performance.now();
      try {
        const status = await bridge.status(target);
        if (current.current !== target) return false;
        const rtt = Math.round(performance.now() - started);
        setState((s) => ({
          ...s,
          status,
          rtt,
          memorySamples: pushSample(s.memorySamples, memoryUsedGiB(status.host)),
          rttSamples: pushSample(s.rttSamples, rtt),
        }));
        return true;
      } catch (err) {
        if (current.current === target) toast("toast.connect_failed", { error: errorText(err) });
        return false;
      }
    },
    [toast],
  );

  const verifyChains = useCallback(async () => {
    const target = current.current;
    if (!target) return;
    setState((s) => ({ ...s, verifying: true }));
    try {
      const verify = await bridge.auditVerify(target);
      if (current.current === target) setState((s) => ({ ...s, verify }));
    } catch (err) {
      toast("toast.verify_failed", { error: errorText(err) });
    } finally {
      setState((s) => ({ ...s, verifying: false }));
    }
  }, [toast]);

  const connect = useCallback(
    async (target: string): Promise<boolean> => {
      current.current = target;
      setState({ ...EMPTY, target, connecting: true });
      const ok = await loadStatus(target);
      if (current.current !== target) return false;
      if (!ok) {
        current.current = null;
        setState(EMPTY);
        return false;
      }
      setState((s) => ({ ...s, connecting: false }));
      await verifyChains();
      return true;
    },
    [loadStatus, verifyChains],
  );

  const refresh = useCallback(async () => {
    const target = current.current;
    if (target) await loadStatus(target);
  }, [loadStatus]);

  return useMemo(() => ({ state, connect, refresh, verifyChains }), [state, connect, refresh, verifyChains]);
}

export type ServerApi = ReturnType<typeof useServer>;
```

- [ ] **Adım 2: Kabuk bileşenlerini yaz**

`desktop/src/renderer/components/NavButton.tsx`:

```tsx
import { useRef, type ComponentType, type RefAttributes } from "react";

export interface AnimatedIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

export type AnimatedIcon = ComponentType<{ size?: number; className?: string } & RefAttributes<AnimatedIconHandle>>;

interface NavButtonProps {
  icon: AnimatedIcon;
  label: string;
  active: boolean;
  onClick: () => void;
  testId: string;
}

/** Menü düğmesi: düğmenin tamamına gelince (ya da odaklanınca) ikon hareket eder. */
export function NavButton({ icon: Icon, label, active, onClick, testId }: NavButtonProps) {
  const ref = useRef<AnimatedIconHandle>(null);
  return (
    <button
      type="button"
      className={active ? "nav-item is-active" : "nav-item"}
      aria-current={active ? "page" : undefined}
      data-testid={testId}
      onClick={onClick}
      onMouseEnter={() => ref.current?.startAnimation()}
      onMouseLeave={() => ref.current?.stopAnimation()}
      onFocus={() => ref.current?.startAnimation()}
      onBlur={() => ref.current?.stopAnimation()}
    >
      <Icon ref={ref} size={18} className="nav-icon" />
      <span>{label}</span>
    </button>
  );
}
```

`desktop/src/renderer/components/EmptyState.tsx`:

```tsx
export function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="empty-state">
      <p className="empty-title">{title}</p>
      <p className="empty-body">{body}</p>
    </div>
  );
}
```

`desktop/src/renderer/components/Titlebar.tsx`:

```tsx
import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";

/**
 * Pencerenin başlık çubuğu. Sürükleme alanı CSS'te (-webkit-app-region).
 * macOS'ta sistem trafik ışıkları kalır; düğmeleri biz çizmeyiz. Logo
 * yeri (.mark) tasarımcının logosu gelene kadar boş.
 */
export function Titlebar() {
  const t = useT();
  const mac = bridge.platform === "darwin";
  return (
    <header className={mac ? "titlebar is-mac" : "titlebar"}>
      <div className="tb-brand" aria-label={t("brand.aria")}>
        <span className="mark" aria-hidden="true" />
        <span className="wordmark">KΛDRΛN</span>
      </div>
      <div className="tb-drag" />
      {mac ? null : (
        <div className="tb-controls">
          <button type="button" className="tb-btn" aria-label={t("window.minimize")} onClick={() => void bridge.windowControl("minimize")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2 6h8" /></svg>
          </button>
          <button type="button" className="tb-btn" aria-label={t("window.maximize")} onClick={() => void bridge.windowControl("maximize")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><rect x="2.5" y="2.5" width="7" height="7" rx="1.5" /></svg>
          </button>
          <button type="button" className="tb-btn tb-close" aria-label={t("window.close")} onClick={() => void bridge.windowControl("close")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M3 3l6 6M9 3l-6 6" /></svg>
          </button>
        </div>
      )}
    </header>
  );
}
```

`desktop/src/renderer/components/ServerSwitcher.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { ChevronDownIcon } from "../icons/index.ts";
import type { AnimatedIconHandle } from "./NavButton.tsx";

interface Props {
  target: string | null;
  profiles: readonly Profile[];
  onSwitch: (target: string) => void;
}

/** Kırıntı yolundaki sunucu seçici (Coolify düzeni): `Servers / hetzner ▾`. */
export function ServerSwitcher({ target, profiles, onSwitch }: Props) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const icon = useRef<AnimatedIconHandle>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  return (
    <div className="switcher" ref={box}>
      <button
        type="button"
        className="crumb crumb-current mono"
        aria-haspopup="listbox"
        aria-expanded={open}
        data-testid="switcher"
        onClick={() => setOpen((o) => !o)}
        onMouseEnter={() => icon.current?.startAnimation()}
        onMouseLeave={() => icon.current?.stopAnimation()}
      >
        {target ?? t("topbar.not_connected")}
        <ChevronDownIcon ref={icon} size={14} />
      </button>
      {open ? (
        <ul className="switcher-menu glass" role="listbox">
          {profiles.length === 0 ? (
            <li className="empty">{t("rail.no_saved")}</li>
          ) : (
            profiles.map((p) => (
              <li key={p.target}>
                <button
                  type="button"
                  role="option"
                  aria-selected={p.target === target}
                  className="server"
                  onClick={() => {
                    setOpen(false);
                    onSwitch(p.target);
                  }}
                >
                  {p.name}
                </button>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
```

`desktop/src/renderer/components/TopBar.tsx`:

```tsx
import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { healthKey, healthTone, type Health } from "../lib/health.ts";
import { ServerSwitcher } from "./ServerSwitcher.tsx";

interface Props {
  target: string | null;
  profiles: readonly Profile[];
  health: Health;
  rtt: number | null;
  verifying: boolean;
  onVerify: () => void;
  onSwitch: (target: string) => void;
}

/** Üst çubuk (Coolify düzeni): kırıntı yolu + sağlık hapı | gecikme + birincil eylem. */
export function TopBar({ target, profiles, health, rtt, verifying, onVerify, onSwitch }: Props) {
  const t = useT();
  return (
    <header className="topbar">
      <nav className="crumbs" aria-label={t("topbar.crumbs")}>
        <span className="crumb-static">{t("topbar.servers")}</span>
        <span className="crumb-sep" aria-hidden="true">/</span>
        <ServerSwitcher target={target} profiles={profiles} onSwitch={onSwitch} />
        {target ? (
          <span className={`pill is-${healthTone(health)}`} data-testid="health">
            <i className="pill-dot" aria-hidden="true" />
            {t(healthKey(health))}
          </span>
        ) : null}
      </nav>
      <div className="topbar-right">
        {rtt !== null ? (
          <span className="pill is-muted mono" title={t("topbar.rtt_title")} data-testid="rtt">
            {t("topbar.rtt", { ms: rtt })}
          </span>
        ) : null}
        <button type="button" className="btn btn-primary" disabled={!target || verifying} onClick={onVerify} data-testid="verify">
          {verifying ? t("topbar.verifying") : t("topbar.verify")}
        </button>
      </div>
    </header>
  );
}
```

`desktop/src/renderer/components/Rail.tsx`:

```tsx
import { useState, type FormEvent } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { ActivityIcon, SettingsIcon, ShieldCheckIcon } from "../icons/index.ts";
import type { Tone } from "../lib/chain.ts";
import type { VersionPayload } from "../lib/types.ts";
import { NavButton } from "./NavButton.tsx";

export type View = "status" | "audit" | "settings";

interface Props {
  profiles: readonly Profile[];
  currentTarget: string | null;
  currentTone: Tone;
  view: View;
  connecting: boolean;
  version: VersionPayload | null;
  onConnect: (target: string) => void;
  onNavigate: (view: View) => void;
}

export function Rail({ profiles, currentTarget, currentTone, view, connecting, version, onConnect, onNavigate }: Props) {
  const t = useT();
  const [target, setTarget] = useState("");

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const value = target.trim();
    if (value) onConnect(value);
  };

  return (
    <aside className="rail glass">
      <form className="connect" onSubmit={submit}>
        <label className="label" htmlFor="target">{t("rail.target_label")}</label>
        <div className="input-row">
          <input
            id="target"
            className="field mono"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder={t("rail.target_placeholder")}
            spellCheck={false}
            autoComplete="off"
            data-testid="target-input"
          />
          <button className="btn btn-primary" type="submit" disabled={connecting || target.trim() === ""} data-testid="connect">
            {connecting ? t("rail.connecting") : t("rail.connect")}
          </button>
        </div>
        <p className="hint">{t("rail.hint")}</p>
      </form>

      <p className="label">{t("rail.saved")}</p>
      {profiles.length === 0 ? (
        <p className="empty">{t("rail.no_saved")}</p>
      ) : (
        <ul className="servers">
          {profiles.map((p) => {
            const active = p.target === currentTarget;
            return (
              <li key={p.target}>
                <button
                  type="button"
                  className={active ? "server is-active" : "server"}
                  title={p.target}
                  data-testid={`server-${p.target}`}
                  onClick={() => {
                    setTarget(p.target);
                    onConnect(p.target);
                  }}
                >
                  <span className={active ? `dot is-${currentTone}` : "dot"} aria-hidden="true" />
                  <span className="server-name">{p.name}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <nav className="nav" aria-label={t("nav.label")}>
        <NavButton icon={ActivityIcon} label={t("nav.status")} active={view === "status"} onClick={() => onNavigate("status")} testId="nav-status" />
        <NavButton icon={ShieldCheckIcon} label={t("nav.audit")} active={view === "audit"} onClick={() => onNavigate("audit")} testId="nav-audit" />
        <NavButton icon={SettingsIcon} label={t("nav.settings")} active={view === "settings"} onClick={() => onNavigate("settings")} testId="nav-settings" />
      </nav>

      <p className="foot mono" data-testid="rail-version">
        {t("rail.version", { version: version ? `v${version.version}` : "…" })}
      </p>
    </aside>
  );
}
```

`desktop/src/renderer/App.tsx` (tamamen yeniden; ekranlar Görev 14–16'da gelene kadar yer tutucu `EmptyState`):

```tsx
import { useCallback, useEffect, useState } from "react";

import { bridge, type Profile } from "./bridge.ts";
import { EmptyState } from "./components/EmptyState.tsx";
import { Rail, type View } from "./components/Rail.tsx";
import { Titlebar } from "./components/Titlebar.tsx";
import { TopBar } from "./components/TopBar.tsx";
import { useServer } from "./hooks/useServer.ts";
import { useT } from "./i18n/I18nProvider.tsx";
import { errorText } from "./lib/errors.ts";
import { healthOf, healthTone } from "./lib/health.ts";
import type { VersionPayload } from "./lib/types.ts";
import { useToast } from "./toast/ToastProvider.tsx";

function initialView(): View {
  const v = new URLSearchParams(window.location.search).get("view");
  return v === "audit" || v === "settings" ? v : "status";
}

export function App() {
  const t = useT();
  const toast = useToast();
  const server = useServer();
  const [view, setView] = useState<View>(initialView);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [version, setVersion] = useState<VersionPayload | null>(null);

  useEffect(() => {
    bridge.loadProfiles().then(setProfiles, (err: unknown) => toast("toast.profiles_failed", { error: errorText(err) }));
    bridge.version().then(setVersion, (err: unknown) => toast("toast.sidecar_failed", { error: errorText(err) }));
  }, [toast]);

  const connect = useCallback(
    async (target: string) => {
      const ok = await server.connect(target);
      if (!ok || profiles.some((p) => p.target === target)) return;
      try {
        setProfiles(await bridge.saveProfiles([...profiles, { name: target, target }]));
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      }
    },
    [server, profiles, toast],
  );

  const health = healthOf(server.state.status, server.state.verify);

  return (
    <div className="app" data-testid="app">
      <Titlebar />
      <div className="body">
        <Rail
          profiles={profiles}
          currentTarget={server.state.target}
          currentTone={healthTone(health)}
          view={view}
          connecting={server.state.connecting}
          version={version}
          onConnect={(target) => void connect(target)}
          onNavigate={setView}
        />
        <main className="stage">
          <TopBar
            target={server.state.target}
            profiles={profiles}
            health={health}
            rtt={server.state.rtt}
            verifying={server.state.verifying}
            onVerify={() => void server.verifyChains()}
            onSwitch={(target) => void connect(target)}
          />
          <EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />
        </main>
      </div>
    </div>
  );
}
```

`desktop/src/renderer/styles/layout.css`:

```css
/* Başlık çubuğu */
.titlebar { height: 40px; display: flex; align-items: center; padding-left: 16px; -webkit-app-region: drag; flex: none; }
.titlebar.is-mac { padding-left: 80px; }
.tb-brand { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.mark { width: 14px; height: 14px; border-radius: 4px; border: 1.5px dashed var(--fg-faint); }
.wordmark { font-weight: var(--w-strong); letter-spacing: 0.14em; }
.tb-drag { flex: 1; }
.tb-controls { display: flex; height: 100%; -webkit-app-region: no-drag; }
.tb-btn { width: 46px; height: 100%; border: 0; background: transparent; color: var(--fg); display: grid; place-items: center; }
.tb-btn svg { width: 12px; height: 12px; fill: none; stroke: currentColor; stroke-width: 1.1; }
.tb-btn:hover { background: rgba(255, 255, 255, calc(0.08 + var(--glow) * 0.1)); }
.tb-close:hover { background: #c42b1c; color: #fff; }

/* Düzen */
.body { flex: 1; display: flex; gap: 14px; padding: 4px 14px 14px; min-height: 0; }
.stage { flex: 1; overflow: auto; display: flex; flex-direction: column; gap: 14px; padding: 2px 4px 4px; min-width: 0; }

/* Sol panel */
.rail { width: 260px; flex: none; padding: 16px; display: flex; flex-direction: column; gap: 8px; }
.input-row { display: flex; gap: 6px; }
.servers { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.server {
  width: 100%; display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: var(--radius-sm);
  border: 1px solid transparent; background: transparent; color: var(--fg); font: inherit; font-size: 13px; text-align: left; cursor: pointer;
}
.server.is-active { background: rgba(255, 255, 255, 0.1); border-color: rgba(255, 255, 255, var(--edge)); }
.server-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: var(--fg-faint); flex: none; }
.dot.is-ok { background: var(--ok); box-shadow: 0 0 8px var(--ok); }
.dot.is-warn { background: var(--warn); box-shadow: 0 0 8px var(--warn); }
.dot.is-bad { background: var(--bad); box-shadow: 0 0 8px var(--bad); }
.nav { margin-top: 8px; display: flex; flex-direction: column; gap: 2px; }
.nav-item {
  display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: var(--radius-sm); font: inherit; font-size: 13px;
  color: var(--fg-dim); cursor: pointer; border: 1px solid transparent; background: transparent; text-align: left;
}
.nav-item.is-active { color: var(--fg); background: rgba(255, 255, 255, 0.1); }
.nav-item.is-active .nav-icon { color: var(--accent); }
.nav-icon { display: grid; place-items: center; }
.foot { margin-top: auto; font-size: 11px; color: var(--fg-faint); }

/* Üst çubuk (Coolify düzeni) */
.topbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 6px 4px 2px; }
.crumbs { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 13px; }
.crumb-static { color: var(--fg-dim); }
.crumb-sep { color: var(--fg-faint); }
.crumb {
  display: inline-flex; align-items: center; gap: 6px; padding: 4px 8px; border-radius: 8px; font: inherit; font-size: 13px;
  color: var(--fg); background: transparent; border: 1px solid transparent; cursor: pointer;
}
.topbar-right { display: flex; align-items: center; gap: 10px; }
.switcher { position: relative; }
.switcher-menu {
  position: absolute; top: calc(100% + 6px); left: 0; z-index: 20; min-width: 220px; margin: 0; padding: 6px; list-style: none;
  background: rgba(var(--tone), 0.95);
}

/* Boş durum */
.empty-state { margin: auto; max-width: 420px; text-align: center; padding: 40px 20px; }
.empty-title { margin: 0 0 8px; font-size: 18px; font-weight: var(--w-strong); }
.empty-body { margin: 0; font-size: 13px; color: var(--fg-dim); line-height: 1.5; }
```

- [ ] **Adım 3: Testler, derleme, duman testi**

```bash
npm run typecheck
npm test
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen:
- Hepsi PASS.
- `no-raw-text` yeni `.tsx` dosyalarında çeviri dışı metin bulmaz; yalnız `KΛDRΛN` ve `/` var, ikisi de izinli.
- i18n testindeki "koddaki her `t("…")` anahtarı" kontrolü de geçer.

- [ ] **Adım 4: Elle bakış**

`npm run dev`. Bakılacaklar:
- Başlık çubuğunda KΛDRΛN ve pencere düğmeleri var; sürükleme çalışıyor.
- Sol panelde hedef kutusu var; menüdeki ikonlar fareyle üstüne gelince hareket ediyor.
- Üst çubukta `Servers / Not connected` yazıyor.
- Var olan bir sunucu profiline tıklayınca sağlık hapı ve gecikme görünüyor. Ekranın gövdesi Görev 14'e kadar boş.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/renderer
git commit -m "feat(masaüstü): kabuk — başlık çubuğu, Coolify düzeninde üst çubuk, sol panel"
```

---
### Görev 14: Status ekranı — Overview şeridi, kartlar, zincirler, son kayıtlar

**Dosyalar:**
- Yeni: `desktop/src/renderer/hooks/useAudit.ts`
- Yeni: `desktop/src/renderer/components/{Sparkline,Tile,InfoCard,ChainCard,AuditTable}.tsx`
- Yeni: `desktop/src/renderer/screens/StatusScreen.tsx`
- Yeni: `desktop/src/renderer/styles/screens.css`
- Değişir: `desktop/src/renderer/App.tsx` (status görünümü), `desktop/src/renderer/main.tsx` (`import "./styles/screens.css";`)

**Arayüzler:**
- Tüketir:
  - `useServer` / `ServerApi`, `View` (Görev 13)
  - `newestWindow`, `olderWindow`, `mergeNewestFirst`, `AUDIT_PAGE` (Görev 3)
  - `chainView`, `outcomeView`, `worstTone`, `Tone` (Görev 3)
  - `humanDuration`, `memoryText`, `describeActor`, `formatTime`, `formatCount` (Görev 3)
  - `sparklinePath` (Görev 3)
- Üretir:
  - `useAudit(target: string | null, size: number)`. Döndürdükleri: `{ records, loading, latest, hasOlder, loadNewest(), loadOlder() }`.
  - Bileşenler:
    - `AuditTable({ records, testId })`. Her satır `data-testid="audit-row"` ve `data-seq` taşır.
    - `ChainCard({ name, status, checked, detail, testId })`. Rozet `.badge.is-<ton>`.
    - `Tile`, `InfoCard`, `Sparkline`
  - `data-testid` değerleri: `screen-status`, `chain-daemon`, `chain-executor`, `audit-preview`, `see-all`, `live`, `tile-chains`.

- [ ] **Adım 1: Audit kancasını yaz**

`desktop/src/renderer/hooks/useAudit.ts`:

```ts
/**
 * Denetim kayıtları, en yeni üstte. Önce latest_seq okunur (limit 1),
 * sonra after_seq = latest − n istenir (lib/audit-window.ts). Eski arayüz
 * (0, 50) istiyor ve EN ESKİ 50'yi "son kayıtlar" diye gösteriyordu.
 */

import { useCallback, useEffect, useState } from "react";

import { bridge } from "../bridge.ts";
import { AUDIT_PAGE, mergeNewestFirst, newestWindow, olderWindow } from "../lib/audit-window.ts";
import { errorText } from "../lib/errors.ts";
import type { AuditRecord } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export function useAudit(target: string | null, size: number) {
  const toast = useToast();
  const [records, setRecords] = useState<AuditRecord[]>([]);
  const [latest, setLatest] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);

  const loadNewest = useCallback(async () => {
    if (!target) {
      setRecords([]);
      setLatest(null);
      return;
    }
    setLoading(true);
    try {
      const head = await bridge.auditList(target, 0, 1);
      const latestSeq = Number(head.latest_seq);
      setLatest(latestSeq);
      if (!Number.isFinite(latestSeq) || latestSeq <= 0) {
        setRecords([]);
        return;
      }
      const w = newestWindow(latestSeq, size);
      const page = await bridge.auditList(target, w.afterSeq, w.limit);
      setRecords(mergeNewestFirst([], page.records));
    } catch (err) {
      toast("toast.audit_failed", { error: errorText(err) });
    } finally {
      setLoading(false);
    }
  }, [target, size, toast]);

  const loadOlder = useCallback(async () => {
    const oldest = records.length > 0 ? Number(records[records.length - 1]!.seq) : 0;
    const w = olderWindow(oldest, AUDIT_PAGE);
    if (!target || !w) return;
    setLoading(true);
    try {
      const page = await bridge.auditList(target, w.afterSeq, w.limit);
      setRecords((cur) => mergeNewestFirst(cur, page.records));
    } catch (err) {
      toast("toast.audit_failed", { error: errorText(err) });
    } finally {
      setLoading(false);
    }
  }, [target, records, toast]);

  useEffect(() => {
    void loadNewest();
  }, [loadNewest]);

  const hasOlder = records.length > 0 && Number(records[records.length - 1]!.seq) > 1;
  return { records, latest, loading, hasOlder, loadNewest, loadOlder };
}
```

- [ ] **Adım 2: Bileşenleri yaz**

`desktop/src/renderer/components/Sparkline.tsx`:

```tsx
import { sparklinePath } from "../lib/samples.ts";

const W = 120;
const H = 28;

/** Küçük çizgi grafik; örnekler yalnız ekran açıkken toplanır (bellekte). */
export function Sparkline({ samples, label }: { samples: readonly number[]; label: string }) {
  const d = sparklinePath(samples, W, H);
  return (
    <svg className="sparkline" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
      {d ? <path d={d} /> : null}
    </svg>
  );
}
```

`desktop/src/renderer/components/Tile.tsx`:

```tsx
import type { ReactNode } from "react";

import type { Tone } from "../lib/chain.ts";

interface Props {
  label: string;
  value: string;
  tone?: Tone;
  testId?: string;
  children?: ReactNode;
}

/** Overview şeridindeki karo: küçük etiket, büyük değer, isteğe bağlı grafik. */
export function Tile({ label, value, tone, testId, children }: Props) {
  return (
    <div className="tile" data-testid={testId}>
      <span className="tile-label">{label}</span>
      <span className={tone ? `tile-value mono is-${tone}` : "tile-value mono"}>{value}</span>
      {children}
    </div>
  );
}
```

`desktop/src/renderer/components/InfoCard.tsx`:

```tsx
import type { Tone } from "../lib/chain.ts";

export interface InfoRow {
  label: string;
  value: string;
  tone?: Tone;
}

export function InfoCard({ title, rows }: { title: string; rows: readonly InfoRow[] }) {
  return (
    <article className="card glass">
      <h3 className="card-title">{title}</h3>
      <dl className="rows">
        {rows.map((r) => (
          <div className="row" key={r.label}>
            <dt>{r.label}</dt>
            <dd className={r.tone ? `is-${r.tone}` : undefined}>{r.value}</dd>
          </div>
        ))}
      </dl>
    </article>
  );
}
```

`desktop/src/renderer/components/ChainCard.tsx`:

```tsx
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { chainView } from "../lib/chain.ts";
import { formatCount } from "../lib/format.ts";

interface Props {
  name: string;
  status: string | undefined;
  checked: string | undefined;
  detail: string | undefined;
  testId: string;
}

/**
 * Zincir kartı. BROKEN (kurcalama şüphesi) ile UNVERIFIABLE (işletim
 * sorunu) farklı renkte; aynı görünürlerse operatör sahte alarma koşar.
 */
export function ChainCard({ name, status, checked, detail, testId }: Props) {
  const t = useT();
  const language = useLanguage();
  const view = chainView(status);
  const n = Number(checked) || 0;
  return (
    <article className={`chain glass is-${view.tone}`} data-testid={testId}>
      <div className="chain-top">
        <h4 className="chain-name mono">{name}</h4>
        <span className={`badge is-${view.tone}`}>{view.label}</span>
      </div>
      <p className="chain-count">
        {status ? t("chains.records", { count: n, n: formatCount(n, language) }) : t("chains.not_verified")}
      </p>
      {detail ? <p className="chain-detail mono">{detail}</p> : null}
    </article>
  );
}
```

`desktop/src/renderer/components/AuditTable.tsx`:

```tsx
import { useState } from "react";

import { bridge } from "../bridge.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { outcomeView } from "../lib/chain.ts";
import { errorText } from "../lib/errors.ts";
import { describeActor, formatTime } from "../lib/format.ts";
import type { AuditRecord } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

const COPIED_MS = 1_500;

/** Denetim tablosu. Parmak izine tıklayınca tamamı panoya kopyalanır. */
export function AuditTable({ records, testId }: { records: readonly AuditRecord[]; testId: string }) {
  const t = useT();
  const language = useLanguage();
  const toast = useToast();
  const [copied, setCopied] = useState<string | null>(null);

  const copy = async (fp: string) => {
    try {
      await bridge.copyText(fp);
      setCopied(fp);
      setTimeout(() => setCopied(null), COPIED_MS);
    } catch (err) {
      toast("toast.copy_failed", { error: errorText(err) });
    }
  };

  return (
    <div className="table-wrap">
      <table className="audit-table" data-testid={testId}>
        <thead>
          <tr>
            <th scope="col">{t("audit.col_seq")}</th>
            <th scope="col">{t("audit.col_time")}</th>
            <th scope="col">{t("audit.col_actor")}</th>
            <th scope="col">{t("audit.col_action")}</th>
            <th scope="col">{t("audit.col_target")}</th>
            <th scope="col">{t("audit.col_outcome")}</th>
          </tr>
        </thead>
        <tbody>
          {records.map((r) => {
            const fp = r.actor?.ssh_key_fingerprint ?? "";
            const actor = describeActor(r.actor) ?? t("audit.unknown_actor");
            const out = outcomeView(r.outcome);
            return (
              <tr key={r.seq} data-testid="audit-row" data-seq={r.seq}>
                <td className="mono">{r.seq}</td>
                <td className="mono">{formatTime(r.ts, language)}</td>
                <td className="mono">
                  {fp ? (
                    <button type="button" className="fp" title={t("audit.copy")} onClick={() => void copy(fp)}>
                      {copied === fp ? t("audit.copied") : actor}
                    </button>
                  ) : (
                    actor
                  )}
                </td>
                <td className="mono">{r.action || "—"}</td>
                <td>{r.target || "—"}</td>
                <td className={`outcome is-${out.tone}`}>{out.label}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
```

- [ ] **Adım 3: Ekranı yaz**

`desktop/src/renderer/screens/StatusScreen.tsx`:

```tsx
import { useEffect, useState } from "react";

import { AuditTable } from "../components/AuditTable.tsx";
import { ChainCard } from "../components/ChainCard.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { InfoCard } from "../components/InfoCard.tsx";
import { Sparkline } from "../components/Sparkline.tsx";
import { Tile } from "../components/Tile.tsx";
import type { ServerApi } from "../hooks/useServer.ts";
import { useAudit } from "../hooks/useAudit.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { chainView, worstTone } from "../lib/chain.ts";
import { humanDuration, memoryText } from "../lib/format.ts";

/** Coolify'daki gibi: açıkken her 10 saniyede yenile. Varsayılan kapalı. */
const LIVE_INTERVAL_MS = 10_000;
const PREVIEW_SIZE = 10;

export function StatusScreen({ server, onSeeAll }: { server: ServerApi; onSeeAll: () => void }) {
  const t = useT();
  const { target, status, verify, rtt, memorySamples, rttSamples } = server.state;
  const [live, setLive] = useState(false);
  const audit = useAudit(target, PREVIEW_SIZE);
  const { refresh } = server;

  useEffect(() => {
    if (!live || !target) return;
    const id = setInterval(() => void refresh(), LIVE_INTERVAL_MS);
    return () => clearInterval(id);
  }, [live, target, refresh]);

  if (!target || !status) return <EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />;

  const host = status.host;
  const reachable = status.executor_reachable;
  const root = status.running_as_user === "root";
  const daemonChain = chainView(verify?.daemon_status);
  const execChain = chainView(verify?.executor_status);

  return (
    <div className="screen" data-testid="screen-status">
      <header className="stage-head">
        <p className="eyebrow">{t("status.eyebrow")}</p>
        <h1 className="title mono">{target}</h1>
        <p className="meta">{reachable ? t("status.meta_ok") : t("status.meta_down")}</p>
      </header>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("status.overview")}</h2>
          <div className="panel-actions">
            <label className="live">
              <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} data-testid="live" />
              <i className={live ? "live-dot is-on" : "live-dot"} aria-hidden="true" />
              {t("status.live")}
            </label>
            <button type="button" className="btn btn-ghost" onClick={() => void refresh()}>
              {t("status.refresh")}
            </button>
          </div>
        </div>
        <div className="tiles">
          <Tile label={t("tile.uptime")} value={humanDuration(status.daemon_uptime_seconds)} />
          <Tile label={t("tile.memory")} value={memoryText(host)}>
            <Sparkline samples={memorySamples} label={t("tile.memory")} />
          </Tile>
          <Tile label={t("tile.rtt")} value={rtt === null ? "—" : t("topbar.rtt", { ms: rtt })}>
            <Sparkline samples={rttSamples} label={t("tile.rtt")} />
          </Tile>
          <Tile label={t("tile.executor")} value={reachable ? t("value.reachable") : t("value.unreachable")} tone={reachable ? "ok" : "warn"} />
          <Tile label={t("tile.chains")} value={`${daemonChain.label} · ${execChain.label}`} tone={worstTone(daemonChain.tone, execChain.tone)} testId="tile-chains" />
        </div>
      </section>

      <section className="cards">
        <InfoCard
          title={t("card.daemon")}
          rows={[
            { label: t("row.version"), value: status.daemon_version || "—" },
            { label: t("row.uptime"), value: humanDuration(status.daemon_uptime_seconds) },
            root
              ? { label: t("row.user"), value: t("value.broken_install", { user: "root" }), tone: "bad" }
              : { label: t("row.user"), value: status.running_as_user || "—" },
          ]}
        />
        <InfoCard
          title={t("card.executor")}
          rows={[
            { label: t("row.state"), value: reachable ? t("value.reachable") : t("value.unreachable"), tone: reachable ? "ok" : "warn" },
            { label: t("row.version"), value: status.executor_version || "—" },
            { label: t("row.docker"), value: host?.docker_version || t("value.none") },
          ]}
        />
        <InfoCard
          title={t("card.host")}
          rows={[
            { label: t("row.name"), value: status.hostname || "—" },
            { label: t("row.kernel"), value: host?.kernel_version || "—" },
            { label: t("row.memory"), value: memoryText(host) },
          ]}
        />
      </section>

      <section className="chains">
        <ChainCard name={t("chains.daemon")} status={verify?.daemon_status} checked={verify?.records_checked} detail={verify?.detail} testId="chain-daemon" />
        <ChainCard name={t("chains.executor")} status={verify?.executor_status} checked={verify?.executor_records_checked} detail={verify?.executor_detail} testId="chain-executor" />
      </section>
      <p className="chain-note">{t("chains.note")}</p>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("audit.recent")}</h2>
          <button type="button" className="btn btn-ghost" onClick={onSeeAll} data-testid="see-all">
            {t("audit.see_all")}
          </button>
        </div>
        <AuditTable records={audit.records} testId="audit-preview" />
      </section>
    </div>
  );
}
```

`desktop/src/renderer/App.tsx`:
- `<EmptyState … />` satırını, `view === "status"` ise `<StatusScreen server={server} onSeeAll={() => setView("audit")} />` gösterecek şekilde değiştir.
- Diğer görünümler için şimdilik `<EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />` kalır.
- `import { StatusScreen } from "./screens/StatusScreen.tsx";` ekle.

```tsx
          {view === "status" ? (
            <StatusScreen server={server} onSeeAll={() => setView("audit")} />
          ) : (
            <EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />
          )}
```

`desktop/src/renderer/styles/screens.css`:

```css
.screen { display: flex; flex-direction: column; gap: 14px; }
.stage-head { padding: 8px 6px 2px; }
.eyebrow { margin: 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.1em; color: var(--accent); }
.title { margin: 4px 0; font-size: 26px; font-weight: var(--w-strong); letter-spacing: -0.01em; overflow-wrap: anywhere; }
.meta { margin: 0; color: var(--fg-dim); font-size: 13px; }

/* Panel ve Coolify tarzı altı çizili bölüm başlığı */
.panel { padding: 14px 18px; }
.panel-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; flex-wrap: wrap; }
.panel-actions { display: flex; align-items: center; gap: 10px; }
.section-title {
  margin: 0; font-size: 13px; font-weight: var(--w-strong); color: var(--fg-dim);
  text-decoration: underline; text-decoration-color: rgba(var(--acc), 0.5); text-underline-offset: 5px;
}
.panel-foot { display: flex; justify-content: center; padding-top: 10px; }

/* Canlı yenile anahtarı */
.live { display: inline-flex; align-items: center; gap: 8px; font-size: 12px; color: var(--fg-dim); cursor: pointer; }
.live input { position: absolute; opacity: 0; width: 1px; height: 1px; }
.live-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--fg-faint); }
.live-dot.is-on { background: var(--ok); box-shadow: 0 0 8px var(--ok); }
.live:focus-within { color: var(--fg); }

/* Overview şeridi */
.tiles { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); }
.tile { padding: 6px 14px 4px; display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.tile + .tile { border-left: 1px solid var(--line); }
.tile-label { font-size: 10.5px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--fg-faint); }
.tile-value { font-size: 18px; font-weight: var(--w-strong); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.sparkline { width: 100%; height: 28px; overflow: visible; }
.sparkline path { fill: none; stroke: var(--accent); stroke-width: 1.5; vector-effect: non-scaling-stroke; }

/* Ayrıntı kartları */
.cards { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
.card { padding: 16px 18px; }
.card-title { margin: 0 0 10px; font-size: 13px; font-weight: var(--w-strong); color: var(--fg-dim); }
.rows { margin: 0; display: flex; flex-direction: column; gap: 8px; }
.row { display: flex; justify-content: space-between; gap: 10px; font-size: 13px; }
.row dt { color: var(--fg-faint); }
.row dd { margin: 0; text-align: right; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }

/* Zincirler */
.chains { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.chain { padding: 14px 18px; }
.chain-top { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.chain-name { margin: 0; font-size: 13px; }
.chain-count { margin: 6px 0 0; font-size: 12px; color: var(--fg-dim); }
.chain-detail { margin: 6px 0 0; font-size: 11.5px; color: var(--fg-faint); overflow-wrap: anywhere; }
.chain-note { margin: -4px 6px 0; font-size: 12px; color: var(--fg-faint); line-height: 1.5; }

/* Denetim tablosu */
.table-wrap { overflow-x: auto; }
.audit-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.audit-table th { text-align: left; font-weight: var(--w-ui); color: var(--fg-faint); padding: 6px 8px; border-bottom: 1px solid var(--line); }
.audit-table td { padding: 8px; border-bottom: 1px solid rgba(255, 255, 255, 0.05); font-variant-numeric: tabular-nums; }
.audit-table tr:last-child td { border-bottom: 0; }
.fp { font: inherit; color: inherit; background: transparent; border: 0; padding: 0; cursor: copy; text-decoration: underline dotted; }
.outcome { font-family: var(--font-mono); font-size: 12px; letter-spacing: 0.04em; }
```

`main.tsx`'e `import "./styles/layout.css";` (Görev 13'te eklenmediyse) ve `import "./styles/screens.css";` ekle.

- [ ] **Adım 4: Derle, testler, duman**

```bash
npm run typecheck
npm test
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen: hepsi PASS.

- [ ] **Adım 5: Elle — gerçek sidecar ile**

`go build -o bin/kadran.exe ./cmd/kadran` (depo kökünden), sonra `npm run dev`. Bir sunucuya bağlan. Bakılacaklar:
- Overview karoları dolu.
- "Live refresh" açılınca 10 saniyede bir gecikme grafiği uzuyor.
- Zincir kartları doğru renkte.
- "Recent audit records" en yeni kaydı üstte gösteriyor. Sunucuda 10'dan fazla kayıt varsa ilk satırın seq'i `kadran audit list` çıktısındaki en büyük sayıyla aynı.

- [ ] **Adım 6: Commit**

```bash
git add desktop/src/renderer
git commit -m "feat(masaüstü): Status ekranı — Overview şeridi, zincirler, en yeni kayıtlar"
```

---

### Görev 15: Audit ekranı — aralık, süzgeç, sonuç çubuğu, daha eskiler

**Dosyalar:**
- Yeni: `desktop/src/renderer/components/{OutcomeBar,Segmented}.tsx`, `desktop/src/renderer/screens/AuditScreen.tsx`
- Değişir: `desktop/src/renderer/App.tsx`, `desktop/src/renderer/styles/screens.css`

**Arayüzler:**
- Tüketir:
  - `useAudit` (Görev 14)
  - `countOutcomes`, `filterRecords`, `OUTCOME_KEYS`, `OUTCOME_TONE`, `OutcomeKey` (Görev 3)
- Üretir:
  - `Segmented<T extends string>({ options: { value: T; label: string; testId?: string }[], value, onChange, label })`
  - `OutcomeBar({ records })`
  - `data-testid` değerleri: `screen-audit`, `audit-full`, `load-older`, `range-50`, `range-200`, `range-1000`, `filter-action`, `filter-outcome`, `outcome-bar`

- [ ] **Adım 1: Bileşenleri ve ekranı yaz**

`desktop/src/renderer/components/Segmented.tsx`:

```tsx
export interface SegOption<T extends string> {
  value: T;
  label: string;
  testId?: string;
}

interface Props<T extends string> {
  options: readonly SegOption<T>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
}

/** Bölümlü seçici; ekran okuyucu için radyo grubu. */
export function Segmented<T extends string>({ options, value, onChange, label }: Props<T>) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          className={o.value === value ? "is-on" : undefined}
          data-testid={o.testId}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
```

`desktop/src/renderer/components/OutcomeBar.tsx`:

```tsx
import { useT } from "../i18n/I18nProvider.tsx";
import { countOutcomes, OUTCOME_KEYS, OUTCOME_TONE } from "../lib/chain.ts";
import type { AuditRecord } from "../lib/types.ts";

/**
 * Yüklü kayıtların sonuç dağılımı (Coolify'daki durum kodu çubuğu gibi).
 * Genişlikler React'in style özelliğiyle (CSSOM) yazılır; CSP style-src
 * yalnız style= NİTELİĞİNİ engeller, bu yol serbest.
 */
export function OutcomeBar({ records }: { records: readonly AuditRecord[] }) {
  const t = useT();
  const counts = countOutcomes(records);
  return (
    <div className="outcomes" data-testid="outcome-bar">
      <div className="outcome-legend">
        {OUTCOME_KEYS.map((k) => (
          <span key={k} className={`legend is-${OUTCOME_TONE[k]}`}>
            <i aria-hidden="true" />
            {t(`outcome.${k}`)}
            <b className="mono">{counts[k]}</b>
          </span>
        ))}
      </div>
      <div className="outcome-track">
        {OUTCOME_KEYS.map((k) =>
          counts[k] > 0 ? <span key={k} className={`outcome-seg is-${OUTCOME_TONE[k]}`} style={{ flexGrow: counts[k] }} /> : null,
        )}
      </div>
    </div>
  );
}
```

`desktop/src/renderer/screens/AuditScreen.tsx`:

```tsx
import { useMemo, useState } from "react";

import { AuditTable } from "../components/AuditTable.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { OutcomeBar } from "../components/OutcomeBar.tsx";
import { Segmented } from "../components/Segmented.tsx";
import { useAudit } from "../hooks/useAudit.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { filterRecords, OUTCOME_KEYS, type OutcomeKey } from "../lib/chain.ts";

const RANGES = ["50", "200", "1000"] as const;
type Range = (typeof RANGES)[number];

export function AuditScreen({ target }: { target: string | null }) {
  const t = useT();
  const [range, setRange] = useState<Range>("50");
  const [action, setAction] = useState("");
  const [outcome, setOutcome] = useState<OutcomeKey | "">("");
  const audit = useAudit(target, Number(range));

  const actions = useMemo(() => [...new Set(audit.records.map((r) => r.action))].sort(), [audit.records]);
  const shown = useMemo(() => filterRecords(audit.records, { action, outcome }), [audit.records, action, outcome]);

  if (!target) return <EmptyState title={t("audit.no_target_title")} body={t("audit.no_target_body")} />;

  return (
    <div className="screen" data-testid="screen-audit">
      <header className="stage-head">
        <p className="eyebrow">{t("audit.eyebrow")}</p>
        <h1 className="title">{t("audit.title")}</h1>
      </header>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("audit.outcomes")}</h2>
          <Segmented
            label={t("audit.range")}
            value={range}
            options={RANGES.map((n) => ({ value: n, label: t("audit.last", { n }), testId: `range-${n}` }))}
            onChange={setRange}
          />
        </div>
        <OutcomeBar records={audit.records} />
      </section>

      <section className="panel glass">
        <div className="panel-head filters">
          <label className="filter">
            {t("audit.filter_action")}
            <select className="sel" value={action} onChange={(e) => setAction(e.target.value)} data-testid="filter-action">
              <option value="">{t("audit.filter_all")}</option>
              {actions.map((a) => (
                <option key={a} value={a}>{a}</option>
              ))}
            </select>
          </label>
          <label className="filter">
            {t("audit.filter_outcome")}
            <select className="sel" value={outcome} onChange={(e) => setOutcome(e.target.value as OutcomeKey | "")} data-testid="filter-outcome">
              <option value="">{t("audit.filter_all")}</option>
              {OUTCOME_KEYS.map((k) => (
                <option key={k} value={k}>{t(`outcome.${k}`)}</option>
              ))}
            </select>
          </label>
          <button type="button" className="btn btn-ghost" onClick={() => void audit.loadNewest()}>
            {t("status.refresh")}
          </button>
        </div>

        {audit.records.length === 0 && !audit.loading ? (
          <p className="empty">{t("audit.empty")}</p>
        ) : (
          <AuditTable records={shown} testId="audit-full" />
        )}

        {audit.hasOlder ? (
          <div className="panel-foot">
            <button type="button" className="btn btn-ghost" disabled={audit.loading} onClick={() => void audit.loadOlder()} data-testid="load-older">
              {audit.loading ? t("audit.loading") : t("audit.load_older")}
            </button>
          </div>
        ) : null}
      </section>
    </div>
  );
}
```

`App.tsx`'te görünüm seçimini genişlet ve `import { AuditScreen } from "./screens/AuditScreen.tsx";` ekle:

```tsx
          {view === "status" ? (
            <StatusScreen server={server} onSeeAll={() => setView("audit")} />
          ) : view === "audit" ? (
            <AuditScreen target={server.state.target} />
          ) : (
            <EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />
          )}
```

`screens.css`'in sonuna ekle:

```css
/* Sonuç çubuğu */
.outcomes { display: flex; flex-direction: column; gap: 10px; }
.outcome-legend { display: flex; gap: 16px; flex-wrap: wrap; font-size: 12px; color: var(--fg-dim); }
.legend { display: inline-flex; align-items: center; gap: 6px; }
.legend i { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.legend b { color: var(--fg); font-weight: var(--w-strong); }
.outcome-track { display: flex; height: 14px; border-radius: 4px; overflow: hidden; background: var(--line); }
.outcome-seg { display: block; min-width: 3px; }
.outcome-seg.is-ok { background: var(--ok); }
.outcome-seg.is-warn { background: var(--warn); }
.outcome-seg.is-bad { background: var(--bad); }

/* Süzgeçler */
.filters { justify-content: flex-start; }
.filter { display: inline-flex; align-items: center; gap: 8px; font-size: 12px; color: var(--fg-dim); }
```

- [ ] **Adım 2: Derle, testler, duman**

```bash
npm run typecheck
npm test
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen: PASS. `no-raw-text`, `OutcomeBar`'daki `style={{ flexGrow }}`'a bakmaz; o bir CSSOM özelliği, metin değil. Duman testi konsolda CSP ihlali olmadığını doğrular.

- [ ] **Adım 3: Commit**

```bash
git add desktop/src/renderer
git commit -m "feat(masaüstü): Audit ekranı — aralık, süzgeç, sonuç çubuğu, daha eskiler"
```

---
### Görev 16: Settings ekranı — Görünüm, Dil, Sunucular, Hakkında

**Dosyalar:**
- Yeni: `desktop/src/renderer/components/{SetRow,FontPicker}.tsx`, `desktop/src/renderer/screens/SettingsScreen.tsx`
- Değişir: `desktop/src/renderer/App.tsx`, `desktop/src/renderer/styles/screens.css`
- Test: `desktop/test/no-raw-text.test.ts` (dosya sayısı eşiği 1 → 15)

**Arayüzler:**
- Tüketir:
  - `useAppearance` (Görev 12)
  - `weightsOf`, `nearestWeight` (Görev 10)
  - `LANGUAGES` (Görev 4)
  - `Segmented` (Görev 15)
  - `Profile`, `bridge` (Görev 9)
- Üretir:
  - `SetRow({ title, hint, children })`
  - `RangeRow({ title, hint, id, value, min, max, onChange })`
  - `FontPicker({ families, family, weight, weights, defaultLabel, onFamily, onWeight, testId })`
  - `SettingsScreen({ profiles, version, onRemoveProfile })`
  - `data-testid` değerleri:
    - `screen-settings`
    - `mode-<mod>`, `accent-<ad>`, `tone-<ad>`, `lang-en`, `lang-tr`
    - `ui-font-family`, `ui-font-weight`, `code-font-family`, `code-font-weight`
    - `font-note`, `about`, `reset`
    - `range-tint`, `range-panel`, `range-edge`, `range-glow`
    - `remove-<target>`

- [ ] **Adım 1: Bileşenleri yaz**

`desktop/src/renderer/components/SetRow.tsx`:

```tsx
import type { ReactNode } from "react";

/** Ayar satırı: solda ad ve tek satırlık açıklama, sağda denetim. */
export function SetRow({ title, hint, children }: { title: string; hint: string; children: ReactNode }) {
  return (
    <div className="set-row">
      <div className="set-label">
        <strong>{title}</strong>
        <span>{hint}</span>
      </div>
      <div className="set-ctl">{children}</div>
    </div>
  );
}

interface RangeRowProps {
  title: string;
  hint: string;
  id: string;
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
}

export function RangeRow({ title, hint, id, value, min, max, onChange }: RangeRowProps) {
  return (
    <SetRow title={title} hint={hint}>
      <input
        type="range"
        id={id}
        min={min}
        max={max}
        value={value}
        aria-label={title}
        data-testid={`range-${id}`}
        onChange={(e) => onChange(Number(e.target.value))}
      />
      <output className="mono" htmlFor={id}>
        {`${value}%`}
      </output>
    </SetRow>
  );
}
```

`desktop/src/renderer/components/FontPicker.tsx`:

```tsx
interface Props {
  families: readonly string[];
  family: string;
  weight: number;
  weights: readonly number[];
  defaultLabel: string;
  onFamily: (family: string) => void;
  onWeight: (weight: number) => void;
  testId: string;
}

/** Aile + kalınlık seçimi. Boş aile: arayüzde sistem, kodda gömülü JetBrains Mono. */
export function FontPicker({ families, family, weight, weights, defaultLabel, onFamily, onWeight, testId }: Props) {
  return (
    <div className="font-row">
      <select className="sel" value={family} aria-label={defaultLabel} onChange={(e) => onFamily(e.target.value)} data-testid={`${testId}-family`}>
        <option value="">{defaultLabel}</option>
        {families.map((f) => (
          <option key={f} value={f}>{f}</option>
        ))}
      </select>
      <select className="sel sel-w" value={String(weight)} aria-label={`${defaultLabel} · ${weight}`} onChange={(e) => onWeight(Number(e.target.value))} data-testid={`${testId}-weight`}>
        {weights.map((w) => (
          <option key={w} value={String(w)}>{w}</option>
        ))}
      </select>
    </div>
  );
}
```

`desktop/src/renderer/screens/SettingsScreen.tsx`:

```tsx
import { useMemo } from "react";

import { useAppearance } from "../appearance/AppearanceProvider.tsx";
import type { Profile } from "../bridge.ts";
import { FontPicker } from "../components/FontPicker.tsx";
import { Segmented } from "../components/Segmented.tsx";
import { RangeRow, SetRow } from "../components/SetRow.tsx";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { formatCount } from "../lib/format.ts";
import { nearestWeight, weightsOf } from "../lib/fonts.ts";
import { LANGUAGES } from "../lib/languages.ts";
import type { VersionPayload } from "../lib/types.ts";
import { ACCENTS, LIMITS, TONES } from "../../shared/appearance.ts";

interface Props {
  profiles: readonly Profile[];
  version: VersionPayload | null;
  onRemoveProfile: (target: string) => void;
}

export function SettingsScreen({ profiles, version, onRemoveProfile }: Props) {
  const t = useT();
  const language = useLanguage();
  const { appearance: a, modes, families, fontsListed, update, reset } = useAppearance();
  const names = useMemo(() => [...families.keys()].sort((x, y) => x.localeCompare(y)), [families]);
  const uiFaces = families.get(a.uiFont.family) ?? [];
  const codeFaces = families.get(a.codeFont.family) ?? [];

  return (
    <div className="screen" data-testid="screen-settings">
      <header className="stage-head">
        <p className="eyebrow">{t("settings.title")}</p>
        <h1 className="title">{t("settings.appearance")}</h1>
        <p className="meta">{t("settings.appearance_meta")}</p>
      </header>

      <section className="panel glass settings">
        <SetRow title={t("settings.window")} hint={t("settings.window_hint")}>
          <Segmented
            label={t("settings.window")}
            value={a.mode}
            options={modes.map((m) => ({ value: m, label: t(`mode.${m}`), testId: `mode-${m}` }))}
            onChange={(mode) => update({ mode })}
          />
        </SetRow>
        <RangeRow title={t("settings.tint")} hint={t("settings.tint_hint")} id="tint" value={a.tint} min={LIMITS.tint[0]} max={LIMITS.tint[1]} onChange={(tint) => update({ tint })} />
        <RangeRow title={t("settings.panel")} hint={t("settings.panel_hint")} id="panel" value={a.panel} min={LIMITS.panel[0]} max={LIMITS.panel[1]} onChange={(panel) => update({ panel })} />
        <RangeRow title={t("settings.edge")} hint={t("settings.edge_hint")} id="edge" value={a.edge} min={LIMITS.edge[0]} max={LIMITS.edge[1]} onChange={(edge) => update({ edge })} />
        <RangeRow title={t("settings.glow")} hint={t("settings.glow_hint")} id="glow" value={a.glow} min={LIMITS.glow[0]} max={LIMITS.glow[1]} onChange={(glow) => update({ glow })} />
        <SetRow title={t("settings.accent")} hint={t("settings.accent_hint")}>
          <Segmented
            label={t("settings.accent")}
            value={a.accent}
            options={ACCENTS.map((x) => ({ value: x, label: t(`accent.${x}`), testId: `accent-${x}` }))}
            onChange={(accent) => update({ accent })}
          />
        </SetRow>
        <SetRow title={t("settings.theme")} hint={t("settings.theme_hint")}>
          <Segmented
            label={t("settings.theme")}
            value={a.tone}
            options={TONES.map((x) => ({ value: x, label: t(`tone.${x}`), testId: `tone-${x}` }))}
            onChange={(tone) => update({ tone })}
          />
        </SetRow>
        <SetRow title={t("settings.ui_font")} hint={t("settings.ui_font_hint")}>
          <FontPicker
            families={names}
            family={a.uiFont.family}
            weight={nearestWeight(uiFaces, a.uiFont.weight)}
            weights={weightsOf(uiFaces)}
            defaultLabel={t("font.system")}
            onFamily={(family) => update({ uiFont: { family, weight: nearestWeight(families.get(family) ?? [], 400) } })}
            onWeight={(weight) => update({ uiFont: { family: a.uiFont.family, weight } })}
            testId="ui-font"
          />
        </SetRow>
        <SetRow title={t("settings.code_font")} hint={t("settings.code_font_hint")}>
          <FontPicker
            families={names}
            family={a.codeFont.family}
            weight={nearestWeight(codeFaces, a.codeFont.weight)}
            weights={weightsOf(codeFaces)}
            defaultLabel={t("font.bundled")}
            onFamily={(family) => update({ codeFont: { family, weight: nearestWeight(families.get(family) ?? [], 400) } })}
            onWeight={(weight) => update({ codeFont: { family: a.codeFont.family, weight } })}
            testId="code-font"
          />
        </SetRow>
        <p className="font-note" data-testid="font-note">
          {fontsListed
            ? t("font.families", { count: names.length, n: formatCount(names.length, language) })
            : t("font.unavailable")}
        </p>
      </section>

      <section className="panel glass settings">
        <SetRow title={t("settings.language")} hint={t("settings.language_hint")}>
          <Segmented
            label={t("settings.language")}
            value={a.language}
            options={LANGUAGES.map((l) => ({ value: l.code, label: l.name, testId: `lang-${l.code}` }))}
            onChange={(lang) => update({ language: lang })}
          />
        </SetRow>
      </section>

      <section className="panel glass settings">
        <SetRow title={t("settings.servers")} hint={t("settings.servers_hint")}>
          {profiles.length === 0 ? (
            <span className="empty">{t("settings.no_servers")}</span>
          ) : (
            <ul className="server-list">
              {profiles.map((p) => (
                <li key={p.target}>
                  <span className="mono">{p.target}</span>
                  <button type="button" className="btn btn-ghost" onClick={() => onRemoveProfile(p.target)} data-testid={`remove-${p.target}`}>
                    {t("settings.remove")}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </SetRow>
      </section>

      <section className="panel glass settings">
        <SetRow title={t("settings.about")} hint={t("settings.about_hint")}>
          <span className="about mono" data-testid="about">
            {t("about.line", { version: version ? `v${version.version}` : "…", protocol: version ? version.protocol : "…" })}
          </span>
        </SetRow>
      </section>

      <div className="set-foot">
        <button type="button" className="btn btn-ghost" onClick={reset} data-testid="reset">
          {t("settings.reset")}
        </button>
      </div>
    </div>
  );
}
```

`App.tsx`:
- `import { SettingsScreen } from "./screens/SettingsScreen.tsx";` ekle.
- Profil silme işlevini ekle.
- Görünüm seçimini tamamla. Artık hiçbir görünüm `EmptyState`'e düşmüyor; `EmptyState` içe aktarması `App.tsx`'ten kalkar.

```tsx
  const removeProfile = useCallback(
    async (target: string) => {
      try {
        setProfiles(await bridge.saveProfiles(profiles.filter((p) => p.target !== target)));
      } catch (err) {
        toast("toast.save_failed", { error: errorText(err) });
      }
    },
    [profiles, toast],
  );
```

```tsx
          {view === "status" ? (
            <StatusScreen server={server} onSeeAll={() => setView("audit")} />
          ) : view === "audit" ? (
            <AuditScreen target={server.state.target} />
          ) : (
            <SettingsScreen profiles={profiles} version={version} onRemoveProfile={(target) => void removeProfile(target)} />
          )}
```

`App.tsx`'te `t` artık kullanılmıyorsa `useT` içe aktarmasını ve `const t = useT();` satırını da kaldır; tip denetimi kullanılmayan değişkene izin verse bile kod temiz kalsın.

`screens.css`'in sonuna ekle:

```css
/* Settings */
.settings { padding: 2px 22px; }
.set-row {
  display: grid; grid-template-columns: 250px minmax(0, 1fr); gap: 28px; align-items: center;
  padding: 14px 0; border-bottom: 1px solid rgba(255, 255, 255, 0.07);
}
.settings > .set-row:last-of-type { border-bottom: 0; }
.set-label strong { display: block; font-size: 13px; font-weight: var(--w-strong); }
.set-label span { display: block; margin-top: 3px; font-size: 12px; color: var(--fg-faint); }
.set-ctl { display: flex; align-items: center; gap: 14px; min-width: 0; }
.set-ctl input[type="range"] { flex: 1; accent-color: var(--accent); }
.set-ctl output { width: 46px; text-align: right; font-size: 12px; color: var(--fg); }
.set-ctl .seg { flex: 1; max-width: 460px; }
.font-row { display: flex; gap: 6px; flex: 1; max-width: 460px; }
.font-row .sel { flex: 1; }
.font-row .sel-w { flex: none; width: 92px; }
.font-note { margin: 0 0 12px; font-size: 11.5px; color: var(--fg-faint); }
.server-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; width: 100%; }
.server-list li { display: flex; align-items: center; justify-content: space-between; gap: 12px; font-size: 13px; }
.about { font-size: 12px; color: var(--fg-dim); }
.set-foot { display: flex; padding: 0 4px 8px; }
```

- [ ] **Adım 2: Korumanın eşiğini yükselt**

`desktop/test/no-raw-text.test.ts`'te `assert.ok(files >= 1, …)` satırını `assert.ok(files >= 15, …)` yap. Artık ekranlar tamam. Bir yol kayması tarama kapsamını sessizce küçültürse test düşer.

- [ ] **Adım 3: Derle, testler, duman**

```bash
npm run typecheck
npm test
npm run build
KADRAN_SMOKE_TEST=1 npx electron .
```

Beklenen: PASS. `no-raw-text` en az 15 `.tsx` dosyası tarar ve sorun bulmaz.

- [ ] **Adım 4: Elle — kullanıcının makinesinde**

`npm run dev` → Settings. Bakılacaklar:
- Kaydırıcılar canlı değişiyor.
- Clear ↔ Acrylic geçişinde pencere yeniden kuruluyor ama yerinde ve boyutunda kalıyor, Settings açık kalıyor.
- Acrylic ↔ Mica anında değişiyor.
- Kod yazı tipi listesinde `JetBrainsMono Nerd Font` var. Seçilince kalınlık listesinde yalnız `500` görünüyor ve hash'ler o yazı tipiyle çiziliyor.
- Türkçe seçilince menü `Durum / Denetim / Ayarlar` oluyor.
- Uygulama kapatılıp açılınca ayarların hepsi duruyor.

- [ ] **Adım 5: Commit**

```bash
git add desktop/src/renderer desktop/test/no-raw-text.test.ts
git commit -m "feat(masaüstü): Settings — görünüm, dil, sunucular, hakkında"
```

---

### Görev 17: Uçtan uca — sahte sidecar ile kullanıcı yolculuğu

**Dosyalar:**
- Yeni: `desktop/test/fixtures/fake-sidecar.mjs`, `desktop/scripts/e2e.mjs`
- Değişir: `desktop/src/main/smoke.ts` (yolculuk eklenir), `desktop/package.json` (`"e2e": "node scripts/e2e.mjs"`)
- Test: `desktop/test/forbidden-patterns.test.ts`

**Arayüzler:**
- Tüketir:
  - fikstürler (Görev 2)
  - bütün `data-testid`'ler (Görev 13–16)
  - `evaluate` (Görev 9)
- Üretir:
  - `npm run e2e` (Electron'u `KADRAN_SMOKE_TEST=1`, `KADRAN_SMOKE_JOURNEY=1` ve sahte sidecar'la açar)
  - sahte sidecar: `latest_seq = 1284`; hedef `fake@down` hata döndürür.

- [ ] **Adım 1: Yasak kalıp testini yaz**

`desktop/test/forbidden-patterns.test.ts`:

```ts
/**
 * Arayüzde HTML enjekte eden hiçbir API kullanılmıyor. Sunucudan gelen
 * metin (hata, denetim alanları) güvenilmez girdi; her zaman düz metin.
 * Motion'ın <style> enjekte eden kipi de (popLayout) yasak: CSP onu
 * engeller ve sessizce bozulur.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");
const FORBIDDEN = [/dangerouslySetInnerHTML/, /\.innerHTML\s*=/, /insertAdjacentHTML/, /\.outerHTML\s*=/, /document\.write\(/, /popLayout/];

async function files(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, e.name);
    if (e.isDirectory()) out.push(...(await files(path)));
    else if (/\.(ts|tsx)$/.test(e.name)) out.push(path);
  }
  return out;
}

test("arayüzde HTML enjeksiyonu yok", async () => {
  const problems: string[] = [];
  const list = await files(rendererDir);
  assert.ok(list.length >= 30, `yalnız ${list.length} dosya tarandı`);
  for (const file of list) {
    const src = await readFile(file, "utf8");
    for (const re of FORBIDDEN) if (re.test(src)) problems.push(`${file}: ${re}`);
  }
  assert.deepEqual(problems, []);
});
```

Çalıştır: `node --test test/forbidden-patterns.test.ts`. Beklenen: PASS (kod temiz).

KIRMIZI ölç: `Toast.tsx`'teki `<p className="toast-text">{t(message.key, message.params)}</p>`'ı geçici olarak `<p className="toast-text" dangerouslySetInnerHTML={{ __html: t(message.key, message.params) }} />` yap ve testi koş: düşmeli. Sonra geri al.

- [ ] **Adım 2: Sahte sidecar'ı yaz**

`desktop/test/fixtures/fake-sidecar.mjs`:

```js
#!/usr/bin/env node
/**
 * Sahte `kadran sidecar`: uçtan uca testte gerçek sunucunun yerine geçer.
 *
 * Yanıtlar elle yazılmıyor: status.json, verify.json ve audit-record.json
 * gerçek protoToJSON çıktısı (cmd/kadran/sidecar_fixtures_test.go). Audit
 * listesi GERÇEK sayfalama kuralını uygular: after_seq HARİÇ, artan sıra,
 * limit 0 → 100, üst sınır 1000 (internal/store/store.go). Böylece "en
 * eski 50'yi son kayıt diye göstermek" hatası burada da görünür.
 *
 * `fake@down` hedefi uzun, İngilizce bir sunucu hatası döner (Türkçe
 * arayüzde de olduğu gibi görünmeli).
 */

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = (name) => JSON.parse(readFileSync(join(here, name), "utf8"));
const status = load("status.json");
const verify = load("verify.json");
const template = load("audit-record.json");

export const LATEST = 1284;
export const DOWN_MESSAGE =
  "could not connect to fake@down: ssh: connect to host down port 22: Connection refused; " +
  "check that the server is running and that this key is allowed in /home/kadran-client/.ssh/authorized_keys";

const ACTIONS = ["deploy", "app.update", "rollback", "backup.create", "app.delete"];
const OUTCOMES = ["AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_FAILURE", "AUDIT_OUTCOME_DENIED"];

function record(seq) {
  const ts = new Date(Date.UTC(2026, 9, 6, 18, 0, 0) - (LATEST - seq) * 60_000).toISOString().replace(".000Z", "Z");
  return { ...template, seq: String(seq), ts, action: ACTIONS[seq % 5], outcome: OUTCOMES[seq % 5] };
}

function auditList(params) {
  const after = Number(params.after_seq ?? 0);
  const raw = Number(params.limit ?? 0);
  const limit = raw <= 0 ? 100 : Math.min(raw, 1000);
  const records = [];
  for (let s = after + 1; s <= LATEST && records.length < limit; s++) records.push(record(s));
  return { records, latest_seq: String(LATEST) };
}

const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  let req;
  try {
    req = JSON.parse(line);
  } catch {
    return;
  }
  const reply = (body) => process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: req.id, ...body }) + "\n");
  const params = req.params ?? {};
  if (params.target === "fake@down") {
    reply({ error: { code: -32000, message: DOWN_MESSAGE } });
    return;
  }
  switch (req.method) {
    case "version":
      reply({ result: { version: "0.5.1", commit: "fake", protocol: 3 } });
      return;
    case "status":
      reply({ result: status });
      return;
    case "audit.list":
      reply({ result: auditList(params) });
      return;
    case "audit.verify":
      reply({ result: verify });
      return;
    default:
      reply({ error: { code: -32601, message: `unknown method "${req.method}"` } });
  }
});
rl.on("close", () => process.exit(0));
```

- [ ] **Adım 3: Yolculuğu `smoke.ts`'e ekle**

`desktop/src/main/smoke.ts`'e şu yardımcıları ve fonksiyonu ekle; mevcut `runSmokeTest`'i aşağıda gösterildiği gibi değiştir:

```ts
const JOURNEY = process.env.KADRAN_SMOKE_JOURNEY === "1";

const q = (selector: string): string => `document.querySelector(${JSON.stringify(selector)})`;

async function waitFor(win: BrowserWindow, expr: string, label: string, timeoutMs = 15_000): Promise<void> {
  const until = Date.now() + timeoutMs;
  while (Date.now() < until) {
    if (await evaluate<boolean>(win, `Boolean(${expr})`)) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${label}`);
}

async function click(win: BrowserWindow, selector: string): Promise<void> {
  const ok = await evaluate<boolean>(win, `(() => { const el = ${q(selector)}; if (!el) return false; el.click(); return true; })()`);
  if (!ok) throw new Error(`not found: ${selector}`);
}

async function text(win: BrowserWindow, selector: string): Promise<string> {
  return evaluate<string>(win, `${q(selector)}?.textContent ?? ""`);
}

async function rowSeqs(win: BrowserWindow, tableId: string): Promise<string[]> {
  return evaluate<string[]>(win, `[...document.querySelectorAll('[data-testid="${tableId}"] [data-testid="audit-row"]')].map((r) => r.dataset.seq)`);
}

function expect(cond: boolean, message: string): void {
  if (!cond) throw new Error(message);
}

/**
 * journey, sahte sidecar'a (test/fixtures/fake-sidecar.mjs) karşı bir
 * kullanıcının yaptıklarını yapar ve ekranı ölçer. Konsol hataları (CSP
 * ihlalleri dahil) runSmokeTest'in dinleyicisinde toplanır; yolculuk üç
 * ekranı da gezer ve ikon animasyonlarını tetikler ki bir ihlal gizli
 * kalmasın.
 */
async function journey(win: BrowserWindow): Promise<void> {
  // Yazı tipi izni: liste boş dönerse izin işleyicisi bozuk demektir.
  const fonts = await evaluate<number>(win, "window.queryLocalFonts ? queryLocalFonts().then((l) => l.length) : -1");
  expect(fonts > 0, `font list is empty or missing (${fonts}); the local-fonts permission may be denied`);

  await click(win, '[data-testid="server-fake@test"]');
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Attention required"`, "health pill");
  await waitFor(win, `${q('[data-testid="chain-executor"] .badge')}?.textContent === "UNVERIFIABLE"`, "executor chain");

  const daemonBadge = await evaluate<string>(win, `${q('[data-testid="chain-daemon"] .badge')}.className`);
  const execBadge = await evaluate<string>(win, `${q('[data-testid="chain-executor"] .badge')}.className`);
  expect((await text(win, '[data-testid="chain-daemon"] .badge')) === "VALID", "daemon chain is not VALID");
  expect(daemonBadge.includes("is-ok") && execBadge.includes("is-warn"), `chain tones wrong: ${daemonBadge} / ${execBadge}`);

  // İnceleme odağı 1: "son kayıtlar" en YENİYİ göstermeli.
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-preview"] [data-testid="audit-row"]').length === 10`, "audit preview");
  const preview = await rowSeqs(win, "audit-preview");
  expect(preview[0] === "1284" && preview[9] === "1275", `preview shows ${preview.join(",")}`);

  await click(win, '[data-testid="nav-audit"]');
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-full"] [data-testid="audit-row"]').length === 50`, "audit page");
  let full = await rowSeqs(win, "audit-full");
  expect(full[0] === "1284" && full[49] === "1235", `audit page shows ${full[0]}…${full[49]}`);
  await click(win, '[data-testid="load-older"]');
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-full"] [data-testid="audit-row"]').length === 100`, "older page");
  full = await rowSeqs(win, "audit-full");
  expect(full[99] === "1185", `older page ends at ${full[99]}`);
  expect(Boolean(await evaluate<boolean>(win, `Boolean(${q('[data-testid="outcome-bar"] .outcome-seg')})`)), "outcome bar is empty");

  await click(win, '[data-testid="nav-settings"]');
  await waitFor(win, q('[data-testid="screen-settings"]'), "settings screen");
  // İkon animasyonu: Motion'ın yazdığı her şey CSP'ye takılmamalı.
  await evaluate(win, `${q('[data-testid="nav-status"]')}.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }))`);
  await new Promise((r) => setTimeout(r, 800));

  await click(win, '[data-testid="lang-tr"]');
  await waitFor(win, `${q('[data-testid="nav-settings"]')}?.textContent === "Ayarlar"`, "Turkish UI");

  // İnceleme odağı 5: Türkçe arayüzde İngilizce sunucu hatası tam ve düz metin.
  await click(win, '[data-testid="server-fake@down"]');
  await waitFor(win, `${q('[data-testid="toast"]')}?.textContent.includes("Connection refused")`, "error toast");
  const toast = await text(win, '[data-testid="toast"] .toast-text');
  expect(toast.startsWith("Bağlanılamadı: could not connect to fake@down"), `toast text: ${toast}`);
  expect(toast.includes("authorized_keys"), "toast text was cut");
}
```

`runSmokeTest`'te, `try { … }` bloğunun SONUNA, Node sızıntısı kontrolünden sonra şunu ekle:

```ts
    if (JOURNEY) {
      try {
        await journey(win);
      } catch (err) {
        problems.push(`journey: ${(err as Error).message}`);
      }
    }
```

Başarı iletisini de şu hâle getir:

```ts
    console.log(
      JOURNEY
        ? "duman testi GEÇTİ: yolculuk tamam (sahte sidecar)"
        : "duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor",
    );
```

- [ ] **Adım 4: Koşucu betiği**

`desktop/scripts/e2e.mjs`:

```js
/**
 * Uçtan uca test: Electron'u duman kipinde, yolculukla ve sahte sidecar'la
 * açar; çıkış kodunu aynen döner.
 *
 * KADRAN_SIDECAR_COMMAND boşluktan bölünüyor (spawn-sidecar.ts); bu yüzden
 * sahte sidecar'ın yolunda boşluk olmamalı. "node" PATH'ten bulunur.
 */

import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import electron from "electron";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = resolve(here, "..");
const fake = join(desktop, "test", "fixtures", "fake-sidecar.mjs");
if (/\s/.test(fake)) {
  console.error(`sahte sidecar yolunda boşluk var, KADRAN_SIDECAR_COMMAND bölünür: ${fake}`);
  process.exit(2);
}

const child = spawn(electron, ["."], {
  cwd: desktop,
  stdio: "inherit",
  env: {
    ...process.env,
    KADRAN_SMOKE_TEST: "1",
    KADRAN_SMOKE_JOURNEY: "1",
    KADRAN_SIDECAR_COMMAND: `node ${fake}`,
  },
});
child.on("exit", (code) => process.exit(code ?? 1));
```

`desktop/package.json` `scripts` içine `"e2e": "node scripts/e2e.mjs",` ekle.

- [ ] **Adım 5: Koş**

```bash
npm run build
npm run e2e
```

Beklenen: `duman testi GEÇTİ: yolculuk tamam (sahte sidecar)` ve çıkış 0.

Bir adım düşerse ileti hangi `data-testid`'nin ne gösterdiğini söyler. Düzeltmeyi testi gevşeterek DEĞİL, bileşeni düzelterek yap.

- [ ] **Adım 6: KIRMIZI ölç**

İki ölçüm; her birinde yalnız `npm run build && npm run e2e` koşulur ve sonra değişiklik geri alınır:
1. `src/renderer/lib/audit-window.ts`'te `afterSeq: Math.max(0, latestSeq - limit)`'i `afterSeq: 0` yap. Beklenen: `journey: preview shows 10,9,8,…`.
2. `src/main/permissions.ts`'te `return ALLOWED_CHECKS.has(permission);`'ı `return false;` yap. Beklenen: `journey: font list is empty or missing (0)…`.

- [ ] **Adım 7: Commit**

```bash
git add desktop/test/fixtures/fake-sidecar.mjs desktop/scripts/e2e.mjs desktop/src/main/smoke.ts desktop/package.json desktop/test/forbidden-patterns.test.ts
git commit -m "test(masaüstü): sahte sidecar'a karşı uçtan uca yolculuk"
```

---
### Görev 18: Lisanslar ve paketleme — üçüncü taraf bildirimleri, Windows klasörü

**Dosyalar:**
- Yeni: `desktop/scripts/notices.mjs`, `desktop/scripts/dist.mjs`
- Değişir: `desktop/package.json` (`"dist"` betiği, `@electron/packager` 20.3.0), `.gitignore` (`desktop/dist/`, `desktop/build/`)
- Test: `desktop/test/licenses.test.ts`

**Arayüzler:**
- Tüketir: `package-lock.json`; `src/renderer/icons/LICENSE` (Görev 11), `src/renderer/fonts/OFL.txt` (Görev 10).
- Üretir:
  - `npm run dist` → `desktop/dist/Kadran-<platform>-x64/`. İçinde `Kadran(.exe)`, `resources/kadran(.exe)` ve `resources/THIRD_PARTY_NOTICES.txt` bulunur.
  - `ALLOWED_LICENSES`

- [ ] **Adım 1: Lisans testini yaz**

`desktop/test/licenses.test.ts`:

```ts
/**
 * Uygulamayla dağıtılan her üçüncü taraf parçasının lisansı izinli
 * listede. Çalışma zamanı paketleri (package-lock'ta dev olmayanlar)
 * arayüze gömülüyor; kopyalanan ikonlar ve yazı tipi de öyle. Kadran
 * Apache-2.0; izinli lisanslar onunla birlikte dağıtılabilenler.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
export const ALLOWED_LICENSES = new Set(["MIT", "ISC", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "0BSD", "OFL-1.1"]);

interface LockPackage {
  version?: string;
  license?: string;
  dev?: boolean;
  devOptional?: boolean;
}

test("çalışma zamanı paketlerinin lisansı izinli", async () => {
  const lock = JSON.parse(await readFile(join(root, "package-lock.json"), "utf8")) as { packages: Record<string, LockPackage> };
  const runtime = Object.entries(lock.packages).filter(([path, p]) => path !== "" && !p.dev && !p.devOptional);
  assert.ok(runtime.length >= 3, `yalnız ${runtime.length} çalışma zamanı paketi — package-lock biçimi değişmiş olabilir`);
  const bad = runtime.filter(([, p]) => !ALLOWED_LICENSES.has(p.license ?? "")).map(([path, p]) => `${path}: ${p.license}`);
  assert.deepEqual(bad, []);
});

test("kopyalanan ikon ve yazı tipi lisansları yerinde", async () => {
  const icons = await readFile(join(root, "src", "renderer", "icons", "LICENSE"), "utf8");
  assert.match(icons, /MIT License/);
  assert.match(icons, /ISC License/);
  const ofl = await readFile(join(root, "src", "renderer", "fonts", "OFL.txt"), "utf8");
  assert.match(ofl, /SIL OPEN FONT LICENSE/i);
});
```

- [ ] **Adım 2: Koş**

Çalıştır: `node --test test/licenses.test.ts`
Beklenen: PASS. Çalışma zamanı paketleri `react`, `react-dom`, `scheduler`, `motion`, `framer-motion`, `motion-dom`, `motion-utils` ve `tslib` (0BSD); hepsi izinli.

Listede izinli olmayan bir lisans çıkarsa listeye EKLEME, dur ve raporla.

- [ ] **Adım 3: Bildirim betiği**

`desktop/scripts/notices.mjs`:

```js
/**
 * build/THIRD_PARTY_NOTICES.txt'i üretir: Kadran'ın lisansı ve NOTICE'u,
 * arayüze gömülen her çalışma zamanı paketinin lisans metni, kopyalanan
 * ikonlar (MIT + ISC) ve JetBrains Mono (OFL). Paketlenmiş uygulamanın
 * resources/ dizinine konur. Electron'un kendi lisanslarını (LICENSE,
 * LICENSES.chromium.html) @electron/packager ekliyor.
 *
 * Lisans dosyası bulunamayan bir paket varsa betik DURUR.
 */

import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, "..");
const repo = join(desktop, "..");
const out = join(desktop, "build", "THIRD_PARTY_NOTICES.txt");

async function licenseText(dir) {
  const name = (await readdir(dir)).find((f) => /^licen[cs]e/i.test(f));
  if (!name) throw new Error(`lisans dosyası yok: ${dir}`);
  return readFile(join(dir, name), "utf8");
}

const lock = JSON.parse(await readFile(join(desktop, "package-lock.json"), "utf8"));
const sections = [
  `KΛDRΛN (Kadran) — Apache-2.0\n\n${(await readFile(join(repo, "LICENSE"), "utf8")).trim()}\n\n${(await readFile(join(repo, "NOTICE"), "utf8")).trim()}`,
];
for (const [path, p] of Object.entries(lock.packages)) {
  if (path === "" || p.dev || p.devOptional) continue;
  const name = path.replace(/^.*node_modules\//, "");
  sections.push(`${name}@${p.version} — ${p.license}\n\n${(await licenseText(join(desktop, path))).trim()}`);
}
sections.push(`Animated icons (pqoqubbw/icons) and Lucide drawings\n\n${(await readFile(join(desktop, "src", "renderer", "icons", "LICENSE"), "utf8")).trim()}`);
sections.push(`JetBrains Mono — OFL-1.1\n\n${(await readFile(join(desktop, "src", "renderer", "fonts", "OFL.txt"), "utf8")).trim()}`);

await mkdir(dirname(out), { recursive: true });
await writeFile(out, sections.join(`\n\n${"=".repeat(72)}\n\n`) + "\n", "utf8");
console.log(`${sections.length} bölüm → ${out}`);
```

- [ ] **Adım 4: Paketleme betiği**

Önce dışa aktarımı doğrula:

```bash
npm install --save-dev --save-exact @electron/packager@20.3.0
node -e "import('@electron/packager').then((m) => console.log(Object.keys(m)))"
```

Beklenen: çıktıda `packager` var. Yoksa dur ve raporla.

`desktop/scripts/dist.mjs`:

```js
/**
 * Bu makinenin platformu için paket klasörü üretir (imzasız):
 *
 *   dist/Kadran-<platform>-x64/
 *     Kadran(.exe)                       pencere başlığı KΛDRΛN
 *     resources/kadran(.exe)             sidecar (go build)
 *     resources/THIRD_PARTY_NOTICES.txt  scripts/notices.mjs
 *
 * Electron paket adı "kadran-desktop" DEĞİŞMEZ: kullanıcı verisi dizini
 * ondan türüyor (K-136); paket yalnız çalıştırılabilirin adını Kadran yapar.
 * Uygulamanın kendisi out/ ve package.json'dan ibaret: arayüzün çalışma
 * zamanı bağımlılıkları Vite ile gömülü, ana süreç yalnız Electron ve Node
 * kullanıyor; node_modules pakete girmez.
 */

import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { packager } from "@electron/packager";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, "..");
const repo = join(desktop, "..");
const exe = process.platform === "win32" ? "kadran.exe" : "kadran";
const sidecar = join(desktop, "build", "sidecar", exe);
const goos = { win32: "windows", darwin: "darwin", linux: "linux" }[process.platform];
if (!goos) throw new Error(`desteklenmeyen platform: ${process.platform}`);

execFileSync("go", ["build", "-trimpath", "-o", sidecar, "./cmd/kadran"], {
  cwd: repo,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: "amd64" },
});
execFileSync(process.execPath, [join(here, "notices.mjs")], { stdio: "inherit" });

const paths = await packager({
  dir: desktop,
  out: join(desktop, "dist"),
  platform: process.platform,
  arch: "x64",
  name: "Kadran",
  executableName: "Kadran",
  overwrite: true,
  asar: true,
  prune: false,
  extraResource: [sidecar, join(desktop, "build", "THIRD_PARTY_NOTICES.txt")],
  ignore: (path) => path !== "" && path !== "/package.json" && !path.startsWith("/out"),
});
console.log(`paket: ${paths.join(", ")}`);
```

`desktop/package.json` `scripts`'e `"dist": "npm run build && node scripts/dist.mjs",` ekle. Depo kökündeki `.gitignore`'a şunları ekle:

```
desktop/dist/
desktop/build/
```

- [ ] **Adım 5: Paketle ve doğrula (Windows)**

```bash
npm run dist
ls dist/Kadran-win32-x64/ dist/Kadran-win32-x64/resources/
```

Beklenen:
- `Kadran.exe`, `LICENSE`, `LICENSES.chromium.html` var.
- `resources/kadran.exe`, `resources/THIRD_PARTY_NOTICES.txt` ve `resources/app.asar` var.

Sonra:

```bash
npx asar list dist/Kadran-win32-x64/resources/app.asar | head -20
```

Beklenen: yalnız `/package.json` ve `/out/...`; `node_modules` yok.

- [ ] **Adım 6: Elle — paketlenmiş uygulama**

`dist/Kadran-win32-x64/Kadran.exe`'yi çalıştır. Bakılacaklar:
- Kayıtlı sunucular, geliştirme sürümündekilerle AYNI. Kullanıcı verisi dizini değişmemiş olmalı.
- Bir sunucuya bağlanıyor; sidecar `resources/kadran.exe`'den bulunuyor.

İlk açılışta SmartScreen uyarısı beklenir (imzasız).

- [ ] **Adım 7: Commit**

```bash
git add desktop/scripts/notices.mjs desktop/scripts/dist.mjs desktop/package.json desktop/package-lock.json desktop/test/licenses.test.ts .gitignore
git commit -m "feat(masaüstü): lisans kontrolü, üçüncü taraf bildirimleri, Windows paketi"
```

---

### Görev 19: Mutasyon betiği — korumalar gerçekten koruyor mu

**Dosyalar:**
- Yeni: `scripts/mutate-desktop.sh`

Betik CI'daki "Mutasyon" işinin döngüsünde koşacak; o iş her betikte `MUTANT DERLENM`, `KEZ ESLESTI` ve `TABAN KIRMIZI` dizelerini arar. Elektron gerekmez: bütün mutantlar birim testlerinde yakalanır.

- [ ] **Adım 1: Betiği yaz**

`scripts/mutate-desktop.sh`:

```bash
#!/usr/bin/env bash
# Masaüstünün korumalarını sınayan testlerin GERÇEKTEN bir şey koruduğunu
# ölçer (K-142).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
#   - Pencerenin güvenlik ayarları (sandbox, contextIsolation): bir XSS
#     renderer'dan Node'a geçer.
#   - İzinler: kamera, mikrofon, bildirim sessizce açılır.
#   - CSP: satır içi betik çalışır.
#   - Audit penceresi: "son kayıtlar" en ESKİ kayıtları gösterir.
#   - Dil dosyaları: Türkçesi eksik ekran ya da İngilizcede Türkçe.
#
# Hepsi birim düzeyinde ölçülüyor (npm test); Electron açılmıyor. Düzenek
# diğer betiklerle aynı: tek eşleşme şartı (K-127), yeşil taban şartı,
# derleme kapısı (K-096).
set -uo pipefail

cd "$(dirname "$0")/../desktop"

if [ ! -d node_modules ]; then
    echo "!! desktop/node_modules yok — önce 'npm ci' (ELECTRON_SKIP_BINARY_DOWNLOAD=1 yeterli). Ölçüm YAPILMADI"
    exit 1
fi

FILES=(
    src/main/window-options.ts
    src/main/permissions.ts
    src/main/ipc-validate.ts
    src/renderer/index.html
    src/renderer/i18n/tr.json
    src/renderer/i18n/en.json
    src/renderer/toast/Toast.tsx
    src/shared/appearance.ts
    src/renderer/lib/audit-window.ts
    src/renderer/lib/health.ts
    src/renderer/lib/fonts.ts
    src/renderer/lib/i18n.ts
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

compiles() { npx tsc --noEmit >/dev/null 2>&1 && npx tsc --noEmit -p tsconfig.renderer.json >/dev/null 2>&1; }
tests_pass() { node --test "test/*.test.ts" >/dev/null 2>&1; }

fail=0

# mutate_in <dosya> <ad> <python-ifadesi>
mutate_in() {
    local file="$1" name="$2" expr="$3"
    restore
    if ! python -c "
import io,sys
class _S(str):
    def replace(self,a,b,*r):
        # TEK eşleşme şart: aranan metin bir yorumda da geçiyorsa
        # replace(…,1) İLKİNİ değiştirir ve kod mutasyonsuz ölçülür (K-127).
        n=self.count(a)
        if n!=1:
            sys.stderr.write('REPLACE '+str(n)+' KEZ ESLESTI (1 olmali): '+repr(a[:70])+chr(10))
            sys.exit(8)
        return _S(str.replace(self,a,b,*r))
p='$file'
s=_S(io.open(p,encoding='utf-8').read())
o=s
$expr
if s==o:
    sys.exit(9)
io.open(p,'w',encoding='utf-8',newline='\n').write(s)
"; then
        echo "  !! MUTASYON UYGULANAMADI: $name — betik bozuk, ölçüm YAPILMADI"
        fail=1
        return
    fi

    # ── MUTANT DERLENMELİ ───────────────────────────────
    # Derlenmeyen bir mutant testleri düşürür ve betik bunu "yakalandı"
    # diye okur (K-096).
    if ! compiles; then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI"
        fail=1
        return
    fi

    if tests_pass; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! compiles || ! tests_pass; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda derleme ya da testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

echo "== Pencere ve izinler =="
mutate_in src/main/window-options.ts "sandbox kapalı" \
    "s=s.replace('sandbox: true,','sandbox: false,',1)"
mutate_in src/main/window-options.ts "contextIsolation kapalı" \
    "s=s.replace('contextIsolation: true,','contextIsolation: false,',1)"
mutate_in src/main/window-options.ts "Clear geçişi yeniden kurmuyor" \
    "s=s.replace('return !(live(from) && live(to));','return false;',1)"
mutate_in src/main/permissions.ts "her izin kontrolüne evet" \
    "s=s.replace('return ALLOWED_CHECKS.has(permission);','return true;',1)"
mutate_in src/main/permissions.ts "her izin isteğine evet" \
    "s=s.replace('export function allowPermissionRequest(_permission: string): boolean {\n  return false;','export function allowPermissionRequest(_permission: string): boolean {\n  return true;',1)"
mutate_in src/main/ipc-validate.ts "pencere eylemi beyaz listesi yok" \
    "s=s.replace('(WINDOW_ACTIONS as readonly string[]).includes(value)','value.length > 0',1)"

echo "== CSP ve HTML =="
mutate_in src/renderer/index.html "CSP'ye unsafe-inline" \
    "s=s.replace(\"script-src 'self';\",\"script-src 'self' 'unsafe-inline';\",1)"
mutate_in src/renderer/toast/Toast.tsx "bildirim HTML olarak işleniyor" \
    "s=s.replace('<p className=\"toast-text\">{t(message.key, message.params)}</p>','<p className=\"toast-text\" dangerouslySetInnerHTML={{ __html: t(message.key, message.params) }} />',1)"

echo "== Dil =="
mutate_in src/renderer/i18n/tr.json "Türkçede eksik anahtar" \
    "s=s.replace('  \"nav.audit\": \"Denetim\",\n','',1)"
mutate_in src/renderer/i18n/en.json "İngilizcede Türkçe metin" \
    "s=s.replace('\"nav.settings\": \"Settings\"','\"nav.settings\": \"Ayarlar\"',1)"
mutate_in src/renderer/lib/i18n.ts "çoğul ters" \
    "s=s.replace('=== 1 ? \"one\" : \"other\"','=== 1 ? \"other\" : \"one\"',1)"

echo "== Görünüm şeması =="
mutate_in src/shared/appearance.ts "bu sistemde olmayan mod kabul ediliyor" \
    "s=s.replace('mode: pick(o.mode, modesFor(info), d.mode),','mode: pick(o.mode, ALL_MODES, d.mode),',1)"
mutate_in src/shared/appearance.ts "tint sınırı yok" \
    "s=s.replace('tint: clampInt(o.tint, LIMITS.tint, d.tint),','tint: typeof o.tint === \"number\" ? o.tint : d.tint,',1)"

echo "== Ekran mantığı =="
mutate_in src/renderer/lib/audit-window.ts "son kayıtlar en eskiyi gösteriyor" \
    "s=s.replace('return { afterSeq: Math.max(0, latestSeq - limit), limit };','return { afterSeq: 0, limit };',1)"
mutate_in src/renderer/lib/health.ts "executor zincirinin kırılması görmezden geliniyor" \
    "s=s.replace('(verify.daemon_status === \"CHAIN_STATUS_INVALID\" || verify.executor_status === \"CHAIN_STATUS_INVALID\")','(verify.daemon_status === \"CHAIN_STATUS_INVALID\")',1)"
mutate_in src/renderer/lib/fonts.ts "italik yüzler kalınlık listesine giriyor" \
    "s=s.replace('faces.filter((f) => !f.italic).map((f) => f.weight)','faces.map((f) => f.weight)',1)"

restore
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutant yakalanmadı ya da ölçülemedi."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
```

- [ ] **Adım 2: Koş**

Depo kökünden:

```bash
bash -n scripts/mutate-desktop.sh
bash scripts/mutate-desktop.sh
```

Beklenen: her satır `yakalandı: …` ve en sonda `Bütün mutasyonlar yakalandı.`

Bir mutant `KEZ ESLESTI` ile düşerse iğne kaynakta bir kereden fazla (ya da hiç) geçiyor. İğneyi daha belirgin yap, testi değil.

- [ ] **Adım 3: Commit**

```bash
git add scripts/mutate-desktop.sh
git commit -m "test(masaüstü): mutasyon betiği — korumalar gerçekten koruyor mu"
```

---

### Görev 20: CI — masaüstü işleri ve mutasyon işine Node

**Dosyalar:**
- Değişir: `.github/workflows/ci.yml`

- [ ] **Adım 1: İki yeni iş ekle**

`jobs:` altına, `mutation:` işinden önce ekle:

```yaml
  # Masaüstü uygulaması (K-142). Bu işe kadar CI masaüstünü HİÇ koşmuyordu:
  # ne birim testleri ne pencerenin gerçekten açıldığı ölçülüyordu.
  desktop:
    name: Masaüstü (ubuntu)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: desktop
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "24"
          cache: npm
          cache-dependency-path: desktop/package-lock.json
      - run: npm ci
      - run: npm run typecheck
      - run: npm test
      - run: npm run build
      - run: npm run test:built
      - name: Çalışma zamanı bağımlılık zafiyetleri
        run: npm audit --omit=dev --audit-level=high
      # Electron'un Chromium kum havuzu yardımcısı SUID root olmalı;
      # --no-sandbox ile çalıştırmak ölçtüğümüz şeyi gevşetirdi.
      - name: Electron kum havuzu ve sanal ekran
        run: |
          sudo apt-get update -qq
          sudo apt-get install -y -qq xvfb libnss3 libatk-bridge2.0-0 libgtk-3-0 libgbm1 libasound2t64
          sudo chown root:root node_modules/electron/dist/chrome-sandbox
          sudo chmod 4755 node_modules/electron/dist/chrome-sandbox
      - name: Uçtan uca (sahte sidecar)
        run: xvfb-run -a npm run e2e

  desktop-windows:
    name: Masaüstü (windows)
    runs-on: windows-latest
    defaults:
      run:
        working-directory: desktop
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/setup
        with:
          go-version: ${{ env.GO_VERSION }}
          buf-version: ${{ env.BUF_VERSION }}
      - uses: actions/setup-node@v4
        with:
          node-version: "24"
          cache: npm
          cache-dependency-path: desktop/package-lock.json
      - run: npm ci
      - run: npm run build
      - name: Uçtan uca (sahte sidecar)
        run: npm run e2e
      - name: Paket (imzasız)
        run: npm run dist
      - name: Paketin içeriği
        shell: bash
        run: |
          set -euo pipefail
          d=dist/Kadran-win32-x64
          for f in Kadran.exe resources/kadran.exe resources/THIRD_PARTY_NOTICES.txt resources/app.asar; do
            test -f "$d/$f" || { echo "::error::pakette yok: $f"; exit 1; }
          done
          if npx asar list "$d/resources/app.asar" | grep -q node_modules; then
            echo "::error::app.asar node_modules taşıyor"; exit 1
          fi
```

Not: `./.github/actions/setup` bileşik eylemi `buf generate` çalıştırıyor. `desktop-windows` işinde Go yalnız sidecar'ı derlemek için gerekli. Eylem Windows'ta çalışmazsa (bileşik eylem bash kullanıyorsa sorun olmaz, ama doğrula) yerine `actions/setup-go@v5` + `go-version: ${{ env.GO_VERSION }}` ve ayrı bir `buf generate` adımı koy. Sidecar `cmd/kadran`'ı derlerken üretilmiş protobuf koduna ihtiyaç duyar.

- [ ] **Adım 2: Mutasyon işine Node ekle**

`mutation:` işinde `actions/setup-python@v5` adımından sonra ekle:

```yaml
      # mutate-desktop.sh birim testlerini node ile koşuyor; Electron
      # ikilisi gerekmiyor (indirme atlanıyor).
      - uses: actions/setup-node@v4
        with:
          node-version: "24"
          cache: npm
          cache-dependency-path: desktop/package-lock.json
      - name: Masaüstü bağımlılıkları
        working-directory: desktop
        env:
          ELECTRON_SKIP_BINARY_DOWNLOAD: "1"
        run: npm ci
```

- [ ] **Adım 3: Yerelde sözdizimi**

```bash
python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml', encoding='utf-8')); print('yaml tamam')"
```

Beklenen: `yaml tamam`. PyYAML yoksa `pip install pyyaml` ile kur.

- [ ] **Adım 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: masaüstü işleri (ubuntu + windows) ve mutasyon işine Node"
```

CI'ın sonucu ancak dal itilince görülür. PR #10 birleşmeden bu dal itilmez (Genel kısıtlar). İtilince bu iki iş ve mutasyon işi izlenir; kırmızı gelirse günlük okunup düzeltilir.

---

### Görev 21: Belgeler — K-142, K-141 4. aşama, README, CHANGELOG, NOTICE

**Dosyalar:**
- Değişir: `docs/decisions.md` (K-141 durum satırı ve 4. aşama bölümü; yeni K-142), `README.md`, `CHANGELOG.md`, `NOTICE`

- [ ] **Adım 1: K-142'yi yaz**

`docs/decisions.md`'nin sonuna ekle. Aşağıdaki iskelet, uygulama boyunca ÖLÇÜLEN değerlerle doldurulur; ölçülmemiş bir sayı yazılmaz:
- test sayısı;
- mutant sayısı;
- uçtan uca yolculuğun adımları;
- paketin boyutu.

```markdown
## K-142 — Masaüstü: cam tasarım, React, izinler, dil seçimi

**Tarih:** 7 Ekim 2026
**Durum:** 1. PARÇA KOD BİTTİ (dal `masaustu-temel`). Tasarım: `docs/specs/2026-10-07-masaustu-temel-design.md`.

### Neden

Kullanıcı masaüstünü baştan istedi: pencere cam gibi (arkadaki masaüstü görünür),
ayarları kendisi yapabilecek, CLI'ın yaptığı her şeyi yapabilecek. İş dört parçaya
bölündü; bu kayıt 1. parçayı (temel + cam tasarım) kapsar.

### Karar

- **Electron kalıyor.** Liquid glass'ın kenar kırılması webde yalnız Chromium'da var
  (SVG yer değiştirme süzgeci + backdrop-filter). Tauri/Wails sistemin web motorunu
  kullanır; macOS ve Linux'ta kırılma olmaz. Bedel: paket ~100 MB.
- **React + Vite + Motion,** mevcut kabuğun içinde. Güvenlik duruşu (sandbox,
  contextIsolation, CSP, dar köprü) ve sidecar istemcisi korundu.
- **Pencere modları:**
  - Windows 11: Clear (transparent) / Acrylic / Mica (backgroundMaterial) / Solid.
  - macOS: Vibrancy.
  - Linux: Clear ve Solid.
  - Clear ↔ diğerleri pencereyi yeniden kurar; saydamlık sonradan açılıp kapanamıyor.
- **İzinler kapandı.** Bugüne kadar Electron her izni veriyordu. Artık yalnız KONTROL
  işleyicisinde `local-fonts` açık. Ölçüldü: Chromium yazı tipi listesini bu adla ve
  kontrol kanalından soruyor; reddedilince boş liste dönüyor. Pano izni yok; kopyalama
  ana süreçte (`copyText`).
- **Dil seçimi (K-141'in "dil seçimi yok" kararı masaüstü için değişti).** Kullanıcı
  istedi; varsayılan İngilizce, Türkçe Settings'te. Test matrisi büyümüyor: `en` ve `tr`
  anahtar eşitliği ve İngilizcede Türkçe harf yokluğu birim testinde. CLI ve sunucu
  İngilizce kalır; sunucudan gelen hata Türkçe arayüzde de İngilizce görünür.
- **Coolify'ın düzeninden** üst çubuk, Overview şeridi, Live refresh, altı çizili bölüm
  başlıkları ve sonuç çubuğu alındı. Kod, logo ve marka rengi alınmadı (Apache-2.0;
  kullanıcı izin aldığını söyledi).

### Ölçülen bir hata

Eski arayüz "son denetim kayıtları" diye `after_seq = 0, limit = 50` istiyordu. Sunucu
`after_seq`'i hariç tutup artan sırada döndüğü için ekranda EN ESKİ 50 kayıt vardı;
zincir 50'yi geçince yeni kayıtlar hiç görünmüyordu. Yeni arayüz önce `latest_seq`'i
okuyor. Hem birim testi hem uçtan uca yolculuk `latest_seq = 1284` → 1284…1275 ölçüyor.
Mutant `afterSeq: 0` iki yerde de yakalanıyor.

### Kanıt

(Uygulama bittiğinde ölçülen değerlerle doldurulur: `npm test` sayısı; `npm run e2e`
yolculuğu; `scripts/mutate-desktop.sh` mutant sayısı; CI işleri; paket içeriği.)

### Kapsam dışı

2–4. parçalar; logo (grafik tasarımcı); kurulum sihirbazı, imza, otomatik güncelleme;
macOS ve Linux'ta görsel doğrulama.
```

- [ ] **Adım 2: K-141'i güncelle**

`docs/decisions.md`'de K-141'in `**Durum:**` satırını şöyle yap:

```markdown
**Durum:** 1. AŞAMA PR #8 (CI 10/10, mutasyon 27/27), 2. AŞAMA PR #9 (`fa4f77a`)
birleşti. 3. AŞAMA PR #10. 4. AŞAMA K-142 ile (masaüstü, dal `masaustu-temel`).
5. aşama sırada.
```

K-141'in sonuna, 3. aşama bölümünden sonra şunu ekle:

```markdown
### 4. aşama: masaüstü

Masaüstü yeniden yazıldı (K-142). Arayüzün metni `src/renderer/i18n/en.json`
(varsayılan) ve `tr.json`'da. Kullanıcıya ulaşan ana süreç hataları (sidecar
istemcisi, profiller, IPC doğrulaması) İngilizce; konsol tanılamaları Türkçe kaldı.

Korumalar:

- `test/i18n.test.ts`: iki dosyanın anahtarları ve yer tutucuları aynı, İngilizcede
  Türkçe harf yok, koddaki her `t("…")` anahtarı var.
- `test/no-raw-text.test.ts`: JSX'te çeviri dışı görünür metin yok (TypeScript AST).
- `test/errors-english.test.ts`: `new …Error(…)` iletileri İngilizce.

Hepsi KIRMIZI ölçüldü; mutasyon betiği iki dil mutantını yakalıyor.

Bu aşamanın kararı K-141'in "yalnız İngilizce" kuralını masaüstü için değiştirdi;
gerekçe K-142'de.
```

- [ ] **Adım 3: README, CHANGELOG, NOTICE**

`README.md`:
- 591. satır civarındaki "The desktop app, the server's journal lines and alarm texts are being translated." cümlesini şu yap: "The desktop app is English by default and can switch to Turkish. The server's journal lines and alarm texts are being translated."
- 625. satırdaki `desktop/             Electron + React` artık doğru. Yanına `+ Vite, glass window modes` ekle.
- 589. satırdaki "The desktop app is read-only" cümlesi DEĞİŞMEZ; 1. parçada doğru.

`CHANGELOG.md` → `## Unreleased` → `### Added` altına (yoksa bölümü aç):

```markdown
- **New desktop app (K-142).** A glass window — Clear, Acrylic, Mica or Vibrancy, with
  the desktop showing through — and Appearance settings you tune yourself: tint, panel
  glass, edge highlight, hover glow, accent, theme, interface and code fonts (any
  installed font; JetBrains Mono ships with the app). Status gets an overview strip with
  live sparklines, a health pill and a server switcher; Audit gets ranges, filters, an
  outcome bar and "load older". English by default, Turkish in Settings.
- **The desktop app asks for no permissions** except the installed-font list. Before
  this release Electron granted every permission request.
```

`### Fixed` altına:

```markdown
- **Desktop "recent audit records" showed the oldest records.** It asked for the first
  50 records after sequence 0; once a chain had more than 50 records, new ones never
  appeared. It now reads the latest sequence first.
```

`NOTICE`'un sonuna:

```
The desktop app bundles:
- Animated icons from pqoqubbw/icons (MIT) drawn from Lucide (ISC); see
  desktop/src/renderer/icons/LICENSE.
- JetBrains Mono (SIL Open Font License 1.1); see desktop/src/renderer/fonts/OFL.txt.
- React, React DOM, scheduler (MIT), Motion (MIT) and tslib (0BSD), listed with their
  license texts in THIRD_PARTY_NOTICES.txt inside the packaged app.
```

- [ ] **Adım 4: Son doğrulama**

```bash
cd desktop
npm run typecheck
npm test
npm run build
npm run test:built
npm run e2e
cd ..
bash scripts/mutate-desktop.sh
export PATH="/c/Program Files/Go/bin:$PATH"
go test ./cmd/kadran -run TestSidecarFixtures -count=1
```

Beklenen: hepsi yeşil. K-142'nin "Kanıt" bölümünü bu çıktılardaki gerçek sayılarla doldur: test sayısı, mutant sayısı, e2e çıktısı.

- [ ] **Adım 5: Commit**

```bash
git add docs/decisions.md README.md CHANGELOG.md NOTICE
git commit -m "docs: K-142 masaüstü, K-141 4. aşama, README, CHANGELOG, NOTICE"
```

---

## Elle kontrol listesi (PR'da işaretlenir)

- [ ] Windows 11'de Acrylic ve Mica gerçekten cam görünüyor.
- [ ] Clear modda kenardan boyutlandırma çalışıyor. Büyüt/eski boyut düğmesi doğru davranıyor; saydam çerçevesiz pencerede `isMaximized` yanlış rapor edebilir.
- [ ] Clear ↔ Acrylic geçişinde pencere yerinde ve boyutunda kalıyor, büyütülmüşse büyütülmüş kalıyor, Settings açık kalıyor.
- [ ] JetBrainsMono Nerd Font Medium kod yazı tipi olarak seçilebiliyor ve hash'ler onunla çiziliyor.
- [ ] Klavyeyle (Tab) gezinmede her öğede odak halkası görünüyor; glow %0'da bile.
- [ ] Paketlenmiş `Kadran.exe` geliştirme sürümüyle aynı kayıtlı sunucuları görüyor ve bir sunucuya bağlanıyor.
- [ ] Türkçe arayüzde uzun bir sunucu hatası bildirimde tam ve satır kırarak görünüyor.
