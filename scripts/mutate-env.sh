#!/usr/bin/env bash
# Ortam değişkeni testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu betiğin var olma sebebi ──────────────────────────────────────
#
# Env zinciri SEKİZ katmandan geçiyor: CLI → proto → doğrulama → depo →
# göç → rollout → execclient → sürücü. Zincirin herhangi bir halkasında
# alanı düşürmek DERLEMEYİ KIRMAZ — eksik alan Go'nun sıfır değerine
# düşer. Yani her katman ayrı ayrı "yeşil" görünürken konteyner ortamsız
# doğabilir.
#
# Bu, ölçek küçültmede bir kez yaşanan hatanın (K-080) aynısı: mekanizma
# bağlandı, son halkası unutuldu, komut "başarılı" dedi.
#
# ── Yazılırken ölçülen ────────────────────────────────────────────────
#
# "UPDATE cümlesinden env_json düşür" mutasyonu İLK TURDA YEŞİL geçti.
# Sebep testin zayıflığıydı: `UpdateApp` satırı bellekte değiştirip
# döndürüyor, dolayısıyla DÖNEN STRUCT'a bakan bir test SQL hiç yazmasa
# bile geçer. Testler diskten yeniden okuyacak şekilde güçlendirildi.
#
# Ders K-080'in tekrarı: kaydın doğru görünmesi, gerçekliğin değiştiğini
# kanıtlamaz.
set -uo pipefail

cd "$(dirname "$0")/.."

ROLLOUT=internal/deploy/rollout.go
STORE_APPS=internal/store/apps.go
STORE_UPD=internal/store/appupdate.go
API_APPS=internal/api/apps.go
API_UPD=internal/api/appupdate.go
API_VAL=internal/api/appvalidate.go
CLI_UPD=cmd/panely/appupdate.go

FILES=("$ROLLOUT" "$STORE_APPS" "$STORE_UPD" "$API_APPS" "$API_UPD" "$API_VAL" "$CLI_UPD")
declare -A BAK
for f in "${FILES[@]}"; do
    BAK["$f"]=$(mktemp)
    cp "$f" "${BAK[$f]}"
done
restore() { for f in "${FILES[@]}"; do cp "${BAK[$f]}" "$f"; done; }
trap restore EXIT

fail=0

# mutate <ad> <dosya> <python-ifadesi> <paket> <test-deseni>
mutate() {
    local name="$1" file="$2" expr="$3" pkg="$4" want="$5"
    restore
    if ! python -c "
import io,sys
p='$file'
s=io.open(p,encoding='utf-8').read()
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

    if go test "$pkg" -run "$want" -count=1 >/dev/null 2>&1; then
        echo "  KIRMIZI OLMADI: $name"
        fail=1
    else
        echo "  yakalandı: $name"
    fi
}

echo "== Ortam değişkeni mutasyonları =="

# ── Son halka: replikaya geçiş ───────────────────────────────────────
#
# Bu üçü bütün zincirin anlamını taşıyor. Env buraya ulaşmazsa üstteki
# yedi katmanın hepsi doğru çalışsa bile uygulama değişkeni göremez.

mutate "createReplica Env'i hic doldurmuyor" "$ROLLOUT" \
    "s=s.replace('Env:           copyEnv(app.Env),','',1)" \
    "./internal/deploy/" "TestDeployPassesEnv|TestHealPassesEnv|TestReplicaEnvIsACopy"

mutate "createReplica bos harita geciriyor" "$ROLLOUT" \
    "s=s.replace('Env:           copyEnv(app.Env),','Env:           map[string]string{},',1)" \
    "./internal/deploy/" "TestDeployPassesEnv|TestHealPassesEnv"

mutate "copyEnv referans paylasiyor (kopya degil)" "$ROLLOUT" \
    "s=s.replace('\tout := make(map[string]string, len(env))\n\tfor k, v := range env {\n\t\tout[k] = v\n\t}\n\treturn out','\treturn env',1)" \
    "./internal/deploy/" "TestReplicaEnvIsACopy"

# ── Depo: yazma ve okuma yolları ─────────────────────────────────────

mutate "appSelect'ten env_json dusuruldu" "$STORE_APPS" \
    "s=s.replace('dockerfile_path, build_args_json, env_json,','dockerfile_path, build_args_json,',1); s=s.replace('&app.DockerfilePath, &argsJSON, &envJSON,','&app.DockerfilePath, &argsJSON,',1); s=s.replace('\tif err := json.Unmarshal([]byte(envJSON), &app.Env); err != nil {\n\t\treturn App{}, fmt.Errorf(\"ortam değişkenleri çözümlenemedi: %w\", err)\n\t}','\t_ = envJSON',1)" \
    "./internal/store/" "Env"

mutate "UPDATE cumlesi env_json yazmiyor" "$STORE_UPD" \
    "s=s.replace('env_json = ?, updated_at = ?','updated_at = ?',1); s=s.replace('\t\tstring(env), app.UpdatedAt.UnixNano(), app.ID,','\t\tapp.UpdatedAt.UnixNano(), app.ID,',1); s=s.replace('\tenv, err := json.Marshal(sortedArgs(app.Env))','\t_, err = json.Marshal(sortedArgs(app.Env))',1)" \
    "./internal/store/" "Env"

mutate "birlestirme yerine tamamen degistirme" "$STORE_UPD" \
    "s=s.replace('\tfor k, v := range app.Env {\n\t\tmerged[k] = v\n\t}\n','',1)" \
    "./internal/store/" "Env"

mutate "EnvRemove yok sayiliyor" "$STORE_UPD" \
    "s=s.replace('\tfor _, k := range remove {\n\t\tdelete(merged, k)\n\t}\n','',1)" \
    "./internal/store/" "Env"

mutate "IsEmpty env'i saymiyor" "$STORE_UPD" \
    "s=s.replace('len(u.Env) == 0 && len(u.EnvRemove) == 0','true',1)" \
    "./internal/store/" "Env"

# ── Denetim: GERİ ALINAMAZ sızıntı ───────────────────────────────────
#
# audit_log ekle-sadece (UPDATE/DELETE tetikleyiciyle yasak). Buraya
# yazılan bir sır SİLİNEMEZ; yedekler de onu taşır. Bu iki mutasyonun
# yakalanması, diğerlerinden daha önemli.

mutate "olusturma yolu ham env DEGERI yaziyor" "$API_APPS" \
    "s=s.replace('params[\"env.\"+k] = \"[REDACTED]\"','params[\"env.\"+k] = spec.GetEnv()[k]',1)" \
    "./internal/api/" "Env"

mutate "guncelleme yolu ham env DEGERI yaziyor" "$API_UPD" \
    "s=s.replace('params[\"env.\"+k] = \"[REDACTED]\"','params[\"env.\"+k] = req.GetEnv()[k]',1)" \
    "./internal/api/" "Env"

# ── Doğrulama ────────────────────────────────────────────────────────

mutate "toplam bayt siniri deger basina cevrildi" "$API_VAL" \
    "s=s.replace('\t\ttotal += len(k) + len(v) + 1 // +1: \"KEY=VALUE\" içindeki eşittir','\t\ttotal = len(k) + len(v) + 1',1)" \
    "./internal/api/" "Env"

mutate "anahtar deseni dogrulanmiyor" "$API_VAL" \
    "s=s.replace('if !buildArgPattern.MatchString(k) {\n\t\t\treturn fmt.Errorf(\"ortam değişkeni adı geçersiz (%q) — \"+','if false {\n\t\t\treturn fmt.Errorf(\"ortam değişkeni adı geçersiz (%q) — \"+',1)" \
    "./internal/api/" "Env"

mutate "girdi sayisi siniri kaldirildi" "$API_VAL" \
    "s=s.replace('if len(env) > maxEnvEntries {','if false {',1)" \
    "./internal/api/" "Env"

mutate "celiski kontrolu (ayarla+sil) devre disi" "$API_VAL" \
    "s=s.replace('if _, both := set[k]; both {','if false {',1)" \
    "./internal/api/" "Env"

mutate "validateEnv AppSpec dogrulamasindan cikarildi" "$API_VAL" \
    "s=s.replace('\tif err := validateEnv(spec.GetEnv()); err != nil {\n\t\treturn err\n\t}\n','',1)" \
    "./internal/api/" "TestCreateApp|TestUpdateApp"

# ── Dürüstlük: sessizce başarılı deme ────────────────────────────────
#
# Bu mutasyon, komutun ÇALIŞMASINI bozmuyor — yalnızca kullanıcıya
# yalan söyletiyor. Yakalanmazsa `app update -env` "güncellendi" der ve
# konteynerin hâlâ eski ortamla koştuğunu gizler.

mutate "yeniden dagitim uyarisi susturuldu" "$API_UPD" \
    "s=s.replace('\tif envChanged {\n\t\tresp.EnvDetail = envNeedsRedeploy(appID)\n\t}\n','',1)" \
    "./internal/api/" "TestUpdateAppWarnsEnv"

# ── CLI ──────────────────────────────────────────────────────────────

mutate "isEmptyUpdate env'i saymiyor (komut sunucuya hic gitmez)" "$CLI_UPD" \
    "s=s.replace('len(req.GetEnv()) == 0 && len(req.GetEnvRemove()) == 0','true',1)" \
    "./cmd/panely/" "TestEnvOnlyUpdateIsNotEmpty"

# ── ELENEN MUTASYON: "set[\"env\"] kontrolunu kaldir" ────────────────
#
# Denendi ve YESIL gecti. Once testin zayifligi sanildi; olculdu ve
# mutasyonun korunan ozelligi KIRMADIGI ortaya cikti.
#
# Sebep, bayrak yardimcisinin davranisi. `-env` hic verilmediginde
# stringMapFlag BOS AMA NIL OLMAYAN bir harita donduruyor (olculdu:
# nil=false, len=0). `req.Env` alanina bos harita atamak ile hic
# atamamak, len() acisindan AYNI sonucu veriyor — ve hem isEmptyUpdate
# hem sunucunun `upd.ChangesEnv()` kontrolu len()'e bakiyor.
#
# Yani kaldirilan kontrol bugun yuk tasimiyor. Kodda KALIYOR cunku
# niyeti belgeliyor ve stringMapFlag bir gun nil donmeye baslarsa
# korumaya devam eder; ama ona bir mutasyon yazmak, olcmedigi bir seyi
# olctugunu iddia eden bir kontrol uretirdi.
#
# Ayni sinif mutate-scaledown.sh'de bir kez daha yasandi (K-080).

echo
if [[ $fail -ne 0 ]]; then
    echo "SONUÇ: en az bir mutasyon yakalanmadı — koruma eksik."
    exit 1
fi
echo "SONUÇ: bütün mutasyonlar yakalandı."
