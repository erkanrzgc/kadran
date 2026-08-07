package dockerdrv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Mount, tek bir hacim bağlamasıdır. Host yolu YOKTUR; sürücü kurar.
type Mount struct {
	VolumeName string
	MountPath  string
	ReadOnly   bool
}

// CreateSpec, tek bir replikanın oluşturulması için gereken her şeydir.
//
// Doğrulanmış, ayrıştırılmış alanlar taşır — proto mesajı DEĞİL. Paket
// yorumundaki gerekçeye bakınız.
type CreateSpec struct {
	AppID     string
	ReleaseID string
	Replica   uint32
	CommitSHA string

	Env           map[string]string
	MemoryBytes   uint64
	CPUMillis     uint32
	BlkioWeight   uint32
	Mounts        []Mount
	ContainerPort uint32
}

// createBody, `POST /containers/create` gövdesidir.
//
// ⚠ ALANLARIN YOKLUĞU TASARIMIN KENDİSİDİR.
//
// `Privileged`, `CapAdd`, `Devices`, `PidMode`, `UsernsMode`, `IpcMode`
// ve `NetworkMode: host` burada YOKTUR. Go'nun sıfır değerleri Docker'ın
// güvenli varsayılanlarına denk geldiği için, bu alanları hiç tanımlamamak
// onları "false gönder"mekten daha güçlüdür: sonradan biri yanlışlıkla
// true atayamaz, çünkü atanacak alan yok.
//
// Şemada temsil edilemeyen bir seçeneğin burada da temsil edilemez
// kalması, beyaz listenin sürücü katmanında da sürdüğü anlamına gelir.
type createBody struct {
	Image        string              `json:"Image"`
	Labels       map[string]string   `json:"Labels"`
	Env          []string            `json:"Env,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig   hostConfig          `json:"HostConfig"`
}

type hostConfig struct {
	Binds       []string `json:"Binds,omitempty"`
	NetworkMode string   `json:"NetworkMode"`
	Memory      int64    `json:"Memory"`
	NanoCpus    int64    `json:"NanoCpus"`
	BlkioWeight uint16   `json:"BlkioWeight"`

	// SecurityOpt YALNIZCA no-new-privileges taşır.
	//
	// seccomp burada BELİRTİLMEZ ve bu kasıtlıdır: Docker varsayılan
	// seccomp profilini kendiliğinden uygular, ama SecurityOpt'a bir
	// seccomp girdisi yazmak onu DEĞİŞTİRMEK demektir. Varsayılanı
	// "açıkça yazalım" diye tekrarlamak, ileride yanlış bir profil
	// yazılmasının yolunu açar.
	SecurityOpt []string `json:"SecurityOpt"`

	// RestartPolicy KASTEN "no"dur. Yeniden başlatma kararı sağlık
	// denetçisinin (panelyd) işidir; Docker'ın kendi döngüsü devreye
	// girerse dağıtım durumu iki yerden yönetilir ve mavi-yeşil geçişte
	// ölmüş bir sürüm kendini geri getirebilirdi.
	RestartPolicy struct {
		Name string `json:"Name"`
	} `json:"RestartPolicy"`
}

// ContainerCreate, tek bir replika oluşturur. Başlatmaz.
//
// İmaj ÇEKİLMEZ: `POST /containers/create` kendiliğinden çekmez ve bu
// ölçüldü (ImageTag'deki nota bakınız). Bu kod yolunda pull yoktur ve
// olmamalıdır — yerel imaj eksikken Docker Hub'daki yabancı bir imajın
// çalıştırılmasının önündeki tek engel budur.
func (c *Client) ContainerCreate(ctx context.Context, spec CreateSpec) error {
	// Sertleştirme yerinde değilse hacim bağlanmaz. Sessizce korumasız
	// bağlamaktansa reddetmek doğru davranış (K-038, K-039).
	if len(spec.Mounts) > 0 {
		if err := c.checkVolumeRootHardened(); err != nil {
			return err
		}
	}

	body := createBody{
		Image:  ImageTag(spec.AppID, spec.CommitSHA),
		Labels: labelsFor(spec.AppID, spec.ReleaseID, spec.Replica, spec.CommitSHA),
		Env:    envList(spec.Env),
		HostConfig: hostConfig{
			Binds:       c.bindList(spec.AppID, spec.Mounts),
			NetworkMode: NetworkName(spec.AppID),
			Memory:      int64(spec.MemoryBytes), //nolint:gosec // doğrulayıcı 1 TiB ile sınırlıyor
			// Docker CPU'yu milyarda bir çekirdek olarak alır; bizim birimimiz
			// binde bir. Çarpan 1e6.
			NanoCpus:    int64(spec.CPUMillis) * 1_000_000,
			BlkioWeight: uint16(spec.BlkioWeight), //nolint:gosec // doğrulayıcı 10-1000 aralığında
			SecurityOpt: []string{"no-new-privileges:true"},
		},
	}
	body.HostConfig.RestartPolicy.Name = "no"

	if spec.ContainerPort > 0 {
		body.ExposedPorts = map[string]struct{}{
			strconv.FormatUint(uint64(spec.ContainerPort), 10) + "/tcp": {},
		}
	}

	q := url.Values{"name": {containerName(spec.AppID, spec.ReleaseID, spec.Replica)}}
	return c.doJSON(ctx, http.MethodPost, "/containers/create", q, body, nil)
}

// envList, ortam değişkenlerini Docker'ın beklediği "K=V" dizisine çevirir.
func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// bindList, hacim bağlamalarını kurar.
//
// Host yolu İSTEKTEN GELMEZ, volumePath ile türetilir. `nodev,nosuid`
// buraya yazılmaz: bind mount bayrakları kaynak mount'tan MİRAS ALINIR ve
// hacim kökü systemd birimiyle sertleştirilmiştir (K-038). Docker'a
// seçenek olarak vermek işe yaramıyor — ölçüldü, sessizce yok sayılıyor.
func (c *Client) bindList(appID string, mounts []Mount) []string {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(mounts))
	for _, m := range mounts {
		bind := c.volumePath(appID, m.VolumeName) + ":" + m.MountPath
		if m.ReadOnly {
			bind += ":ro"
		}
		out = append(out, bind)
	}
	return out
}

// ── Seçiciyle çalışan uçlar ──────────────────────────────────────────

// Selector, bir sürümün konteynerlerini seçer.
//
// Replica nil ise sürümün TÜM replikaları seçilir.
type Selector struct {
	AppID     string
	ReleaseID string
	Replica   *uint32
}

// Container, listelenen bir konteynerin Panely'nin umursadığı kadarıdır.
type Container struct {
	ID        string
	AppID     string
	ReleaseID string
	Replica   uint32
	State     string
	CreatedAt time.Time

	// IPAddress, konteynerin UYGULAMA AĞINDAKİ adresi. Durmuş
	// konteynerlerde boştur — Docker adresi ancak çalışırken atar.
	//
	// Ters vekil hostta çalıştığı ve konteynerler host portu yayınlamadığı
	// için panelyd'nin vekile verecek adresi yalnızca buradan öğrenilir.
	IPAddress string
}

// listEntry, `GET /containers/json` yanıtının kullanılan alanlarıdır.
//
// Docker'ın tam tipini almak yerine dar bir yapı: ayrıcalıklı yüzeyi
// büyütmemek için (K-002) ve çözümlenmeyen alanların ileride sessizce
// anlam kazanmasını engellemek için.
type listEntry struct {
	ID      string            `json:"Id"`
	State   string            `json:"State"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`

	// Yalnızca uygulama ağındaki adres okunur; Docker'ın ağ nesnesinin
	// tamamı alınmaz (aynı gerekçe: dar yapı, sessiz anlam kazanma yok).
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// ContainerList, Panely'nin yönettiği konteynerleri döndürür.
//
// appID boş bırakılabilir; o zaman `panely.app_id` taşıyan TÜM
// konteynerler dönülür (panelyd bir kaydı kaybederse öksüzleri ancak
// böyle bulabilir).
func (c *Client) ContainerList(ctx context.Context, appID string) ([]Container, error) {
	f := map[string][]string{"label": {labelAppID}}
	if appID != "" {
		f["label"] = []string{labelAppID + "=" + appID}
	}
	filters, err := json.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("docker: filtre kodlanamadı: %w", err)
	}

	q := url.Values{"all": {"true"}, "filters": {string(filters)}}
	var entries []listEntry
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json", q, nil, &entries); err != nil {
		return nil, err
	}

	out := make([]Container, 0, len(entries))
	for _, e := range entries {
		// ⚠ ETİKETLER BURADA YENİDEN DOĞRULANIR.
		//
		// Filtreyi daemon uyguluyor. Bir güvenlik özelliğini uzak tarafın
		// doğru davranmasına bağlamak, o tarafa güvenmek demektir — oysa
		// bu sürücünün var olma sebebi tam olarak güvenmemek. Filtre
		// bozulsa, atlansa veya bir daemon hatası fazladan kayıt döndürse
		// bile etiketsiz hiçbir konteyner listeye giremez.
		id, ok := parseLabels(e.Labels)
		if !ok {
			continue
		}
		if appID != "" && id.AppID != appID {
			continue
		}
		ct := Container{
			ID:        e.ID,
			AppID:     id.AppID,
			ReleaseID: id.ReleaseID,
			Replica:   id.Replica,
			State:     e.State,
			CreatedAt: time.Unix(e.Created, 0).UTC(),
		}
		// Adres YALNIZCA uygulamanın kendi ağından okunur. Konteyner
		// başka bir ağa da bağlıysa oradaki adresi bilerek göz ardı
		// ediliyor: vekilin ulaşması gereken yer uygulama ağıdır.
		ct.IPAddress = e.NetworkSettings.Networks[NetworkName(id.AppID)].IPAddress
		out = append(out, ct)
	}
	return out, nil
}

// identity, etiketlerden çözülen adres üçlüsüdür.
type identity struct {
	AppID     string
	ReleaseID string
	Replica   uint32
}

// parseLabels, Panely etiketlerini çözer. Üçü de yoksa false döner.
func parseLabels(labels map[string]string) (identity, bool) {
	appID, hasApp := labels[labelAppID]
	releaseID, hasRelease := labels[labelReleaseID]
	replicaStr, hasReplica := labels[labelReplica]
	if !hasApp || !hasRelease || !hasReplica || appID == "" || releaseID == "" {
		return identity{}, false
	}
	replica, err := strconv.ParseUint(replicaStr, 10, 32)
	if err != nil {
		return identity{}, false
	}
	return identity{AppID: appID, ReleaseID: releaseID, Replica: uint32(replica)}, true
}

// matching, seçiciye uyan konteynerleri bulur.
//
// Daima ContainerList üzerinden gider, yani etiket doğrulaması her yolda
// uygulanır. Konteyner kimliği yalnızca BURADA, kendi listemizden elde
// edilir; hiçbir uçta dışarıdan alınmaz.
func (c *Client) matching(ctx context.Context, sel Selector) ([]Container, error) {
	all, err := c.ContainerList(ctx, sel.AppID)
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(all))
	for _, ct := range all {
		if ct.ReleaseID != sel.ReleaseID {
			continue
		}
		if sel.Replica != nil && ct.Replica != *sel.Replica {
			continue
		}
		out = append(out, ct)
	}
	return out, nil
}

// ContainerStart, seçilen konteynerleri başlatır ve kaç tanesinin
// etkilendiğini döndürür.
func (c *Client) ContainerStart(ctx context.Context, sel Selector) (int, error) {
	return c.forEach(ctx, sel, func(ct Container) error {
		return c.doJSON(ctx, http.MethodPost, "/containers/"+ct.ID+"/start", nil, nil, nil)
	})
}

// ContainerStop, seçilen konteynerleri durdurur.
func (c *Client) ContainerStop(ctx context.Context, sel Selector, timeout uint32) (int, error) {
	q := url.Values{}
	if timeout > 0 {
		q.Set("t", strconv.FormatUint(uint64(timeout), 10))
	}
	return c.forEach(ctx, sel, func(ct Container) error {
		return c.doJSON(ctx, http.MethodPost, "/containers/"+ct.ID+"/stop", q, nil, nil)
	})
}

// ContainerRemove, seçilen konteynerleri durdurup siler.
//
// HACİMLERE DOKUNMAZ (`v=false`). Konteyner silmek geri alınabilir bir
// işlemdir; hacim silmek değildir ve §1.3 gereği TOTP kapısına tabidir.
// İkisini birleştirmek, geri alınamaz işlemi geri alınabilir bir isteğin
// yan etkisi hâline getirirdi.
func (c *Client) ContainerRemove(ctx context.Context, sel Selector) (int, error) {
	q := url.Values{"force": {"true"}, "v": {"false"}}
	return c.forEach(ctx, sel, func(ct Container) error {
		return c.doJSON(ctx, http.MethodDelete, "/containers/"+ct.ID, q, nil, nil)
	})
}

// forEach, seçilen her konteyner için işlemi uygular.
//
// İlk hatada DURUR ve o ana kadarki sayıyı hatayla birlikte döndürür.
// Sessizce devam etmek, çağıranın "hepsi oldu" sanmasına yol açardı;
// kısmi sonucu da atmak, denetim kaydına yanlış sayı yazdırırdı.
func (c *Client) forEach(ctx context.Context, sel Selector, fn func(Container) error) (int, error) {
	matches, err := c.matching(ctx, sel)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, ct := range matches {
		if err := fn(ct); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
