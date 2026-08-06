package dockerdrv

import (
	"fmt"
	"os"
	"strings"
)

// ════════════════════════════════════════════════════════════════════
//  HACİM KÖKÜNÜN SERTLEŞTİRİLDİĞİ ÇALIŞMA ANINDA DOĞRULANIR
// ════════════════════════════════════════════════════════════════════
//
// `nodev,nosuid`, bir systemd .mount birimi tarafından sağlanır
// (deploy/systemd/var-lib-panely-volumes.mount). Peki neden bir de burada
// kontrol ediliyor?
//
// Çünkü o birim BİR KEZ SESSİZCE ÇALIŞMADI ve kimse fark etmedi (K-039):
// kurulumda üç kontrol de yeşildi, ama yeniden başlatmadan sonra systemd
// bir sıralama döngüsünü kırarken mount işini sildi. Sonuç: hacim kökü
// sertleştirilmemişti ve setuid-root bir ikili konteynerde euid=0 ile
// çalışabiliyordu. Hiçbir hata, hiçbir uyarı.
//
// Bir güvenlik özelliğinin varlığı, onu sağlaması beklenen bileşenin
// doğru davrandığı VARSAYIMINA bırakılamaz. Bu kontrol varsayımı ölçüme
// çevirir: bayraklar yoksa hacim bağlanmaz.
//
// Ölçüldü: panely-exec.service'in sertleştirmesi (ProtectSystem=strict,
// ProtectProc=invisible, ProcSubset=pid) /proc/self/mountinfo'yu
// GİZLEMİYOR — aynı direktifleri taşıyan geçici bir birimden mount
// satırı ve doğru bayraklar okunabildi.

// requiredMountFlags, hacim kökünde bulunması ZORUNLU bayraklar.
var requiredMountFlags = []string{"nodev", "nosuid"}

// mountinfoPath, ayrı değişken çünkü testler onu değiştiriyor.
var mountinfoPath = "/proc/self/mountinfo"

// checkVolumeRootHardened, hacim kökünün nodev,nosuid ile bağlandığını
// çekirdekten doğrular.
//
// `systemctl is-active` YETMEZ: birim "active" görünürken bayraklar yok
// sayılmış olabilir — Docker'ın local sürücüsünde tam olarak bu oluyor
// (K-038). Tek güvenilir kaynak çekirdeğin kendisidir.
func (c *Client) checkVolumeRootHardened() error {
	opts, ok, err := mountOptionsFor(c.volumeRoot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf(
			"hacim kökü %q ayrı bir mount DEĞİL — nodev,nosuid uygulanamaz; "+
				"var-lib-panely-volumes.mount birimi etkin mi?", c.volumeRoot)
	}
	for _, want := range requiredMountFlags {
		if !hasOption(opts, want) {
			return fmt.Errorf(
				"hacim kökü %q üzerinde %q bayrağı yok (etkin: %s) — "+
					"hacim sertleştirilmeden bağlanmaz", c.volumeRoot, want, opts)
		}
	}
	return nil
}

// mountOptionsFor, verilen yolun KENDİSİ bir mount noktasıysa etkin
// seçeneklerini döndürür.
//
// Alt dizin eşleşmesi KABUL EDİLMEZ: yalnızca tam eşleşme sayılır. Aksi
// hâlde `/` mount'unun seçenekleri hacim kökününmüş gibi okunabilir ve
// kontrol sistematik olarak yanlış cevap verirdi.
func mountOptionsFor(target string) (opts string, found bool, err error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return "", false, fmt.Errorf("mount bilgisi okunamadı: %w", err)
	}

	// mountinfo(5): ... 5=mount noktası  6=mount seçenekleri ...
	// Aynı yola birden çok mount yığılabilir; SONUNCUSU etkin olandır.
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		// Mount noktaları boşluk gibi karakterleri sekizlik kaçışla yazar
		// (\040). Hacim kökünde bunlar yok ama çözmek doğru davranış.
		if unescapeMountPath(fields[4]) == target {
			opts, found = fields[5], true
		}
	}
	return opts, found, nil
}

// hasOption, virgülle ayrılmış seçenek listesinde TAM eşleşme arar.
//
// Alt dizgi araması yanlış olurdu: "nodev" dizgisi "nodevfoo" içinde de
// geçer ve kontrol sessizce geçerdi.
func hasOption(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}

// unescapeMountPath, mountinfo'nun sekizlik kaçışlarını çözer.
func unescapeMountPath(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}
