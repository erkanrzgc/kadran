#!/usr/bin/env bash
# Masaüstünün korumalarını sınayan testlerin GERÇEKTEN bir şey koruduğunu
# ölçer (K-142, K-165).
#
# ── Neyin bozulması EN PAHALI ───────────────────────────────────────
#
#   - Güven sınırı (Go, desktop/internal): doğrulama, yerel onay, işlem
#     kapısı, hata arıtma. Ele geçmiş bir arayüz sunucuya istediğini yaptırır.
#   - İzinler: kamera, mikrofon, konum sessizce açılır (Permissions-Policy).
#   - CSP: satır içi betik çalışır.
#   - Audit penceresi: "son kayıtlar" en ESKİ kayıtları gösterir.
#   - Dil dosyaları: Türkçesi eksik ekran ya da İngilizcede Türkçe.
#
# İki ölçüm: arayüz (TypeScript, npm test) ve masaüstünün Go paketleri
# (go test ./internal/...). İkisi de birim düzeyinde; pencere açılmıyor.
# Düzenek diğer betiklerle aynı: tek eşleşme şartı (K-127), yeşil taban
# şartı, derleme kapısı (K-096).
#
# kapsam: desktop/
set -uo pipefail

cd "$(dirname "$0")/../desktop"

if [ ! -d node_modules ]; then
    echo "!! desktop/node_modules yok — önce 'npm ci'. Ölçüm YAPILMADI"
    exit 1
fi

FILES=(
    src/renderer/index.html
    src/renderer/i18n/tr.json
    src/renderer/i18n/en.json
    src/renderer/toast/Toast.tsx
    src/renderer/styles/tokens.css
    src/renderer/styles/layout.css
    src/renderer/styles/base.css
    src/renderer/styles/controls.css
    src/renderer/lib/theme-switch.ts
    src/renderer/lib/nav-slider.ts
    src/renderer/lib/fleet.ts
    src/renderer/lib/providers.ts
    src/renderer/lib/audit-window.ts
    src/renderer/lib/health.ts
    src/renderer/lib/fonts.ts
    src/renderer/lib/i18n.ts
    src/renderer/lib/single-flight.ts
    src/renderer/lib/serial.ts
    src/renderer/lib/profile-store.ts
    src/renderer/lib/audit-view.ts
    src/renderer/lib/verification-state.ts
    src/renderer/lib/management-results.ts
    src/renderer/lib/resources.ts
    src/renderer/lib/resource-chart.ts
    src/renderer/lib/format.ts
    internal/guard/ipc.go
    internal/guard/payload.go
    internal/confirm/confirm.go
    internal/confirm/box.go
    internal/redact/redact.go
    internal/appearance/appearance.go
    internal/appearance/look.go
    internal/win/policy.go
    internal/headers/headers.go
    internal/bridge/bridge.go
    internal/bridge/management.go
    internal/bridge/servers.go
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

go_compiles() { go vet ./internal/... >/dev/null 2>&1; }
go_tests_pass() { go test -count=1 -timeout 120s ./internal/... >/dev/null 2>&1; }

# mutate_in <dosya> <ad> <python-ifadesi>: arayüz (TypeScript) ölçümü.
mutate_in() { HARNESS=ts mutate "$@"; }
# mutate_go <dosya> <ad> <python-ifadesi>: masaüstünün Go paketleri.
mutate_go() { HARNESS=go mutate "$@"; }

mutate() {
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
    local build=compiles pass=tests_pass
    if [ "$HARNESS" = go ]; then build=go_compiles pass=go_tests_pass; fi
    if ! "$build"; then
        echo "  !! MUTANT DERLENMİYOR: $name — ölçüm YAPILMADI"
        fail=1
        return
    fi

    if "$pass"; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

# Taban YEŞİL olmalı: kırmızı bir taban her mutantı "yakalandı" gösterirdi.
if ! compiles || ! tests_pass || ! go_compiles || ! go_tests_pass; then
    echo "!! TABAN KIRMIZI: mutasyonsuz kodda derleme ya da testler düşüyor — ölçüm YAPILMADI"
    exit 1
fi

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

echo "== Cam yalnız çerçevede (stil) =="
mutate_in src/renderer/styles/tokens.css "malzemesiz sistemde çerçeve saydam kalıyor" \
    "s=s.replace(':root[data-material=\"none\"] { --shell: var(--frame-solid); }\n','',1)"
mutate_in src/renderer/styles/tokens.css "saydamlık kapalıyken çerçeve cam kalıyor" \
    "s=s.replace('  :root { --shell: var(--frame-solid); }\n','',1)"
mutate_in src/renderer/styles/layout.css "içerik paneli saydam (masaüstü içerikte görünüyor)" \
    "s=s.replace('.page-host > .stage { background: var(--sheet);','.page-host > .stage { background: transparent;',1)"
mutate_in src/renderer/styles/layout.css "kapat düğmesi köşeden içeride (boşluk üst çubuğu da kapsıyor)" \
    "s=s.replace('.main { flex: 1; min-width: 0; display: flex; flex-direction: column; }','.main { flex: 1; min-width: 0; display: flex; flex-direction: column; padding: 0 var(--frame-gap) var(--frame-gap) 0; }',1)"

echo "== Tema geçişi =="
mutate_in src/renderer/lib/theme-switch.ts "tema kaydı bekliyor (cam arayüzden geç döner)" \
    "s=s.replace('return next.theme !== prev.theme ? 0 : SAVE_DELAY_MS;','return SAVE_DELAY_MS;',1)"
mutate_in src/renderer/lib/theme-switch.ts "geçişte animasyonlar kapanmıyor (ekran parça parça döner)" \
    "s=s.replace('  root.classList.add(SWITCHING_CLASS);\n','',1)"
mutate_in src/renderer/lib/theme-switch.ts "animasyonlar hiç geri açılmıyor" \
    "s=s.replace('  later(() => root.classList.remove(SWITCHING_CLASS));\n','',1)"
mutate_in src/renderer/lib/theme-switch.ts "renkler animasyonsuz hesaplanmadan sınıf kalkıyor" \
    "s=s.replace('  settle();\n','',1)"
mutate_in src/renderer/styles/base.css "geçiş sınıfı animasyonu kapatmıyor" \
    "s=s.replace(':root.theme-switching *::after { transition: none !important; }',':root.theme-switching *::after { }',1)"
mutate_in src/renderer/styles/layout.css "seçili satırın camı ilk açılışta da kayıyor" \
    "s=s.replace('.nav-pill { position: absolute; top: 0;','.nav-pill { transition: transform 220ms; position: absolute; top: 0;',1)"
mutate_in src/renderer/styles/layout.css "satır kendi zeminini alıyor (gösterge ile iki zemin)" \
    "s=s.replace('.nav-item:not(:disabled):hover { color: var(--fg); }','.nav-item:not(:disabled):hover { color: var(--fg); background: var(--flat); }',1)"
mutate_in src/renderer/lib/nav-slider.ts "gösterge kabın kenarlığını düşmüyor (panelde 1 px kayık)" \
    "s=s.replace('x: item.left - container.left - border.left + scroll.left,','x: item.left - container.left + scroll.left,',1)"
mutate_in src/renderer/lib/nav-slider.ts "gösterge kaydırılmış kapta içerikle kaymıyor" \
    "s=s.replace('y: item.top - container.top - border.top + scroll.top,','y: item.top - container.top - border.top,',1)"
mutate_in src/renderer/styles/controls.css "düğme basınca yine aşağı iniyor" \
    "s=s.replace('.btn:not(:disabled):active { filter: brightness(0.94); }','.btn:not(:disabled):active { transform: translateY(2px); }',1)"
mutate_in src/renderer/styles/layout.css "KADRAN panelle hizasız (şerit yüksekliği kadar değil)" \
    "s=s.replace('padding: 48px 16px 0;','padding: 0 16px;',1)"

echo "== Filo doğruluğu =="
mutate_in src/renderer/lib/fleet.ts "bozuk kurulum yeşil görünüyor" \
    "s=s.replace('state === \"unreachable\" || isBrokenInstall(data.status)','state === \"unreachable\"',1)"
mutate_in src/renderer/lib/fleet.ts "okunamayan alarm listesi sağlıklı sayılıyor" \
    "s=s.replace(' && data.alarms !== undefined && data.alarms.alarms.length === 0',' && (data.alarms?.alarms.length ?? 0) === 0',1)"
mutate_in src/renderer/lib/fleet.ts "okunamayan sunucu toplu listede gizleniyor" \
    "s=s.replace('unread: cards.filter((c) => c.state !== \"loading\" && c[list] === null).map((c) => c.profile.name),','unread: [],',1)"
mutate_in src/renderer/lib/fleet.ts "adında totp geçen sunucu giriş istiyor sanılıyor" \
    "s=s.replace('message.includes(\"kadran login\")','/totp|kadran login/i.test(message)',1)"
mutate_in src/renderer/lib/providers.ts "__proto__ sağlayıcı nesneye düşüyor" \
    "s=s.replace('const known = PROVIDERS.get(provider);','const known = (Object.fromEntries(PROVIDERS) as Record<string, { name: string; color: string }>)[provider];',1)"

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
mutate_in src/renderer/lib/profile-store.ts "odakta yeniden okumuyor (CLI'ın eklediği görünmez)" \
    "s=s.replace('reload: () => serial(read),','reload: () => serial(async () => { if (!loaded) await read(); }),',1)"
mutate_in src/renderer/lib/profile-store.ts "CLI'da silinmiş sunucu kaldırılınca kart kalıyor" \
    "s=s.replace('if (serverListReason(err) !== \"not_found\") throw err;','if (serverListReason(err) !== \"not_found_x\") throw err;',1)"
mutate_in src/renderer/lib/profile-store.ts "CLI'da silinmiş sunucu düzenlenince liste tazelenmiyor" \
    "s=s.replace('if (serverListReason(err) === \"not_found\") await read()','if (serverListReason(err) === \"not_found_x\") await read()',1)"
mutate_in src/renderer/lib/audit-view.ts "yükleme hatası 'kayıt yok' gösteriyor" \
    "s=s.replace('if (failed) return \"audit.load_failed\";','void failed;',1)"

echo "== Yönetim yanıtları (arayüz) =="
mutate_in src/renderer/lib/management-results.ts "validateManagementResult her şeyi geçiriyor" \
    "s=s.replace('if (!valid)','if (false)',1)"
# Sidecar'ın iptal taklidi: sunucuda koşmuş iş "vazgeçildi" diye gösterilir.
mutate_in src/renderer/lib/management-results.ts "sidecar'ın {cancelled:true} yanıtı iptal sayılıyor" \
    "s=s.replace('  let valid = false;','  if (JSON.stringify(value) === JSON.stringify({ cancelled: true })) return value;\n  let valid = false;',1)"
mutate_in src/renderer/lib/verification-state.ts "başarısız doğrulama kırık zincir kanıtını siliyor" \
    "s=s.replace('? previous : null','? null : null',1)"

echo "== Güven sınırı: girdi doğrulama (Go) =="
# İğnelerdeki ters bölü chr(92), dolar chr(36) ile kuruluyor: kabuk/Python
# kaçış katmanlarına takılmasın.
mutate_go internal/guard/ipc.go "hedefte biçim karakteri (bidi) kabul ediliyor" \
    "s=s.replace(chr(92)+'p{Cc}'+chr(92)+'p{Cf}]',chr(92)+'p{Cc}]',1)"
mutate_go internal/guard/ipc.go "'-' ile başlayan hedef kabul ediliyor" \
    "s=s.replace('if strings.HasPrefix(trimmed, \"-\") {','if strings.HasPrefix(trimmed, \"--\") {',1)"
mutate_go internal/guard/ipc.go "pencere eylemi beyaz listesi yok" \
    "s=s.replace('if value == a {','if value == a || value != \"\" {',1)"
mutate_go internal/guard/ipc.go "onay metni her şeyi kabul ediyor" \
    "s=s.replace('if value != appID {','if false && value != appID {',1)"
mutate_go internal/guard/payload.go "update'te null skaler geçiyor (kutu ile sunucu farklı anlar)" \
    "s=s.replace('if update.Has(key) && field(update, key) == nil {','if false && update.Has(key) && field(update, key) == nil {',1)"
mutate_go internal/guard/payload.go "yasak bağlama kökü /dev kabul ediliyor" \
    "s=s.replace('[]string{\"/proc\", \"/sys\", \"/dev\"}','[]string{\"/proc\", \"/sys\"}',1)"
mutate_go internal/guard/payload.go "yükte bilinmeyen alan geçiyor" \
    "s=s.replace('if !slices.Contains(fields, key) {','if false && !slices.Contains(fields, key) {',1)"

echo "== Güven sınırı: yerel onay ve kapı (Go) =="
mutate_go internal/bridge/management.go "onay kutusu atlanıyor" \
    "s=s.replace('ok, err := b.confirmed(req)','ok, err := true, error(nil)',1)"
mutate_go internal/bridge/management.go "vazgeç onay sayılıyor" \
    "s=s.replace('if !ok {','if false && !ok {',1)"
mutate_go internal/bridge/management.go "fake@test koşulu gevşetilmiş (her hedefte atlanıyor)" \
    "s=s.replace('if b.deps.Fixture && req.Target == fixtureTarget {','if b.deps.Fixture {',1)"
mutate_go internal/bridge/management.go "tek kutu sınırı devre dışı (kutu yağmuru)" \
    "s=s.replace('if b.dialogOpen {','if b.dialogOpen && false {',1)"
mutate_go internal/bridge/management.go "kapı devre dışı (aynı işi iki kez başlatır)" \
    "s=s.replace('if b.active[key] {','if false && b.active[key] {',1)"
# Her durum değiştiren yöntemin onaylı akışı AYRI ayrı atlatılır: biri
# doğrudan dağıtıcıya giderse öbürlerinin testi onu örtemez.
mutate_go internal/bridge/management.go "oluşturma onaysız gidiyor" \
    "s=s.replace('return b.mutate(app, confirm.Request{Kind: confirm.KindCreate, Target: t, Spec: s}, \"apps.create\",','_ = app; return b.dispatch(\"apps.create\",',1)"
mutate_go internal/bridge/management.go "güncelleme onaysız gidiyor" \
    "s=s.replace('return b.mutate(id, confirm.Request{Kind: confirm.KindUpdate, Target: t, App: id, Update: u}, \"apps.update\",','return b.dispatch(\"apps.update\",',1)"
mutate_go internal/bridge/management.go "dağıtım onaysız gidiyor" \
    "s=s.replace('return b.mutate(id, confirm.Request{Kind: confirm.KindDeploy, Target: t, App: id, Ref: r}, \"apps.deploy\",','return b.dispatch(\"apps.deploy\",',1)"
mutate_go internal/bridge/management.go "silme ve geri alma onaysız gidiyor" \
    "s=s.replace('return b.mutate(id, confirm.Request{Kind: kind, Target: t, App: id}, method,','_ = kind; return b.dispatch(method,',1)"
mutate_go internal/bridge/management.go "yedek onaysız gidiyor" \
    "s=s.replace('return b.mutate(\"'+chr(36)+'backup\", confirm.Request{Kind: confirm.KindBackup, Target: t}, \"backups.create\",','return b.dispatch(\"backups.create\",',1)"
mutate_go internal/bridge/bridge.go "arayüze beyaz listede olmayan yöntem açılıyor" \
    "s=s+chr(10)+'func (b *Bridge) Debug() string { return \"x\" }'+chr(10)"
mutate_go internal/bridge/bridge.go "cam diske yazıldıktan sonra değişiyor (geç döner)" \
    "s=s.replace('	b.deps.Host.SetLook(appearance.Look(appearance.Sanitize(raw).Theme, b.deps.Material()))'+chr(10)+'	return b.deps.Settings.Save(raw)','	saved, err := b.deps.Settings.Save(raw)'+chr(10)+'	b.deps.Host.SetLook(appearance.Look(appearance.Sanitize(raw).Theme, b.deps.Material()))'+chr(10)+'	return saved, err',1)"

echo "== Güven sınırı: onay kutusunun metni (Go) =="
mutate_go internal/confirm/box.go "update kutusuna env DEĞERİ giriyor" \
    "s=s.replace('if keys := record(get(u, \"env\")).Keys(); len(keys) > 0 {','if keys := withValues(record(get(u, \"env\"))); len(keys) > 0 {',1); s=s+chr(10)+'func withValues(o *jsonv.Object) []string {'+chr(10)+'	var out []string'+chr(10)+'	for _, k := range o.Keys() {'+chr(10)+'		v, _ := o.Get(k)'+chr(10)+'		out = append(out, k+\"=\"+jsString(v))'+chr(10)+'	}'+chr(10)+'	return out'+chr(10)+'}'+chr(10)"
mutate_go internal/confirm/box.go "oluşturma kutusuna derleme argümanı DEĞERİ giriyor" \
    "s=s.replace('if keys := record(get(s, \"build_args\")).Keys(); len(keys) > 0 {','if keys := withValues(record(get(s, \"build_args\"))); len(keys) > 0 {',1); s=s+chr(10)+'func withValues(o *jsonv.Object) []string {'+chr(10)+'	var out []string'+chr(10)+'	for _, k := range o.Keys() {'+chr(10)+'		v, _ := o.Get(k)'+chr(10)+'		out = append(out, k+\"=\"+jsString(v))'+chr(10)+'	}'+chr(10)+'	return out'+chr(10)+'}'+chr(10)"
mutate_go internal/confirm/box.go "oluşturma kutusunda kopya sayısı yok" \
    "s=s.replace('lines = append(lines, t(\"line.replicas\", params{\"count\": count(get(s, \"replicas\"))}))','_ = s',1)"
mutate_go internal/confirm/box.go "oluşturma kutusunda kaynak sınırları yok" \
    "s=s.replace('lines = append(lines, t(\"line.limits\", params{','_ = append(lines, t(\"line.limits\", params{',1)"
mutate_go internal/confirm/confirm.go "kutu JSON sayısı biçimindeki sınırı boş gösteriyor (onaylanan ≠ gönderilen)" \
    "s=s.replace('if err == nil && f >= 0 && f == math.Trunc(f)','if err == nil && f < 0 && f == math.Trunc(f)',1)"
mutate_go internal/confirm/confirm.go "kutuda uzun değer baştan kesiliyor (host gizlenir)" \
    "s=s.replace('return head.String() + string(rune(ellipsis)) + tail.String()','return head.String() + string(rune(ellipsis))',1)"
mutate_go internal/confirm/confirm.go "kesme kaçışsız uzunluğu sayıyor (değer ~6 kat büyür)" \
    "s=s.replace('for i < len(parts) && headLen+units(parts[i]) <= half {','for i < len(parts) && i < half {',1)"
mutate_go internal/confirm/confirm.go "U+2000 üstü görünmezler kaçışlanmıyor (bidi, U+2028, U+3000)" \
    "s=s.replace('func invisible(r rune) bool {','func invisible(r rune) bool {'+chr(10)+'	if r >= 0x2000 {'+chr(10)+'		return false'+chr(10)+'	}',1)"
mutate_go internal/confirm/confirm.go "birleşik işaretler kaçışlanmıyor" \
    "s=s.replace('unicode.Cn, unicode.M, ','unicode.Cn, ',1)"
mutate_go internal/confirm/confirm.go "boş görünen Hangul dolgusu kaçışlanmıyor" \
    "s=s.replace('0x3164: true, ','',1)"
mutate_go internal/confirm/confirm.go "değerdeki … kaçışlanmıyor (sahte kesme)" \
    "s=s.replace(', ellipsis: true}','}',1)"

echo "== Güven sınırı: hata metni (Go) =="
mutate_go internal/redact/redact.go "gerçek sebep (data) atılıyor, yalnız 'call failed' kalıyor" \
    "s=s.replace('raw = message + \": \" + data','raw = message',1)"
mutate_go internal/redact/redact.go "URL kimlik bilgisi silinmiyor" \
    "s=s.replace('text = urlUserinfo.ReplaceAllString(text,','text = noop(text,',1); s=s+chr(10)+'func noop(s, _ string) string { return s }'+chr(10)"
mutate_go internal/redact/redact.go "x-access-token silinmiyor" \
    "s=s.replace('text = xAccessToken.ReplaceAllString(text,','text = noop(text,',1); s=s+chr(10)+'func noop(s, _ string) string { return s }'+chr(10)"
mutate_go internal/redact/redact.go "github_pat_ jetonu silinmiyor" \
    "s=s.replace('text = githubPAT.ReplaceAllString(text,','text = noop(text,',1); s=s+chr(10)+'func noop(s, _ string) string { return s }'+chr(10)"
mutate_go internal/redact/redact.go "ghp_/gho_/ghu_/ghs_/ghr_ jetonu silinmiyor" \
    "s=s.replace('return githubToken.ReplaceAllString(text, redacted)','return text',1)"
mutate_go internal/redact/redact.go "biçim (bidi) karakterleri geçiyor" \
    "s=s.replace(chr(92)+'p{Cc}'+chr(92)+'p{Cf}]',chr(92)+'p{Cc}]',1)"
mutate_go internal/redact/redact.go "uzunluk sınırı yok" \
    "s=s.replace('if len(runes) <= MaxErrorChars {','if len(runes) <= MaxErrorChars*1000 {',1)"
mutate_go internal/redact/redact.go "redaksiyon karakter temizliğinden ÖNCE (bölünmüş jeton kaçar)" \
    "s=s.replace('return limit(Secrets(stripUnsafe(raw)))','return limit(stripUnsafe(Secrets(raw)))',1)"
mutate_go internal/bridge/bridge.go "yöntemler ham dağıtıcı hatasını geçiriyor" \
    "s=s.replace('errors.New(redact.Text(failure.Message, failure.Data))','errors.New(redact.Text(failure.Message, \"\") + \": \" + failure.Data)',1)"
mutate_go internal/bridge/servers.go "sunucu listesi ham dağıtıcı hatasını geçiriyor" \
    "s=s.replace('errors.New(redact.Text(failure.Message, failure.Data))','errors.New(redact.Text(failure.Message, \"\") + \": \" + failure.Data)',1)"
mutate_go internal/bridge/servers.go "bilinen sebep sabit koda çevrilmiyor" \
    "s=s.replace('if reasons[failure.Data] {','if false && reasons[failure.Data] {',1)"

echo "== İzinler ve görünüm (Go) =="
mutate_go internal/headers/headers.go "kamera izni açık" \
    "s=s.replace('\"camera\", \"microphone\",','\"microphone\",',1)"
mutate_go internal/headers/headers.go "yazı tipi listesi her kökene açık" \
    "s=s.replace('\"local-fonts=(self)\"','\"local-fonts=*\"',1)"
mutate_go internal/appearance/look.go "Windows 10'a cam veriliyor" \
    "s=s.replace('windowsBuild >= win11Build','windowsBuild > 0',1)"
mutate_go internal/appearance/look.go "malzemesiz sistemde pencere saydam açılıyor" \
    "s=s.replace('if material == None {','if material == \"x\" {',1)"
mutate_go internal/appearance/look.go "açık temada cam kapanıyor (cam yalnız koyuda)" \
    "s=s.replace('if material == None {','if material == None || theme == \"light\" {',1)"
mutate_go internal/appearance/look.go "açık tema yerel koyuda kalıyor" \
    "s=s.replace('Background: RGBA{}, Scheme: theme}','Background: RGBA{}, Scheme: \"dark\"}',1)"
mutate_go internal/appearance/look.go "tema değişince camın tonu yeniden verilmiyor" \
    "s=s.replace('next.Material == Accent && next.Scheme != prev.Scheme','false',1)"
mutate_go internal/appearance/look.go "yazı tipi kaydı camı yeniden kuruyor (titreme)" \
    "s=s.replace('next.Material == Accent && next.Scheme != prev.Scheme','next.Material == Accent',1)"
mutate_go internal/appearance/look.go "zemin rengi her kayıtta yeniden veriliyor" \
    "s=s.replace('Background: prev.Background != next.Background,','Background: true,',1)"
mutate_go internal/appearance/look.go "koyu tonun saydamlığı sıfır (simsiyah)" \
    "s=s.replace('return 0x30181816','return 0x00181816',1)"
mutate_go internal/appearance/look.go "açık temada koyu ton (cam film)" \
    "s=s.replace('return 0xa0f7f4f4','return 0x30181816',1)"
mutate_go internal/appearance/look.go "ton temaya bakmıyor" \
    "s=s.replace('if scheme == \"dark\" {','if true {',1)"
mutate_go internal/win/policy.go "cam kapatılınca efekt kalıyor" \
    "s=s.replace('state := int32(accentDisabled)','state := int32(accentEnableAcrylicBlurBehind)',1)"
mutate_go internal/win/policy.go "düz bulanıklık (Electron'da arka siyah)" \
    "s=s.replace('accentEnableAcrylicBlurBehind = 4','accentEnableAcrylicBlurBehind = 3',1)"
mutate_go internal/win/policy.go "ton bayrağı yok (renk yok sayılır)" \
    "s=s.replace('Flags: accentFlagGradientColor','Flags: 0',1)"
mutate_go internal/appearance/appearance.go "eski sürümün yazı tipi taşınıyor" \
    "s=s.replace('!isNumber || version != Version','!isNumber && version != Version',1)"
mutate_go internal/appearance/appearance.go "yazı tipi kalınlığı sınırsız" \
    "s=s.replace('return int(math.Min(900, math.Max(100, r)))','return int(r)',1)"
mutate_go internal/appearance/appearance.go "bilinmeyen tema dosyadan geçiyor" \
    "s=s.replace('[]string{\"dark\", \"light\"}','[]string{\"dark\", \"light\", \"glass\", \"mica\", \"__proto__\"}',1)"
mutate_go internal/appearance/appearance.go "varsayılan tema koyu" \
    "s=s.replace('Theme:    \"light\",','Theme:    \"dark\",',1)"

echo "== Canlı kaynak kullanımı (K-166) =="
mutate_in src/renderer/lib/resources.ts "sayaç geri gidince hız eksi (host yeniden başladı)" \
    "s=s.replace('if (prev === null || next === null || next < prev) return null;','if (prev === null || next === null) return null;',1)"
mutate_in src/renderer/lib/resources.ts "yeniden başlayan konteynerin CPU'su eksi" \
    "s=s.replace('if (prev === null || next === null || next < prev || total <= 0) return null;','if (prev === null || next === null || total <= 0) return null;',1)"
mutate_in src/renderer/lib/resources.ts "replika değişince uygulama hızı hesaplanıyor" \
    "s=s.replace('if (!prev || prev.containers !== next.containers) return EMPTY_APP;','if (!prev) return EMPTY_APP;',1)"
mutate_in src/renderer/lib/resources.ts "uygulama CPU'su çekirdek sayısını yok sayıyor" \
    "s=s.replace('const hostNs = seconds * 1e9 * cores;','const hostNs = seconds * 1e9;',1)"
mutate_in src/renderer/lib/resources.ts "CPU boşta oranını gösteriyor" \
    "s=s.replace('? null : 100 - idle,','? null : idle,',1)"
mutate_in src/renderer/lib/resources.ts "aynı anlı iki ölçüm sonsuz hız veriyor" \
    "s=s.replace('if (!prev || seconds <= 0) {','if (!prev) {',1)"
mutate_in src/renderer/lib/resources.ts "eski sunucu hata sayılıyor (yükseltin demiyor)" \
    "s=s.replace('? \"unsupported\" : \"failed\";','? \"failed\" : \"failed\";',1)"
mutate_in src/renderer/lib/resources.ts "ölçülemeyen alan 0 oluyor" \
    "s=s.replace('=== null ? null : read(raw[key]);','=== null ? 0 : read(raw[key]);',1)"
mutate_in src/renderer/lib/resources.ts "eksi sayaç metni kabul ediliyor" \
    "s=s.replace('/^'+chr(92)+'d+\$/.test(value)','/^-?'+chr(92)+'d+\$/.test(value)',1)"
mutate_in src/renderer/lib/resources.ts "ölçülemeyen uygulamalar 'uygulama yok' sayılıyor" \
    "s=s.replace('if (r.apps_measured === true) {','if (r.apps_measured !== undefined) {',1)"
mutate_in src/renderer/lib/resources.ts "hızı bilinmeyen uygulama tablonun üstünde" \
    "s=s.replace('if (cx === null) return 1;','if (cx === null) return -1;',1)"
mutate_in src/renderer/lib/resources.ts "grafik geçmişi sınırsız büyüyor" \
    "s=s.replace('return next.length > max ? next.slice(next.length - max) : next;','return next;',1)"
mutate_in src/renderer/lib/resources.ts "bozuk ölçüm eksi yüzde veriyor" \
    "s=s.replace('return Math.max(0, ((total - available) / total) * 100);','return ((total - available) / total) * 100;',1)"
mutate_in src/renderer/lib/resource-chart.ts "ölçülemeyen aralık çizgiyle bağlanıyor" \
    "s=s.replace('      pen = false;','      pen = pen;',1)"
mutate_in src/renderer/lib/resource-chart.ts "ölçeği aşan değer kutudan taşıyor" \
    "s=s.replace('(Math.min(v, max) / max)','(v / max)',1)"
mutate_in src/renderer/lib/resource-chart.ts "boş grafikte ölçek sıfır (sıfıra bölme)" \
    "s=s.replace('return top > 0 ? top * 1.2 : 1;','return top * 1.2;',1)"
mutate_in src/renderer/lib/format.ts "küçük yüzde ondalıksız" \
    "s=s.replace('< 10 ? 1 : 0, locale)','< 10 ? 0 : 0, locale)',1)"
mutate_in src/renderer/lib/format.ts "yüzde arayüz dilinde yazılmıyor" \
    "s=s.replace('return value.toLocaleString(locale, {','return value.toLocaleString(\"en\", {',1)"
mutate_in src/renderer/i18n/tr.json "Türkçe yüzde işareti sonda" \
    "s=s.replace('\"unit.percent\": \"%{value}\",','\"unit.percent\": \"{value}%\",',1)"

restore
if [ "$fail" -ne 0 ]; then
    echo "SONUÇ: en az bir mutant yakalanmadı ya da ölçülemedi."
    exit 1
fi
echo "Bütün mutasyonlar yakalandı."
