package dockerdrv

import (
	"fmt"
	"path"
	"strconv"
)

// ════════════════════════════════════════════════════════════════════
//  ADLAR İSTEKTEN ALINMAZ — HEPSİ BURADA KURULUR
// ════════════════════════════════════════════════════════════════════
//
// İmaj etiketi, ağ adı, konteyner adı ve hacim yolu; hepsi doğrulanmış
// parçalardan TÜRETİLİR. Sürücünün dışarıdan hazır bir ad kabul eden tek
// bir fonksiyonu bile yoktur.
//
// Sebep, şemadaki `image` alanının yokluğuyla aynı: serbest bir imaj adı
// ele geçirilmiş bir panelyd'nin keyfî bir imajı çalıştırmasına izin
// verirdi ve beyaz listenin tamamını anlamsız kılardı. Aynı akıl yürütme
// ağ adı için de geçerli — seçilebilseydi mevcut bir veritabanının ağına
// bağlanmak mümkün olurdu.

const (
	// labelAppID ve arkadaşları, Panely'nin sahiplik işaretidir.
	//
	// Adresleme bunlarla yapılır, konteyner kimliğiyle DEĞİL. Bu etiketleri
	// taşımayan hiçbir konteynere dokunulmaz: Panely'nin oluşturmadığı bir
	// konteyner (veritabanı, başka bir kullanıcının işi) bu şemayla
	// adreslenemez.
	labelAppID     = "panely.app_id"
	labelReleaseID = "panely.release_id"
	labelReplica   = "panely.replica"

	// labelCommitSHA denetim içindir, adresleme için DEĞİL.
	//
	// release_id ↔ commit_sha bağı bir DAEMON değişmezidir; executor'ın
	// veritabanı yoktur ve bunu doğrulayamaz. Etiketi yazmak, sapmanın
	// sonradan denetim zincirlerinin karşılaştırılmasıyla YAKALANABİLİR
	// olmasını sağlar (exec.proto'daki nota bakınız).
	labelCommitSHA = "panely.commit_sha"
)

// ImageTag, imaj etiketini kurar: panely/<app_id>:<commit_sha>
//
// Kayıtsız (registry'siz) bir addır. Docker böyle bir adı çözemezse
// docker.io/panely/<app_id> olarak YORUMLAR — ama bu bir risk değil,
// çünkü `POST /containers/create` kendiliğinden imaj ÇEKMEZ.
//
// Bu varsayılmadı, ölçüldü: Docker Hub'da KESİNLİKLE var olan bir imaj
// (busybox:1.36) yerelden silinip create çağrıldığında daemon HTTP 404
// döndürdü ve imajı çekmedi. Yani create hiçbir koşulda çekmiyor; "ad
// Hub'da yok" ile "create çekmiyor" ayrımı bu şekilde ölçüldü.
func ImageTag(appID, commitSHA string) string {
	return fmt.Sprintf("panely/%s:%s", appID, commitSHA)
}

// NetworkName, uygulamanın iç ağını adlandırır: panely-<app_id>
func NetworkName(appID string) string {
	return "panely-" + appID
}

// containerName, tek bir replikayı adlandırır.
//
// Alt çizgi ayırıcı olarak seçildi: app_id ve release_id tire İÇEREBİLİR
// (`^[a-z][a-z0-9-]*$`), bu yüzden tireyle ayırmak iki farklı üçlünün aynı
// ada çözülmesine izin verirdi — ör. (app "a-b", release "c") ile
// (app "a", release "b-c"). Alt çizgi bu karakter kümelerinde yok.
func containerName(appID, releaseID string, replica uint32) string {
	return fmt.Sprintf("panely_%s_%s_%d", appID, releaseID, replica)
}

// labelsFor, bir replikanın taşıyacağı etiketleri üretir.
func labelsFor(appID, releaseID string, replica uint32, commitSHA string) map[string]string {
	return map[string]string{
		labelAppID:     appID,
		labelReleaseID: releaseID,
		labelReplica:   strconv.FormatUint(uint64(replica), 10),
		labelCommitSHA: commitSHA,
	}
}

// volumePath, bir hacmin host üzerindeki yolunu kurar.
//
// `path` kullanılıyor, `filepath` DEĞİL. Bu yol hedef Linux sunucusunda
// yorumlanacak ve geliştirme Windows'ta yapılıyor: `filepath` Windows
// kurallarını uygular ve ters eğik çizgiyi ayırıcı sayardı.
//
// Kaçış mümkün değil: appID ve volumeName doğrulayıcıları eğik çizgi ve
// nokta İÇERMEYEN karakter kümeleri zorluyor, dolayısıyla `..` ve `/`
// temsil edilemez.
func (c *Client) volumePath(appID, volumeName string) string {
	return path.Join(c.volumeRoot, appID, volumeName)
}
