package execclient

import (
	"context"
	"fmt"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// containerTimeout, konteyner yaşam döngüsü çağrıları için üst sınır.
//
// Burada sabit bir sınır GÜVENLİ: bu uçlar akış DEĞİL, istek/yanıt turu.
// K-044'ün "akan uçta zaman aşımı olmaz" yasağı ImageBuild ve
// ContainerLogs içindi; onlar gövdeyi dakikalarca açık tutuyor.
//
// Durdurma bunun dışında: Docker'a verilen zarif kapanma penceresi
// çağrının kendisinden uzun olabilir, o yüzden StopReplicas kendi
// sınırını hesaplıyor.
const containerTimeout = 60 * time.Second

// Replica, hostta duran tek bir konteynerdir.
//
// Executor'ın döndürdüğü proto mesajının panelyd tarafındaki karşılığı.
// Ayrı bir tip olmasının sebebi `Addr`: proxydrv'nin ihtiyaç duyduğu şey
// "adres" değil, "trafiğe HAZIR bir adres" ve ikisi aynı değil.
type Replica struct {
	AppID     string
	ReleaseID string
	Index     uint32

	State     panelyv1.ContainerState
	IPAddress string
	CreatedAt time.Time
}

// Running, konteynerin çalışır durumda olduğunu bildirir.
//
// Ayrı bir yöntem: durum sabitini çağıran yerlerde tekrar tekrar
// karşılaştırmak, bir gün RESTARTING'i de "çalışıyor" sayan bir kopyanın
// belirmesine davetiye olurdu. RESTARTING çalışmıyor demektir — trafiği
// oraya yollamak 502 üretir.
func (r Replica) Running() bool {
	return r.State == panelyv1.ContainerState_CONTAINER_STATE_RUNNING
}

// Routable, konteynerin trafik alabilecek durumda OLDUĞUNU bildirir.
//
// Çalışıyor olmak YETMEZ: adres de gerekir. `ip_address` durmuş
// konteynerlerde boştur ve yeni başlamış bir konteynerde AĞ KURULANA
// KADAR da boş olabilir. Bu geçici durum bir HATA DEĞİL — çağıran
// beklemeli, vazgeçmemeli.
func (r Replica) Routable() bool {
	return r.Running() && r.IPAddress != ""
}

// EnsureNetwork, uygulamanın izole ağını var eder ve adını döndürür.
//
// Ağ adı executor tarafından KURULUYOR, panelyd'den alınmıyor: aksi hâlde
// ele geçirilmiş bir panelyd konteynerleri `host` ağına koyabilirdi.
func (c *Client) EnsureNetwork(ctx context.Context, appID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	resp, err := c.rpc.NetworkEnsure(ctx, &panelyv1.NetworkEnsureRequest{AppId: appID})
	if err != nil {
		return "", fmt.Errorf("uygulama ağı kurulamadı (%s): %w", appID, err)
	}
	if resp.GetNetworkName() == "" {
		// Adsız bir ağ, sonraki ContainerCreate'in sessizce varsayılan
		// köprüye düşmesi demekti — yani izolasyonun kaybı.
		return "", fmt.Errorf("executor ağ adı döndürmedi (%s) — izolasyon kanıtlanamadı", appID)
	}
	return resp.GetNetworkName(), nil
}

// CreateReplicaOptions, tek bir replikanın oluşturma parametreleridir.
type CreateReplicaOptions struct {
	AppID     string
	ReleaseID string
	Index     uint32
	CommitSHA string

	Env           map[string]string
	ContainerPort uint32
	Limits        Limits
	Volumes       []VolumeMount
}

// Limits, kaynak kotalarıdır. Hiçbiri sıfır olamaz (§2.2): limitsiz
// konteyner yok ve sınırsızlığı TEMSİL EDİLEMEZ kılmak, doğrulamaktan
// güçlü.
type Limits struct {
	MemoryBytes uint64
	CPUMillis   uint32
	BlkioWeight uint32
}

// VolumeMount, kalıcı hacim bağlamasıdır.
//
// ⚠ HOST YOLU TAŞIMAZ, yalnızca bir hacim ADI. Yol alıp doğrulamak
// TOCTOU'ya açıktır; girdiyi hiç almamak sınıfın tamamını siler. Yolu
// executor kendisi kuruyor.
type VolumeMount struct {
	VolumeName string
	MountPath  string
	ReadOnly   bool
}

// CreateReplica, tek bir konteyner oluşturur (başlatmaz).
func (c *Client) CreateReplica(ctx context.Context, opts CreateReplicaOptions) error {
	if err := opts.Limits.validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	volumes := make([]*panelyv1.VolumeMount, 0, len(opts.Volumes))
	for _, v := range opts.Volumes {
		volumes = append(volumes, &panelyv1.VolumeMount{
			VolumeName: v.VolumeName,
			MountPath:  v.MountPath,
			ReadOnly:   v.ReadOnly,
		})
	}

	_, err := c.rpc.ContainerCreate(ctx, &panelyv1.ContainerCreateRequest{
		Ref:       containerRef(opts.AppID, opts.ReleaseID, opts.Index),
		CommitSha: opts.CommitSHA,
		Env:       opts.Env,
		Limits: &panelyv1.ResourceLimits{
			MemoryBytes: opts.Limits.MemoryBytes,
			CpuMillis:   opts.Limits.CPUMillis,
			BlkioWeight: opts.Limits.BlkioWeight,
		},
		Volumes:       volumes,
		ContainerPort: opts.ContainerPort,
	})
	if err != nil {
		return fmt.Errorf("konteyner oluşturulamadı (%s/%s #%d): %w",
			opts.AppID, opts.ReleaseID, opts.Index, err)
	}
	return nil
}

// validate, limitlerin sıfır olmadığını doğrular.
//
// Executor de aynı kontrolü yapıyor ve asıl otorite ORASI. Buradaki
// kopya, hatanın oluştuğu yere yakın ve okunabilir bir mesajla çıkması
// için: sapma yönü her hâlükârda güvenli (iki taraftan biri katıysa
// istek reddedilir).
func (l Limits) validate() error {
	switch {
	case l.MemoryBytes == 0:
		return fmt.Errorf("bellek limiti sıfır olamaz — limitsiz konteyner yok")
	case l.CPUMillis == 0:
		return fmt.Errorf("CPU limiti sıfır olamaz — limitsiz konteyner yok")
	case l.BlkioWeight == 0:
		return fmt.Errorf("blkio ağırlığı sıfır olamaz — limitsiz konteyner yok")
	}
	return nil
}

// StartReplica, tek bir konteyneri başlatır.
func (c *Client) StartReplica(ctx context.Context, appID, releaseID string, index uint32) error {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	resp, err := c.rpc.ContainerStart(ctx, &panelyv1.ContainerStartRequest{
		Selector: replicaSelector(appID, releaseID, index),
	})
	if err != nil {
		return fmt.Errorf("konteyner başlatılamadı (%s/%s #%d): %w",
			appID, releaseID, index, err)
	}
	// POZİTİF ÖLÇÜT. "Hata almadım" bir başarı kanıtı değil: seçici
	// hiçbir konteynerle eşleşmediyse executor da hata döndürmez, sıfır
	// döndürür. Bu kontrol olmasaydı, var olmayan bir replikayı
	// "başlattık" sayıp trafiği oraya çevirirdik.
	if resp.GetAffected() == 0 {
		return fmt.Errorf(
			"başlatma hiçbir konteyneri etkilemedi (%s/%s #%d) — konteyner yok",
			appID, releaseID, index)
	}
	return nil
}

// StopRelease, bir sürümün TÜM konteynerlerini durdurur.
//
// Replika seçilmiyor: boşaltma sırasında sürümün tamamı iniyor ve tek
// tek durdurmak, aradaki pencerede yarım inmiş bir sürüm bırakırdı.
func (c *Client) StopRelease(ctx context.Context, appID, releaseID string, grace time.Duration) (uint32, error) {
	// Zaman aşımı, Docker'a verilen zarif kapanma penceresinden UZUN
	// olmak zorunda; kısa olsaydı bağlam, konteynerler daha inerken
	// iptal olurdu.
	ctx, cancel := context.WithTimeout(ctx, grace+containerTimeout)
	defer cancel()

	resp, err := c.rpc.ContainerStop(ctx, &panelyv1.ContainerStopRequest{
		Selector:       releaseSelector(appID, releaseID),
		TimeoutSeconds: uint32(grace.Seconds()),
	})
	if err != nil {
		return 0, fmt.Errorf("sürüm durdurulamadı (%s/%s): %w", appID, releaseID, err)
	}
	// Sıfır burada HATA DEĞİL: zaten durmuş bir sürümü durdurmak geçerli
	// ve boşaltma yolunun tekrar çalıştırılabilir olması gerekiyor.
	return resp.GetAffected(), nil
}

// RemoveRelease, bir sürümün tüm konteynerlerini siler.
func (c *Client) RemoveRelease(ctx context.Context, appID, releaseID string) (uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	resp, err := c.rpc.ContainerRemove(ctx, &panelyv1.ContainerRemoveRequest{
		Selector: releaseSelector(appID, releaseID),
	})
	if err != nil {
		return 0, fmt.Errorf("sürüm konteynerleri silinemedi (%s/%s): %w", appID, releaseID, err)
	}
	return resp.GetAffected(), nil
}

// ListReplicas, uygulamanın hostta duran TÜM konteynerlerini döndürür.
//
// ── Neden hostu soruyoruz, SQLite'ı değil? ──────────────────────────
//
// SQLite ne İSTEDİĞİMİZİ biliyor, host ne OLDUĞUNU. Blue-green geçişinin
// sağlık kapısı ikincisine bakmak zorunda: "kaydettim" ile "çalışıyor"
// arasındaki farkı görmeyen bir kapı, trafiği ölü bir sürüme taşır.
func (c *Client) ListReplicas(ctx context.Context, appID string) ([]Replica, error) {
	ctx, cancel := context.WithTimeout(ctx, containerTimeout)
	defer cancel()

	resp, err := c.rpc.ContainerList(ctx, &panelyv1.ContainerListRequest{AppId: appID})
	if err != nil {
		return nil, fmt.Errorf("konteynerler listelenemedi (%s): %w", appID, err)
	}

	out := make([]Replica, 0, len(resp.GetContainers()))
	for _, mc := range resp.GetContainers() {
		ref := mc.GetRef()
		out = append(out, Replica{
			AppID:     ref.GetRelease().GetAppId(),
			ReleaseID: ref.GetRelease().GetReleaseId(),
			Index:     ref.GetReplica(),
			State:     mc.GetState(),
			IPAddress: mc.GetIpAddress(),
			CreatedAt: mc.GetCreatedAt().AsTime(),
		})
	}
	return out, nil
}

func containerRef(appID, releaseID string, index uint32) *panelyv1.ContainerRef {
	return &panelyv1.ContainerRef{
		Release: &panelyv1.ReleaseRef{AppId: appID, ReleaseId: releaseID},
		Replica: index,
	}
}

// replicaSelector, TEK bir replikayı seçer.
func replicaSelector(appID, releaseID string, index uint32) *panelyv1.ContainerSelector {
	return &panelyv1.ContainerSelector{
		Release: &panelyv1.ReleaseRef{AppId: appID, ReleaseId: releaseID},
		Replica: &index,
	}
}

// releaseSelector, sürümün TÜM replikalarını seçer.
//
// `Replica` alanı `optional`: BOŞ bırakmak "hepsi" demek, sıfır yazmak
// "0 numaralı replika". İkisi arasındaki fark proto3'te ancak `optional`
// ile ifade edilebiliyordu ve karıştırılması, boşaltma sırasında yalnızca
// ilk replikayı durdurup gerisini ayakta bırakırdı.
func releaseSelector(appID, releaseID string) *panelyv1.ContainerSelector {
	return &panelyv1.ContainerSelector{
		Release: &panelyv1.ReleaseRef{AppId: appID, ReleaseId: releaseID},
	}
}
