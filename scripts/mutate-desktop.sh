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
    src/renderer/lib/single-flight.ts
    src/renderer/lib/serial.ts
    src/renderer/lib/profile-store.ts
    src/renderer/lib/audit-view.ts
    src/renderer/lib/verification-state.ts
    src/main/management-core.ts
    src/main/management-ipc.ts
    src/main/management-results.ts
    src/main/management-confirmation.ts
    src/main/management-payload.ts
)
BAKDIR=$(mktemp -d)
bak() { printf '%s/%s' "$BAKDIR" "${1//\//__}"; }
for f in "${FILES[@]}"; do cp "$f" "$(bak "$f")"; done
restore() { for f in "${FILES[@]}"; do cp "$(bak "$f")" "$f"; done; }
trap restore EXIT

compiles() { npx tsc --noEmit >/dev/null 2>&1 && npx tsc --noEmit -p tsconfig.renderer.json >/dev/null 2>&1; }
# Test başına süre sınırı: bir mutant testi ASIP bırakırsa (bekleyen bir
# söz hiç çözülmezse) ölçüm saatlerce takılmasın; asılı test kırmızıdır.
tests_pass() { node --test --test-timeout=120000 "test/*.test.ts" >/dev/null 2>&1; }

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

echo "== Güvenlik düzeltmeleri =="
mutate_in src/renderer/lib/audit-window.ts "bozuk latest_seq kabul ediliyor" \
    "s=s.replace(' || !/^(0|[1-9][0-9]*)\$/.test(raw)','',1)"
mutate_in src/renderer/lib/audit-window.ts "eksik en yeni sayfa kabul ediliyor" \
    "s=s.replace('return records.some((r) => r.seq === head);','return true;',1)"
mutate_in src/renderer/lib/health.ts "bayat veri Healthy gösteriyor" \
    "s=s.replace('if (!stale || !status) return h;','return h;',1)"
mutate_in src/renderer/lib/health.ts "bayatken kötü haber gizleniyor" \
    "s=s.replace(': [\"status.meta_stale\", \"status.meta_down\"]',': [\"status.meta_stale\"]',1)"
mutate_in src/renderer/lib/single-flight.ts "aynı sunucuya üst üste istek" \
    "s=s.replace('if (existing) return existing;','void existing;',1)"
mutate_in src/renderer/lib/serial.ts "profil yazmaları sırasız" \
    "s=s.replace('const result = tail.then(job, job);','const result = job();',1)"
mutate_in src/renderer/lib/profile-store.ts "diskten okumadan yazıyor" \
    "s=s.replace('current ?? apply(await io.load());','current ?? [];',1)"
mutate_in src/renderer/lib/audit-view.ts "yükleme hatası 'kayıt yok' gösteriyor" \
    "s=s.replace('if (failed) return \"audit.load_failed\";','void failed;',1)"
mutate_in src/main/ipc-validate.ts "'-' ile başlayan hedef kabul ediliyor" \
    "s=s.replace('trimmed.startsWith(\"-\")','trimmed.startsWith(\"--\")',1)"

echo "== Yönetim yüzeyi (ana süreç onayı) =="
# Her durum değiştiren kanalın onay çağrısı AYRI ayrı silinir: biri unutulursa
# öbürlerinin testi onu örtemez (son inceleme C1).
for kind in 'create|{ kind: \"create\", target, spec }' \
            'update|{ kind: \"update\", target, app: app_id, update }' \
            'deploy|{ kind: \"deploy\", target, app: app_id, ref }' \
            'delete|{ kind: \"delete\", target, app: app_id }' \
            'rollback|{ kind: \"rollback\", target, app: app_id }' \
            'backup|{ kind: \"backup\", target }'; do
    name="${kind%%|*}"
    arg="${kind#*|}"
    mutate_in src/main/management-core.ts "$name işleminin yerel onayı silinmiş" \
        "s=s.replace('if (!(await confirm(event, $arg))) return cancelled();','void 0;',1)"
done
mutate_in src/main/management-core.ts "iptal düğmesi onay sayılıyor" \
    "s=s.replace('return response === CONFIRM_BUTTON;','return true;',1)"
mutate_in src/main/management-core.ts "fake@test koşulu gevşetilmiş (her hedefte atlanıyor)" \
    "s=s.replace('if (deps.isFixture && request.target === \"fake@test\") return true;','if (deps.isFixture) return true;',1)"
mutate_in src/main/management-ipc.ts "requireConfirmation her şeyi kabul ediyor" \
    "s=s.replace('if (value !== appId)','if (false)',1)"
mutate_in src/main/management-ipc.ts "kapı devre dışı (aynı işi iki kez başlatır)" \
    "s=s.replace('if (active.has(key)) throw new Error(\"an operation for this app is already running\");','void key;',1)"
# IPC süresi sidecar'ınkinden kısaysa masaüstü önce vazgeçer, kapı iş
# sunucuda sürerken serbest kalır ve ikinci bir geri alma başlatılabilir.
mutate_in src/main/management-core.ts "geri alma/silme IPC süresi sidecar'dan kısa (eski 2 dk)" \
    "s=s.replace('const LIFECYCLE_TIMEOUT_MS = 4 * 60_000 + SIDECAR_MARGIN_MS;','const LIFECYCLE_TIMEOUT_MS = 2 * 60_000;',1)"
mutate_in src/main/management-core.ts "IPC payı sıfır (sidecar ile aynı anda düşer)" \
    "s=s.replace('const SIDECAR_MARGIN_MS = 60_000;','const SIDECAR_MARGIN_MS = 0;',1)"
mutate_in src/main/management-core.ts "geri alma okuma süresiyle çağrılıyor" \
    "s=s.replace('return invoke(\"apps.rollback\", target, { app_id, confirmation }, LIFECYCLE_TIMEOUT_MS);','return invoke(\"apps.rollback\", target, { app_id, confirmation });',1)"
mutate_in src/main/management-results.ts "validateManagementResult her şeyi geçiriyor" \
    "s=s.replace('if (!valid)','if (false)',1)"
mutate_in src/renderer/lib/verification-state.ts "başarısız doğrulama kırık zincir kanıtını siliyor" \
    "s=s.replace('? previous : null','? null : null',1)"
mutate_in src/main/management-confirmation.ts "update kutusuna env DEĞERİ giriyor" \
    "s=s.replace('Object.keys(record(u.env))','Object.entries(record(u.env)).map(([k, v]) => k + \"=\" + String(v))',1)"
mutate_in src/main/management-payload.ts "yasak bağlama kökü /dev kabul ediliyor" \
    "s=s.replace('[\"/proc\", \"/sys\", \"/dev\"]','[\"/proc\", \"/sys\"]',1)"
mutate_in src/main/ipc-validate.ts "hedefte biçim karakteri (bidi) kabul ediliyor" \
    "s=s.replace('[\\\\p{Cc}\\\\p{Cf}]','[\\\\p{Cc}]',1)"

restore
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutant yakalanmadı ya da ölçülemedi."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
