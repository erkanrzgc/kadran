package dockerdrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Sink, bir akış karesini çağırana taşır.
//
// Sürücü akışı KENDİ tüketmez ve tamponlamaz: derleme çıktısı sınırsız
// büyüklükte olabilir ve bu kod AYRICALIKLI süreçte çalışır. Kare geldiği
// anda çağırana verilir; sink hata döndürürse akış orada biter.
type Sink func(data []byte, isStderr bool) error

// BuildSpec, tek bir imaj derlemesini tarif eder.
//
// ContextURL BURADA KURULMAZ, çağırandan gelir — ama serbest bir dizgi
// olarak değil: onu üreten tek yer exec.BuildContextURL'dir ve orada şema
// sabit, kullanıcı bilgisi yok, fragment 40 haneli sha. Bu alan bir URL
// "kabul etmiyor", doğrulanmış parçalardan üretilmiş bir URL taşıyor.
type BuildSpec struct {
	AppID      string
	CommitSHA  string
	ContextURL string
	Dockerfile string
	BuildArgs  map[string]string
}

// buildFrame, derleme akışının tek bir JSON karesidir.
//
// Akış satır ayrılmış JSON'dur; json.Decoder ardışık değerleri kendisi
// çözer. Alanlar ÖLÇÜLDÜ (gerçek daemon, Docker 29.1.3) — varsayılmadı.
type buildFrame struct {
	Stream string `json:"stream"`
	// Status, taban imaj çekilirken gelen ilerleme satırları.
	Status string `json:"status"`
	Error  string `json:"error"`
	// Aux, BAŞARININ tek pozitif kanıtı. Aşağıdaki nota bakınız.
	Aux *struct {
		ID string `json:"ID"`
	} `json:"aux"`
}

// ImageBuild, uzak git bağlamından bir imaj derletir ve imaj kimliğini
// döndürür.
//
// ════════════════════════════════════════════════════════════════════
//
//	BAŞARISIZLIK ÜÇ BİÇİMDE GELİYOR, YALNIZCA BİRİ HTTP HATASI
//
// ════════════════════════════════════════════════════════════════════
//
// Gerçek daemon üzerinde ölçüldü (git daemon ile kurulan üç depo):
//
//	çekme aşaması hatası  → HTTP 500, {"message":"error fetching: ..."}
//	                        (do() bunu apiError'a çevirir)
//	derleme ORTASINDA hata → HTTP 200 + son karede
//	                        {"error":"...","errorDetail":{...}}
//	                        ve AUX KARESİ HİÇ GELMEZ
//	başarı                → HTTP 200 + {"aux":{"ID":"sha256:..."}}
//	                        ve "error" alanı HİÇ GELMEZ
//
// Yani durum koduna bakmak başarısız derlemelerin bir kısmını BAŞARILI
// gösterirdi. Akışın ayrıştırılması zorunlu.
//
// # Neden "hata karesi yok" yetmiyor
//
// İlk tasarım kabul ölçütü olarak hata karesinin YOKLUĞUNU alıyordu. Bu,
// bir OLUMSUZUN YOKLUĞUDUR: ayrıştırıcıda bir hata olsa ya da Docker
// ileride dördüncü bir başarısızlık biçimi eklese, kontrol sessizce
// "başarılı" derdi.
//
// Ölçüm daha iyisini verdi: `aux` karesi başarıda DAİMA geliyor, ortada
// ölen derlemede HİÇ gelmiyor. Bu yüzden ölçüt POZİTİF: aux görülmediyse
// derleme başarısızdır, hata karesi hiç gelmemiş olsa bile.
//
// Bu aynı zamanda "imaj var mı" diye sormaktan da güçlü. Etiket
// panely/<app>:<sha> biçiminde ve aynı commit daha önce derlenmiş olabilir;
// o durumda başarısız bir derlemeden sonra ESKİ imaj bulunur ve kontrol
// yanılırdı. aux karesi BU derlemenin ürettiği kimliktir.
func (c *Client) ImageBuild(ctx context.Context, spec BuildSpec, sink Sink) (string, error) {
	q := url.Values{
		"remote": {spec.ContextURL},
		// Etiket İSTEKTEN ALINMAZ; app_id ve sha'dan kurulur.
		"t": {ImageTag(spec.AppID, spec.CommitSHA)},
		// Ara konteynerleri her durumda temizle: başarısız derlemeler
		// sunucuda ölü konteyner bırakmasın.
		"rm":      {"true"},
		"forcerm": {"true"},
	}
	if spec.Dockerfile != "" {
		q.Set("dockerfile", spec.Dockerfile)
	}
	// İmaja Panely etiketi YAZILMIYOR. `labels` parametresinin çalıştığı
	// ölçüldü, ama sahiplik işaretini etiket taşımak zorunda değil: ad
	// zaten `panely/<app_id>:<sha>` ve bu önek imajı tanımlamaya yetiyor.
	// Öksüz imaj tespiti Faz 2'nin kapsamında; ihtiyaç doğduğunda eklenir.
	//
	// ⚠ Derleme argümanlarının DEĞERLERİ imaj geçmişinde görünür — bu da
	// ölçüldü (`docker history` çıktısında düz metin). exec.proto bunu
	// yazıyor; burada tekrar ediliyor çünkü değerleri tele koyan yer burası.
	if len(spec.BuildArgs) > 0 {
		args, err := json.Marshal(spec.BuildArgs)
		if err != nil {
			return "", fmt.Errorf("docker: buildargs kodlanamadı: %w", err)
		}
		q.Set("buildargs", string(args))
	}

	resp, err := c.do(ctx, http.MethodPost, "/build", q, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	return consumeBuild(resp.Body, sink)
}

// consumeBuild, derleme akışını tüketir ve imaj kimliğini döndürür.
func consumeBuild(body io.Reader, sink Sink) (string, error) {
	dec := json.NewDecoder(body)
	imageID, buildErr := "", ""

	for {
		var f buildFrame
		if err := dec.Decode(&f); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Akış yarıda kesildi (bağlam iptali, kopan soket). Derlemeyi
			// başarılı SAYMIYORUZ: aux görülmediyse aşağıdaki kontrol zaten
			// hata döndürecek, görüldüyse imaj gerçekten üretilmiştir.
			if imageID == "" {
				return "", fmt.Errorf("docker: derleme akışı okunamadı: %w", err)
			}
			break
		}

		if f.Aux != nil {
			imageID = f.Aux.ID
		}
		// Hata metni çağırana gider ama DENETİME GİRMEZ: kullanıcının
		// deposundan gelen metni taşır (exec/record.go).
		if f.Error != "" {
			buildErr = f.Error
		}

		// `stream` ile `status` AYNI karede gelmiyor — ölçüldü: taban imaj
		// çekilirken 13 status karesi geldi ve hiçbiri stream taşımıyordu.
		// Bu yüzden ikisi tek çıkışta birleştirilebiliyor.
		out := f.Stream
		if f.Status != "" {
			out = f.Status + "\n"
		}
		if out != "" {
			if err := sink([]byte(out), false); err != nil {
				return "", err
			}
		}
		if f.Error != "" {
			if err := sink([]byte(f.Error+"\n"), true); err != nil {
				return "", err
			}
		}
	}

	if buildErr != "" {
		return "", fmt.Errorf("docker: derleme başarısız: %s", buildErr)
	}
	if imageID == "" {
		// Ne hata karesi ne aux karesi. Derlemenin başarılı olduğuna dair
		// hiçbir POZİTİF kanıt yok; başarısız sayılır.
		return "", errors.New(
			"docker: derleme kimlik karesi (aux) üretmedi — başarı kanıtlanamadı")
	}
	return imageID, nil
}
