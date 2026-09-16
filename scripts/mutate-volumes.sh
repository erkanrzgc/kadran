#!/usr/bin/env bash
# Kalıcı disk testlerinin GERÇEKTEN bir şey koruduğunu sınar.
#
# Her mutasyon korunan bir özelliği KASTEN bozar; test kırmızıya
# dönmezse o özellik korunmuyor demektir.
#
# ── Bu betiğin var olma sebebi ──────────────────────────────────────
#
# Hacim zinciri DOKUZ katmandan geçiyor: CLI → proto → doğrulama → depo →
# göç → rollout → execclient → sürücü → SAHİPLİK. Halkalardan birini
# düşürmek derlemeyi kırmaz.
#
# Bedeli env'dekinden AĞIR. Env düşerse uygulama açılmaz ve belirti
# hemen görünür. Hacim düşerse uygulama AÇILIR, veriyi konteynerin kendi
# katmanına yazar, ve veri BİR SONRAKİ DAĞITIMDA kaybolur. Belirti
# "disk çalışmıyor" değil, haftalar sonra "verilerim gitti".
#
# ── Env'den öğrenilen ve buraya baştan uygulanan ────────────────────
#
# env'de "UPDATE cümlesinden sütun düşür" mutasyonu ilk turda YEŞİL
# kaldı: testler `UpdateApp`'in DÖNÜŞÜNE bakıyordu, diske değil. Buradaki
# depo testleri baştan `GetApp` ile diskten okuyor.
set -uo pipefail

cd "$(dirname "$0")/.."

ROLLOUT=internal/deploy/rollout.go
STORE_APPS=internal/store/apps.go
STORE_UPD=internal/store/appupdate.go
API_APPS=internal/api/apps.go
API_UPD=internal/api/appupdate.go
API_VAL=internal/api/volumevalidate.go
API_SPEC=internal/api/appvalidate.go
DRV_OWN=internal/dockerdrv/volumeown.go
DRV_CNT=internal/dockerdrv/container.go
CLI_UPD=cmd/panely/appupdate.go
CLI_FLAG=cmd/panely/volumeflag.go

FILES=("$ROLLOUT" "$STORE_APPS" "$STORE_UPD" "$API_APPS" "$API_UPD" "$API_VAL"
       "$API_SPEC" "$DRV_OWN" "$DRV_CNT" "$CLI_UPD" "$CLI_FLAG")
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
    if ! python3 -c "
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

echo "== Kalıcı disk mutasyonları =="

# ── Son halka: replikaya geçiş ───────────────────────────────────────

mutate "createReplica Volumes'i hic doldurmuyor" "$ROLLOUT" \
    "s=s.replace('\t\tVolumes:       replicaVolumes(app.Volumes),\n','',1)" \
    "./internal/deploy/" "Volume"

mutate "ReadOnly bayragi dusuruldu" "$ROLLOUT" \
    "s=s.replace('\t\t\tReadOnly: v.ReadOnly,','\t\t\tReadOnly: false,',1)" \
    "./internal/deploy/" "Volume"

mutate "replicaVolumes referans paylasiyor" "$ROLLOUT" \
    "s=s.replace('\tout := make([]execclient.VolumeMount, 0, len(vols))','\tif true { return *(*[]execclient.VolumeMount)(nil) }\n\tout := make([]execclient.VolumeMount, 0, len(vols))',1)" \
    "./internal/deploy/" "Volume"

# ── SAHİPLİK: env'de karşılığı olmayan katman ───────────────────────
#
# Bu üçü olmadan hacim bağlanır ama uygulama YAZAMAZ. Canlıda ölçüldü:
# uid 101 ile koşan konteyner root:root dizine "Permission denied" alıyor.

mutate "chown hic cagrilmiyor" "$DRV_OWN" \
    "s=s.replace('if err := chownDir(dir, uid); err != nil {','if false {',1)" \
    "./internal/dockerdrv/" "Volume|Image|Chown|Create"

mutate "MkdirAll kaldirildi (Docker'a birakildi)" "$DRV_OWN" \
    "s=s.replace('if err := os.MkdirAll(dir, 0o750); err != nil {','if false {',1)" \
    "./internal/dockerdrv/" "Volume|Create"

mutate "sayisal olmayan USER sessizce 0 oluyor" "$DRV_OWN" \
    "s=s.replace('\t\treturn 0, fmt.Errorf(\n\t\t\t\"imaj','\t\treturn 0, error(nil); _ = fmt.Errorf(\n\t\t\t\"imaj',1)" \
    "./internal/dockerdrv/" "Volume|Image|NonNumeric|Create"

mutate "prepareVolumes ContainerCreate'ten cikarildi" "$DRV_CNT" \
    "s=s.replace('\t\tif err := c.prepareVolumes(ctx, spec); err != nil {\n\t\t\treturn err\n\t\t}\n','',1)" \
    "./internal/dockerdrv/" "Volume|Create"

# ── Depo: yazma ve okuma yolları ─────────────────────────────────────

mutate "appSelect'ten volumes_json dusuruldu" "$STORE_APPS" \
    "s=s.replace('build_args_json, env_json, volumes_json,\n\t       container_port','build_args_json, env_json,\n\t       container_port',1); s=s.replace('&argsJSON, &envJSON, &volsJSON,','&argsJSON, &envJSON,',1); s=s.replace('\tif err := json.Unmarshal([]byte(volsJSON), &app.Volumes); err != nil {','\tif false {',1)" \
    "./internal/store/" "Volume"

mutate "UPDATE cumlesi volumes_json yazmiyor" "$STORE_UPD" \
    "s=s.replace('env_json = ?, volumes_json = ?, updated_at = ?','env_json = ?, updated_at = ?',1); s=s.replace('string(env), string(vols), app.UpdatedAt.UnixNano()','string(env), app.UpdatedAt.UnixNano()',1); s=s.replace('\tvols, err := json.Marshal(sortedVolumes(app.Volumes))','\t_, err = json.Marshal(sortedVolumes(app.Volumes))',1)" \
    "./internal/store/" "Volume"

mutate "birlestirme yerine tamamen degistirme" "$STORE_UPD" \
    "s=s.replace('\tmerged = append(merged, app.Volumes...)\n','',1)" \
    "./internal/store/" "Volume"

mutate "VolumeRemove yok sayiliyor" "$STORE_UPD" \
    "s=s.replace('\tfor _, name := range remove {\n\t\tfor i := range merged {','\tfor _, name := range []string(nil) {\n\t\tfor i := range merged {',1)" \
    "./internal/store/" "Volume"

mutate "IsEmpty hacimleri saymiyor" "$STORE_UPD" \
    "s=s.replace('len(u.Volumes) == 0 && len(u.VolumeRemove) == 0','true',1)" \
    "./internal/store/" "Volume"

mutate "sortedVolumes siralamiyor (belirlenimsiz JSON)" "$STORE_APPS" \
    "s=s.replace('\tsort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })\n','',1)" \
    "./internal/store/" "Volume"

# ── Doğrulama ────────────────────────────────────────────────────────

mutate "baglama noktasi mutlak olmak zorunda degil" "$API_VAL" \
    "s=s.replace('\tcase !path.IsAbs(p):','\tcase false:',1)" \
    "./internal/api/" "Volume"

mutate "/proc /sys /dev yasagi kaldirildi" "$API_VAL" \
    "s=s.replace('\tfor _, root := range forbiddenMountRoots {','\tfor _, root := range []string(nil) {',1)" \
    "./internal/api/" "Volume"

mutate "ic ice baglama denetimi devre disi" "$API_VAL" \
    "s=s.replace('\t\t\tif pathOverlaps(prev, mp) {','\t\t\tif false {',1)" \
    "./internal/api/" "Volume"

mutate "ayni ad iki kez kabul ediliyor" "$API_VAL" \
    "s=s.replace('\t\tif seenNames[name] {','\t\tif false {',1)" \
    "./internal/api/" "Volume"

mutate "celiski kontrolu (ekle+ayir) devre disi" "$API_VAL" \
    "s=s.replace('\t\t\tif v.GetName() == name {','\t\t\tif false {',1)" \
    "./internal/api/" "Volume"

mutate "validateVolumes AppSpec dogrulamasindan cikarildi" "$API_SPEC" \
    "s=s.replace('\tif err := validateVolumes(spec.GetVolumes()); err != nil {\n\t\treturn err\n\t}\n','',1)" \
    "./internal/api/" "TestCreateApp|TestUpdateApp"

# ── Çevrim: read_only sessizce düşerse ───────────────────────────────

mutate "proto->depo cevriminde ReadOnly dusuyor" "$API_APPS" \
    "s=s.replace('\t\t\tReadOnly:  v.GetReadOnly(),','\t\t\tReadOnly:  false,',1)" \
    "./internal/api/" "Volume"

mutate "depo->proto cevriminde ReadOnly dusuyor" "$API_APPS" \
    "s=s.replace('\t\t\tReadOnly:  v.ReadOnly,','\t\t\tReadOnly:  false,',1)" \
    "./internal/api/" "Volume"

# ── Dürüstlük: sessizce başarılı deme ────────────────────────────────

mutate "yeniden dagitim uyarisi susturuldu" "$API_UPD" \
    "s=s.replace('\tif volumesChanged {\n\t\tresp.VolumeDetail = volumesNeedRedeploy(appID)\n\t}\n','',1)" \
    "./internal/api/" "TestUpdateAppWarnsVolumes"

# ── CLI ──────────────────────────────────────────────────────────────

mutate "isEmptyUpdate hacimleri saymiyor" "$CLI_UPD" \
    "s=s.replace('len(req.GetVolumes()) == 0 && len(req.GetVolumeRemove()) == 0','true',1)" \
    "./cmd/panely/" "TestVolumeOnlyUpdateIsNotEmptyCLI"

mutate "host yolu bicimi sessizce kabul ediliyor" "$CLI_FLAG" \
    "s=s.replace('\tif strings.HasPrefix(name, \"/\") || strings.HasPrefix(name, \".\") {','\tif false {',1)" \
    "./cmd/panely/" "TestParseVolumeFlag"

mutate ":ro soneki yok sayiliyor" "$CLI_FLAG" \
    "s=s.replace('\t\t\tspec.readOnly = true','\t\t\tspec.readOnly = false',1)" \
    "./cmd/panely/" "TestParseVolumeFlag|TestVolumeFlag"

mutate "bilinmeyen sonek kabul ediliyor" "$CLI_FLAG" \
    "s=s.replace('\t\tdefault:\n\t\t\treturn volumeSpec{}, fmt.Errorf(','\t\tdefault:\n\t\t\t_ = fmt.Sprintf(',1)" \
    "./cmd/panely/" "TestParseVolumeFlag"

echo
if [[ $fail -ne 0 ]]; then
    echo "SONUÇ: en az bir mutasyon yakalanmadı — koruma eksik."
    exit 1
fi
echo "SONUÇ: bütün mutasyonlar yakalandı."
